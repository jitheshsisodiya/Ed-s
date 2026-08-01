//go:build windows

package netroute

import (
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// windowsRouter drives the PowerShell NetTCPIP cmdlets rather than the
// legacy `route.exe`, because they address interfaces by name and return
// structured output — `route print` has to be screen-scraped, and its
// interface indices differ from the names Wintun reports.
type windowsRouter struct{}

// New returns the router for this platform.
func New() Router { return windowsRouter{} }

func (windowsRouter) Snapshot() (Default, error) {
	// The lowest-metric default route is the one Windows is actually
	// using. RouteMetric alone is not the whole story — the effective
	// metric adds the interface's — so both are summed.
	const script = `
$r = Get-NetRoute -DestinationPrefix '0.0.0.0/0' -ErrorAction Stop |
  Sort-Object { $_.RouteMetric + (Get-NetIPInterface -InterfaceIndex $_.ifIndex -AddressFamily IPv4).InterfaceMetric } |
  Select-Object -First 1
if (-not $r) { exit 1 }
[pscustomobject]@{
  Gateway = $r.NextHop
  Interface = (Get-NetAdapter -InterfaceIndex $r.ifIndex).Name
} | ConvertTo-Json -Compress`

	out, err := powershell(script)
	if err != nil {
		return Default{}, fmt.Errorf("netroute: read default route: %w", err)
	}
	return parseWindowsDefault(out)
}

// parseWindowsDefault decodes the JSON object the snapshot script emits.
func parseWindowsDefault(out []byte) (Default, error) {
	var raw struct {
		Gateway   string `json:"Gateway"`
		Interface string `json:"Interface"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Default{}, ErrNoDefaultRoute
	}
	gw := net.ParseIP(strings.TrimSpace(raw.Gateway))
	if gw == nil || gw.IsUnspecified() || raw.Interface == "" {
		return Default{}, ErrNoDefaultRoute
	}
	return Default{Gateway: gw, Interface: raw.Interface}, nil
}

func (windowsRouter) PinEndpoint(ip net.IP, via Default) error {
	if via.Gateway == nil || via.Interface == "" {
		return ErrNoDefaultRoute
	}
	// New-NetRoute fails if the prefix already exists, and this runs on
	// reconnect paths, so the stale entry goes first.
	return powershellRun(fmt.Sprintf(
		`Remove-NetRoute -DestinationPrefix '%[1]s' -Confirm:$false -ErrorAction SilentlyContinue
New-NetRoute -DestinationPrefix '%[1]s' -InterfaceAlias '%[2]s' -NextHop '%[3]s' -PolicyStore ActiveStore -ErrorAction Stop | Out-Null`,
		hostPrefix(ip), via.Interface, via.Gateway))
}

func (windowsRouter) UnpinEndpoint(ip net.IP) error {
	return powershellRun(fmt.Sprintf(
		`Remove-NetRoute -DestinationPrefix '%s' -Confirm:$false -ErrorAction SilentlyContinue`,
		hostPrefix(ip)))
}

func (windowsRouter) CaptureDefault(iface string) error {
	var b strings.Builder
	for _, prefix := range splitDefault {
		fmt.Fprintf(&b, `Remove-NetRoute -DestinationPrefix '%[1]s' -InterfaceAlias '%[2]s' -Confirm:$false -ErrorAction SilentlyContinue
New-NetRoute -DestinationPrefix '%[1]s' -InterfaceAlias '%[2]s' -PolicyStore ActiveStore -ErrorAction Stop | Out-Null
`, prefix, iface)
	}
	// IPv6 is best-effort: a host with it disabled still gets v4 captured.
	for _, prefix := range splitDefaultV6 {
		fmt.Fprintf(&b, `New-NetRoute -DestinationPrefix '%s' -InterfaceAlias '%s' -PolicyStore ActiveStore -ErrorAction SilentlyContinue | Out-Null
`, prefix, iface)
	}
	return powershellRun(b.String())
}

func (windowsRouter) ReleaseDefault(iface string) error {
	var b strings.Builder
	for _, prefix := range append(append([]string{}, splitDefault...), splitDefaultV6...) {
		fmt.Fprintf(&b, `Remove-NetRoute -DestinationPrefix '%s' -InterfaceAlias '%s' -Confirm:$false -ErrorAction SilentlyContinue
`, prefix, iface)
	}
	return powershellRun(b.String())
}

func powershell(script string) ([]byte, error) {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}

func powershellRun(script string) error {
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("netroute: powershell: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
