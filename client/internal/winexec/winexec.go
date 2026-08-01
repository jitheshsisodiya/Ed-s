// Package winexec runs helper programs without putting a console window on
// screen.
//
// Bringing a tunnel up on Windows means a handful of short PowerShell and
// netsh calls — assigning the address, adding routes, arming the kill switch.
// Launched the ordinary way from a GUI application, each one flashes a black
// console window over whatever the person is doing. It looks broken, and on a
// slow machine several of them stack up.
package winexec

import "os/exec"

// Command is exec.Command with any console window suppressed.
func Command(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	hide(cmd)
	return cmd
}
