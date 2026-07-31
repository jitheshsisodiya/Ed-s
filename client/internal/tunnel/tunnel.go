package tunnel

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/client/internal/holepunch"
	"github.com/jitheshsisodiya/Ed-s/client/internal/stun"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/client/internal/coordination/gen"
)

// Defaults for the orchestrator's timers.
const (
	DefaultHeartbeatInterval   = 20 * time.Second
	DefaultMonitorInterval     = 15 * time.Second
	DefaultPunchTimeout        = 5 * time.Second
	DefaultRenegotiateInterval = 30 * time.Second
	defaultStreamRetryBase     = time.Second
	defaultStreamRetryMax      = 30 * time.Second
)

// Options configures a Tunnel.
type Options struct {
	// Device is the WireGuard interface (already up, address assigned).
	Device WireGuardDevice
	// Coordinator is the control-plane gRPC client.
	Coordinator Coordinator
	// Discoverer performs STUN discovery. Optional: when nil, the tunnel
	// runs without server-reflexive candidates and relies on relay fallback.
	Discoverer EndpointDiscoverer
	// Prober sends hole-punch probes. It must send from the same socket
	// WireGuard uses, so probes and handshakes share a NAT mapping.
	Prober holepunch.Prober

	// NetworkID is the network to join.
	NetworkID string
	// DeviceName, OS, OSVersion and ClientVersion identify this device.
	DeviceName    string
	OS            string
	OSVersion     string
	ClientVersion string
	// PublicKey is this device's WireGuard public key.
	PublicKey string
	// PreferredRegion biases relay selection.
	PreferredRegion string

	// Timer overrides; zero values fall back to the Default* constants.
	HeartbeatInterval   time.Duration
	MonitorInterval     time.Duration
	PunchTimeout        time.Duration
	RenegotiateInterval time.Duration

	// Logf receives operational messages.
	Logf func(format string, args ...any)
}

// Tunnel orchestrates one device's participation in one network.
type Tunnel struct {
	mu sync.Mutex

	dev    WireGuardDevice
	coord  Coordinator
	disc   EndpointDiscoverer
	prober holepunch.Prober

	opts Options

	deviceID        string
	networkID       string
	networkName     string
	virtualIP       string
	cidr            string
	dnsServers      []string
	preferredRegion string

	discovery DiscoveryResult
	peers     map[uuid.UUID]*peer

	heartbeatInterval   time.Duration
	monitorInterval     time.Duration
	punchTimeout        time.Duration
	renegotiateInterval time.Duration

	started bool
	logf    func(format string, args ...any)
}

// New builds a Tunnel. Call Start to run it.
func New(opts Options) (*Tunnel, error) {
	if opts.Device == nil {
		return nil, errors.New("tunnel: a WireGuard device is required")
	}
	if opts.Coordinator == nil {
		return nil, errors.New("tunnel: a coordinator is required")
	}
	if opts.NetworkID == "" {
		return nil, errors.New("tunnel: a network ID is required")
	}
	if opts.PublicKey == "" {
		return nil, errors.New("tunnel: a device public key is required")
	}
	if opts.Prober == nil {
		// Without a prober, hole punching can't run; relay fallback still
		// works, so this is a degraded but valid configuration.
		opts.Prober = func(*net.UDPAddr) error { return errors.New("tunnel: no prober configured") }
	}
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}

	orDefault := func(v, def time.Duration) time.Duration {
		if v <= 0 {
			return def
		}
		return v
	}

	return &Tunnel{
		dev:                 opts.Device,
		coord:               opts.Coordinator,
		disc:                opts.Discoverer,
		prober:              opts.Prober,
		opts:                opts,
		networkID:           opts.NetworkID,
		preferredRegion:     opts.PreferredRegion,
		peers:               make(map[uuid.UUID]*peer),
		heartbeatInterval:   orDefault(opts.HeartbeatInterval, DefaultHeartbeatInterval),
		monitorInterval:     orDefault(opts.MonitorInterval, DefaultMonitorInterval),
		punchTimeout:        orDefault(opts.PunchTimeout, DefaultPunchTimeout),
		renegotiateInterval: orDefault(opts.RenegotiateInterval, DefaultRenegotiateInterval),
		logf:                logf,
	}, nil
}

