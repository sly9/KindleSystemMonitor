//go:build windows

package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// We register login-time autostart via HKCU\...\Run instead of Task Scheduler
// because /SC ONLOGON on modern Windows requires admin rights (the trigger
// hooks the user-logon flow at a system level). HKCU\Run is pure user-scope,
// no UAC, no admin — perfect for an auto-login wall-display box.
const (
	procName     = "kindle-dash.exe"
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "KindleDash"

	// DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP — start without inheriting
	// our console, and so Ctrl-C in our terminal doesn't reach the child.
	detachedNoGroup = 0x00000008 | 0x00000200

	// CREATE_NO_WINDOW: for our short-lived helper invocations (tasklist /
	// taskkill). When the daemon is started detached and has no console,
	// without this flag Windows would pop a fresh console window for each
	// child — visibly flashing every interval.
	createNoWindow = 0x08000000
)

// cmdNoWindow builds an exec.Cmd that won't spawn a console window. Use this
// instead of exec.Command for any helper invoked from the daemon's main loop.
func cmdNoWindow(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	return c
}

type winService struct{}

func newService() Service { return winService{} }

func (winService) Install(binPath string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open HKCU\\%s: %w", runKeyPath, err)
	}
	defer k.Close()
	// Quote the path so spaces are OK; Windows' Run-key parser handles the
	// outer quotes and treats the rest as args.
	cmdLine := fmt.Sprintf(`"%s" run`, binPath)
	if err := k.SetStringValue(runValueName, cmdLine); err != nil {
		return fmt.Errorf("set HKCU\\%s\\%s: %w", runKeyPath, runValueName, err)
	}
	return nil
}

func (winService) Uninstall() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}

func (winService) Start() error {
	bin, err := installedBinPath()
	if err != nil {
		return fmt.Errorf("locate registered binary: %w (run `install` first)", err)
	}
	cmd := exec.Command(bin, "run")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedNoGroup}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn %s: %w", bin, err)
	}
	// Release the OS handle; we don't wait.
	_ = cmd.Process.Release()
	return nil
}

func (winService) Stop() error {
	pids := otherKindleDashPIDs()
	for _, pid := range pids {
		_ = cmdNoWindow("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
	}
	return nil
}

func (winService) Status() (Status, error) {
	s := Status{}
	if v, err := readRunValue(); err == nil {
		s.Installed = true
		s.Detail = fmt.Sprintf(`HKCU\%s\%s = %s`, runKeyPath, runValueName, v)
	}
	if pids := otherKindleDashPIDs(); len(pids) > 0 {
		s.Running = true
		s.Detail += fmt.Sprintf("\nrunning PIDs: %v", pids)
	}
	return s, nil
}

func readRunValue() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	return v, err
}

func installedBinPath() (string, error) {
	v, err := readRunValue()
	if err != nil {
		return "", err
	}
	// The registered value is `"C:\path\to\kindle-dash.exe" run`; pull out the quoted exe.
	if strings.HasPrefix(v, `"`) {
		if end := strings.Index(v[1:], `"`); end >= 0 {
			return v[1 : 1+end], nil
		}
	}
	if parts := strings.Fields(v); len(parts) > 0 {
		return parts[0], nil
	}
	return "", fmt.Errorf("malformed registry value: %q", v)
}

// otherKindleDashPIDs returns kindle-dash.exe PIDs that aren't our own
// process — used so `status` and `stop` don't count/kill themselves.
func otherKindleDashPIDs() []int {
	selfPID := os.Getpid()
	out, _ := cmdNoWindow("tasklist",
		"/NH", "/FO", "CSV",
		"/FI", "IMAGENAME eq "+procName,
	).CombinedOutput()
	var pids []int
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, `"`+procName) {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 2 {
			continue
		}
		pidStr := strings.Trim(fields[1], `" `)
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid == selfPID {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}
