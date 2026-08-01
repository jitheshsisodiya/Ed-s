package local

import (
	"net"
	"strings"
	"testing"
)

// This string is shown to somebody as "point your other machines here", so a
// wrong answer sends them somewhere that does not exist. The case that
// reached a user was Radmin VPN's adapter: 26.0.0.0/8 is real, routable,
// publicly allocated space Radmin squats on, and the old code returned it
// because it was simply the first interface that was up.
func TestIsPrivateIPv4RejectsOtherVPNAdapters(t *testing.T) {
	for _, addr := range []string{
		"26.164.7.109", // Radmin VPN
		"25.0.0.1",     // Hamachi
		"100.64.1.1",   // carrier-grade NAT
		"169.254.5.5",  // link-local
		"8.8.8.8",      // public
		"127.0.0.1",    // this machine only
	} {
		if isPrivateIPv4(net.ParseIP(addr)) {
			t.Errorf("%s was offered as a local network address", addr)
		}
	}
}

func TestIsPrivateIPv4AcceptsRealLANs(t *testing.T) {
	for _, addr := range []string{"192.168.1.20", "10.0.0.5", "172.16.4.4"} {
		if !isPrivateIPv4(net.ParseIP(addr)) {
			t.Errorf("%s was not recognised as a local network address", addr)
		}
	}
}

// An empty answer is correct when there is nothing good to say; a plausible
// but unreachable one is not.
func TestLANURLIsUsableOrEmpty(t *testing.T) {
	got := lanURL(8080)
	if got == "" {
		t.Skip("this machine has no private address, which is a valid answer")
	}
	if !strings.HasPrefix(got, "http://") || !strings.HasSuffix(got, ":8080") {
		t.Fatalf("lanURL returned %q, which is not an address and port", got)
	}
	host := strings.TrimSuffix(strings.TrimPrefix(got, "http://"), ":8080")
	if !isPrivateIPv4(net.ParseIP(host)) {
		t.Fatalf("lanURL returned %q, which is not on a local network", got)
	}
}
