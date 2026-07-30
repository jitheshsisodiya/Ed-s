//go:build darwin

package wireguard

import (
	"fmt"
	"net"
	"os/exec"
)

// AssignAddress assigns the tunnel's virtual IP using `ifconfig` (utun
// interfaces on macOS are point-to-point, so both the local and a
// destination address are supplied) and brings the interface up.
// Requires root.
func (d *Device) AssignAddress(addr net.IP, network *net.IPNet) error {
	// utun is a point-to-point interface; ifconfig utunN inet <local>
	// <dest> netmask <mask> is the conventional invocation. We use the
	// network's own address as the point-to-point peer placeholder since
	// NexusVPN's mesh routes host routes per-peer via AllowedIPs/routes
	// rather than relying on the interface's own dest address.
	mask := net.IP(network.Mask).String()
	if out, err := exec.Command("ifconfig", d.name, "inet", addr.String(), addr.String(), "netmask", mask).CombinedOutput(); err != nil {
		return fmt.Errorf("wireguard: ifconfig assign address: %w: %s", err, out)
	}
	if out, err := exec.Command("ifconfig", d.name, "mtu", fmt.Sprintf("%d", d.mtu), "up").CombinedOutput(); err != nil {
		return fmt.Errorf("wireguard: ifconfig up: %w: %s", err, out)
	}
	// Ensure the mesh CIDR is routed through the tunnel.
	if out, err := exec.Command("route", "-n", "add", "-net", network.String(), "-interface", d.name).CombinedOutput(); err != nil {
		return fmt.Errorf("wireguard: route add: %w: %s", err, out)
	}
	return nil
}

// AddRoute adds a route for a peer/subnet CIDR out via this tunnel
// interface.
func (d *Device) AddRoute(network *net.IPNet) error {
	out, err := exec.Command("route", "-n", "add", "-net", network.String(), "-interface", d.name).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wireguard: route add: %w: %s", err, out)
	}
	return nil
}
