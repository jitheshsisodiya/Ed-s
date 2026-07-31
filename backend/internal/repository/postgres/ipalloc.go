package postgres

import (
	"encoding/binary"
	"fmt"
	"net"
)

// maxScannedHosts bounds how many candidate addresses NextFreeVirtualIP
// will consider before giving up, protecting against pathologically large
// CIDR blocks (e.g. accidentally supplying a /8).
const maxScannedHosts = 1 << 20 // ~1M addresses

// nextFreeHostIP returns the lowest host address inside cidr (excluding the
// network and broadcast addresses for IPv4) that is not present in used.
// IPv6 CIDRs have no broadcast address concept; only the all-zero host
// address is excluded there.
func nextFreeHostIP(cidr string, used map[string]struct{}) (string, error) {
	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", fmt.Errorf("invalid cidr %q: %w", cidr, err)
	}

	if v4 := ipnet.IP.To4(); v4 != nil {
		return nextFreeIPv4(ipnet, used)
	}
	_ = ip
	return "", fmt.Errorf("unsupported address family for cidr %q", cidr)
}

func nextFreeIPv4(ipnet *net.IPNet, used map[string]struct{}) (string, error) {
	base := ipnet.IP.To4()
	ones, bits := ipnet.Mask.Size()
	hostBits := bits - ones
	if hostBits <= 0 {
		return "", fmt.Errorf("cidr %s has no usable host addresses", ipnet.String())
	}

	total := uint32(1) << uint(hostBits)
	if total > maxScannedHosts {
		total = maxScannedHosts
	}

	baseInt := binary.BigEndian.Uint32(base)
	// Skip the network address (offset 0). Skip the broadcast address
	// (offset total-1) only for genuine subnets (hostBits > 0, i.e. not a
	// /32 which has no broadcast).
	start := uint32(1)
	end := total - 1
	if hostBits == 0 {
		start = 0
		end = 0
	}

	for offset := start; offset < end; offset++ {
		candidateInt := baseInt + offset
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], candidateInt)
		candidate := net.IP(b[:]).String()
		if _, taken := used[candidate]; !taken {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free addresses in %s", ipnet.String())
}