// Register performs the initial device registration and installs the peers
// the control plane already knows about. It is called by Start, and exposed
// separately so callers can learn the assigned virtual IP before the
// long-running loops begin (the address must be assigned to the interface
// before traffic can flow).
func (t *Tunnel) Register(ctx context.Context) (*coordinationv1.RegisterDeviceResponse, error) {
	resp, err := t.coord.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: t.opts.PublicKey,
		NetworkId:       t.networkID,
		DeviceName:      t.opts.DeviceName,
		Os:              t.opts.OS,
		OsVersion:       t.opts.OSVersion,
		ClientVersion:   t.opts.ClientVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("tunnel: register device: %w", err)
	}

	t.mu.Lock()
	t.deviceID = resp.GetDeviceId()
	t.virtualIP = resp.GetAssignedVirtualIp()
	t.cidr = resp.GetNetworkCidr()
	t.dnsServers = resp.GetDnsServers()
	t.mu.Unlock()

	t.logf("registered as %s with virtual IP %s on %s", resp.GetDeviceId(), resp.GetAssignedVirtualIp(), resp.GetNetworkCidr())
	return resp, nil
}

// Start registers (unless Register was already called), installs known
// peers, and runs the heartbeat, peer-update and monitor loops until ctx is
// cancelled.
func (t *Tunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	alreadyRegistered := t.deviceID != ""
	if t.started {
		t.mu.Unlock()
		return errors.New("tunnel: already started")
	}
	t.started = true
	t.mu.Unlock()

	var existingPeers []*coordinationv1.Peer
	if !alreadyRegistered {
		resp, err := t.Register(ctx)
		if err != nil {
			return err
		}
		existingPeers = resp.GetExistingPeers()
	}

	// Discover our public endpoint before announcing ourselves, so the
	// first heartbeat already carries a usable candidate for other peers.
	t.runDiscovery(ctx)

	for _, p := range existingPeers {
		if err := t.addPeer(ctx, p); err != nil {
			t.logf("add peer: %v", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); t.heartbeatLoop(ctx) }()
	go func() { defer wg.Done(); t.peerUpdateLoop(ctx) }()
	go func() { defer wg.Done(); t.monitorLoop(ctx) }()
	wg.Wait()

	t.shutdownPeers()
	return ctx.Err()
}

// runDiscovery refreshes this device's STUN view of itself.
func (t *Tunnel) runDiscovery(ctx context.Context) {
	if t.disc == nil {
		return
	}
	discCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := t.disc.Discover(discCtx)
	if err != nil {
		t.logf("STUN discovery failed: %v", err)
		return
	}

	t.mu.Lock()
	t.discovery = result
	t.mu.Unlock()

	t.logf("discovered public endpoint %s (NAT: %s)", endpointString(result.PublicEndpoint), result.NATType)
}

// heartbeatLoop reports liveness, endpoint and traffic counters, and honors
// the server's requested cadence.
func (t *Tunnel) heartbeatLoop(ctx context.Context) {
	interval := t.heartbeatInterval
	timer := time.NewTimer(0) // fire immediately
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		next := t.sendHeartbeat(ctx, interval)
		timer.Reset(next)
	}
}

// sendHeartbeat sends one heartbeat and returns the interval to wait before
// the next one.
func (t *Tunnel) sendHeartbeat(ctx context.Context, fallback time.Duration) time.Duration {
	t.mu.Lock()
	deviceID := t.deviceID
	discovery := t.discovery
	t.mu.Unlock()

	if deviceID == "" {
		return fallback
	}

	var sent, received uint64
	if stats, err := t.dev.Stats(); err == nil {
		for _, s := range stats {
			sent += s.TxBytes
			received += s.RxBytes
		}
	}

	rpcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := t.coord.Heartbeat(rpcCtx, &coordinationv1.HeartbeatRequest{
		DeviceId:        deviceID,
		PublicEndpoint:  endpointToProto(discovery.PublicEndpoint),
		PrivateEndpoint: endpointToProto(discovery.LocalEndpoint),
		NatType:         natTypeToProto(discovery.NATType),
		BytesSent:       sent,
		BytesReceived:   received,
	})
	if err != nil {
		if ctx.Err() == nil {
			t.logf("heartbeat failed: %v", err)
		}
		return fallback
	}
	if secs := resp.GetNextHeartbeatSeconds(); secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return fallback
}

