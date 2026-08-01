// Package netroute manipulates the host's routing table so a WireGuard
// tunnel can carry the default route — the "exit node" mode, where every
// packet leaves through a peer instead of only the peers' own addresses.
//
// The whole package exists to get one thing right: a tunnel that carries
// the default route must not carry its own transport. The UDP packets
// WireGuard sends to the exit node are ordinary internet traffic, so if
// 0.0.0.0/0 points at the tunnel they get encrypted and sent to the exit
// node, whose reply is itself encrypted and sent to the exit node, and the
// connection is dead before the first handshake completes. Every platform
// here therefore pins a host route to the exit node's real endpoint through
// the physical gateway *before* touching the default route, and removes it
// last on the way back out.
//
// The second thing it gets right is crash safety. None of these
// implementations delete the existing default route. They install the
// 0.0.0.0/1 + 128.0.0.0/1 pair, which is longer-prefix and therefore wins,
// while leaving the original route untouched underneath. A client that is
// killed with SIGKILL leaves two routes bound to an interface that no
// longer exists — which the kernel drops with the interface — and the
// original default is still there. Deleting and restoring would leave a
// machine with no route at all if the process died in between.
package netroute

import (
	"fmt"
	"net"
)

// Default describes the host's default route as it was before the tunnel
// touched anything.
type Default struct {
	// Gateway is the next hop for off-link traffic.
	Gateway net.IP
	// Interface is the physical interface that gateway is reached through.
	Interface string
}

// String renders a Default for logs.
func (d Default) String() string {
	if d.Gateway == nil {
		return "none"
	}
	return fmt.Sprintf("%s via %s", d.Gateway, d.Interface)
}

// ErrNoDefaultRoute is returned when the host has no default route, which
// makes exit-node mode meaningless: there is no physical path to pin the
// tunnel's own transport through.
var ErrNoDefaultRoute = fmt.Errorf("netroute: host has no default route")

// splitDefault is the pair of routes that covers the whole IPv4 space in
// two halves. Each is a /1, so both beat a /0 on prefix length without the
// original default ever being removed.
var splitDefault = []string{"0.0.0.0/1", "128.0.0.0/1"}

// splitDefaultV6 does the same for IPv6.
var splitDefaultV6 = []string{"::/1", "8000::/1"}

// Router installs and removes the routes that make a tunnel the default
// path. Implementations are per-platform; every method is safe to call
// twice, because teardown runs on paths that may already have torn down.
type Router interface {
	// Snapshot records the host's default route before anything changes.
	Snapshot() (Default, error)

	// PinEndpoint routes a single host — the exit node's real, public
	// endpoint — through the physical gateway, so the tunnel does not
	// swallow its own transport.
	PinEndpoint(ip net.IP, via Default) error

	// UnpinEndpoint removes that host route.
	UnpinEndpoint(ip net.IP) error

	// CaptureDefault points the whole address space at the tunnel.
	CaptureDefault(iface string) error

	// ReleaseDefault removes the routes CaptureDefault installed.
	ReleaseDefault(iface string) error
}

// hostPrefix returns the single-address CIDR for an IP: /32 for IPv4,
// /128 for IPv6.
func hostPrefix(ip net.IP) string {
	if ip.To4() != nil {
		return ip.String() + "/32"
	}
	return ip.String() + "/128"
}

// isIPv6 reports whether an address is IPv6, so callers pick the right
// half-space pair.
func isIPv6(ip net.IP) bool { return ip.To4() == nil }
