//go:build darwin

package killswitch

import (
	"fmt"
	"os/exec"
	"strings"
)

// pfSwitch drives pf through one of Apple's pre-declared anchors.
//
// macOS ships an /etc/pf.conf containing `anchor "com.apple/*"`, so rules
// loaded under that path are evaluated without this package ever editing
// the system's pf.conf. Writing to pf.conf directly is what makes VPN
// clients on this platform notorious: an interrupted uninstall leaves an
// edited system file behind, and the next OS update silently reverts it.
// Loading into a sub-anchor means release is `pfctl -a <anchor> -F all` and
// the machine is exactly as it was.
type pfSwitch struct{}

// New returns the switch for this platform.
func New() Switch { return pfSwitch{} }

// anchorPath sits under the anchor Apple already references. The numeric
// prefix orders it among any siblings.
const anchorPath = "com.apple/250.nexusvpn"

func (pfSwitch) Engage(p Policy) error {
	if p.Interface == "" {
		return fmt.Errorf("killswitch: no tunnel interface to permit")
	}

	// pf must be enabled for any anchor to be evaluated. -E takes a
	// reference so a later -X releases only ours, leaving pf enabled if
	// something else also asked for it.
	if out, err := exec.Command("pfctl", "-E").CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "already enabled") {
			return fmt.Errorf("killswitch: enable pf: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	cmd := exec.Command("pfctl", "-a", anchorPath, "-f", "-")
	cmd.Stdin = strings.NewReader(buildRules(p))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("killswitch: load anchor: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// buildRules renders the pf policy.
//
// pf is last-match-wins, the opposite of nftables, so the blanket block
// comes first and every exception after it. Reversing that order would
// produce a ruleset that loads cleanly and blocks everything.
func buildRules(p Policy) string {
	var b strings.Builder

	b.WriteString("set block-policy return\n")
	b.WriteString("block out all\n")
	b.WriteString("pass out on lo0 all\n")
	fmt.Fprintf(&b, "pass out on %s all\n", p.Interface)

	// DHCP renewal, or a lease expiring under lock takes the machine's
	// address with it.
	b.WriteString("pass out proto udp from any port 68 to any port 67\n")
	b.WriteString("pass out proto udp from any port 546 to any port 547\n")

	// The tunnel's own transport, without which the lock is permanent.
	for _, ep := range p.Endpoints {
		if !ep.IsValid() {
			continue
		}
		fmt.Fprintf(&b, "pass out proto udp to %s port %d\n", ep.Addr().Unmap(), ep.Port())
	}

	if p.AllowLAN {
		for _, n := range p.LocalNetworks {
			if n.IsValid() {
				fmt.Fprintf(&b, "pass out to %s\n", n)
			}
		}
		b.WriteString("pass out to 224.0.0.0/4\n")
		b.WriteString("pass out to 255.255.255.255\n")
		b.WriteString("pass out to ff00::/8\n")
	}

	return b.String()
}

func (pfSwitch) Release() error {
	// Flushing the anchor leaves it declared but empty, which evaluates to
	// nothing — the same as never having been there.
	if out, err := exec.Command("pfctl", "-a", anchorPath, "-F", "all").CombinedOutput(); err != nil {
		if strings.Contains(string(out), "No such file or directory") {
			return nil
		}
		return fmt.Errorf("killswitch: flush anchor: %w: %s", err, strings.TrimSpace(string(out)))
	}
	// Drop our reference. pf stays enabled if anything else holds one.
	_ = exec.Command("pfctl", "-X").Run()
	return nil
}

func (pfSwitch) Engaged() (bool, error) {
	out, err := exec.Command("pfctl", "-a", anchorPath, "-s", "rules").CombinedOutput()
	if err != nil {
		// An anchor that has never been loaded is not an error condition,
		// it is the un-engaged state.
		return false, nil
	}
	return strings.Contains(string(out), "block"), nil
}
