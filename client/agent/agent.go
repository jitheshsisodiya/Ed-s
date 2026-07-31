// Package agent is the public API of the NexusVPN client engine.
//
// It exists so every frontend — the nexusvpnctl CLI, the Wails desktop app,
// and any embedder — drives the same code path. Connectivity behaviour
// therefore cannot drift between them, and the internal packages stay
// internal.
package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/client/internal/apiclient"
	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
	"github.com/jitheshsisodiya/Ed-s/client/internal/coordination"
	"github.com/jitheshsisodiya/Ed-s/client/internal/disco"
	"github.com/jitheshsisodiya/Ed-s/client/internal/tunnel"
	"github.com/jitheshsisodiya/Ed-s/client/internal/wireguard"
)

// ErrMFARequired is returned by Login when the account has MFA enabled and
// no code was supplied. Callers should prompt and retry with the code.
var ErrMFARequired = fmt.Errorf("mfa required")

// DefaultSTUNServers are used when the config does not override them. Two
// independent servers are required to detect symmetric NAT.
var DefaultSTUNServers = []string{"stun.l.google.com:19302", "stun1.l.google.com:19302"}

// Session is the stored login state.
type Session struct {
	LoggedIn   bool   `json:"loggedIn"`
	ServerURL  string `json:"serverUrl"`
	Email      string `json:"email"`
	DeviceName string `json:"deviceName"`
	PublicKey  string `json:"publicKey"`
}

// Network is a network as presented to a frontend.
type Network struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CIDR        string `json:"cidr"`
	Role        string `json:"role"`
	MemberCount int    `json:"memberCount"`
	DeviceCount int    `json:"deviceCount"`
	InviteCode  string `json:"inviteCode"`
	// Joined reports whether this device holds local state for the network.
	Joined bool `json:"joined"`
}

// Peer is one peer's live connectivity.
type Peer struct {
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	// OS is the peer's platform, so a frontend can show a recognisable
	// icon per device rather than one generic glyph.
	OS            string `json:"os"`
	VirtualIP     string `json:"virtualIp"`
	Mode          string `json:"mode"`
	Endpoint      string `json:"endpoint"`
	LastHandshake string `json:"lastHandshake"`
	BytesSent     uint64 `json:"bytesSent"`
	BytesReceived uint64 `json:"bytesReceived"`
	// LatencyMs is the measured round-trip time, or -1 if not yet probed.
	LatencyMs int `json:"latencyMs"`
	// Quality is the plain-language connection health a user actually sees:
	// "Excellent", "Good", "Limited" or "Offline". Derived here rather than
	// in each frontend so the CLI and the apps can never disagree about what
	// a connection is worth.
	Quality string `json:"quality"`
}

// Connection health, in the words shown to users. The thresholds are about
// perceptibility, not networking: under 50ms feels instant, under 150ms
// feels responsive, and anything relayed is working-but-slower by
// definition because it takes an extra hop.
const (
	QualityExcellent = "Excellent"
	QualityGood      = "Good"
	QualityLimited   = "Limited"
	QualityOffline   = "Offline"
)

// describeQuality turns a path mode and a measured round-trip time into the
// single word a user sees. It never surfaces the mode or the number.
func describeQuality(mode string, latencyMs int) string {
	switch mode {
	case "offline", "":
		return QualityOffline
	case "relay":
		// A relay works; it just costs an extra hop.
		return QualityLimited
	case "connecting":
		return QualityOffline
	}

	switch {
	case latencyMs < 0:
		// Direct path established but not yet probed: report the path we
		// know is good rather than pretending it is offline.
		return QualityGood
	case latencyMs < 50:
		return QualityExcellent
	case latencyMs < 150:
		return QualityGood
	default:
		return QualityLimited
	}
}

// Status is the live tunnel state.
type Status struct {
	Connected      bool   `json:"connected"`
	NetworkID      string `json:"networkId"`
	NetworkName    string `json:"networkName"`
	InterfaceName  string `json:"interfaceName"`
	VirtualIP      string `json:"virtualIp"`
	CIDR           string `json:"cidr"`
	NATType        string `json:"natType"`
	PublicEndpoint string `json:"publicEndpoint"`
	Peers          []Peer `json:"peers"`
}