// peerUpdateLoop consumes the server-streaming peer topology feed,
// reconnecting with exponential backoff whenever the stream breaks.
func (t *Tunnel) peerUpdateLoop(ctx context.Context) {
	backoff := defaultStreamRetryBase

	for {
		if ctx.Err() != nil {
			return
		}

		t.mu.Lock()
		deviceID := t.deviceID
		t.mu.Unlock()
		if deviceID == "" {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}

		stream, err := t.coord.StreamPeerUpdates(ctx, deviceID)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			t.logf("peer stream failed: %v (retrying in %s)", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, defaultStreamRetryMax)
			continue
		}

		// A healthy stream resets the backoff.
		backoff = defaultStreamRetryBase
		t.consumeStream(ctx, stream)
	}
}

// consumeStream reads peer updates until the stream ends or errors.
func (t *Tunnel) consumeStream(ctx context.Context, stream PeerUpdateStream) {
	for {
		update, err := stream.Recv()
		if err != nil {
			if ctx.Err() == nil {
				t.logf("peer stream closed: %v", err)
			}
			return
		}
		t.applyPeerUpdate(ctx, update)
	}
}

// applyPeerUpdate reconciles one topology change.
func (t *Tunnel) applyPeerUpdate(ctx context.Context, update *coordinationv1.PeerUpdate) {
	p := update.GetPeer()
	if p == nil {
		return
	}
	deviceID, err := uuid.Parse(p.GetDeviceId())
	if err != nil {
		return
	}

	// Never treat ourselves as a peer.
	t.mu.Lock()
	self := t.deviceID
	t.mu.Unlock()
	if p.GetDeviceId() == self {
		return
	}

	switch update.GetType() {
	case coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_LEFT:
		t.removePeer(deviceID)

	case coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_JOINED:
		if err := t.addPeer(ctx, p); err != nil {
			t.logf("add peer: %v", err)
		}

	case coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_ENDPOINT_CHANGED,
		coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_STATUS_CHANGED:
		t.handleEndpointChange(ctx, deviceID, p)

	default:
		// An unknown update type still tells us the peer exists.
		if err := t.addPeer(ctx, p); err != nil {
			t.logf("add peer: %v", err)
		}
	}
}

// handleEndpointChange updates a peer's known endpoint and renegotiates if
// it moved (a NAT rebind, or a switch between Wi-Fi and cellular).
func (t *Tunnel) handleEndpointChange(ctx context.Context, deviceID uuid.UUID, p *coordinationv1.Peer) {
	t.mu.Lock()
	pr, known := t.peers[deviceID]
	t.mu.Unlock()

	if !known {
		if err := t.addPeer(ctx, p); err != nil {
			t.logf("add peer: %v", err)
		}
		return
	}

	newEndpoint := endpointFromProto(p.GetLastKnownEndpoint())
	if newEndpoint == nil {
		return
	}

	pr.mu.Lock()
	moved := pr.remote == nil || pr.remote.String() != newEndpoint.String()
	if moved {
		pr.remote = newEndpoint
	}
	mode := pr.mode
	pr.mu.Unlock()

	if !moved {
		return
	}

	t.logf("peer %s moved to %s", pr.deviceName, newEndpoint)
	// A peer that moved is worth re-punching: it may now be directly
	// reachable even if it previously wasn't.
	if mode != ModeDirect {
		go t.negotiate(ctx, pr)
		return
	}
	// On an established direct path, just follow the new endpoint.
	if err := t.dev.UpdateEndpoint(pr.publicKey, newEndpoint); err != nil {
		t.logf("peer %s: update endpoint: %v", pr.deviceName, err)
		go t.negotiate(ctx, pr)
	}
}

// monitorLoop watches handshake freshness and renegotiates dead paths,
// which is what makes reconnection automatic after a network change.
func (t *Tunnel) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(t.monitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.checkPeerHealth(ctx)
		}
	}
}

