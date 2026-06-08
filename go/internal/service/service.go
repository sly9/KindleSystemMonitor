// Package service handles user-level autostart registration: a Windows
// scheduled task triggered at logon, or a macOS LaunchAgent. Both are
// designed to NOT require admin/root — they run as the current user, ideal
// for an auto-login "wall display" machine.
package service

// TaskName is the schtasks task name (Windows) and forms part of the
// launchd Label (macOS, "com.kindledash.dash").
const TaskName = "KindleDash"

type Status struct {
	Installed bool
	Running   bool
	Detail    string
}

type Service interface {
	Install(binPath string) error
	Uninstall() error
	Start() error
	Stop() error
	Status() (Status, error)
}

// New returns the platform-appropriate Service implementation.
func New() Service { return newService() }
