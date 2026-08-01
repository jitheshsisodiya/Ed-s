//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// taskName is the scheduled task that launches NexusVPN at sign-in.
const taskName = "NexusVPN"

const startupSupported = true

// Launching at sign-in goes through the task scheduler rather than the usual
// Run registry key, because this app asks for administrator in order to
// create the tunnel adapter, and Windows silently skips elevated programs
// listed under Run — the entry looks right and nothing ever starts. A task
// registered to run with highest privileges is the mechanism that survives
// UAC, and the app is already elevated when it registers one.

func startsWithSystem() bool {
	return schtasks("/Query", "/TN", taskName) == nil
}

func setStartsWithSystem(on bool) error {
	if !on {
		if !startsWithSystem() {
			return nil
		}
		return schtasks("/Delete", "/TN", taskName, "/F")
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find where this app is installed: %w", err)
	}
	// The path is passed unquoted: Go quotes an argument containing spaces
	// on the way out, which is exactly what schtasks wants and what hand
	// written quotes would double up.
	return schtasks("/Create", "/TN", taskName, "/TR", exe,
		"/SC", "ONLOGON", "/RL", "HIGHEST", "/F")
}

func schtasks(args ...string) error {
	cmd := exec.Command("schtasks.exe", args...)
	// Without this a console window flashes on screen every time the app
	// checks whether the task exists.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%s", msg)
		}
		return err
	}
	return nil
}
