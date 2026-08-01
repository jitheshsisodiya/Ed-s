//go:build linux

package egress

import (
	"net/netip"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func config() Config {
	return Config{
		Interface:    "nexus0",
		TunnelPrefix: netip.MustParsePrefix("100.84.0.0/16"),
	}
}

// An exit node that forwards whatever arrives on its tunnel is an open
// relay wearing someone else's address, so the scoping rules are asserted
// by name.
func TestForwardingIsScopedToTheNetwork(t *testing.T) {
	rules := buildRules(config())

	if !strings.Contains(rules, `iifname "nexus0" ip saddr 100.84.0.0/16 accept`) {
		t.Errorf("traffic from inside the network is not accepted:\n%s", rules)
	}
	if !strings.Contains(rules, `iifname "nexus0" drop`) {
		t.Errorf("traffic arriving on the tunnel from outside the network is not "+
			"dropped, which makes this host an open relay:\n%s", rules)
	}
	if !strings.Contains(rules, `ip saddr 100.84.0.0/16 oifname != "nexus0" masquerade`) {
		t.Errorf("forwarded traffic is not masqueraded, so replies would never "+
			"come back:\n%s", rules)
	}
}

// The drop has to come after the accept: nftables is first-match-wins
// within a chain, so reversing them drops everything.
func TestScopedAcceptPrecedesTheDrop(t *testing.T) {
	rules := buildRules(config())
	accept := strings.Index(rules, "ip saddr 100.84.0.0/16 accept")
	drop := strings.Index(rules, `iifname "nexus0" drop`)
	if accept < 0 || drop < 0 {
		t.Fatalf("expected both rules:\n%s", rules)
	}
	if accept > drop {
		t.Fatalf("the drop precedes the accept, so all forwarded traffic dies:\n%s", rules)
	}
}

// Masquerade rather than a fixed SNAT: an exit node on DHCP changes address
// without warning, and a pinned rule would rewrite to one it no longer has.
func TestNATIsMasqueradeNotFixed(t *testing.T) {
	rules := buildRules(config())
	if !strings.Contains(rules, "masquerade") {
		t.Fatalf("NAT is not masquerade:\n%s", rules)
	}
	if strings.Contains(rules, "snat to") {
		t.Fatalf("a fixed SNAT would break on any address change:\n%s", rules)
	}
}

func TestEnableRejectsAnUnscopedConfig(t *testing.T) {
	f := New()
	if err := f.Enable(Config{Interface: "nexus0"}); err == nil {
		t.Fatal("forwarding was enabled with no source prefix, which is an open relay")
	}
	if err := f.Enable(Config{TunnelPrefix: config().TunnelPrefix}); err == nil {
		t.Fatal("forwarding was enabled with no interface")
	}
}

// The real thing, in a throwaway namespace: the ruleset has to load, and
// the kernel's forwarding switch has to go back to what it was.
func TestForwardingAgainstRealKernel(t *testing.T) {
	if os.Getenv("NEXUSVPN_NETNS_CHILD") == "" {
		reexecInNetns(t)
		return
	}

	f := New()

	before := readForwarding(t)

	if err := f.Enable(config()); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	enabled, err := f.Enabled()
	if err != nil {
		t.Fatalf("Enabled: %v", err)
	}
	if !enabled {
		t.Fatal("the forwarder did not report itself enabled")
	}
	if got := readForwarding(t); got != "1" {
		t.Fatalf("kernel forwarding is %q after Enable, want 1", got)
	}

	live := nftDump(t)
	for _, want := range []string{"masquerade", `iifname "nexus0"`, "100.84.0.0/16"} {
		if !strings.Contains(live, want) {
			t.Fatalf("%q is missing from the loaded ruleset:\n%s", want, live)
		}
	}

	// Enabling twice must not stack chains.
	if err := f.Enable(config()); err != nil {
		t.Fatalf("Enable is not idempotent: %v", err)
	}
	if n := strings.Count(nftDump(t), "chain postrouting"); n != 1 {
		t.Fatalf("after a second Enable there are %d postrouting chains, want 1", n)
	}

	if err := f.Disable(); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if strings.Contains(nftTables(t), natTable) {
		t.Fatal("the egress table outlived Disable")
	}
	// A host that was not forwarding before must not be forwarding after.
	if got := readForwarding(t); got != before {
		t.Fatalf("kernel forwarding is %q after Disable, want %q as it was found", got, before)
	}

	if err := f.Disable(); err != nil {
		t.Fatalf("Disable is not idempotent: %v", err)
	}
}

func reexecInNetns(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("unshare"); err != nil {
		t.Skip("unshare is not available; skipping the real-kernel forwarding test")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		t.Skip("nftables is not installed; skipping the real-kernel forwarding test")
	}

	cmd := exec.Command("unshare", "--net", "--map-root-user",
		os.Args[0], "-test.run", "TestForwardingAgainstRealKernel", "-test.v")
	cmd.Env = append(os.Environ(), "NEXUSVPN_NETNS_CHILD=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("forwarding test inside the namespace failed: %v\n%s", err, out)
	}
	t.Logf("namespace run:\n%s", out)
}

func readForwarding(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(forwardV4)
	if err != nil {
		t.Skipf("cannot read %s in this namespace: %v", forwardV4, err)
	}
	return strings.TrimSpace(string(b))
}

func nftDump(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("nft", "list", "table", "inet", natTable).CombinedOutput()
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
