// Package wireguard manages a userspace WireGuard tunnel using
// golang.zx2c4.com/wireguard (wireguard-go). It creates a userspace TUN
// device (via golang.zx2c4.com/wireguard/tun, backed by Wintun on Windows,
// utun on macOS, /dev/net/tun on Linux) and drives it with the in-process
// wireguard-go device.Device, configured through the standard WireGuard
// UAPI textual protocol (https://www.wireguard.com/xplatform/). Because it
// never depends on a kernel WireGuard module, the same code path works on
// Windows, Windows Server, macOS and Linux.
//
// Creating the TUN device itself requires elevated privileges (root on
// Linux/macOS, Administrator on Windows) since it creates a new network
// interface; everything else (peer configuration, handshake/traffic
// introspection) is plain in-process Go code.
package wireguard

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/jitheshsisodiya/Ed-s/client/internal/wintundll"
)

// DefaultMTU matches WireGuard's conventional default MTU, leaving room
// for the WireGuard/UDP/IP overhead under a standard 1500-byte Ethernet
// MTU path.
const DefaultMTU = 1420

// InterfaceConfig configures a new tunnel interface.
type InterfaceConfig struct {
	// Name is the requested OS interface name (e.g. "nexus0"). The OS or
	// tun backend may adjust it (notably on macOS, which assigns utunN);
	// use Device.Name() after Up() for the actual name.
	Name string
	// PrivateKeyBase64 is this device's WireGuard private key in
	// WireGuard's standard base64 wire format (see internal/config).
	PrivateKeyBase64 string
	// ListenPort is the UDP port WireGuard listens on. 0 lets the OS
	// choose an ephemeral port.
	ListenPort int
	// MTU is the tunnel MTU; DefaultMTU is used if zero.
	MTU int
	// LogVerbose enables wireguard-go's verbose internal logging.
	LogVerbose bool
	// Bind, if set, replaces the default UDP bind. Passing a
	// disco.Bind lets discovery probes share this socket — and therefore
	// this NAT mapping — with tunnel traffic, so a probe that succeeds
	// proves the path WireGuard will actually use.
	Bind conn.Bind
}

// PeerConfig describes a WireGuard peer to add or update.
type PeerConfig struct {
	PublicKeyBase64 string
	// Endpoint is the peer's current UDP endpoint. Nil leaves the
	// endpoint unset/unchanged.
	Endpoint *net.UDPAddr
	// AllowedIPs is normally just the peer's single virtual /32 (or /128
	// for IPv6) address in a hub-and-spoke mesh, but is expressed as a
	// slice for generality (e.g. subnet routers in the future).
	AllowedIPs []net.IPNet
	// PersistentKeepaliveSeconds, if >0, tells WireGuard to send a
	// keepalive on this interval, useful for keeping NAT/firewall
	// mappings alive on the relay/hole-punch path. 0 disables it.
	PersistentKeepaliveSeconds int
}

// PeerStats reports live counters/handshake state for one peer, parsed
// from the UAPI "get" operation.
type PeerStats struct {
	PublicKeyBase64 string
	Endpoint        string
	LastHandshake   time.Time
	TxBytes         uint64
	RxBytes         uint64
	AllowedIPs      []string
}

// Device wraps a running userspace WireGuard tunnel.
type Device struct {
	mu     sync.Mutex
	tunDev tun.Device
	dev    *device.Device
	logger *device.Logger
	name   string
	mtu    int
}

