//go:build linux

package netroute

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// linuxRouter drives iproute2, which is present on every distribution this
// client claims to support.
type linuxRouter struct{}

// New returns the router for this platform.
func New() Router { return linuxRouter{} }

func (linuxRouter) Snapshot() (Default, error) {
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return Default{}, fmt.Errorf("netroute: read default route: %w", err)
	}
	return parseLinuxDefault(string(out))
}

// parseLinuxDefault reads the first default route out of `ip route show
// default` output, which looks like:
//
//	default via 192.0.2.1 dev eth0 proto dhcp src 192.0.2.15 metric 100
//
// Only the gateway and the device matter. Multiple defaults (multi-homed
// hosts) are resolved by taking the first, which is the one the kernel
// listed first and therefore the one with the lowest metric.
func parseLinuxDefault(out string) (Default, error) {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "default" {
			continue
		}
		var d Default
		for i := 0; i+1 < len(fields); i++ {
			switch fields[i] {
			case "via":
				d.Gateway = net.ParseIP(fields[i+1])
			case "dev":
				d.Interface = fields[i+1]
			}
		}
		if d.Gateway != nil && d.Interface != "" {
			return d, nil
		}
	}
	return Default{}, ErrNoDefaultRoute
}

func (linuxRouter) PinEndpoint(ip net.IP, via Default) error {
	if via.Gateway == nil || via.Interface == "" {
		return ErrNoDefaultRoute
	}
	// `replace` rather than `add`: this runs on reconnect paths where the
	// pin may already exist, and failing there would block a recovery.
	return run("ip", "route", "replace", hostPrefix(ip),
		"via", via.Gateway.String(), "dev", via.Interface)
}

func (linuxRouter) UnpinEndpoint(ip net.IP) error {
	// A pin that is already gone is the state we wanted.
	if err := run("ip", "route", "del", hostPrefix(ip)); err != nil && !isNoSuchProcess(err) {
		return err
	}
	return nil
}

func (linuxRouter) CaptureDefault(iface string) error {
	for _, prefix := range append(append([]string{}, splitDefault...), splitDefaultV6...) {
		family := "-4"
		if strings.Contains(prefix, ":") {
			family = "-6"
		}
		if err := run("ip", family, "route", "replace", prefix, "dev", iface); err != nil {
			// A host with IPv6 disabled cannot take the v6 half, and that
			// is not a failure: v4 traffic is still captured.
			if family == "-6" {
				continue
			}
			return err
		}
	}
	return nil
}

func (linuxRouter) ReleaseDefault(iface string) error {
	var firstErr error
	for _, prefix := range append(append([]string{}, splitDefault...), splitDefaultV6...) {
		family := "-4"
		if strings.Contains(prefix, ":") {
			family = "-6"
		}
		if err := run("ip", family, "route", "del", prefix, "dev", iface); err != nil {
			if isNoSuchProcess(err) || family == "-6" {
				continue
			}
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("netroute: %s %s: %w: %s",
			name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// isNoSuchProcess recognises iproute2's way of saying "that route is not
// there", which is the outcome a delete wanted anyway.
func isNoSuchProcess(err error) bool {
	s := err.Error()
	return strings.Contains(s, "No such process") ||
		strings.Contains(s, "Cannot find device") ||
		strings.Contains(s, "not found")
}
