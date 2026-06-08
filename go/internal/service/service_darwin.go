//go:build darwin

package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const label = "com.kindledash.dash"

type macService struct{}

func newService() Service { return &macService{} }

func plistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

func logDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cache", "kindle-dash"), nil
}

// xmlSafe escapes user-controlled strings for embedding inside <string>...</string>.
func xmlSafe(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}

func (macService) Install(binPath string) error {
	pp, err := plistPath()
	if err != nil {
		return err
	}
	ld, err := logDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(pp), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(ld, 0o700); err != nil {
		return err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>%s/stdout.log</string>
  <key>StandardErrorPath</key>
  <string>%s/stderr.log</string>
</dict>
</plist>
`, xmlSafe(label), xmlSafe(binPath), xmlSafe(ld), xmlSafe(ld))

	if err := os.WriteFile(pp, []byte(plist), 0o644); err != nil {
		return err
	}

	// Unload the previous version (if any), then load the fresh plist. The
	// stderr from `unload` when nothing is loaded is harmless — ignore it.
	_ = exec.Command("launchctl", "unload", pp).Run()
	if out, err := exec.Command("launchctl", "load", pp).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %v\n%s", err, out)
	}
	return nil
}

func (macService) Uninstall() error {
	pp, err := plistPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", pp).Run()
	if err := os.Remove(pp); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (macService) Start() error {
	if out, err := exec.Command("launchctl", "start", label).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl start: %v\n%s", err, out)
	}
	return nil
}

func (macService) Stop() error {
	if out, err := exec.Command("launchctl", "stop", label).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl stop: %v\n%s", err, out)
	}
	return nil
}

func (macService) Status() (Status, error) {
	s := Status{}
	pp, _ := plistPath()
	if _, err := os.Stat(pp); err == nil {
		s.Installed = true
	}
	out, _ := exec.Command("launchctl", "list").CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[2] == label {
			// fields: PID  STATUS  Label. PID "-" means installed but not running.
			s.Running = fields[0] != "-"
			s.Detail = line
			break
		}
	}
	return s, nil
}
