package stun

import (
	"context"
	"fmt"
	"net"
	"net/netip"
)

// NATType is a coarse classification of the NAT/firewall behavior a
// device is behind, matching the NATType enum in the coordination gRPC
// contract (proto/coordination/v1/coordination.proto).
type NATType int

const (
	NATUnknown NATType = iota
	// NATOpen means the device has a public, unfiltered address (no NAT):
	// the mapped address equals the local address and unsolicited traffic
	// is not blocked.
	NATOpen
	// NATFullCone means once a local port is mapped to a public
	// endpoint, any external host can reach it through that mapping.
	NATFullCone
	// NATRestrictedCone means only hosts the device has previously sent
	// a packet to (by IP) can reach it back on the mapped port.
	NATRestrictedCone
	// NATPortRestrictedCone is like NATRestrictedCone but also requires
	// the source port to match.
	NATPortRestrictedCone
	// NATSymmetric means the device gets a different mapped endpoint per
	// destination, making hole punching unreliable; relay fallback is
	// usually required.
	NATSymmetric
)

func (t NATType) String() string {
	switch t {
	case NATOpen:
		return "open"
	case NATFullCone:
		return "full_cone"
	case NATRestrictedCone:
		return "restricted_cone"
	case NATPortRestrictedCone:
		return "port_restricted_cone"
	case NATSymmetric:
		return "symmetric"
	default:
		return "unknown"
	}
}

// ClassifyResult is the outcome of NAT type classification.
type ClassifyResult struct {
	Type NATType
	// PublicAddr is this device's discovered public (server-reflexive)
	// endpoint, as seen by the primary STUN server.
	PublicAddr netip.AddrPort
	// LocalAddr is the local socket address used for the probes.
	LocalAddr netip.AddrPort
}

// Classify runs the classic (RFC 3489 §10.1-derived) STUN NAT type
// discovery algorithm using two independent STUN servers, over conn.
// conn should be a UDP socket not otherwise in use for the duration of
// the call (it will be given read/write deadlines and will consume any
// STUN responses that arrive on it).
//
// primaryServer and secondaryServer must resolve to different IP
// addresses; secondaryServer is only used to detect symmetric NAT by
// comparing the mapped endpoint seen by two different servers. If the
// mapped endpoint differs between them, the NAT is symmetric. Some tests
// additionally rely on primaryServer supporting the (optional, legacy)
// CHANGE-REQUEST attribute to distinguish full-cone / restricted-cone /
// port-restricted-cone; if the server doesn't support it, those requests
// simply time out and classification conservatively falls back to
// port-restricted-cone (the safest assumption for hole-punch timeout
// tuning).
func Classify(ctx context.Context, conn net.PacketConn, primaryServer, secondaryServer string, opts ...Option) (ClassifyResult, error) {
	client := NewClient(conn, opts...)

	localAddr, err := udpAddrToAddrPort(conn.LocalAddr())
	if err != nil {
		return ClassifyResult{}, err
	}

	// Test I: basic binding request against the primary server.
	res1, err := client.Binding(ctx, primaryServer)
	if err != nil {
		return ClassifyResult{}, fmt.Errorf("stun: primary server %s unreachable: %w", primaryServer, err)
	}

	result := ClassifyResult{PublicAddr: res1.Mapped, LocalAddr: localAddr}

	if addrPortIPEqual(res1.Mapped, localAddr) {
		// No NAT rewrote our address. Distinguish a truly open host from
		// one behind a symmetric firewall that just happens to preserve
		// the port, via Test II (ask the server to answer from a
		// different IP+port).
		if _, err := client.bindingWithChangeRequest(ctx, primaryServer, true, true); err == nil {
			result.Type = NATOpen
			return result, nil
		}
		result.Type = NATPortRestrictedCone
		return result, nil
	}

	// A NAT is rewriting our address. Test II: does the server get a
	// response through when replying from a different IP+port? If so,
	// any host can reach us through the mapping (full cone).
	if _, err := client.bindingWithChangeRequest(ctx, primaryServer, true, true); err == nil {
		result.Type = NATFullCone
		return result, nil
	}

	// Compare mappings seen by two independent servers to detect
	// symmetric NAT (a different external port per destination makes
	// hole punching against a peer-reported endpoint unreliable).
	if secondaryServer != "" {
		res2, err := client.Binding(ctx, secondaryServer)
		if err == nil && res2.Mapped != res1.Mapped {
			result.Type = NATSymmetric
			return result, nil
		}
	}

	// Test III: does the server get a response through when replying
	// from the same IP but a different port? If so we're behind a
	// restricted cone (filters by IP only); otherwise port-restricted
	// (filters by IP+port), the most common residential/enterprise NAT
	// behavior.
	if _, err := client.bindingWithChangeRequest(ctx, primaryServer, false, true); err == nil {
		result.Type = NATRestrictedCone
		return result, nil
	}

	result.Type = NATPortRestrictedCone
	return result, nil
}

func addrPortIPEqual(a, b netip.AddrPort) bool {
	return a.Addr().Unmap() == b.Addr().Unmap() && a.Port() == b.Port()
}
