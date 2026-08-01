//go:build linux

package netroute

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestParseLinuxDefault(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		wantGW  string
		wantDev string
		wantErr error
	}{
		{
			name:    "dhcp default",
			out:     "default via 192.0.2.1 dev eth0 proto dhcp src 192.0.2.15 metric 100\n",
			wantGW:  "192.0.2.1",
			wantDev: "eth0",
		},
		{
			name:    "minimal form",
			out:     "default via 10.0.0.1 dev wlan0\n",
			wantGW:  "10.0.0.1",
			wantDev: "wlan0",
		},
		{
			// Multi-homed: the kernel lists the lowest metric first, so
			// the first line is the route in use.
			name:    "two defaults takes the first",
			out:     "default via 10.0.0.1 dev wlan0 metric 50\ndefault via 192.0.2.1 dev eth0 metric 600\n",
			wantGW:  "10.0.0.1",
			wantDev: "wlan0",
		},
		{
			// An on-link default has no gateway to pin the tunnel's
			// transport through, so it is not usable for exit-node mode.
			name:    "on-link default is not usable",
			out:     "default dev ppp0 scope link\n",
			wantErr: ErrNoDefaultRoute,
		},
		{name: "no default at all", out: "", wantErr: ErrNoDefaultRoute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseLinuxDefault(tc.out)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Gateway.String() != tc.wantGW || got.Interface != tc.wantDev {
				t.Fatalf("got %s, want %s via %s", got, tc.wantGW, tc.wantDev)
			}
		})
	}
}

func TestHostPrefix(t *testing.T) {
	if got := hostPrefix(net.ParseIP("203.0.113.7")); got != "203.0.113.7/32" {
		t.Fatalf("IPv4 host prefix = %q", got)
	}
	if got := hostPrefix(net.ParseIP("2001:db8::1")); got != "2001:db8::1/128" {
		t.Fatalf("IPv6 host prefix = %q", got)
	}
}

// The split default must actually cover the whole space, or exit-node mode
// silently leaks half the internet outside the tunnel.
func TestSplitDefaultCoversEverything(t *testing.T) {
	var nets []*net.IPNet
	for _, prefix := range splitDefault {
		_, n, err := net.ParseCIDR(prefix)
		if err != nil {
			t.Fatalf("parse %q: %v", prefix, err)
		}
		nets = append(nets, n)
	}

	for _, addr := range []string{
		"0.0.0.0", "1.1.1.1", "127.0.0.1", "127.255.255.255",
		"128.0.0.0", "192.0.2.1", "203.0.113.7", "255.255.255.255",
	} {
		ip := net.ParseIP(addr)
		covered := false
		for _, n := range nets {
			if n.Contains(ip) {
				covered = true
				break
			}
		}
		if !covered {
			t.Fatalf("%s is not covered by the split default", addr)
		}
	}

	// And each half must be longer-prefix than a /0, so it wins against
	// the original default without that route ever being deleted.
	for _, n := range nets {
		ones, _ := n.Mask.Size()
		if ones != 1 {
			t.Fatalf("%s has prefix length %d, want 1", n, ones)
		}
	}
}

