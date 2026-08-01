package main

import "testing"

// TestInviteFromArgs guards the boundary where an operating system hands this
// program a string somebody else wrote. Anything reached this way should have
// to look like an invite link and nothing else.
func TestInviteFromArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"nothing", nil, ""},
		{"plain launch", []string{"--debug"}, ""},
		{"invite link", []string{"nexusvpn://join/ABC123"}, "ABC123"},
		{"uppercase scheme", []string{"NEXUSVPN://JOIN/ABC123"}, "ABC123"},
		{"after other arguments", []string{"--x", "nexusvpn://join/ABC123"}, "ABC123"},

		// A bare code is fine to paste into the join dialog, where a person
		// meant it. Arriving as a command-line argument, it is just a word.
		{"bare code", []string{"ABC123"}, ""},

		// The https form is what a browser opens, and a browser opens it in
		// a browser. Honouring it here would let any argument that happens to
		// be a URL steer this app at a network.
		{"https link", []string{"https://example.com/join/ABC123"}, ""},

		{"other scheme", []string{"otherapp://join/ABC123"}, ""},
		{"scheme prefix only", []string{"nexusvpnx://join/ABC123"}, ""},
		{"empty code", []string{"nexusvpn://join/"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := inviteFromArgs(tc.args); got != tc.want {
				t.Errorf("inviteFromArgs(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestTakePendingInviteClearsIt: the code is acted on once. A reload that
// re-offered a dismissed invite would keep reopening a dialog somebody has
// already said no to.
func TestTakePendingInviteClearsIt(t *testing.T) {
	a := NewApp()
	a.offerInvite("ABC123")

	if got := a.TakePendingInvite(); got != "ABC123" {
		t.Fatalf("first take = %q, want ABC123", got)
	}
	if got := a.TakePendingInvite(); got != "" {
		t.Fatalf("second take = %q, want it forgotten", got)
	}
}
