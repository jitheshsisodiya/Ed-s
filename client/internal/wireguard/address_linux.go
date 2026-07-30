//go:build linux

package wireguard

import (
	"fmt"
	"net"
	"os/exec"
)

// AssignAddress assigns the tunnel's virtual IP and brings the interface
// up using the `ip` command from iproute2 (present on essentially every
// modern Linux distribution). Requires root (CAP_NET_ADMIN).
func (d *Device) AssignAddress(addr net.IP, network *net.IPNet) error {
	cidr := fmt.Sprintf("%s/%d", addr.String(), maskSize(network))
	if out, err := exec.Command("ip", "address", "add", "dev", d.name, cidr).CombinedOutput(); err != nil {
		return fmt.Errorf("wireguard: ip address add: %w: %s", err, out)
	}
	if out, err := exec.Command("ip", "link", "set", "up", "dev", d.name, "mtu", itoa(d.mtu)).CombinedOutput(); err != nil {
		return fmt.Errorf("wireguard: ip link set up: %w: %s", err, out)
	}
	return nil
}

// AddRoute adds a route for a peer/subnet CIDR out via this tunnel
// interface. For a hub-and-spoke mesh this is typically implied by the
// AllowedIPs already covering the /32, but is exposed for future subnet
// routing.
func (d *Device) AddRoute(network *net.IPNet) error {
	out, err := exec.Command("ip", "route", "replace", network.String(), "dev", d.name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wireguard: ip route replace: %w: %s", err, out)
	}
	return nil
}

func maskSize(n *net.IPNet) int {
	ones, _ := n.Mask.Size()
	return ones
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