// checkPeerHealth renegotiates peers whose handshake has gone stale.
func (t *Tunnel) checkPeerHealth(ctx context.Context) {
	t.mu.Lock()
	peers := make([]*peer, 0, len(t.peers))
	for _, pr := range t.peers {
		peers = append(peers, pr)
	}
	t.mu.Unlock()

	for _, pr := range peers {
		stats, ok, err := t.dev.PeerStats(pr.publicKey)
		if err != nil || !ok {
			continue
		}

		fresh := !stats.LastHandshake.IsZero() && time.Since(stats.LastHandshake) < HandshakeStaleAfter
		if fresh {
			// A path that's working shouldn't be disturbed, but do record
			// that a peer we thought was offline has come back.
			pr.mu.Lock()
			if pr.mode == ModeOffline || pr.mode == ModeConnecting {
				if pr.proxy != nil {
					pr.mode = ModeRelay
				} else {
					pr.mode = ModeDirect
				}
			}
			pr.mu.Unlock()
			continue
		}

		pr.mu.Lock()
		mode := pr.mode
		since := time.Since(pr.lastAttempt)
		pr.mu.Unlock()

		// Give a brand-new peer time to finish its first negotiation before
		// declaring the path stale.
		if mode == ModeConnecting && since < t.renegotiateInterval {
			continue
		}
		if since < t.renegotiateInterval {
			continue
		}

		t.logf("peer %s: handshake stale, renegotiating", pr.deviceName)
		go t.negotiate(ctx, pr)
	}
}

// shutdownPeers releases every peer's resources.
func (t *Tunnel) shutdownPeers() {
	t.mu.Lock()
	peers := make([]*peer, 0, len(t.peers))
	for _, pr := range t.peers {
		peers = append(peers, pr)
	}
	t.peers = make(map[uuid.UUID]*peer)
	t.mu.Unlock()

	for _, pr := range peers {
		pr.close()
	}
}

// Status returns a snapshot of the tunnel and its peers.
func (t *Tunnel) Status() Status {
	t.mu.Lock()
	status := Status{
		Connected:   t.deviceID != "",
		NetworkID:   t.networkID,
		NetworkName: t.networkName,
		DeviceID:    t.deviceID,
		VirtualIP:   t.virtualIP,
		CIDR:        t.cidr,
		NATType:     t.discovery.NATType.String(),
	}
	if t.discovery.PublicEndpoint != nil {
		status.PublicEndpoint = t.discovery.PublicEndpoint.String()
	}
	peers := make([]*peer, 0, len(t.peers))
	for _, pr := range t.peers {
		peers = append(peers, pr)
	}
	t.mu.Unlock()

	if t.dev != nil {
		status.InterfaceName = t.dev.Name()
	}
	for _, pr := range peers {
		status.Peers = append(status.Peers, pr.snapshot(t.dev))
	}
	return status
}

// SetNetworkName records a human-readable network name for status output.
func (t *Tunnel) SetNetworkName(name string) {
	t.mu.Lock()
	t.networkName = name
	t.mu.Unlock()
}

// VirtualIP returns the address assigned by the control plane.
func (t *Tunnel) VirtualIP() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.virtualIP
}

// DNSServers returns the network's DNS servers, if any.
func (t *Tunnel) DNSServers() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dnsServers
}

// STUNDiscoverer adapts the stun package to EndpointDiscoverer.
type STUNDiscoverer struct {
	// Conn is the socket to probe from. For correct hole punching this
	// should be the same socket WireGuard uses, so the NAT mapping STUN
	// discovers is the one peers will actually reach.
	Conn net.PacketConn
	// PrimaryServer and SecondaryServer are STUN servers; two independent
	// servers are needed to detect symmetric NAT.
	PrimaryServer   string
	SecondaryServer string
}

// Discover implements EndpointDiscoverer.
func (d STUNDiscoverer) Discover(ctx context.Context) (DiscoveryResult, error) {
	result, err := stun.Classify(ctx, d.Conn, d.PrimaryServer, d.SecondaryServer)
	if err != nil {
		return DiscoveryResult{}, err
	}

	out := DiscoveryResult{NATType: result.Type}
	if result.PublicAddr.IsValid() {
		out.PublicEndpoint = net.UDPAddrFromAddrPort(result.PublicAddr)
	}
	if result.LocalAddr.IsValid() {
		out.LocalEndpoint = net.UDPAddrFromAddrPort(result.LocalAddr)
	}
	return out, nil
}
