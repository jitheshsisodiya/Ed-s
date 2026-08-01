// Package egress turns this machine into a usable exit node: it forwards
// packets arriving on the tunnel out to the internet and masquerades them
// behind its own address.
//
// It is the other half of netroute. netroute is what a client does to send
// its traffic to an exit node; this is what the exit node does so that
// traffic goes anywhere. Advertising without it is a promise the machine
// cannot keep — peers route their whole internet through a host that
// silently drops every packet, which is worse than not offering at all.
// Advertise is therefore gated on Supported.
package egress

import "net/netip"

// Config describes what to forward.
type Config struct {
	// Interface is the tunnel device packets arrive on.
	Interface string
	// TunnelPrefix is the network's virtual range. Only traffic from
	// inside it is forwarded, so a misconfigured route elsewhere on the
	// machine cannot turn this host into an open relay for the internet.
	TunnelPrefix netip.Prefix
}

// Forwarder installs and removes the forwarding and NAT rules.
type Forwarder interface {
	// Enable starts forwarding. Safe to call when already enabled.
	Enable(c Config) error
	// Disable restores the machine to what it was, including any kernel
	// forwarding setting that was off before Enable turned it on.
	Disable() error
	// Enabled reports whether the rules are installed, read from the
	// system rather than from memory.
	Enabled() (bool, error)
}

// natTable is the dedicated rule container, kept separate from the kill
// switch's so either can be torn down without touching the other.
const natTable = "nexusvpn_egress"
