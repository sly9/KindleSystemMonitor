// Package lock implements a cross-platform single-instance pidfile.
// We write the current PID into a well-known file under %LOCALAPPDATA% (or
// ~/.cache on Unix). On Acquire, if the file's PID belongs to a live process,
// we fail; otherwise we overwrite and proceed.
package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

type Handle struct {
	path string
}

// DefaultPath returns the OS-conventional pidfile location.
func DefaultPath() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
		}
		return filepath.Join(base, "kindle-dash", "dash.pid")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "kindle-dash", "dash.pid")
}

// Acquire writes os.Getpid() into pidPath. If pidPath already holds a live PID,
// returns an error naming that PID — caller should refuse to start. Stale
// pidfiles (PID dead or unparseable) are silently overwritten.
func Acquire(pidPath string) (*Handle, error) {
	if data, err := os.ReadFile(pidPath); err == nil {
		s := strings.TrimSpace(string(data))
		if pid, perr := strconv.Atoi(s); perr == nil && pid > 0 {
			alive, _ := process.PidExists(int32(pid))
			if alive {
				return nil, fmt.Errorf("another kindle-dash is running (pid=%d, pidfile=%s)", pid, pidPath)
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		return nil, err
	}
	return &Handle{path: pidPath}, nil
}

// Release removes the pidfile. Idempotent.
func (h *Handle) Release() {
	if h == nil {
		return
	}
	_ = os.Remove(h.path)
}