// ConnectOptions tunes a Connect call. The zero value is valid.
type ConnectOptions struct {
	// InterfaceName is the requested tunnel interface name.
	InterfaceName string
	// ListenPort is WireGuard's UDP port; 0 picks an ephemeral one.
	ListenPort int
	// GRPCAddr overrides the coordination endpoint. Empty derives it from
	// the server URL (same host, port 9090).
	GRPCAddr string
	// InsecureSkipVerify disables gRPC TLS verification. Test deployments
	// only: it removes protection against an intercepting proxy.
	InsecureSkipVerify bool
	// ClientVersion is reported to the control plane.
	ClientVersion string
	// Logf receives engine progress messages.
	Logf func(format string, args ...any)
}

// Agent owns the local configuration and, while connected, one tunnel.
type Agent struct {
	store *config.Store

	mu      sync.Mutex
	tun     *tunnel.Tunnel
	cancel  context.CancelFunc
	closers []func()
	done    chan struct{}
}

// New builds an Agent backed by the default per-OS config location.
func New() (*Agent, error) {
	store, err := config.NewStore()
	if err != nil {
		return nil, err
	}
	return &Agent{store: store}, nil
}

// NewWithStore builds an Agent against an explicit config path (for tests).
func NewWithStore(path string) *Agent {
	return &Agent{store: config.NewStoreAt(path)}
}

// Session reports the stored login state.
func (a *Agent) Session() (Session, error) {
	cfg, err := a.store.Load()
	if err != nil {
		return Session{}, err
	}
	return Session{
		LoggedIn:   cfg.IsLoggedIn(),
		ServerURL:  cfg.ServerURL,
		Email:      cfg.UserEmail,
		DeviceName: cfg.DeviceName,
		PublicKey:  cfg.Keypair.PublicKey,
	}, nil
}

// Login authenticates and stores the session. It returns ErrMFARequired if
// the account needs a TOTP code that was not supplied.
func (a *Agent) Login(ctx context.Context, serverURL, email, password, mfaCode string) error {
	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" {
		return fmt.Errorf("a server URL is required")
	}
	if email == "" || password == "" {
		return fmt.Errorf("email and password are required")
	}

	client := apiclient.New(apiBaseURL(serverURL), apiclient.Options{})
	tokens, err := client.Login(ctx, email, password, mfaCode)
	if err != nil {
		return err
	}
	if tokens.MFARequired {
		return ErrMFARequired
	}

	_, err = a.store.Update(func(c *config.Config) error {
		c.ServerURL = serverURL
		c.AccessToken = tokens.AccessToken
		c.RefreshToken = tokens.RefreshToken
		c.UserEmail = email
		return nil
	})
	return err
}

// Logout revokes the session server-side (best effort) and clears it locally,
// so signing out always succeeds even while offline.
func (a *Agent) Logout(ctx context.Context) error {
	a.Disconnect()

	cfg, err := a.store.Load()
	if err != nil {
		return err
	}
	if cfg.IsLoggedIn() {
		_ = a.client(cfg).Logout(ctx)
	}

	_, err = a.store.Update(func(c *config.Config) error {
		c.AccessToken = ""
		c.RefreshToken = ""
		c.UserEmail = ""
		return nil
	})
	return err
}

// ListNetworks returns the account's networks.
func (a *Agent) ListNetworks(ctx context.Context) ([]Network, error) {
	cfg, client, err := a.authed()
	if err != nil {
		return nil, err
	}
	networks, err := client.ListNetworks(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]Network, 0, len(networks))
	for _, n := range networks {
		_, joined := cfg.Network(n.ID)
		out = append(out, Network{
			ID: n.ID, Name: n.Name, Description: n.Description, CIDR: n.CIDR,
			Role: n.Role, MemberCount: n.MemberCount, DeviceCount: n.DeviceCount,
			InviteCode: n.InviteCode, Joined: joined,
		})
	}
	return out, nil
}

// CreateNetwork creates a network and records it locally.
func (a *Agent) CreateNetwork(ctx context.Context, name, description, cidr string) (Network, error) {
	_, client, err := a.authed()
	if err != nil {
		return Network{}, err
	}
	if strings.TrimSpace(name) == "" {
		return Network{}, fmt.Errorf("a network name is required")
	}
	if strings.TrimSpace(cidr) == "" {
		cidr = "10.77.0.0/24"
	}

	n, err := client.CreateNetwork(ctx, name, description, cidr, nil)
	if err != nil {
		return Network{}, err
	}
	a.rememberNetwork(n.ID, n.Name, n.CIDR)
	return Network{
		ID: n.ID, Name: n.Name, Description: n.Description, CIDR: n.CIDR,
		Role: n.Role, InviteCode: n.InviteCode, Joined: true,
	}, nil
}

