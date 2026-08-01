package agent

import "testing"

// Every one of these is something a person could plausibly paste into the
// join field. Each one has to work, because the alternative is telling
// somebody their invite is invalid when it is not.
func TestParseInviteCode(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"bare code", "K7M2QP", "K7M2QP"},
		{"lower case", "k7m2qp", "K7M2QP"},
		{"padded by a copy-paste", "  K7M2QP\n", "K7M2QP"},
		{"app link", "nexusvpn://join/K7M2QP", "K7M2QP"},
		{"app link, opaque form", "nexusvpn:join/K7M2QP", "K7M2QP"},
		{"app link with a trailing slash", "nexusvpn://join/K7M2QP/", "K7M2QP"},
		{"app link, mixed case scheme", "NexusVPN://join/k7m2qp", "K7M2QP"},
		{"web link", "https://nexus.example.com/join/K7M2QP", "K7M2QP"},
		{"web link with a query string", "https://nexus.example.com/join/K7M2QP?from=email", "K7M2QP"},
		{"plain http", "http://nexus.example.com/join/K7M2QP", "K7M2QP"},

		{"empty", "", ""},
		{"whitespace only", "   ", ""},
		// A link we do not recognise is not an invite. Guessing at its last
		// path segment would send a stranger's URL fragment to the server.
		{"someone else's scheme", "mailto:alice@example.com", ""},
		{"unrelated scheme", "ftp://example.com/join/K7M2QP", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseInviteCode(tc.input); got != tc.want {
				t.Fatalf("ParseInviteCode(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// A link we generate must be a link we accept.
func TestInviteLinkRoundTrips(t *testing.T) {
	for _, code := range []string{"K7M2QP", "b3xq7t", "0123456789ABCDEF"} {
		link := InviteLink(code)
		if got := ParseInviteCode(link); got != ParseInviteCode(code) {
			t.Fatalf("ParseInviteCode(InviteLink(%q)) = %q, want %q",
				code, got, ParseInviteCode(code))
		}
	}
}
