//go:build windows

package service

import (
	"fmt"
	"os/exec"
	"strings"
)

const procName = "kindle-dash.exe"

type winService struct{}

func newService() Service { return &winService{} }

// Install registers a schtasks ONLOGON task that runs the binary at login.
// /RL LIMITED keeps the task running as a standard user (no UAC prompt).
// A brief console window flash at login is the price of a single binary;
// for completely silent autostart, build with -ldflags="-H=windowsgui" and
// install that variant instead.
func (winService) Install(binPath string) error {
	tr := fmt.Sprintf(`"%s" run`, binPath)
	out, err := exec.Command("schtasks",
		"/Create",
		"/TN", TaskName,
		"/TR", tr,
		"/SC", "ONLOGON",
		"/RL", "LIMITED",
		"/F",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Create: %v\n%s", err, out)
	}
	return nil
}

func (winService) Uninstall() error {
	out, err := exec.Command("schtasks", "/Delete", "/TN", TaskName, "/F").CombinedOutput()
	if err != nil {
		// "cannot find the file specified" → already not installed; treat as success.
		s := string(out)
		if strings.Contains(s, "cannot find") || strings.Contains(s, "does not exist") {
			return nil
		}
		return fmt.Errorf("schtasks /Delete: %v\n%s", err, out)
	}
	return nil
}

func (winService) Start() error {
	out, err := exec.Command("schtasks", "/Run", "/TN", TaskName).CombinedOutput()
	if err != nil {
		return fmt.Errorf("schtasks /Run: %v\n%s", err, out)
	}
	return nil
}

func (winService) Stop() error {
	// /End terminates the running scheduled task instance (if any).
	_ = exec.Command("schtasks", "/End", "/TN", TaskName).Run()
	// Also kill any kindle-dash.exe started outside the scheduler (manual `run`).
	_ = exec.Command("taskkill", "/F", "/IM", procName, "/T").Run()
	return nil
}

func (winService) Status() (Status, error) {
	s := Status{}
	out, err := exec.Command("schtasks", "/Query", "/TN", TaskName, "/FO", "LIST").CombinedOutput()
	if err == nil {
		s.Installed = true
		s.Detail = strings.TrimSpace(string(out))
	}
	pout, _ := exec.Command("tasklist", "/NH", "/FO", "CSV", "/FI", "IMAGENAME eq "+procName).CombinedOutput()
	if strings.Contains(string(pout), procName) {
		s.Running = true
	}
	return s, nil
}