// JoinNetwork joins by invite code and records the network locally.
func (a *Agent) JoinNetwork(ctx context.Context, inviteCode string) (Network, error) {
	_, client, err := a.authed()
	if err != nil {
		return Network{}, err
	}
	inviteCode = strings.TrimSpace(inviteCode)
	if inviteCode == "" {
		return Network{}, fmt.Errorf("an invite code is required")
	}

	n, err := client.JoinNetwork(ctx, inviteCode)
	if err != nil {
		return Network{}, err
	}
	a.rememberNetwork(n.ID, n.Name, n.CIDR)
	return Network{
		ID: n.ID, Name: n.Name, Description: n.Description, CIDR: n.CIDR,
		Role: n.Role, MemberCount: n.MemberCount, DeviceCount: n.DeviceCount,
		Joined: true,
	}, nil
}

// ForgetNetwork drops local state for a network. Membership and the device
// registration remain server-side, so this cannot silently orphan a device
// that other members can still see.
func (a *Agent) ForgetNetwork(networkID string) error {
	_, err := a.store.Update(func(c *config.Config) error {
		if _, ok := c.Networks[networkID]; !ok {
			return fmt.Errorf("network %s is not joined locally", networkID)
		}
		delete(c.Networks, networkID)
		return nil
	})
	return err
}

// Devices lists the devices registered on a network.
func (a *Agent) Devices(ctx context.Context, networkID string) ([]apiclient.Device, error) {
	_, client, err := a.authed()
	if err != nil {
		return nil, err
	}
	return client.ListDevices(ctx, networkID)
}

