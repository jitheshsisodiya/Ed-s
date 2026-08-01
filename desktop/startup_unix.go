//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Launching at sign-in is a file on both of these platforms: an XDG autostart
// entry on Linux, a per-user LaunchAgent on macOS. Both live under the user's
// own home directory, so neither needs privileges to write and neither
// affects anybody else who signs into the machine.

var startupSupported = runtime.GOOS == "linux" || runtime.GOOS == "darwin"

func startsWithSystem() bool {
	path, err := startupPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

func setStartsWithSystem(on bool) error {
	path, err := startupPath()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not remove %s: %w", path, err)
		}
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not find where this app is installed: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(startupFile(exe)), 0o644)
}

func startupPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "LaunchAgents", "com.nexusvpn.desktop.plist"), nil
	case "linux":
		base := os.Getenv("XDG_CONFIG_HOME")
		if base == "" {
			base = filepath.Join(home, ".config")
		}
		return filepath.Join(base, "autostart", "nexusvpn.desktop"), nil
	default:
		return "", fmt.Errorf("starting at sign-in is not supported on %s", runtime.GOOS)
	}
}

func startupFile(exe string) string {
	if runtime.GOOS == "darwin" {
		return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>com.nexusvpn.desktop</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlEscape(exe) + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
	}
	return `[Desktop Entry]
Type=Application
Name=NexusVPN
Exec=` + exe + `
Terminal=false
X-GNOME-Autostart-enabled=true
`
}

// xmlEscape covers the three characters that can appear in a filesystem path
// and break a plist. Reaching for encoding/xml to write four lines of static
// document would be the larger dependency.
func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}
