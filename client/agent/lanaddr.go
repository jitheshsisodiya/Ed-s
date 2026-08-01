package agent

import (
	"net"
	"strings"
	"time"
)

// LANAddress returns the address other machines on this network should use to
// reach this one, or empty if there is no sensible answer.
//
// "The first interface that is up and not loopback" is the obvious
// implementation and it is wrong on any machine that has ever installed
// another VPN. Radmin VPN, Hamachi, VirtualBox, WSL and NexusVPN itself all
// present interfaces that are up, not loopback, and carry addresses no phone
// on the Wi-Fi can reach. Handing one of those to somebody as "the address of
// this machine" sends them somewhere that does not exist.
//
// So two things are true of anything returned here: the operating system
// would use it to reach the outside world, or failing that it is at least in
// a range reserved for private networks — and never an address that merely
// happens to be on some adapter.
func LANAddress() string {
	if ip := routableSourceAddress(); ip != "" {
		return ip
	}
	return firstPrivateAddress()
}

// routableSourceAddress asks the operating system which address it would send
// from, which is the question actually being asked.
//
// The UDP "connection" sends nothing — connect on a datagram socket only
// fixes the peer and picks a route — so this reaches no network and needs
// nothing to be reachable. It is simply the shortest way to read the routing
// table's answer without parsing it per platform.
func routableSourceAddress() string {
	conn, err := net.DialTimeout("udp4", "192.0.2.1:9", 2*time.Second)
	if err != nil {
		return ""
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil {
		return ""
	}
	// Checked rather than trusted: when a VPN is carrying the default route,
	// the answer is that tunnel's address, which is exactly the kind of
	// address this function exists to avoid.
	if !isPrivateIPv4(addr.IP) {
		return ""
	}
	return addr.IP.String()
}

// firstPrivateAddress falls back to enumeration, restricted to the ranges
// reserved for private networks. A machine with no route to the internet can
// still be the one hosting a network in a room with no internet.
func firstPrivateAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// NexusVPN's own tunnel carries a private address too, so it would
		// otherwise be a candidate for "where other machines can reach this
		// one" — which is circular: a machine that is not on the network yet
		// cannot use an address that only exists on it.
		if strings.HasPrefix(strings.ToLower(iface.Name), "nexus") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			n, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if isPrivateIPv4(n.IP) {
				return n.IP.To4().String()
			}
		}
	}
	return ""
}

// isPrivateIPv4 reports whether an address belongs to a range reserved for
// private networks.
//
// Deliberately narrower than "not public". Radmin VPN hands out addresses in
// 26.0.0.0/8, which is real, routable, publicly allocated space that it
// squats on; those are not private, are not this machine's LAN address, and
// must not be offered as one. Link-local and carrier-grade NAT are excluded
// for the same reason: neither is an address a phone on the same Wi-Fi can
// use to find this machine.
func isPrivateIPv4(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil || v4.IsLoopback() || v4.IsLinkLocalUnicast() {
		return false
	}
	switch {
	case v4[0] == 10:
		return true // 10.0.0.0/8
	case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
		return true // 172.16.0.0/12
	case v4[0] == 192 && v4[1] == 168:
		return true // 192.168.0.0/16
	}
	return false
}
