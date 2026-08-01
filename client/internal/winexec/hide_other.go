//go:build !windows

package winexec

import "os/exec"

// Nothing to hide away from Windows: there is no console window to begin
// with. The package exists on every platform so callers do not need build
// tags of their own.
func hide(*exec.Cmd) {}
