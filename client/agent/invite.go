package agent

import (
	"net/url"
	"strings"
)

// InviteScheme is the URI a NexusVPN invite is shared as. A bare code works
// everywhere too; the scheme exists so an invite can be sent as a link, put
// in a QR code, or registered as a handler by an installer without the
// recipient having to know which of those they were given.
const InviteScheme = "nexusvpn"

// ParseInviteCode extracts the invite code from whatever a person actually
// pasted.
//
// People do not paste codes; they paste the message they were sent. That is
// a bare code, a nexusvpn://join/CODE link, an https://host/join/CODE link
// from a browser, or any of those with a trailing slash, a query string, or
// surrounding whitespace. Every one of those means the same thing, so every
// one of them is accepted rather than answered with "invalid invite code".
//
// The result is upper-cased because invite codes are generated from a
// case-insensitive alphabet; it is not validated further, since only the
// control plane knows which codes exist.
func ParseInviteCode(input string) string {
	s := strings.TrimSpace(input)
	if s == "" {
		return ""
	}

	// Strip a URI wrapper if there is one. Anything that fails to parse is
	// treated as a bare code — a code is never a valid URL, so a parse
	// failure carries no information worth reporting.
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		switch {
		case strings.EqualFold(u.Scheme, InviteScheme):
			// nexusvpn://join/CODE parses with Host="join" and Path="/CODE";
			// nexusvpn:join/CODE parses with an empty Host and Opaque set.
			s = lastSegment(u.Opaque + u.Host + u.Path)
		case strings.EqualFold(u.Scheme, "http"), strings.EqualFold(u.Scheme, "https"):
			s = lastSegment(u.Path)
		default:
			return ""
		}
	}

	s = strings.TrimSpace(strings.Trim(s, "/"))
	return strings.ToUpper(s)
}

// InviteLink renders a code as the shareable form.
func InviteLink(code string) string {
	return InviteScheme + "://join/" + strings.ToUpper(strings.TrimSpace(code))
}

// lastSegment returns the final non-empty path segment, so both
// "join/K7M2QP" and "/join/K7M2QP/" yield "K7M2QP".
func lastSegment(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if p := strings.TrimSpace(parts[i]); p != "" && !strings.EqualFold(p, "join") {
			return p
		}
	}
	return ""
}
