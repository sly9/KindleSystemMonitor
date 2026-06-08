//go:build windows

package service

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

// We register an elevated logon-triggered task with Task Scheduler.
//
// PawnIO (used for CPU temperature) requires the daemon to run as
// administrator. /RL HIGHEST does exactly that — the install command itself
// must be elevated, but once registered the task starts at user logon with
// admin privileges and no UAC prompt.
//
// Previously we used HKCU\...\Run (no admin needed) which still works for
// users who don't care about CPU temp. The Install() path here transitions
// from that — we delete the legacy registry entry to avoid double-start.
const (
	procName     = "kindle-dash.exe"
	taskScName   = TaskName
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "KindleDash"

	createNoWindow = 0x08000000
)

func cmdNoWindow(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	return c
}

type winService struct{}

func newService() Service { return winService{} }

func (winService) Install(binPath string) error {
	tr := fmt.Sprintf(`"%s" run`, binPath)
	out, err := cmdNoWindow("schtasks",
		"/Create",
		"/TN", taskScName,
		"/TR", tr,
		"/SC", "ONLOGON",
		"/RL", "HIGHEST",
		"/F",
	).CombinedOutput()
	if err != nil {
		s := string(out)
		if strings.Contains(s, "Access is denied") || strings.Contains(s, "拒绝访问") {
			return fmt.Errorf("schtasks /Create needs admin (re-run the install script with UAC):\n%s", s)
		}
		return fmt.Errorf("schtasks /Create: %v\n%s", err, out)
	}
	// Drop the legacy HKCU\Run entry if a previous version of kindle-dash
	// installed it, so we don't autostart twice.
	if k, oerr := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE); oerr == nil {
		_ = k.DeleteValue(runValueName)
		k.Close()
	}
	return nil
}

func (winService) Uninstall() error {
	out, err := cmdNoWindow("schtasks", "/Delete", "/TN", taskScName, "/F").CombinedOutput()
	if err != nil {
		s := string(out)
		if !strings.Contains(s, "cannot find") && !strings.Contains(s, "does not exist") {
			return fmt.Errorf("schtasks /Delete: %v\n%s", err, out)
		}
	}
	// Best-effort: also remove any legacy registry-Run entry.
	if k, oerr := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE); oerr == nil {
		_ = k.DeleteValue(runValueName)
		k.Close()
	}
	return nil
}

func (winService) Start() error {
	// Asking the Task Scheduler to /Run the task launches the binary with the
	// HIGHEST privileges we registered — no UAC prompt at runtime.
	out, err := cmdNoWindow("schtasks", "/Run", "/TN", taskScName).CombinedOutput()
	if err != nil {
		s := string(out)
		if strings.Contains(s, "cannot find") || strings.Contains(s, "does not exist") {
			return fmt.Errorf("not installed — run `kindle-dash install` first (elevated)")
		}
		return fmt.Errorf("schtasks /Run: %v\n%s", err, out)
	}
	return nil
}

func (winService) Stop() error {
	_ = cmdNoWindow("schtasks", "/End", "/TN", taskScName).Run()
	for _, pid := range otherKindleDashPIDs() {
		_ = cmdNoWindow("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
	}
	return nil
}

func (winService) Status() (Status, error) {
	s := Status{}

	// schtasks /Query — task installed?
	out, err := cmdNoWindow("schtasks", "/Query", "/TN", taskScName, "/FO", "LIST").CombinedOutput()
	if err == nil {
		s.Installed = true
		s.Detail = strings.TrimSpace(string(out))
	} else {
		// Fall back to checking the legacy HKCU\Run entry.
		if v, rerr := readRunValue(); rerr == nil {
			s.Installed = true
			s.Detail = fmt.Sprintf("(legacy HKCU\\Run) %s\\%s = %s", runKeyPath, runValueName, v)
		}
	}

	if pids := otherKindleDashPIDs(); len(pids) > 0 {
		s.Running = true
		s.Detail += fmt.Sprintf("\nrunning PIDs: %v", pids)
	}
	return s, nil
}

// otherKindleDashPIDs returns kindle-dash.exe PIDs that aren't our own process.
func otherKindleDashPIDs() []int {
	selfPID := syscall.Getpid()
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
		pid, perr := strconv.Atoi(pidStr)
		if perr != nil || pid == selfPID {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// readRunValue reads the legacy HKCU\...\Run entry, for backward-compat status.
func readRunValue() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValueName)
	if err != nil {
		return "", err
	}
	return v, nil
}

// keep errors package usable even if some platforms below don't use it
var _ = errors.New