// RotateDeviceKey generates a fresh keypair, invalidating the old public key
// network-wide once the device re-registers.
func (a *Agent) RotateDeviceKey() (string, error) {
	a.Disconnect()

	kp, err := config.GenerateKeypair()
	if err != nil {
		return "", err
	}
	cfg, err := a.store.Update(func(c *config.Config) error {
		c.Keypair = kp
		// Registrations made with the old key are now stale.
		for _, n := range c.Networks {
			n.DeviceID = ""
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return cfg.Keypair.PublicKey, nil
}

// SetDeviceName renames this device.
func (a *Agent) SetDeviceName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("a device name is required")
	}
	_, err := a.store.Update(func(c *config.Config) error {
		c.DeviceName = name
		return nil
	})
	return err
}

// ResolveNetwork picks the network to act on: the one requested, or the only
// locally joined network when exactly one exists.
func (a *Agent) ResolveNetwork(networkID string) (id, name string, err error) {
	cfg, err := a.store.Load()
	if err != nil {
		return "", "", err
	}
	if networkID != "" {
		if state, ok := cfg.Network(networkID); ok {
			return state.NetworkID, state.Name, nil
		}
		// Not joined locally; the control plane is the authority on whether
		// the ID is valid.
		return networkID, "", nil
	}

	switch len(cfg.Networks) {
	case 0:
		return "", "", fmt.Errorf("no networks joined")
	case 1:
		for _, state := range cfg.Networks {
			return state.NetworkID, state.Name, nil
		}
	}
	return "", "", fmt.Errorf("multiple networks joined - specify which one")
}

// Connect brings up the tunnel for a network. It returns once the interface
// is up, addressed and registered; peer negotiation continues in the
// background. Call Disconnect (or Wait) to stop it.
//
// Creating a TUN device is privileged on every platform.
func (a *Agent) Connect(networkID string, opts ConnectOptions) error {
	a.mu.Lock()
	if a.tun != nil {
		a.mu.Unlock()
		return fmt.Errorf("already connected")
	}
	a.mu.Unlock()

	cfg, client, err := a.authed()
	if err != nil {
		return err
	}
	if networkID == "" {
		return fmt.Errorf("a network must be selected")
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		return fmt.Errorf("creating a tunnel interface requires root/Administrator privileges")
	}

	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ifaceName := opts.InterfaceName
	if ifaceName == "" {
		ifaceName = "nexus0"
	}

	// Discovery probes share WireGuard's socket, so a probe that gets
	// through proves the path the tunnel will actually use, and opens the
	// pinhole it needs. Probing from a separate socket would open a mapping
	// for the wrong socket entirely.
	discoBind := disco.NewBind()

	dev, err := wireguard.New(wireguard.InterfaceConfig{
		Name:             ifaceName,
		PrivateKeyBase64: cfg.Keypair.PrivateKey,
		ListenPort:       opts.ListenPort,
		Bind:             discoBind,
	})
	if err != nil {
		return fmt.Errorf("create tunnel interface: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	closers := []func(){func() { _ = dev.Close() }}
	abort := func(err error) error {
		cancel()
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
		return err
	}

	target, insecureGRPC := grpcTarget(cfg.ServerURL, opts.GRPCAddr)
	coord, err := coordination.Dial(ctx, target, client, coordination.DialOptions{
		Insecure:           insecureGRPC,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	})
	if err != nil {
		return abort(fmt.Errorf("connect to coordination service: %w", err))
	}
	closers = append(closers, func() { _ = coord.Close() })

	// Discovery and hole-punch probes share one socket so both observe the
	// same NAT mapping.
	probeConn, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		return abort(fmt.Errorf("open discovery socket: %w", err))
	}
	closers = append(closers, func() { _ = probeConn.Close() })

	stunServers := cfg.STUNServers
	if len(stunServers) < 2 {
		stunServers = DefaultSTUNServers
	}

	// The prober answers inbound probes immediately (which is how the other
	// side discovers this path) and records round-trip times for status.
	// Its own device ID is filled in after registration below.
	prober := disco.NewProber(discoBind, uuid.Nil)

	tun, err := tunnel.New(tunnel.Options{
		Device:      dev,
		Coordinator: coordAdapter{coord},
		PathProber:  prober,
		Discoverer: tunnel.STUNDiscoverer{
			Conn:            probeConn,
			PrimaryServer:   stunServers[0],
			SecondaryServer: stunServers[1],
		},
		Prober: func(addr *net.UDPAddr) error {
			// A single zero byte opens the NAT pinhole; it is not a valid
			// WireGuard packet, so the peer ignores it harmlessly.
			_, err := probeConn.WriteToUDP([]byte{0}, addr)
			return err
		},
		NetworkID:     networkID,
		DeviceName:    cfg.DeviceName,
		OS:            DeviceOS(),
		OSVersion:     runtime.GOARCH,
		ClientVersion: opts.ClientVersion,
		PublicKey:     cfg.Keypair.PublicKey,
		Logf:          logf,
	})
	if err != nil {
		return abort(err)
	}

	// Register before starting the loops: the interface must be addressed
	// before any traffic can flow.
	resp, err := tun.Register(ctx)
	if err != nil {
		return abort(err)
	}

	// Probes are attributed by device ID, which only exists after
	// registration.
	if id, err := uuid.Parse(resp.GetDeviceId()); err == nil {
		prober.SetSelfID(id)
	}

	virtualIP := net.ParseIP(resp.GetAssignedVirtualIp())
	if virtualIP == nil {
		return abort(fmt.Errorf("control plane assigned a malformed address %q", resp.GetAssignedVirtualIp()))
	}
	_, netCIDR, err := net.ParseCIDR(resp.GetNetworkCidr())
	if err != nil {
		return abort(fmt.Errorf("control plane returned a malformed CIDR %q: %w", resp.GetNetworkCidr(), err))
	}
	if err := dev.AssignAddress(virtualIP, netCIDR); err != nil {
		return abort(fmt.Errorf("assign %s to %s: %w", virtualIP, dev.Name(), err))
	}
	if err := dev.AddRoute(netCIDR); err != nil {
		return abort(fmt.Errorf("route %s via %s: %w", netCIDR, dev.Name(), err))
	}

	if _, name, err := a.ResolveNetwork(networkID); err == nil && name != "" {
		tun.SetNetworkName(name)
	}
	a.persistNetworkState(networkID, resp.GetDeviceId(), resp.GetAssignedVirtualIp(), resp.GetNetworkCidr(), resp.GetDnsServers())

	done := make(chan struct{})
	a.mu.Lock()
	a.tun = tun
	a.cancel = cancel
	a.closers = closers
	a.done = done
	a.mu.Unlock()

	go func() {
		defer close(done)
		if err := tun.Start(ctx); err != nil && ctx.Err() == nil {
			logf("tunnel stopped: %v", err)
		}
		a.release()
	}()

	logf("connected to %s as %s", resp.GetNetworkCidr(), resp.GetAssignedVirtualIp())
	return nil
}

// Disconnect stops a running tunnel and releases its interface.
func (a *Agent) Disconnect() {
	a.mu.Lock()
	cancel := a.cancel
	done := a.done
	a.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
}

// Wait blocks until the tunnel stops (or returns immediately if none runs).
func (a *Agent) Wait() {
	a.mu.Lock()
	done := a.done
	a.mu.Unlock()
	if done != nil {
		<-done
	}
}

// Status returns the live tunnel state; Connected is false when idle.
func (a *Agent) Status() Status {
	a.mu.Lock()
	tun := a.tun
	a.mu.Unlock()

	if tun == nil {
		return Status{}
	}

	s := tun.Status()
	out := Status{
		Connected:      s.Connected,
		NetworkID:      s.NetworkID,
		NetworkName:    s.NetworkName,
		InterfaceName:  s.InterfaceName,
		VirtualIP:      s.VirtualIP,
		CIDR:           s.CIDR,
		NATType:        s.NATType,
		PublicEndpoint: s.PublicEndpoint,
	}
	for _, p := range s.Peers {
		handshake := ""
		if !p.LastHandshake.IsZero() {
			handshake = p.LastHandshake.UTC().Format(time.RFC3339)
		}
		out.Peers = append(out.Peers, Peer{
			DeviceID: p.DeviceID, DeviceName: p.DeviceName, OS: p.OS, VirtualIP: p.VirtualIP,
			Mode: string(p.Mode), Endpoint: p.Endpoint, LastHandshake: handshake,
			BytesSent: p.BytesSent, BytesReceived: p.BytesReceived,
			LatencyMs: p.LatencyMs,
			Quality:   describeQuality(string(p.Mode), p.LatencyMs),
		})
	}
	return out
}

// --- internals ---

func (a *Agent) client(cfg *config.Config) *apiclient.Client {
	client := apiclient.New(apiBaseURL(cfg.ServerURL), apiclient.Options{
		OnTokenRefresh: func(tp apiclient.TokenPair) {
			// Persist rotated tokens so a refreshed session survives restarts.
			_, _ = a.store.Update(func(c *config.Config) error {
				c.AccessToken = tp.AccessToken
				c.RefreshToken = tp.RefreshToken
				return nil
			})
		},
	})
	client.SetTokens(cfg.AccessToken, cfg.RefreshToken)
	return client
}

func (a *Agent) authed() (*config.Config, *apiclient.Client, error) {
	cfg, err := a.store.Load()
	if err != nil {
		return nil, nil, err
	}
	if !cfg.IsLoggedIn() {
		return nil, nil, fmt.Errorf("not signed in")
	}
	return cfg, a.client(cfg), nil
}

func (a *Agent) rememberNetwork(id, name, cidr string) {
	_, _ = a.store.Update(func(c *config.Config) error {
		state, ok := c.Networks[id]
		if !ok {
			state = &config.NetworkState{NetworkID: id, JoinedAt: time.Now()}
			c.Networks[id] = state
		}
		state.Name = name
		state.CIDR = cidr
		return nil
	})
}

func (a *Agent) persistNetworkState(networkID, deviceID, virtualIP, cidr string, dns []string) {
	_, _ = a.store.Update(func(c *config.Config) error {
		state, ok := c.Networks[networkID]
		if !ok {
			state = &config.NetworkState{NetworkID: networkID, JoinedAt: time.Now()}
			c.Networks[networkID] = state
		}
		state.DeviceID = deviceID
		state.VirtualIP = virtualIP
		state.CIDR = cidr
		state.DNSServers = dns
		return nil
	})
}

func (a *Agent) release() {
	a.mu.Lock()
	closers := a.closers
	a.tun = nil
	a.cancel = nil
	a.closers = nil
	a.mu.Unlock()

	for i := len(closers) - 1; i >= 0; i-- {
		closers[i]()
	}
}

// apiBaseURL derives the REST base URL, tolerating an explicit suffix.
func apiBaseURL(serverURL string) string {
	trimmed := strings.TrimRight(serverURL, "/")
	if strings.HasSuffix(trimmed, "/api/v1") {
		return trimmed
	}
	return trimmed + "/api/v1"
}

// grpcTarget derives the coordination gRPC address from the server URL:
// same host, port 9090, unless overridden.
func grpcTarget(serverURL, override string) (target string, insecure bool) {
	if override != "" {
		return override, false
	}
	host := serverURL
	for _, prefix := range []string{"https://", "http://"} {
		if strings.HasPrefix(host, prefix) {
			insecure = prefix == "http://"
			host = strings.TrimPrefix(host, prefix)
			break
		}
	}
	host = strings.TrimRight(host, "/")
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return net.JoinHostPort(host, "9090"), insecure
}

// DeviceOS reports this device's OS using the control plane's vocabulary.
func DeviceOS() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "macos"
	case "linux":
		return "linux"
	default:
		return "unknown"
	}
}

// coordAdapter adapts *coordination.Client to tunnel.Coordinator; only the
// stream's named return type differs between the two packages.
type coordAdapter struct{ *coordination.Client }

func (a coordAdapter) StreamPeerUpdates(ctx context.Context, deviceID string) (tunnel.PeerUpdateStream, error) {
	return a.Client.StreamPeerUpdates(ctx, deviceID)
}
