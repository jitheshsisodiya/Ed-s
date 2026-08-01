//go:build linux

package egress

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Supported reports whether this build can act as an exit node.
//
// Linux can: the kernel forwards and nftables masquerades, both from
// userspace with no driver to install. Windows would need Internet
// Connection Sharing or a WFP callout driver, and macOS needs a pf NAT
// anchor plus a sysctl Apple has moved twice — neither is something to ship
// untested, so those builds report false and the client refuses to
// advertise rather than promising what it cannot deliver.
func Supported() bool { return true }

// nftForwarder drives nftables plus the kernel's forwarding sysctls.
type nftForwarder struct {
	mu sync.Mutex
	// priorForwarding remembers whether the kernel was already forwarding
	// before Enable, so Disable does not switch off something the machine
	// was doing for its own reasons — a host that was routing for a
	// container network before we arrived must still be routing after we
	// leave.
	priorForwardingV4 string
	priorForwardingV6 string
	restoreForwarding bool
}

// New returns the forwarder for this platform.
func New() Forwarder { return &nftForwarder{} }

const (
	forwardV4 = "/proc/sys/net/ipv4/ip_forward"
	forwardV6 = "/proc/sys/net/ipv6/conf/all/forwarding"
)

func (f *nftForwarder) Enable(c Config) error {
	if c.Interface == "" {
		return fmt.Errorf("egress: no tunnel interface to forward from")
	}
	if !c.TunnelPrefix.IsValid() {
		// Without a source prefix the masquerade rule would match anything
		// arriving on the interface, and a host that forwards whatever it
		// is handed is an open relay.
		return fmt.Errorf("egress: a tunnel prefix is required so forwarding is scoped to the network")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return fmt.Errorf("egress: nftables is required but `nft` was not found: %w", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Only the first Enable records the prior state. A second one would
	// read back the value this package just wrote and remember "forwarding
	// was already on", so Disable would leave the machine forwarding for
	// good — and re-enabling is a normal thing to do when the tunnel
	// reconnects.
	if !f.restoreForwarding {
		priorV4, _ := os.ReadFile(forwardV4)
		priorV6, _ := os.ReadFile(forwardV6)
		f.priorForwardingV4 = strings.TrimSpace(string(priorV4))
		f.priorForwardingV6 = strings.TrimSpace(string(priorV6))
		f.restoreForwarding = true
	}

	if err := os.WriteFile(forwardV4, []byte("1\n"), 0o644); err != nil {
		return fmt.Errorf("egress: enable IPv4 forwarding: %w", err)
	}
	// IPv6 forwarding is best-effort: a host with IPv6 disabled has no such
	// file, and v4 forwarding alone is a working exit node.
	_ = os.WriteFile(forwardV6, []byte("1\n"), 0o644)

	ruleset := "table inet " + natTable + " {\n" + buildRules(c) + "}\n"
	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader("delete table inet " + natTable + "\n" + ruleset)
	if out, err := cmd.CombinedOutput(); err != nil {
		retry := exec.Command("nft", "-f", "-")
		retry.Stdin = strings.NewReader(ruleset)
		if out2, err2 := retry.CombinedOutput(); err2 != nil {
			// Put forwarding back before reporting failure: leaving the
			// kernel forwarding with no NAT rules is the worst of both.
			f.restoreForwardingLocked()
			return fmt.Errorf("egress: load ruleset: %w: %s (after: %s)",
				err2, strings.TrimSpace(string(out2)), strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// buildRules renders the forward filter and the masquerade.
func buildRules(c Config) string {
	var b strings.Builder
	family := "ip"
	if c.TunnelPrefix.Addr().Is6() {
		family = "ip6"
	}

	// Forwarding is scoped to traffic that came from inside this network.
	// A blanket "accept anything on this interface" would forward whatever
	// a peer chose to spoof, and this host's address would be the one
	// answering for it.
	b.WriteString("  chain forward {\n")
	b.WriteString("    type filter hook forward priority filter; policy accept;\n")
	b.WriteString("    ct state established,related accept\n")
	fmt.Fprintf(&b, "    iifname %q %s saddr %s accept\n", c.Interface, family, c.TunnelPrefix)
	fmt.Fprintf(&b, "    iifname %q drop\n", c.Interface)
	b.WriteString("  }\n")

	// Masquerade rather than SNAT to a fixed address: an exit node on DHCP
	// or a mobile connection changes address without warning, and a pinned
	// SNAT rule would silently start rewriting to an address the host no
	// longer holds.
	b.WriteString("  chain postrouting {\n")
	b.WriteString("    type nat hook postrouting priority srcnat; policy accept;\n")
	fmt.Fprintf(&b, "    %s saddr %s oifname != %q masquerade\n", family, c.TunnelPrefix, c.Interface)
	b.WriteString("  }\n")

	return b.String()
}

func (f *nftForwarder) Disable() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var firstErr error
	if _, err := exec.LookPath("nft"); err == nil {
		out, err := exec.Command("nft", "delete", "table", "inet", natTable).CombinedOutput()
		if err != nil &&
			!strings.Contains(string(out), "No such file or directory") &&
			!strings.Contains(string(out), "does not exist") {
			firstErr = fmt.Errorf("egress: delete table: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	f.restoreForwardingLocked()
	return firstErr
}

// restoreForwardingLocked puts the kernel's forwarding switches back the way
// they were found. The caller holds f.mu.
func (f *nftForwarder) restoreForwardingLocked() {
	if !f.restoreForwarding {
		return
	}
	f.restoreForwarding = false
	if f.priorForwardingV4 != "" {
		_ = os.WriteFile(forwardV4, []byte(f.priorForwardingV4+"\n"), 0o644)
	}
	if f.priorForwardingV6 != "" {
		_ = os.WriteFile(forwardV6, []byte(f.priorForwardingV6+"\n"), 0o644)
	}
}

func (f *nftForwarder) Enabled() (bool, error) {
	if _, err := exec.LookPath("nft"); err != nil {
		return false, nil
	}
	out, err := exec.Command("nft", "list", "tables", "inet").Output()
	if err != nil {
		return false, fmt.Errorf("egress: list tables: %w", err)
	}
	return strings.Contains(string(out), natTable), nil
}
