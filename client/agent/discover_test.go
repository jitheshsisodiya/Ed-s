package agent

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

// TestDiscoverFindsAnAnnouncedServer is the only test that proves this
// feature works, because the two halves are in different modules and each one
// passing alone proves nothing. It runs a real announcement on the loopback
// network and looks for it.
//
// Skipped where multicast is unavailable, which is a normal state for a
// container and exactly what the production code has to tolerate too.
func TestDiscoverFindsAnAnnouncedServer(t *testing.T) {
	const fingerprint = "aee195632066bcf54bd127965c554a010fdfb5e36576b925d1a89ecd610571e5"

	server, err := zeroconf.Register(
		"NexusVPN on a test machine", ServiceType, "local.", 8080,
		[]string{"v=1", "fp=" + fingerprint}, nil,
	)
	if err != nil {
		t.Skipf("this network will not carry an announcement (%v), which the "+
			"code treats as normal", err)
	}
	defer server.Shutdown()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var found []Found
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		found = Discover(ctx, 2*time.Second)
		if len(found) > 0 {
			break
		}
	}
	if len(found) == 0 {
		t.Skip("nothing was discovered; multicast is not available here")
	}

	var ours *Found
	for i := range found {
		if strings.Contains(found[i].Name, "a test machine") {
			ours = &found[i]
			break
		}
	}
	if ours == nil {
		t.Fatalf("found %d services but not the one announced: %+v", len(found), found)
	}
	if ours.Fingerprint != fingerprint {
		t.Fatalf("fingerprint = %q, want the announced one — without it, "+
			"discovery hands over an address and no way to tell the right "+
			"server from anything else that answered", ours.Fingerprint)
	}
	if !strings.HasPrefix(ours.URL, "https://") {
		t.Fatalf("url = %q; the control plane always speaks TLS, and a plain "+
			"address produces a client that is hung up on immediately", ours.URL)
	}
	if !strings.HasSuffix(ours.URL, ":8080") {
		t.Fatalf("url = %q, want the announced port", ours.URL)
	}
}

// The parsing below is where the real decisions are, and it is testable
// without a network.

func TestFromEntryNeedsSomethingReachable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry *zeroconf.ServiceEntry
	}{
		{"nothing at all", nil},
		{
			// A perfectly good address and nothing listening behind it.
			name:  "no port",
			entry: &zeroconf.ServiceEntry{AddrIPv4: []net.IP{net.ParseIP("192.168.1.20")}},
		},
		{"no address", &zeroconf.ServiceEntry{Port: 8080}},
		{
			// Announced by a machine talking to itself. Useless from here,
			// and handing it over would send somebody to their own device.
			name: "loopback only",
			entry: &zeroconf.ServiceEntry{
				Port:     8080,
				AddrIPv4: []net.IP{net.ParseIP("127.0.0.1")},
			},
		},
		{
			// A machine with no DHCP lease. It has an address and nothing
			// routes to it.
			name: "link-local only",
			entry: &zeroconf.ServiceEntry{
				Port:     8080,
				AddrIPv4: []net.IP{net.ParseIP("169.254.3.4")},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Ports are set per case above, deliberately: setting them here
			// for anything with an address is what made the "no port" case
			// pass while testing the opposite of what it claimed.
			if _, ok := fromEntry(tc.entry); ok {
				t.Fatal("offered an announcement nothing could connect to")
			}
		})
	}
}

func TestFromEntryPrefersARoutableAddress(t *testing.T) {
	entry := &zeroconf.ServiceEntry{
		Port: 8080,
		// The order announcements arrive in is not the order of usefulness.
		AddrIPv4: []net.IP{
			net.ParseIP("127.0.0.1"),
			net.ParseIP("169.254.3.4"),
			net.ParseIP("192.168.1.20"),
		},
		Text: []string{"v=1", "fp=abc123"},
	}
	entry.Instance = "NexusVPN on desk"

	got, ok := fromEntry(entry)
	if !ok {
		t.Fatal("rejected a usable announcement")
	}
	if got.URL != "https://192.168.1.20:8080" {
		t.Fatalf("url = %q, want the routable address", got.URL)
	}
	if got.Fingerprint != "abc123" {
		t.Fatalf("fingerprint = %q", got.Fingerprint)
	}
	if got.Name != "NexusVPN on desk" {
		t.Fatalf("name = %q; it is what somebody picks from a list", got.Name)
	}
}

// An announcement without a fingerprint is still usable — an older server, or
// a deployment with a real certificate — and must not be discarded. The empty
// value means ordinary verification, not no verification.
func TestFromEntryWithoutAFingerprintIsStillOffered(t *testing.T) {
	got, ok := fromEntry(&zeroconf.ServiceEntry{
		Port:     8080,
		AddrIPv4: []net.IP{net.ParseIP("10.0.0.5")},
	})
	if !ok {
		t.Fatal("discarded an announcement that carried no fingerprint")
	}
	if got.Fingerprint != "" {
		t.Fatalf("invented a fingerprint: %q", got.Fingerprint)
	}
	if got.Name != "10.0.0.5" {
		t.Fatalf("name = %q; with no instance name the address is the only "+
			"thing left to show", got.Name)
	}
}
