//go:build darwin

package netroute

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// darwinRouter drives BSD `route`, which is what macOS ships and what
// wg-quick uses on the same platform.
type darwinRouter struct{}

// New returns the router for this platform.
func New() Router { return darwinRouter{} }

func (darwinRouter) Snapshot() (Default, error) {
	out, err := exec.Command("route", "-n", "get", "default").Output()
	if err != nil {
		return Default{}, fmt.Errorf("netroute: read default route: %w", err)
	}
	return parseDarwinDefault(string(out))
}

// parseDarwinDefault reads `route -n get default`, whose output is a block
// of "key: value" lines:
//
//	   route to: default
//	destination: default
//	    gateway: 192.0.2.1
//	  interface: en0
func parseDarwinDefault(out string) (Default, error) {
	var d Default
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "gateway":
			d.Gateway = net.ParseIP(value)
		case "interface":
			d.Interface = value
		}
	}
	if d.Gateway == nil || d.Interface == "" {
		return Default{}, ErrNoDefaultRoute
	}
	return d, nil
}

func (darwinRouter) PinEndpoint(ip net.IP, via Default) error {
	if via.Gateway == nil {
		return ErrNoDefaultRoute
	}
	// BSD route has no "replace", so a stale pin is deleted first. Its
	// failure is ignored: not being there is the state add wants.
	_ = exec.Command("route", "-n", "delete", "-host", ip.String()).Run()
	return run("route", "-n", "add", "-host", ip.String(), via.Gateway.String())
}

func (darwinRouter) UnpinEndpoint(ip net.IP) error {
	if err := run("route", "-n", "delete", "-host", ip.String()); err != nil && !isNotInTable(err) {
		return err
	}
	return nil
}

func (darwinRouter) CaptureDefault(iface string) error {
	for _, prefix := range splitDefault {
		_ = exec.Command("route", "-n", "delete", "-net", prefix).Run()
		if err := run("route", "-n", "add", "-net", prefix, "-interface", iface); err != nil {
			return err
		}
	}
	for _, prefix := range splitDefaultV6 {
		_ = exec.Command("route", "-n", "delete", "-inet6", "-net", prefix).Run()
		// A host with IPv6 off cannot take these, which leaves v4 captured
		// and is not a failure.
		_ = exec.Command("route", "-n", "add", "-inet6", "-net", prefix, "-interface", iface).Run()
	}
	return nil
}

func (darwinRouter) ReleaseDefault(string) error {
	var firstErr error
	for _, prefix := range splitDefault {
		if err := run("route", "-n", "delete", "-net", prefix); err != nil && !isNotInTable(err) {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	for _, prefix := range splitDefaultV6 {
		_ = exec.Command("route", "-n", "delete", "-inet6", "-net", prefix).Run()
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

// isNotInTable recognises BSD route's way of saying the entry is already
// gone.
func isNotInTable(err error) bool {
	s := err.Error()
	return strings.Contains(s, "not in table") || strings.Contains(s, "No such process")
}
