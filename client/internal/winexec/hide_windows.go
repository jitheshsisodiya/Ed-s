//go:build windows

package winexec

import (
	"os/exec"
	"syscall"
)

// createNoWindow stops the console subsystem from allocating one at all.
// HideWindow alone asks for it to start hidden, which on some Windows builds
// still paints a frame for an instant; the pair between them covers both.
const createNoWindow = 0x08000000

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
