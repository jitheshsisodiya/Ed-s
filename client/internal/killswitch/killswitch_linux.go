//go:build linux

package killswitch

import (
	"fmt"
	"os/exec"
	"strings"
)

// nftSwitch drives nftables through a single atomic ruleset load.
//
// nft rather than iptables because the whole ruleset goes in as one
// transaction: there is no window where half the rules are live and the
// machine is blocking traffic it should pass, or passing traffic it should
// block. It also gives us our own table, so `nft delete table` on release
// cannot disturb whatever the distribution's firewall put in the shared
// chains.
type nftSwitch struct{}

// New returns the switch for this platform.
func New() Switch { return nftSwitch{} }

func (nftSwitch) Engage(p Policy) error {
	if p.Interface == "" {
		return fmt.Errorf("killswitch: no tunnel interface to permit")
	}
	if _, err := exec.LookPath("nft"); err != nil {
		return fmt.Errorf("killswitch: nftables is required but `nft` was not found: %w", err)
	}

	// Replacing the table wholesale rather than adding to it makes engage
	// idempotent: calling it twice yields the same ruleset, never two
	// copies of it.
	ruleset := "table inet " + tableName + " {\n" + buildChains(p) + "}\n"

	cmd := exec.Command("nft", "-f", "-")
	cmd.Stdin = strings.NewReader("delete table inet " + tableName + "\n" + ruleset)
	if out, err := cmd.CombinedOutput(); err != nil {
		// The delete fails on the first run because there is nothing to
		// delete, so retry with just the ruleset before reporting failure.
		retry := exec.Command("nft", "-f", "-")
		retry.Stdin = strings.NewReader(ruleset)
		if out2, err2 := retry.CombinedOutput(); err2 != nil {
			return fmt.Errorf("killswitch: load ruleset: %w: %s (after: %s)",
				err2, strings.TrimSpace(string(out2)), strings.TrimSpace(string(out)))
		}
	}
	return nil
}

// buildChains renders the output-filtering policy.
//
// Only output is filtered. Input needs no rule: a machine that cannot send
// cannot have solicited anything, and blocking inbound as well would break
// the loopback services and DHCP replies the host needs to stay usable.
func buildChains(p Policy) string {
	var b strings.Builder

	b.WriteString("  chain output {\n")
	b.WriteString("    type filter hook output priority filter; policy drop;\n")

	// Established flows first: it is the cheapest match and it keeps the
	// tunnel's own long-lived sessions alive across a rule reload.
	b.WriteString("    ct state established,related accept\n")

	b.WriteString("    oifname \"lo\" accept\n")
	fmt.Fprintf(&b, "    oifname %q accept\n", p.Interface)

	// DHCP renewal. A lease that expires while the switch is engaged takes
	// the machine's address with it, and then nothing works — including the
	// recovery this switch is supposed to allow.
	b.WriteString("    udp dport { 67, 68 } accept\n")
	b.WriteString("    udp dport 546 accept\n")

	// The tunnel's own transport. Without this the lock is permanent.
	var v4, v6 []string
	for _, ep := range p.Endpoints {
		if !ep.IsValid() {
			continue
		}
		rule := fmt.Sprintf("ip daddr %s udp dport %d accept", ep.Addr().Unmap(), ep.Port())
		if ep.Addr().Unmap().Is4() {
			v4 = append(v4, rule)
		} else {
			v6 = append(v6, fmt.Sprintf("ip6 daddr %s udp dport %d accept",
				ep.Addr(), ep.Port()))
		}
	}
	for _, r := range append(v4, v6...) {
		fmt.Fprintf(&b, "    %s\n", r)
	}

	if p.AllowLAN {
		for _, n := range p.LocalNetworks {
			if !n.IsValid() {
				continue
			}
			if n.Addr().Is4() {
				fmt.Fprintf(&b, "    ip daddr %s accept\n", n)
			} else {
				fmt.Fprintf(&b, "    ip6 daddr %s accept\n", n)
			}
		}
		// Multicast and broadcast discovery, which is how a printer or a
		// Chromecast is found at all.
		b.WriteString("    ip daddr 224.0.0.0/4 accept\n")
		b.WriteString("    ip daddr 255.255.255.255 accept\n")
		b.WriteString("    ip6 daddr ff00::/8 accept\n")
	}

	// Everything else is refused rather than dropped, so an application
	// fails immediately with "network unreachable" instead of hanging until
	// it times out. A user staring at a stalled browser cannot tell a kill
	// switch from a broken network; one that errors at once is legible.
	b.WriteString("    meta l4proto { tcp, udp } reject\n")
	b.WriteString("  }\n")

	return b.String()
}

func (nftSwitch) Release() error {
	if _, err := exec.LookPath("nft"); err != nil {
		// Nothing could have been installed without it.
		return nil
	}
	out, err := exec.Command("nft", "delete", "table", "inet", tableName).CombinedOutput()
	if err != nil {
		// Already gone is the state release wanted.
		if strings.Contains(string(out), "No such file or directory") ||
			strings.Contains(string(out), "does not exist") {
			return nil
		}
		return fmt.Errorf("killswitch: delete table: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (nftSwitch) Engaged() (bool, error) {
	if _, err := exec.LookPath("nft"); err != nil {
		return false, nil
	}
	out, err := exec.Command("nft", "list", "tables", "inet").Output()
	if err != nil {
		return false, fmt.Errorf("killswitch: list tables: %w", err)
	}
	return strings.Contains(string(out), tableName), nil
}
