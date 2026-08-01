package agent

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
)

// PairingLink is what a QR code on a signed-in screen carries.
//
// It holds the server as well as the code, because the address is the part a
// person cannot be expected to know — it is whatever private IP the router
// handed this machine — and having it travel with the code is most of the
// point. A phone that reads one of these needs to be told nothing at all.
type PairingLink struct {
	// URL is the whole thing, ready to render as a QR code.
	URL string `json:"url"`
	// ServerURL and Token are the parts, so a caller can show or use either.
	ServerURL string `json:"serverUrl"`
	Token     string `json:"token"`
	// ExpiresIn is how many seconds the code is good for, so a screen showing
	// one can count down rather than going quietly stale.
	ExpiresIn int `json:"expiresIn"`
	// Fingerprint is the certificate the scanning device should expect, and
	// refuse to talk to anything else holding. Carried here because the QR
	// code is the one channel between these two machines that nobody on the
	// network is sitting in the middle of.
	Fingerprint string `json:"fingerprint"`
}

// pairPath is the host part of nexusvpn://pair?…, kept distinct from join so
// a reader can tell a code that carries a session from one that does not.
const pairPath = "pair"

// StartPairing asks the control plane for a code that will sign another
// device in as this account, on this network.
//
// fingerprint is this server's certificate, which the scanning device pins.
// Passed in rather than discovered, because only the process hosting the
// control plane knows it, and an agent talking to somebody else's server has
// no business inventing one.
func (a *Agent) StartPairing(ctx context.Context, networkID, fingerprint string) (*PairingLink, error) {
	cfg, client, err := a.authed()
	if err != nil {
		return nil, err
	}

	p, err := client.StartPairing(ctx, networkID)
	if err != nil {
		return nil, err
	}

	// The address other machines can reach, not the loopback one this
	// process happens to use: a phone reading this is by definition not on
	// this machine.
	server := reachableServerURL(cfg.ServerURL)

	if fingerprint == "" {
		fingerprint = cfg.ServerFingerprint
	}

	q := url.Values{}
	q.Set("s", server)
	q.Set("t", p.Token)
	if fingerprint != "" {
		q.Set("f", fingerprint)
	}

	return &PairingLink{
		URL:         fmt.Sprintf("%s://%s?%s", InviteScheme, pairPath, q.Encode()),
		ServerURL:   server,
		Token:       p.Token,
		ExpiresIn:   p.ExpiresIn,
		Fingerprint: fingerprint,
	}, nil
}

// ParsePairingLink pulls the server and code out of a scanned link. Both
// parts are required: a code without a server cannot be redeemed anywhere.
func ParsePairingLink(input string) (serverURL, token, fingerprint string, ok bool) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", "", false
	}
	u, err := url.Parse(s)
	if err != nil || !strings.EqualFold(u.Scheme, InviteScheme) {
		return "", "", "", false
	}
	if !strings.EqualFold(strings.Trim(u.Opaque+u.Host+u.Path, "/"), pairPath) {
		return "", "", "", false
	}

	q := u.Query()
	serverURL = strings.TrimSpace(q.Get("s"))
	token = strings.TrimSpace(q.Get("t"))
	// The fingerprint is optional so a link from a deployment with a real
	// certificate still works. Its absence means ordinary verification, not
	// no verification.
	fingerprint = strings.TrimSpace(q.Get("f"))
	if serverURL == "" || token == "" {
		return "", "", "", false
	}
	return serverURL, token, fingerprint, true
}

// ClaimPairing redeems a scanned link: it stores the server, adopts the
// session the code buys, and reports which network to connect to.
//
// Everything is written in one update so a failure part-way through cannot
// leave a session pointing at one server and an address belonging to another.
func (a *Agent) ClaimPairing(ctx context.Context, link string) (networkID string, err error) {
	serverURL, token, fingerprint, ok := ParsePairingLink(link)
	if !ok {
		return "", errors.New("that is not a NexusVPN pairing code")
	}

	cfg, err := a.store.Load()
	if err != nil {
		return "", err
	}
	cfg.ServerURL = strings.TrimRight(serverURL, "/")
	// Installed before the claim, because the claim itself is the first
	// request to that server and must already be refusing impostors.
	cfg.ServerFingerprint = fingerprint

	claimed, err := a.client(cfg).ClaimPairing(ctx, token)
	if err != nil {
		return "", err
	}

	if _, err := a.store.Update(func(c *config.Config) error {
		c.ServerURL = cfg.ServerURL
		c.ServerFingerprint = fingerprint
		c.AccessToken = claimed.AccessToken
		c.RefreshToken = claimed.RefreshToken
		c.UserEmail = claimed.Email
		return nil
	}); err != nil {
		return "", err
	}
	return claimed.NetworkID, nil
}

// reachableServerURL turns an address this process uses into one another
// machine can use.
//
// The desktop app talks to its own control plane over loopback, which is
// correct for itself and useless to anybody else — a phone told 127.0.0.1
// looks for the server on the phone. Where this machine has a routable
// address, it is substituted; where it does not, the original is returned
// rather than a guess.
func reachableServerURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return raw
	}
	lan := LANAddress()
	if lan == "" {
		return raw
	}
	if port := u.Port(); port != "" {
		u.Host = lan + ":" + port
	} else {
		u.Host = lan
	}
	return u.String()
}
