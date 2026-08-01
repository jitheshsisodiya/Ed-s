package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/grandcat/zeroconf"
)

// ServiceType is how a NexusVPN control plane announces itself locally. It
// must match the announcing side in backend/local.
const ServiceType = "_nexusvpn._tcp"

// Found is a control plane discovered on this network.
type Found struct {
	// Name is what to show somebody choosing between several.
	Name string `json:"name"`
	// URL is what to point a client at.
	URL string `json:"url"`
	// Fingerprint is the certificate that server presents, carried in the
	// announcement so a device learns which one to expect at the same moment
	// it learns where to look. Without it, discovery hands over an address
	// and no way to tell the right server from anything else that answered —
	// which is worse than no discovery, because it looks like it worked.
	Fingerprint string `json:"fingerprint"`
}

// Discover looks for control planes on the local network.
//
// This is the answer to addresses going stale. Every address written down
// somewhere — typed into a phone, carried in a pairing code, remembered in a
// config file — is right until the router hands that machine a different one,
// which it will, because addresses are leases. Asking the network where
// something is has no expiry.
//
// Returns an empty list rather than an error when nothing answers: a network
// that filters multicast, which plenty of guest and enterprise wireless does,
// is a normal thing to be on and not a failure to report.
func Discover(ctx context.Context, wait time.Duration) []Found {
	if wait <= 0 {
		wait = 3 * time.Second
	}
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil
	}

	entries := make(chan *zeroconf.ServiceEntry, 8)
	found := map[string]Found{}
	done := make(chan struct{})

	go func() {
		defer close(done)
		for entry := range entries {
			if f, ok := fromEntry(entry); ok {
				// Keyed by URL so a server answering on several interfaces —
				// which one with both Wi-Fi and Ethernet does — is one entry
				// per address rather than the same machine listed twice under
				// the same address.
				found[f.URL] = f
			}
		}
	}()

	browseCtx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	if err := resolver.Browse(browseCtx, ServiceType, "local.", entries); err != nil {
		return nil
	}
	<-browseCtx.Done()
	<-done

	out := make([]Found, 0, len(found))
	for _, f := range found {
		out = append(out, f)
	}
	// Sorted so the list does not reshuffle between two searches that found
	// the same things, which reads as instability.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// fromEntry turns an announcement into something usable, or reports that it
// is not one.
func fromEntry(entry *zeroconf.ServiceEntry) (Found, bool) {
	if entry == nil || entry.Port == 0 {
		return Found{}, false
	}

	host := firstUsableAddress(entry)
	if host == "" {
		return Found{}, false
	}

	fingerprint := ""
	for _, txt := range entry.Text {
		if after, ok := strings.CutPrefix(txt, "fp="); ok {
			fingerprint = strings.TrimSpace(after)
		}
	}

	name := strings.TrimSpace(entry.Instance)
	if name == "" {
		name = host
	}
	return Found{
		Name: name,
		// https because the control plane always speaks TLS. An announcement
		// carrying a plain address would produce a client that connects and
		// is immediately hung up on.
		URL:         fmt.Sprintf("https://%s:%d", host, entry.Port),
		Fingerprint: fingerprint,
	}, true
}

// firstUsableAddress picks an address another machine can actually reach.
//
// An announcement can carry several, including link-local ones that are
// useless from here. IPv4 is preferred because a home network is where this
// runs and IPv4 is what such a network reliably routes.
func firstUsableAddress(entry *zeroconf.ServiceEntry) string {
	for _, ip := range entry.AddrIPv4 {
		if ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return ip.String()
		}
	}
	for _, ip := range entry.AddrIPv6 {
		if ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			return "[" + ip.String() + "]"
		}
	}
	return ""
}