// The real thing, against a real kernel routing table, inside a throwaway
// network namespace so the sandbox's own networking is never touched.
//
// This is the test that would have caught a wrong `ip` invocation, which
// unit tests over parsers cannot.
func TestRoutingAgainstRealKernel(t *testing.T) {
	if os.Getenv("NEXUSVPN_NETNS_CHILD") == "" {
		reexecInNetns(t)
		return
	}

	r := New()

	// A namespace starts with only loopback, so the fixture builds the
	// shape a real host has: an interface, an address, a default route.
	// veth is used rather than dummy because veth is compiled into every
	// kernel that supports namespaces at all, while dummy is a module that
	// may not be loaded.
	mustRun(t, "ip", "link", "add", "veth0", "type", "veth", "peer", "name", "veth0p")
	mustRun(t, "ip", "address", "add", "192.0.2.15/24", "dev", "veth0")
	mustRun(t, "ip", "link", "set", "up", "dev", "veth0")
	mustRun(t, "ip", "route", "add", "default", "via", "192.0.2.1", "dev", "veth0")

	def, err := r.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if def.Gateway.String() != "192.0.2.1" || def.Interface != "veth0" {
		t.Fatalf("Snapshot = %s, want 192.0.2.1 via veth0", def)
	}

	// Pin an exit node's endpoint through the physical gateway.
	endpoint := net.ParseIP("203.0.113.7")
	if err := r.PinEndpoint(endpoint, def); err != nil {
		t.Fatalf("PinEndpoint: %v", err)
	}
	if !routeExists(t, "203.0.113.7") {
		t.Fatal("the endpoint pin was not installed")
	}

	// Pinning twice must succeed: reconnects run this path again.
	if err := r.PinEndpoint(endpoint, def); err != nil {
		t.Fatalf("PinEndpoint is not idempotent: %v", err)
	}

	// Capture the default onto a second interface standing in for the
	// tunnel.
	mustRun(t, "ip", "link", "add", "nexus0", "type", "veth", "peer", "name", "nexus0p")
	mustRun(t, "ip", "address", "add", "100.84.0.2/32", "dev", "nexus0")
	mustRun(t, "ip", "link", "set", "up", "dev", "nexus0")

	if err := r.CaptureDefault("nexus0"); err != nil {
		t.Fatalf("CaptureDefault: %v", err)
	}

	// The point of the whole package: ordinary traffic now resolves to the
	// tunnel, while the exit node's own endpoint still resolves to the
	// physical interface. If that second half regressed, the tunnel would
	// be carrying its own transport and nothing would connect.
	if dev := routeDevFor(t, "1.1.1.1"); dev != "nexus0" {
		t.Fatalf("traffic to 1.1.1.1 goes out %q, want nexus0", dev)
	}
	if dev := routeDevFor(t, "203.0.113.7"); dev != "veth0" {
		t.Fatalf("the exit node's endpoint goes out %q, want veth0 — "+
			"the tunnel is carrying its own transport", dev)
	}

	// The original default must still be there underneath, so a crash
	// leaves a routable machine.
	if !defaultRouteIntact(t) {
		t.Fatal("the original default route was removed; a crash here would leave the host unroutable")
	}

	// Teardown restores the original path.
	if err := r.ReleaseDefault("nexus0"); err != nil {
		t.Fatalf("ReleaseDefault: %v", err)
	}
	if dev := routeDevFor(t, "1.1.1.1"); dev != "veth0" {
		t.Fatalf("after release, traffic goes out %q, want veth0", dev)
	}
	if err := r.ReleaseDefault("nexus0"); err != nil {
		t.Fatalf("ReleaseDefault is not idempotent: %v", err)
	}

	if err := r.UnpinEndpoint(endpoint); err != nil {
		t.Fatalf("UnpinEndpoint: %v", err)
	}
	if routeExists(t, "203.0.113.7") {
		t.Fatal("the endpoint pin outlived teardown")
	}
	if err := r.UnpinEndpoint(endpoint); err != nil {
		t.Fatalf("UnpinEndpoint is not idempotent: %v", err)
	}
}

// reexecInNetns runs this test binary again inside a fresh network
// namespace. Manipulating routes needs CAP_NET_ADMIN, which unshare grants
// within the new namespace without the test needing real root.
func reexecInNetns(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare is not available; skipping the real-kernel routing test")
	}
	if _, err := exec.LookPath("ip"); err != nil {
		t.Skip("iproute2 is not installed; skipping the real-kernel routing test")
	}

	cmd := exec.Command("unshare", "--net", "--map-root-user",
		os.Args[0], "-test.run", "TestRoutingAgainstRealKernel", "-test.v")
	cmd.Env = append(os.Environ(), "NEXUSVPN_NETNS_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("routing test inside the namespace failed: %v\n%s", err, out)
	}
	t.Logf("namespace run:\n%s", out)
}

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
}

// routeDevFor asks the kernel which interface it would actually use for an
// address — the only answer that matters.
func routeDevFor(t *testing.T, addr string) string {
	t.Helper()
	out, err := exec.Command("ip", "-o", "route", "get", addr).Output()
	if err != nil {
		t.Fatalf("ip route get %s: %v", addr, err)
	}
	fields := strings.Fields(string(out))
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "dev" {
			return fields[i+1]
		}
	}
	t.Fatalf("no device in route for %s: %s", addr, out)
	return ""
}

func routeExists(t *testing.T, prefix string) bool {
	t.Helper()
	out, err := exec.Command("ip", "route", "show", prefix).Output()
	if err != nil {
		return false
	}
	return len(strings.Fields(string(out))) > 0
}

func defaultRouteIntact(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return false
	}
	d, err := parseLinuxDefault(string(out))
	return err == nil && d.Interface == "veth0"
}
