//go:build windows

package wireguard

import (
	"fmt"
	"net"
	"os/exec"
)

// AssignAddress assigns the tunnel's virtual IP and brings the Wintun
// adapter up. Uses PowerShell's NetTCPIP cmdlets (New-NetIPAddress /
// Set-NetIPInterface), which — unlike `netsh`/`route` — accept the
// adapter's alias (name) directly and don't require resolving an
// interface index first. Requires an elevated (Administrator) process,
// and the Wintun driver — bundled by the desktop installer / documented
// in README.md — to already be available so tun.CreateTUN could create
// the adapter in the first place.
func (d *Device) AssignAddress(addr net.IP, network *net.IPNet) error {
	prefixLen := maskSize(network)
	script := fmt.Sprintf(
		`New-NetIPAddress -InterfaceAlias '%s' -IPAddress '%s' -PrefixLength %d -ErrorAction Stop | Out-Null; `+
			`Set-NetIPInterface -InterfaceAlias '%s' -NlMtuBytes %d -ErrorAction SilentlyContinue`,
		d.name, addr.String(), prefixLen, d.name, d.mtu,
	)
	if out, err := runPowerShell(script); err != nil {
		return fmt.Errorf("wireguard: assign address via PowerShell: %w: %s", err, out)
	}
	return nil
}

// AddRoute adds a route for a peer/subnet CIDR out via this tunnel
// interface.
func (d *Device) AddRoute(network *net.IPNet) error {
	prefixLen := maskSize(network)
	script := fmt.Sprintf(
		`New-NetRoute -InterfaceAlias '%s' -DestinationPrefix '%s/%d' -ErrorAction Stop | Out-Null`,
		d.name, network.IP.String(), prefixLen,
	)
	if out, err := runPowerShell(script); err != nil {
		return fmt.Errorf("wireguard: add route via PowerShell: %w: %s", err, out)
	}
	return nil
}

func runPowerShell(script string) ([]byte, error) {
	return exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
}

func maskSize(n *net.IPNet) int {
	ones, _ := n.Mask.Size()
	return ones
}
