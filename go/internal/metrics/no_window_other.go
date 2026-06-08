//go:build !windows

package metrics

import "syscall"

// On non-Windows, there's nothing to suppress — pass nil.
func noWindowAttr() *syscall.SysProcAttr { return nil }