// New creates the TUN device and brings up the WireGuard engine, but does
// not assign an IP address to the interface — call ConfigureAddress (or
// the platform-specific AssignAddress helper) separately so callers can
// sequence privilege-requiring steps explicitly.
//
// Requires root/Administrator to create the OS network interface.
func New(cfg InterfaceConfig) (*Device, error) {
	if cfg.PrivateKeyBase64 == "" {
		return nil, fmt.Errorf("wireguard: private key is required")
	}
	mtu := cfg.MTU
	if mtu == 0 {
		mtu = DefaultMTU
	}

	// On Windows the tunnel adapter comes from the Wintun driver, which has
	// to exist on disk before the call below can find it. Everywhere else
	// this is a no-op.
	if err := wintundll.Ensure(); err != nil {
		return nil, fmt.Errorf("wireguard: install the Wintun driver: %w", err)
	}

	tunDev, err := tun.CreateTUN(cfg.Name, mtu)
	if err != nil {
		return nil, fmt.Errorf("wireguard: create TUN device %q: %w", cfg.Name, err)
	}
	actualName, err := tunDev.Name()
	if err != nil {
		actualName = cfg.Name
	}

	logLevel := device.LogLevelError
	if cfg.LogVerbose {
		logLevel = device.LogLevelVerbose
	}
	logger := device.NewLogger(logLevel, fmt.Sprintf("(%s) ", actualName))

	bind := cfg.Bind
	if bind == nil {
		bind = conn.NewDefaultBind()
	}
	dev := device.NewDevice(tunDev, bind, logger)

	d := &Device{
		tunDev: tunDev,
		dev:    dev,
		logger: logger,
		name:   actualName,
		mtu:    mtu,
	}

	uapiConf, err := buildInterfaceUAPI(cfg.PrivateKeyBase64, cfg.ListenPort)
	if err != nil {
		dev.Close()
		return nil, err
	}
	if err := dev.IpcSet(uapiConf); err != nil {
		dev.Close()
		return nil, fmt.Errorf("wireguard: configure interface: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return nil, fmt.Errorf("wireguard: bring device up: %w", err)
	}
	return d, nil
}

// Name returns the actual OS-assigned interface name.
func (d *Device) Name() string { return d.name }

// MTU returns the configured tunnel MTU.
func (d *Device) MTU() int { return d.mtu }

// ListenPort returns the UDP port the WireGuard engine actually bound to
// (useful when InterfaceConfig.ListenPort was 0 for an ephemeral port).
func (d *Device) ListenPort() (int, error) {
	raw, err := d.dev.IpcGet()
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(raw, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok && k == "listen_port" {
			p, err := strconv.Atoi(v)
			if err != nil {
				return 0, fmt.Errorf("wireguard: parse listen_port: %w", err)
			}
			return p, nil
		}
	}
	return 0, fmt.Errorf("wireguard: listen_port not present in device state")
}

// Close tears down the WireGuard engine and TUN device.
func (d *Device) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dev.Close()
	return nil
}

// UpsertPeer adds a peer, or updates an existing peer's endpoint/allowed
// IPs/keepalive in place (peers are matched by public key, per the UAPI
// protocol; this call does not remove other peers).
func (d *Device) UpsertPeer(p PeerConfig) error {
	uapiConf, err := buildPeerUAPI(p, false)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.dev.IpcSet(uapiConf); err != nil {
		return fmt.Errorf("wireguard: configure peer %s: %w", shortKey(p.PublicKeyBase64), err)
	}
	return nil
}

// UpdateEndpoint changes just a peer's endpoint (used after a successful
// hole punch, or when falling back to/away from a relay), leaving its
// AllowedIPs untouched.
func (d *Device) UpdateEndpoint(publicKeyBase64 string, endpoint *net.UDPAddr) error {
	return d.UpsertPeer(PeerConfig{
		PublicKeyBase64: publicKeyBase64,
		Endpoint:        endpoint,
	})
}

// RemovePeer removes a peer by public key.
func (d *Device) RemovePeer(publicKeyBase64 string) error {
	uapiConf, err := buildPeerUAPI(PeerConfig{PublicKeyBase64: publicKeyBase64}, true)
	if err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.dev.IpcSet(uapiConf); err != nil {
		return fmt.Errorf("wireguard: remove peer %s: %w", shortKey(publicKeyBase64), err)
	}
	return nil
}

// Stats returns live per-peer counters and handshake timestamps by
// querying the UAPI "get" operation.
func (d *Device) Stats() ([]PeerStats, error) {
	raw, err := d.dev.IpcGet()
	if err != nil {
		return nil, err
	}
	return parseStats(raw)
}

