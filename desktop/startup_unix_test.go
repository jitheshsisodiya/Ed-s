//go:build !windows

package main

import (
	"os"
	"strings"
	"testing"
)

// TestStartWithSystemRoundTrip checks the setting is really a file on disk
// that names this program, and that turning it off removes it. A toggle that
// reports success without writing anything would look identical in the UI.
func TestStartWithSystemRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	if startsWithSystem() {
		t.Fatal("a fresh home should not be set to start at sign-in")
	}

	if err := setStartsWithSystem(true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !startsWithSystem() {
		t.Fatal("enable reported success but the setting did not stick")
	}

	path, err := startupPath()
	if err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(blob), exe) {
		t.Fatalf("%s does not name this program:\n%s", path, blob)
	}

	if err := setStartsWithSystem(false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if startsWithSystem() {
		t.Fatal("disable reported success but the setting is still on")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("%s survived being turned off", path)
	}
}

// TestDisableWhenAlreadyOffSucceeds: the UI calls this on every toggle, and a
// missing file is the desired state, not a failure to report.
func TestDisableWhenAlreadyOffSucceeds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	if err := setStartsWithSystem(false); err != nil {
		t.Fatalf("turning off something already off: %v", err)
	}
}
