//go:build windows

package killswitch

import (
	"fmt"
	"os/exec"
	"strings"
)

// wfpSwitch drives Windows Firewall through the NetSecurity cmdlets.
//
// Windows evaluates allow rules before block rules, so the pattern used on
// every other platform — one blanket block plus exceptions — does not work
// here: the exceptions would be evaluated but the block would lose to every
// pre-existing allow rule on the machine, of which a normal Windows install
// has hundreds. The only correct way to deny by default is to change the
// profile's outbound default action, which is what this does, recording the
// previous value so release restores exactly what was there.
//
// Every rule carries a Group so release can delete precisely this package's
// rules and nothing else.
type wfpSwitch struct{}

// New returns the switch for this platform.
func New() Switch { return wfpSwitch{} }

// ruleGroup tags every rule this package creates.
const ruleGroup = "NexusVPN Kill Switch"

// policyStoreKey holds the outbound default action that was in force before
// the switch engaged, so release can put it back rather than assuming the
// machine started at "Allow".
const policyStoreKey = `HKLM:\SOFTWARE\NexusVPN`

func (wfpSwitch) Engage(p Policy) error {
	if p.Interface == "" {
		return fmt.Errorf("killswitch: no tunnel interface to permit")
	}

	var b strings.Builder

	// Start from a clean slate so engaging twice does not stack rules.
	fmt.Fprintf(&b, "Remove-NetFirewallRule -Group '%s' -ErrorAction SilentlyContinue\n", ruleGroup)

	// Record the outbound default before changing it. Saving it under our
	// own key means an interrupted release can still find the original.
	fmt.Fprintf(&b, `
if (-not (Test-Path '%[1]s')) { New-Item -Path '%[1]s' -Force | Out-Null }
if ($null -eq (Get-ItemProperty -Path '%[1]s' -Name 'PriorOutboundAction' -ErrorAction SilentlyContinue)) {
  $prior = (Get-NetFirewallProfile -Profile Domain,Private,Public |
    Select-Object -First 1).DefaultOutboundAction
  Set-ItemProperty -Path '%[1]s' -Name 'PriorOutboundAction' -Value ([string]$prior)
}
`, policyStoreKey)

	// The tunnel itself.
	fmt.Fprintf(&b, `New-NetFirewallRule -DisplayName 'NexusVPN tunnel' -Group '%s' -Direction Outbound -Action Allow -InterfaceAlias '%s' -ErrorAction Stop | Out-Null
`, ruleGroup, p.Interface)

	// Loopback, or every local service on the machine stops answering.
	fmt.Fprintf(&b, `New-NetFirewallRule -DisplayName 'NexusVPN loopback' -Group '%s' -Direction Outbound -Action Allow -RemoteAddress 127.0.0.1,::1 -ErrorAction Stop | Out-Null
`, ruleGroup)

	// DHCP renewal: a lease expiring under lock strips the machine's
	// address and nothing recovers after that.
	fmt.Fprintf(&b, `New-NetFirewallRule -DisplayName 'NexusVPN DHCP' -Group '%s' -Direction Outbound -Action Allow -Protocol UDP -RemotePort 67,547 -ErrorAction Stop | Out-Null
`, ruleGroup)

	// The tunnel's own transport, without which the lock is permanent.
	for i, ep := range p.Endpoints {
		if !ep.IsValid() {
			continue
		}
		fmt.Fprintf(&b, `New-NetFirewallRule -DisplayName 'NexusVPN endpoint %d' -Group '%s' -Direction Outbound -Action Allow -Protocol UDP -RemoteAddress '%s' -RemotePort %d -ErrorAction Stop | Out-Null
`, i, ruleGroup, ep.Addr().Unmap(), ep.Port())
	}

	if p.AllowLAN {
		var addrs []string
		for _, n := range p.LocalNetworks {
			if n.IsValid() {
				addrs = append(addrs, n.String())
			}
		}
		addrs = append(addrs, "224.0.0.0/4", "255.255.255.255", "ff00::/8")
		fmt.Fprintf(&b, `New-NetFirewallRule -DisplayName 'NexusVPN local network' -Group '%s' -Direction Outbound -Action Allow -RemoteAddress %s -ErrorAction Stop | Out-Null
`, ruleGroup, strings.Join(addrs, ","))
	}

	// Flip the default last, so the exceptions are already in place when
	// the machine starts denying. Doing this first would briefly cut the
	// tunnel's own transport and could drop the connection this is meant
	// to be protecting.
	b.WriteString(`Set-NetFirewallProfile -Profile Domain,Private,Public -DefaultOutboundAction Block -ErrorAction Stop
`)

	return powershellRun(b.String())
}

func (wfpSwitch) Release() error {
	// Restore the recorded default first: leaving the machine denying with
	// its exceptions already deleted is the one ordering that strands a
	// user offline.
	script := fmt.Sprintf(`
$prior = 'Allow'
$saved = Get-ItemProperty -Path '%[1]s' -Name 'PriorOutboundAction' -ErrorAction SilentlyContinue
if ($saved -and $saved.PriorOutboundAction) { $prior = $saved.PriorOutboundAction }
if ($prior -ne 'Block') {
  Set-NetFirewallProfile -Profile Domain,Private,Public -DefaultOutboundAction $prior -ErrorAction SilentlyContinue
}
Remove-ItemProperty -Path '%[1]s' -Name 'PriorOutboundAction' -ErrorAction SilentlyContinue
Remove-NetFirewallRule -Group '%[2]s' -ErrorAction SilentlyContinue
`, policyStoreKey, ruleGroup)

	return powershellRun(script)
}

func (wfpSwitch) Engaged() (bool, error) {
	out, err := powershell(fmt.Sprintf(
		`@(Get-NetFirewallRule -Group '%s' -ErrorAction SilentlyContinue).Count`, ruleGroup))
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(string(out)) != "0", nil
}

func powershell(script string) ([]byte, error) {
	return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
}

func powershellRun(script string) error {
	out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return fmt.Errorf("killswitch: powershell: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