// PeerStats returns stats for a single peer, or ok=false if the peer is
// not currently configured.
func (d *Device) PeerStats(publicKeyBase64 string) (stats PeerStats, ok bool, err error) {
	all, err := d.Stats()
	if err != nil {
		return PeerStats{}, false, err
	}
	for _, s := range all {
		if s.PublicKeyBase64 == publicKeyBase64 {
			return s, true, nil
		}
	}
	return PeerStats{}, false, nil
}

// buildInterfaceUAPI renders the UAPI "set" payload for the interface's
// own private key and listen port.
func buildInterfaceUAPI(privateKeyBase64 string, listenPort int) (string, error) {
	hexKey, err := base64KeyToHex(privateKeyBase64)
	if err != nil {
		return "", fmt.Errorf("wireguard: invalid private key: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", hexKey)
	fmt.Fprintf(&b, "listen_port=%d\n", listenPort)
	return b.String(), nil
}

// buildPeerUAPI renders the UAPI "set" payload for a single peer
// operation (add/update, or remove).
func buildPeerUAPI(p PeerConfig, remove bool) (string, error) {
	hexKey, err := base64KeyToHex(p.PublicKeyBase64)
	if err != nil {
		return "", fmt.Errorf("wireguard: invalid peer public key: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "public_key=%s\n", hexKey)
	if remove {
		b.WriteString("remove=true\n")
		return b.String(), nil
	}
	if p.Endpoint != nil {
		fmt.Fprintf(&b, "endpoint=%s\n", p.Endpoint.String())
	}
	fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", p.PersistentKeepaliveSeconds)
	if len(p.AllowedIPs) > 0 {
		b.WriteString("replace_allowed_ips=true\n")
		for _, ipnet := range p.AllowedIPs {
			fmt.Fprintf(&b, "allowed_ip=%s\n", ipnet.String())
		}
	}
	return b.String(), nil
}

func base64KeyToHex(key string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return "", err
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("expected 32-byte key, got %d bytes", len(raw))
	}
	return hex.EncodeToString(raw), nil
}

func hexKeyToBase64(hexKey string) (string, error) {
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func shortKey(b64 string) string {
	if len(b64) <= 8 {
		return b64
	}
	return b64[:8] + "…"
}

// parseStats walks the UAPI "get" textual protocol, which lists the
// interface's own fields first, then repeats a public_key= line to start
// each subsequent peer block.
func parseStats(raw string) ([]PeerStats, error) {
	var (
		stats   []PeerStats
		current *PeerStats
	)
	flush := func() {
		if current != nil {
			stats = append(stats, *current)
			current = nil
		}
	}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "public_key":
			flush()
			b64, err := hexKeyToBase64(val)
			if err != nil {
				return nil, fmt.Errorf("wireguard: parse public_key: %w", err)
			}
			current = &PeerStats{PublicKeyBase64: b64}
		case "endpoint":
			if current != nil {
				current.Endpoint = val
			}
		case "last_handshake_time_sec":
			if current != nil {
				sec, err := strconv.ParseInt(val, 10, 64)
				if err == nil && sec > 0 {
					current.LastHandshake = time.Unix(sec, 0)
				}
			}
		case "tx_bytes":
			if current != nil {
				n, _ := strconv.ParseUint(val, 10, 64)
				current.TxBytes = n
			}
		case "rx_bytes":
			if current != nil {
				n, _ := strconv.ParseUint(val, 10, 64)
				current.RxBytes = n
			}
		case "allowed_ip":
			if current != nil {
				current.AllowedIPs = append(current.AllowedIPs, val)
			}
		}
	}
	flush()
	return stats, nil
}

// ParseAllowedIP parses a CIDR (e.g. "10.77.0.5/32") into a net.IPNet,
// defaulting to a /32 (or /128 for IPv6) host route if no prefix is given.
func ParseAllowedIP(s string) (net.IPNet, error) {
	if !strings.Contains(s, "/") {
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return net.IPNet{}, fmt.Errorf("parse address %q: %w", s, err)
		}
		bits := 32
		if addr.Is6() {
			bits = 128
		}
		s = fmt.Sprintf("%s/%d", s, bits)
	}
	_, ipnet, err := net.ParseCIDR(s)
	if err != nil {
		return net.IPNet{}, fmt.Errorf("parse CIDR %q: %w", s, err)
	}
	return *ipnet, nil
}
