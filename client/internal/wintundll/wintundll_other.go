//go:build !windows

package wintundll

// Ensure does nothing away from Windows: Linux and macOS create tunnel
// interfaces through the kernel directly, with no driver to ship.
func Ensure() error { return nil }
