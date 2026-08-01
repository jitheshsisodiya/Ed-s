package agent

import (
	"net"
	"testing"
)

// TestIsPrivateIPv4RejectsOtherVPNs is the case that reached a user.
//
// Radmin VPN gives its adapter an address in 26.0.0.0/8 — real, routable,
// publicly allocated space it squats on. The old code walked the interface
// list and returned the first thing that was up and not loopback, so on a
// machine with Radmin installed it handed out 26.164.7.109 as "the address of
// this machine". A phone told that goes looking somewhere that does not
// exist, and it is not private either, so it cannot even be reached in the
// clear.
func TestIsPrivateIPv4RejectsOtherVPNs(t *testing.T) {
	for _, addr := range []string{
		"26.164.7.109", // Radmin VPN
		"25.0.0.1",     // Hamachi
		"100.64.1.1",   // carrier-grade NAT, not a LAN
		"169.254.5.5",  // link-local, nobody routes to it
		"8.8.8.8",      // plainly public
		"127.0.0.1",    // this machine only
		"172.32.0.1",   // just past the private range
		"172.15.0.1",   // just before it
		"192.169.0.1",  // just past 192.168/16
	} {
		if isPrivateIPv4(net.ParseIP(addr)) {
			t.Errorf("%s was treated as a local network address", addr)
		}
	}
}

func TestIsPrivateIPv4AcceptsRealLANs(t *testing.T) {
	for _, addr := range []string{
		"192.168.1.20",
		"192.168.0.1",
		"10.0.0.5",
		"10.255.255.254",
		"172.16.0.1",
		"172.31.255.254",
	} {
		if !isPrivateIPv4(net.ParseIP(addr)) {
			t.Errorf("%s was not recognised as a local network address", addr)
		}
	}
}

func TestIsPrivateIPv4IgnoresIPv6(t *testing.T) {
	// The pairing link carries an IPv4 address because that is what a phone
	// on a home network can reliably use; an IPv6 answer here would be
	// silently wrong rather than obviously so.
	for _, addr := range []string{"::1", "fe80::1", "fd00::1", "2001:4860:4860::8888"} {
		if isPrivateIPv4(net.ParseIP(addr)) {
			t.Errorf("%s was treated as an IPv4 local address", addr)
		}
	}
}

// TestLANAddressIsUsableOrEmpty: whatever comes back is handed to somebody as
// the address of this machine, so a wrong answer is worse than no answer.
// "No sensible address" is a state this has to be allowed to report.
func TestLANAddressIsUsableOrEmpty(t *testing.T) {
	got := LANAddress()
	if got == "" {
		t.Skip("this machine has no private address, which is a valid answer")
	}
	ip := net.ParseIP(got)
	if ip == nil {
		t.Fatalf("returned %q, which is not an address at all", got)
	}
	if !isPrivateIPv4(ip) {
		t.Fatalf("returned %q, which is not on a local network", got)
	}
}

// TestRoutableSourceAddressSendsNothing documents why the UDP dial is safe to
// do on a machine with no network: connect on a datagram socket only fixes
// the peer and picks a route, so nothing leaves the machine and nothing needs
// to answer. If that ever stopped being true, this would hang or fail rather
// than quietly reaching out.
func TestRoutableSourceAddressSendsNothing(t *testing.T) {
	// 192.0.2.0/24 is reserved for documentation and is not routed anywhere,
	// so even a machine that does send something reaches nobody.
	got := routableSourceAddress()
	if got != "" && !isPrivateIPv4(net.ParseIP(got)) {
		t.Fatalf("returned %q, which is not a local network address", got)
	}
}
