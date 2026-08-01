package agent

import "testing"

// The link is the whole interface between two devices that have never met,
// so every shape it can arrive in is worth pinning.
func TestParsePairingLink(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  string
		server string
		token  string
		ok     bool
	}{
		{
			name:   "a link this app generated",
			input:  "nexusvpn://pair?s=http%3A%2F%2F192.168.1.20%3A8080&t=abc.def.ghi",
			server: "http://192.168.1.20:8080",
			token:  "abc.def.ghi",
			ok:     true,
		},
		{
			name:   "the opaque form, which some QR readers produce",
			input:  "nexusvpn:pair?s=http%3A%2F%2F10.0.0.5%3A8080&t=tok",
			server: "http://10.0.0.5:8080",
			token:  "tok",
			ok:     true,
		},
		{
			name:   "mixed case scheme, because scanners rewrite them",
			input:  "NexusVPN://PAIR?s=http%3A%2F%2F10.0.0.5%3A8080&t=tok",
			server: "http://10.0.0.5:8080",
			token:  "tok",
			ok:     true,
		},
		{
			name:   "surrounding whitespace from a paste",
			input:  "  nexusvpn://pair?s=http%3A%2F%2F10.0.0.5%3A8080&t=tok  ",
			server: "http://10.0.0.5:8080",
			token:  "tok",
			ok:     true,
		},

		// A join link carries a network invite and no session. Treating one
		// as the other would produce a claim against a token that is not one.
		{name: "an invite link is not a pairing link", input: "nexusvpn://join/ABC123"},

		{name: "no token", input: "nexusvpn://pair?s=http%3A%2F%2F10.0.0.5%3A8080"},
		{name: "no server", input: "nexusvpn://pair?t=tok"},
		{name: "someone else's scheme", input: "otherapp://pair?s=http%3A%2F%2Fx&t=tok"},
		{name: "empty", input: ""},
		{name: "not a link at all", input: "ABC123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, token, ok := ParsePairingLink(tc.input)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if server != tc.server || token != tc.token {
				t.Fatalf("got (%q, %q), want (%q, %q)", server, token, tc.server, tc.token)
			}
		})
	}
}

// TestReachableServerURLReplacesLoopback is the reason the link carries a
// server at all. The desktop app talks to its own control plane on
// 127.0.0.1, which on a phone means the phone.
func TestReachableServerURLReplacesLoopback(t *testing.T) {
	lan := lanAddress()
	if lan == "" {
		t.Skip("this machine has no routable address to substitute")
	}

	got := reachableServerURL("http://127.0.0.1:8080")
	if got == "http://127.0.0.1:8080" {
		t.Fatal("handed a phone the address of the phone")
	}
	want := "http://" + lan + ":8080"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// An address somebody deliberately configured is left alone: they know
// something about their network that this process does not.
func TestReachableServerURLLeavesRealAddressesAlone(t *testing.T) {
	for _, raw := range []string{
		"http://192.168.1.50:8080",
		"https://vpn.example.com",
		"http://10.1.2.3:9000",
	} {
		if got := reachableServerURL(raw); got != raw {
			t.Errorf("rewrote %q to %q", raw, got)
		}
	}
}

// A link is only useful if the thing that reads it is the thing that wrote
// it, so the two are checked against each other rather than separately.
func TestGeneratedLinksParseBack(t *testing.T) {
	for _, server := range []string{
		"http://192.168.1.20:8080",
		"http://10.0.0.5:8080",
		"https://vpn.example.com",
	} {
		link := "nexusvpn://pair?s=" + urlEscape(server) + "&t=some.jwt.value"
		gotServer, gotToken, ok := ParsePairingLink(link)
		if !ok {
			t.Fatalf("could not read back a link for %q", server)
		}
		if gotServer != server {
			t.Errorf("server round-tripped as %q, want %q", gotServer, server)
		}
		if gotToken != "some.jwt.value" {
			t.Errorf("token round-tripped as %q", gotToken)
		}
	}
}

func urlEscape(s string) string {
	out := ""
	for _, r := range s {
		switch {
		case r == ':' || r == '/' || r == '?' || r == '&' || r == '=':
			out += "%" + hexByte(byte(r))
		default:
			out += string(r)
		}
	}
	return out
}

func hexByte(b byte) string {
	const digits = "0123456789ABCDEF"
	return string([]byte{digits[b>>4], digits[b&0xf]})
}
