//go:build windows

package metrics

import "syscall"

// CREATE_NO_WINDOW: when a detached daemon (no console) execs a console
// subprocess like nvidia-smi, Windows would otherwise allocate a brand-new
// console window for it. This flag tells the kernel "no console, period" so
// the user doesn't see flashes every interval.
const createNoWindow = 0x08000000

func noWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: createNoWindow}
}
