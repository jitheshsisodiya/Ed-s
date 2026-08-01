//go:build linux

package killswitch

import (
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func policy() Policy {
	return Policy{
		Interface: "nexus0",
		Endpoints: []netip.AddrPort{
			netip.MustParseAddrPort("203.0.113.7:51820"),
			netip.MustParseAddrPort("[2001:db8::1]:51820"),
		},
		LocalNetworks: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.0/24"),
			netip.MustParsePrefix("fd00::/64"),
		},
		AllowLAN: true,
	}
}

// The rules that keep a locked machine usable are the ones most likely to
// be dropped by a careless edit, so each is asserted by name.
func TestRulesetKeepsRecoveryPossible(t *testing.T) {
	rules := buildChains(policy())

	required := map[string]string{
		`policy drop`:                                  "the default must be to block, not to allow",
		`oifname "lo" accept`:                          "loopback must stay up or local services break",
		`oifname "nexus0" accept`:                      "the tunnel is the protected path and must be permitted",
		`udp dport { 67, 68 } accept`:                  "a DHCP lease expiring under lock would strip the machine's address",
		`ip daddr 203.0.113.7 udp dport 51820 accept`:  "without its own transport the lock is permanent",
		`ip6 daddr 2001:db8::1 udp dport 51820 accept`: "IPv6 endpoints need the same escape hatch",
		`ct state established,related accept`:          "existing flows must survive a rule reload",
	}
	for rule, why := range required {
		if !strings.Contains(rules, rule) {
			t.Errorf("missing %q: %s\n\nruleset:\n%s", rule, why, rules)
		}
	}
}

func TestAllowLANControlsLocalReachability(t *testing.T) {
	with := buildChains(policy())
	if !strings.Contains(with, "ip daddr 192.168.1.0/24 accept") {
		t.Error("AllowLAN did not permit the local subnet")
	}
	if !strings.Contains(with, "ip daddr 224.0.0.0/4 accept") {
		t.Error("AllowLAN did not permit multicast, so nothing on the LAN is discoverable")
	}

	p := policy()
	p.AllowLAN = false
	without := buildChains(p)
	if strings.Contains(without, "192.168.1.0/24") {
		t.Error("the local subnet stayed reachable with AllowLAN off, which is the hostile-network case")
	}
	// Recovery must still be possible on a hostile network.
	if !strings.Contains(without, "ip daddr 203.0.113.7 udp dport 51820 accept") {
		t.Error("locking down the LAN also blocked the tunnel's transport")
	}
}

// Refusing rather than dropping is a deliberate choice: an application that
// errors at once is legible, one that hangs looks like a broken network.
func TestBlockedTrafficIsRefusedNotDropped(t *testing.T) {
	rules := buildChains(policy())
	if !strings.Contains(rules, "reject") {
		t.Fatalf("blocked traffic is dropped rather than refused:\n%s", rules)
	}
}

// The real thing, against a real nftables, inside a throwaway network
// namespace. A ruleset that reads correctly but does not load is worthless,
// and only nft itself can say which this is.
func TestRulesetLoadsIntoRealNftables(t *testing.T) {
	if os.Getenv("NEXUSVPN_NETNS_CHILD") == "" {
		reexecInNetns(t)
		return
	}

	s := New()

	engaged, err := s.Engaged()
	if err != nil {
		t.Fatalf("Engaged: %v", err)
	}
	if engaged {
		t.Fatal("a fresh namespace reported the switch already engaged")
	}

	if err := s.Engage(policy()); err != nil {
		t.Fatalf("Engage: %v", err)
	}

	engaged, err = s.Engaged()
	if err != nil {
		t.Fatalf("Engaged after engage: %v", err)
	}
	if !engaged {
		t.Fatal("the switch did not report itself engaged after loading")
	}

	// Read the rules back from the kernel: the ones that matter must have
	// survived nft's own parsing and normalisation.
	live := nftDump(t)
	for _, want := range []string{"policy drop", `oifname "nexus0"`, "203.0.113.7"} {
		if !strings.Contains(live, want) {
			t.Fatalf("%q is not in the loaded ruleset:\n%s", want, live)
		}
	}

	// Engaging twice must leave one table, not two stacked copies.
	if err := s.Engage(policy()); err != nil {
		t.Fatalf("Engage is not idempotent: %v", err)
	}
	if n := strings.Count(nftDump(t), "chain output"); n != 1 {
		t.Fatalf("after a second Engage there are %d output chains, want 1", n)
	}

	// Release must remove our table and leave any other table alone.
	mustRun(t, "nft", "add", "table", "inet", "somebody_elses_table")

	if err := s.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	tables := nftTables(t)
	if strings.Contains(tables, tableName) {
		t.Fatal("the kill switch table outlived Release")
	}
	if !strings.Contains(tables, "somebody_elses_table") {
		t.Fatal("Release deleted a table this package did not create")
	}

	if err := s.Release(); err != nil {
		t.Fatalf("Release is not idempotent: %v", err)
	}
}

func reexecInNetns(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare is not available; skipping the real-nftables test")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nftables is not installed; skipping the real-nftables test")
	}

	cmd := exec.Command("unshare", "--net", "--map-root-user",
		os.Args[0], "-test.run", "TestRulesetLoadsIntoRealNftables", "-test.v")
	cmd.Env = append(os.Environ(), "NEXUSVPN_NETNS_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("nftables test inside the namespace failed: %v\n%s", err, out)
	}
	t.Logf("namespace run:\n%s", out)
}

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", name, args, err, out)
	}
}

func nftDump(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("nft", "list", "table", "inet", tableName).CombinedOutput()
	if err != nil {
		t.Fatalf("nft list table: %v: %s", err, out)
	}
	return string(out)
}

func nftTables(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("nft", "list", "tables").CombinedOutput()
	if err != nil {
		t.Fatalf("nft list tables: %v: %s", err, out)
	}
	return string(out)
}

func TestParseEndpoint(t *testing.T) {
	if _, ok := parseEndpoint("203.0.113.7:51820"); !ok {
		t.Error("a valid IPv4 endpoint was rejected")
	}
	if _, ok := parseEndpoint("[2001:db8::1]:51820"); !ok {
		t.Error("a valid IPv6 endpoint was rejected")
	}
	// A hostname is not an address: resolving one here would silently
	// permit whatever it happened to point at when the rules were built.
	if _, ok := parseEndpoint("relay.example.com:51820"); ok {
		t.Error("a hostname was accepted as an endpoint")
	}
}
