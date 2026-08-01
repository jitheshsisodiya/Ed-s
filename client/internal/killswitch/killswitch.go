// Package killswitch blocks traffic that would otherwise leave the machine
// unprotected while an exit-node tunnel is down.
//
// It only makes sense alongside exit-node mode. In the mesh's normal
// split-tunnel shape a dropped tunnel means peers are unreachable and
// nothing else changes, so there is nothing to protect: blocking traffic
// would take away someone's internet to protect them from a risk they never
// had. When the tunnel carries the default route, a drop is different — the
// host silently falls back to its physical default and every packet the
// user believed was tunnelled goes out in the clear. That gap is what this
// closes.
//
// Two rules govern the design:
//
// The rules live in a dedicated container per platform — an nftables table,
// a pf anchor, a firewall rule group — so releasing them can never touch a
// rule the user or their distribution put there. Nothing here edits a
// shared chain.
//
// And what stays permitted is chosen so a locked machine is still a usable
// machine: loopback, DHCP, the local subnet, and the tunnel's own transport
// to its endpoints. Blocking the LAN would cut a user off from their
// printer and their router's admin page to protect them from an exposure
// that only exists on the path to the internet.
package killswitch

import (
	"fmt"
	"net"
	"net/netip"
)

// Policy describes what stays reachable while the switch is engaged.
type Policy struct {
	// Interface is the tunnel device. Traffic out of it is always allowed:
	// it is the protected path, and when the tunnel recovers this is what
	// carries the recovery.
	Interface string

	// Endpoints are the real UDP addresses of the peers and relays this
	// client must keep talking to, which is how the tunnel gets back up.
	// Blocking these would make the lock permanent — the client could
	// never re-handshake — so the switch would have to be released by
	// hand, on a machine with no internet, which nobody can do.
	Endpoints []netip.AddrPort

	// LocalNetworks are the on-link subnets to leave alone: the printer,
	// the NAS, the router's admin page. Exposure the tunnel was hiding is
	// on the path to the internet, not on the path to the next room.
	LocalNetworks []netip.Prefix

	// AllowLAN, when false, drops LocalNetworks and locks the machine down
	// to loopback plus the tunnel. Correct on a hostile network — a
	// conference or hotel LAN — where the local subnet is exactly what you
	// are hiding from.
	AllowLAN bool
}

// Switch installs and removes the block. Every method is safe to call when
// the switch is already in the state being asked for, because both engage
// and release run on recovery paths that may have run already.
type Switch interface {
	// Engage blocks everything outside the policy.
	Engage(p Policy) error

	// Release removes every rule this package installed, and nothing else.
	Release() error

	// Engaged reports whether the rules are currently installed, read from
	// the system rather than from memory — a process that was killed and
	// restarted has to be able to find its own leftovers.
	Engaged() (bool, error)
}

// tableName is the dedicated rule container, identical across platforms so
// an operator finds the same word wherever they look.
const tableName = "nexusvpn_killswitch"

// DefaultLocalNetworks returns the on-link prefixes of every interface
// except the tunnel, which is what a sane AllowLAN policy permits.
func DefaultLocalNetworks(exclude string) ([]netip.Prefix, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("killswitch: list interfaces: %w", err)
	}

	var out []netip.Prefix
	for _, iface := range ifaces {
		if iface.Name == exclude || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(n.IP)
			if !ok {
				continue
			}
			ones, _ := n.Mask.Size()
			// Masked to the network address: a prefix carrying host bits
			// is rejected by netip and by nft alike.
			p := netip.PrefixFrom(addr.Unmap(), ones).Masked()
			if p.IsValid() {
				out = append(out, p)
			}
		}
	}
	return out, nil
}

// parseEndpoint turns a host:port string into an endpoint the policy can
// hold. A hostname is rejected rather than resolved: resolving here would
// permit whatever the name happened to point at when the rules were built,
// and firewall rules must name addresses, not intentions.
func parseEndpoint(s string) (netip.AddrPort, bool) {
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		return netip.AddrPort{}, false
	}
	return ap, true
}

// ParseEndpoints converts a set of host:port strings, silently dropping any
// that are not literal addresses. Callers pass the endpoints WireGuard is
// actually using, which are always literals by the time they get here.
func ParseEndpoints(addrs []string) []netip.AddrPort {
	out := make([]netip.AddrPort, 0, len(addrs))
	for _, a := range addrs {
		if ap, ok := parseEndpoint(a); ok {
			out = append(out, ap)
		}
	}
	return out
}
