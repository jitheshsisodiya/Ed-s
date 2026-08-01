package main

import "testing"

// TestQuitRequested guards the flag an installer depends on. Getting it wrong
// is not a crash: the installer's request is ignored, the running copy holds
// its own executable open, and the upgrade fails with "Error opening file for
// writing" naming a file whose owner is the app being upgraded.
func TestQuitRequested(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want bool
	}{
		{"no arguments", nil, false},
		{"double dash", []string{"--quit"}, true},
		{"single dash", []string{"-quit"}, true},
		{"windows slash", []string{"/quit"}, true},
		{"among others", []string{"--verbose", "--quit"}, true},
		{"an invite link is not a quit", []string{"nexusvpn://join/ABC123"}, false},
		{"a similar word is not the flag", []string{"--quitely"}, false},
		{"bare word is not the flag", []string{"quit"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := QuitRequested(tc.args); got != tc.want {
				t.Errorf("QuitRequested(%q) = %v, want %v", tc.args, got, tc.want)
			}
		})
	}
}
