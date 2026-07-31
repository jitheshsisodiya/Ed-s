package tunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/client/internal/stun"
	"github.com/jitheshsisodiya/Ed-s/client/internal/wireguard"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/client/internal/coordination/gen"
)

// --- Fakes ---

// fakeDevice records the WireGuard operations the orchestrator performs and
// lets tests control the handshake timestamps that drive its state machine.
type fakeDevice struct {
	mu sync.Mutex

	peers     map[string]wireguard.PeerConfig
	handshake map[string]time.Time
	removed   []string
	upserts   int
	closed    bool
}

func newFakeDevice() *fakeDevice {
	return &fakeDevice{
		peers:     map[string]wireguard.PeerConfig{},
		handshake: map[string]time.Time{},
	}
}

func (d *fakeDevice) Name() string { return "nexus-test0" }

func (d *fakeDevice) ListenPort() (int, error) { return 51820, nil }

func (d *fakeDevice) UpsertPeer(p wireguard.PeerConfig) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.peers[p.PublicKeyBase64] = p
	d.upserts++
	return nil
}

func (d *fakeDevice) UpdateEndpoint(key string, endpoint *net.UDPAddr) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.peers[key]
	if !ok {
		return errors.New("no such peer")
	}
	p.Endpoint = endpoint
	d.peers[key] = p
	return nil
}

func (d *fakeDevice) RemovePeer(key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.peers, key)
	d.removed = append(d.removed, key)
	return nil
}

func (d *fakeDevice) PeerStats(key string) (wireguard.PeerStats, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.peers[key]
	if !ok {
		return wireguard.PeerStats{}, false, nil
	}
	stats := wireguard.PeerStats{
		PublicKeyBase64: key,
		LastHandshake:   d.handshake[key],
		TxBytes:         100,
		RxBytes:         200,
	}
	if p.Endpoint != nil {
		stats.Endpoint = p.Endpoint.String()
	}
	return stats, true, nil
}

func (d *fakeDevice) Stats() ([]wireguard.PeerStats, error) {
	d.mu.Lock()
	keys := make([]string, 0, len(d.peers))
	for k := range d.peers {
		keys = append(keys, k)
	}
	d.mu.Unlock()

	out := make([]wireguard.PeerStats, 0, len(keys))
	for _, k := range keys {
		s, ok, _ := d.PeerStats(k)
		if ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (d *fakeDevice) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	return nil
}

// setHandshake simulates a completed WireGuard handshake with a peer.
func (d *fakeDevice) setHandshake(key string, at time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.handshake[key] = at
}

func (d *fakeDevice) peerConfig(key string) (wireguard.PeerConfig, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	p, ok := d.peers[key]
	return p, ok
}

func (d *fakeDevice) peerCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.peers)
}

// fakeCoordinator implements Coordinator with scriptable responses.
type fakeCoordinator struct {
	mu sync.Mutex

	registerResp *coordinationv1.RegisterDeviceResponse
	registerErr  error
	registerReq  *coordinationv1.RegisterDeviceRequest

	heartbeats  []*coordinationv1.HeartbeatRequest
	heartbeatCh chan struct{}
	nextBeat    int32

	iceResp *coordinationv1.ICEExchangeResponse
	iceErr  error
	iceReqs []*coordinationv1.ICEExchangeRequest

	relayResp *coordinationv1.RequestRelayResponse
	relayErr  error
	relayReqs []*coordinationv1.RequestRelayRequest

	stream    *fakeStream
	streamErr error
}

func newFakeCoordinator() *fakeCoordinator {
	return &fakeCoordinator{
		registerResp: &coordinationv1.RegisterDeviceResponse{
			DeviceId:          uuid.NewString(),
			AssignedVirtualIp: "10.77.0.2",
			NetworkCidr:       "10.77.0.0/24",
			DnsServers:        []string{"1.1.1.1"},
		},
		heartbeatCh: make(chan struct{}, 32),
		stream:      newFakeStream(),
	}
}

func (c *fakeCoordinator) RegisterDevice(_ context.Context, req *coordinationv1.RegisterDeviceRequest) (*coordinationv1.RegisterDeviceResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.registerReq = req
	if c.registerErr != nil {
		return nil, c.registerErr
	}
	return c.registerResp, nil
}

func (c *fakeCoordinator) Heartbeat(_ context.Context, req *coordinationv1.HeartbeatRequest) (*coordinationv1.HeartbeatResponse, error) {
	c.mu.Lock()
	c.heartbeats = append(c.heartbeats, req)
	next := c.nextBeat
	c.mu.Unlock()

	select {
	case c.heartbeatCh <- struct{}{}:
	default:
	}
	return &coordinationv1.HeartbeatResponse{Ok: true, NextHeartbeatSeconds: next}, nil
}

func (c *fakeCoordinator) StreamPeerUpdates(ctx context.Context, _ string) (PeerUpdateStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.streamErr != nil {
		return nil, c.streamErr
	}
	// Bind the stream to the caller's context, matching gRPC: a cancelled
	// context aborts an in-flight Recv rather than blocking forever.
	return c.stream.withContext(ctx), nil
}

func (c *fakeCoordinator) ExchangeICECandidates(_ context.Context, req *coordinationv1.ICEExchangeRequest) (*coordinationv1.ICEExchangeResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.iceReqs = append(c.iceReqs, req)
	if c.iceErr != nil {
		return nil, c.iceErr
	}
	if c.iceResp != nil {
		return c.iceResp, nil
	}
	return &coordinationv1.ICEExchangeResponse{}, nil
}

func (c *fakeCoordinator) RequestRelay(_ context.Context, req *coordinationv1.RequestRelayRequest) (*coordinationv1.RequestRelayResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.relayReqs = append(c.relayReqs, req)
	if c.relayErr != nil {
		return nil, c.relayErr
	}
	return c.relayResp, nil
}

func (c *fakeCoordinator) relayCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.relayReqs)
}

func (c *fakeCoordinator) iceCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.iceReqs)
}

func (c *fakeCoordinator) lastHeartbeat() *coordinationv1.HeartbeatRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.heartbeats) == 0 {
		return nil
	}
	return c.heartbeats[len(c.heartbeats)-1]
}

// fakeStream is a controllable PeerUpdateStream.
type fakeStream struct {
	updates chan *coordinationv1.PeerUpdate
	closed  chan struct{}
	once    sync.Once
	// ctx binds the stream to the RPC's context, as a real gRPC stream is.
	ctx context.Context
}

func newFakeStream() *fakeStream {
	return &fakeStream{
		updates: make(chan *coordinationv1.PeerUpdate, 16),
		closed:  make(chan struct{}),
		ctx:     context.Background(),
	}
}

// withContext returns a view of the stream bound to ctx, sharing the same
// update channel so tests can keep pushing through the original handle.
func (s *fakeStream) withContext(ctx context.Context) *fakeStream {
	return &fakeStream{updates: s.updates, closed: s.closed, ctx: ctx}
}

func (s *fakeStream) Recv() (*coordinationv1.PeerUpdate, error) {
	select {
	case u := <-s.updates:
		return u, nil
	case <-s.closed:
		return nil, io.EOF
	case <-s.ctx.Done():
		return nil, s.ctx.Err()
	}
}

func (s *fakeStream) push(u *coordinationv1.PeerUpdate) { s.updates <- u }

func (s *fakeStream) close() { s.once.Do(func() { close(s.closed) }) }

// fakeDiscoverer returns a canned STUN result.
type fakeDiscoverer struct {
	result DiscoveryResult
	err    error
}

func (d fakeDiscoverer) Discover(context.Context) (DiscoveryResult, error) {
	return d.result, d.err
}

// --- Helpers ---

func testPeer(virtualIP string) *coordinationv1.Peer {
	return &coordinationv1.Peer{
		DeviceId:   uuid.NewString(),
		DeviceName: "peer-" + virtualIP,
		PublicKey:  "peerkey" + virtualIP,
		VirtualIp:  virtualIP,
		LastKnownEndpoint: &coordinationv1.Endpoint{
			Ip: "203.0.113.10", Port: 51820, Protocol: "udp",
		},
	}
}

func newTestTunnel(t *testing.T, dev *fakeDevice, coord *fakeCoordinator, opts ...func(*Options)) *Tunnel {
	t.Helper()
	o := Options{
		Device:      dev,
		Coordinator: coord,
		Discoverer: fakeDiscoverer{result: DiscoveryResult{
			PublicEndpoint: &net.UDPAddr{IP: net.ParseIP("198.51.100.5"), Port: 51820},
			LocalEndpoint:  &net.UDPAddr{IP: net.ParseIP("192.168.1.20"), Port: 51820},
			NATType:        stun.NATPortRestrictedCone,
		}},
		Prober:              func(*net.UDPAddr) error { return nil },
		NetworkID:           uuid.NewString(),
		DeviceName:          "test-device",
		OS:                  "linux",
		PublicKey:           "selfkey",
		PunchTimeout:        100 * time.Millisecond,
		HeartbeatInterval:   50 * time.Millisecond,
		MonitorInterval:     50 * time.Millisecond,
		RenegotiateInterval: 20 * time.Millisecond,
		Logf:                func(string, ...any) {},
	}
	for _, fn := range opts {
		fn(&o)
	}
	tun, err := New(o)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tun
}

// runTunnel starts a tunnel in the background and returns a stop function.
func runTunnel(t *testing.T, tun *Tunnel) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = tun.Start(ctx)
	}()
	stop := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("tunnel did not shut down within 5s")
		}
	}
	t.Cleanup(stop)
	return stop
}

// eventually polls until cond holds or the deadline passes.
func eventually(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// --- Tests ---

func TestNewValidatesRequiredOptions(t *testing.T) {
	cases := map[string]Options{
		"no device":      {Coordinator: newFakeCoordinator(), NetworkID: "n", PublicKey: "k"},
		"no coordinator": {Device: newFakeDevice(), NetworkID: "n", PublicKey: "k"},
		"no network":     {Device: newFakeDevice(), Coordinator: newFakeCoordinator(), PublicKey: "k"},
		"no public key":  {Device: newFakeDevice(), Coordinator: newFakeCoordinator(), NetworkID: "n"},
	}
	for name, opts := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := New(opts); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestRegisterStoresAssignedAddressing(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	tun := newTestTunnel(t, dev, coord)

	resp, err := tun.Register(context.Background())
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if resp.GetAssignedVirtualIp() != "10.77.0.2" {
		t.Fatalf("virtual IP = %q", resp.GetAssignedVirtualIp())
	}
	if tun.VirtualIP() != "10.77.0.2" {
		t.Fatalf("tunnel virtual IP = %q", tun.VirtualIP())
	}
	if got := tun.DNSServers(); len(got) != 1 || got[0] != "1.1.1.1" {
		t.Fatalf("dns servers = %v", got)
	}

	// The registration must carry this device's identity.
	coord.mu.Lock()
	req := coord.registerReq
	coord.mu.Unlock()
	if req.GetDevicePublicKey() != "selfkey" {
		t.Fatalf("public key = %q", req.GetDevicePublicKey())
	}
	if req.GetOs() != "linux" {
		t.Fatalf("os = %q", req.GetOs())
	}
}

func TestRegisterPropagatesError(t *testing.T) {
	coord := newFakeCoordinator()
	coord.registerErr = errors.New("control plane down")
	tun := newTestTunnel(t, newFakeDevice(), coord)

	if _, err := tun.Register(context.Background()); err == nil {
		t.Fatal("expected registration to fail")
	}
}

func TestExistingPeersAreInstalled(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	p := testPeer("10.77.0.3")
	coord.registerResp.ExistingPeers = []*coordinationv1.Peer{p}

	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	eventually(t, 2*time.Second, "peer installation", func() bool {
		return dev.peerCount() == 1
	})

	cfg, ok := dev.peerConfig(p.GetPublicKey())
	if !ok {
		t.Fatal("peer was not installed in WireGuard")
	}
	// AllowedIPs must be the peer's single /32, not a broad route.
	if len(cfg.AllowedIPs) != 1 {
		t.Fatalf("allowed IPs = %v", cfg.AllowedIPs)
	}
	ones, bits := cfg.AllowedIPs[0].Mask.Size()
	if ones != 32 || bits != 32 {
		t.Fatalf("allowed IP mask = /%d (of %d), want /32", ones, bits)
	}
	if !cfg.AllowedIPs[0].IP.Equal(net.ParseIP("10.77.0.3").To4()) {
		t.Fatalf("allowed IP = %v", cfg.AllowedIPs[0].IP)
	}
}

func TestHeartbeatReportsEndpointAndCounters(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	select {
	case <-coord.heartbeatCh:
	case <-time.After(2 * time.Second):
		t.Fatal("no heartbeat was sent")
	}

	eventually(t, 2*time.Second, "heartbeat contents", func() bool {
		hb := coord.lastHeartbeat()
		return hb != nil && hb.GetPublicEndpoint() != nil
	})

	hb := coord.lastHeartbeat()
	if hb.GetPublicEndpoint().GetIp() != "198.51.100.5" {
		t.Fatalf("public endpoint = %q", hb.GetPublicEndpoint().GetIp())
	}
	if hb.GetPrivateEndpoint().GetIp() != "192.168.1.20" {
		t.Fatalf("private endpoint = %q", hb.GetPrivateEndpoint().GetIp())
	}
	if hb.GetNatType() != coordinationv1.NATType_NAT_TYPE_PORT_RESTRICTED_CONE {
		t.Fatalf("nat type = %v", hb.GetNatType())
	}
	if hb.GetDeviceId() == "" {
		t.Fatal("heartbeat carried no device ID")
	}
}

func TestHeartbeatHonorsServerCadence(t *testing.T) {
	coord := newFakeCoordinator()
	coord.nextBeat = 1 // server asks for 1s
	tun := newTestTunnel(t, newFakeDevice(), coord)
	runTunnel(t, tun)

	// First heartbeat is immediate.
	select {
	case <-coord.heartbeatCh:
	case <-time.After(2 * time.Second):
		t.Fatal("no initial heartbeat")
	}

	// The configured 50ms interval would produce many beats in 500ms; the
	// server's 1s cadence should suppress them.
	time.Sleep(500 * time.Millisecond)
	coord.mu.Lock()
	count := len(coord.heartbeats)
	coord.mu.Unlock()
	if count > 2 {
		t.Fatalf("sent %d heartbeats in 500ms despite a 1s server cadence", count)
	}
}

func TestPeerJoinedAndLeftViaStream(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	p := testPeer("10.77.0.4")
	coord.stream.push(&coordinationv1.PeerUpdate{
		Type:      coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_JOINED,
		NetworkId: tun.Status().NetworkID,
		Peer:      p,
	})

	eventually(t, 2*time.Second, "peer join", func() bool { return dev.peerCount() == 1 })

	coord.stream.push(&coordinationv1.PeerUpdate{
		Type:      coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_LEFT,
		NetworkId: tun.Status().NetworkID,
		Peer:      p,
	})

	eventually(t, 2*time.Second, "peer removal", func() bool { return dev.peerCount() == 0 })

	dev.mu.Lock()
	removed := len(dev.removed)
	dev.mu.Unlock()
	if removed != 1 {
		t.Fatalf("RemovePeer called %d times, want 1", removed)
	}
}

func TestSelfUpdatesAreIgnored(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	eventually(t, 2*time.Second, "registration", func() bool { return tun.Status().DeviceID != "" })

	// The control plane echoing our own device back must not create a peer
	// pointing at ourselves.
	self := tun.Status().DeviceID
	coord.stream.push(&coordinationv1.PeerUpdate{
		Type: coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_JOINED,
		Peer: &coordinationv1.Peer{
			DeviceId:  self,
			PublicKey: "selfkey",
			VirtualIp: "10.77.0.2",
		},
	})

	time.Sleep(200 * time.Millisecond)
	if dev.peerCount() != 0 {
		t.Fatal("the local device was installed as its own peer")
	}
}

func TestSuccessfulHolePunchYieldsDirectMode(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	p := testPeer("10.77.0.5")
	coord.registerResp.ExistingPeers = []*coordinationv1.Peer{p}
	coord.iceResp = &coordinationv1.ICEExchangeResponse{
		PeerCandidates: []*coordinationv1.ICECandidate{{
			Endpoint: &coordinationv1.Endpoint{Ip: "203.0.113.10", Port: 51820, Protocol: "udp"},
			Type:     "srflx", Priority: 100,
		}},
	}

	// A handshake lands as soon as punching starts.
	dev.setHandshake(p.GetPublicKey(), time.Now().Add(time.Second))

	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	eventually(t, 3*time.Second, "direct connection", func() bool {
		for _, ps := range tun.Status().Peers {
			if ps.PublicKey == p.GetPublicKey() && ps.Mode == ModeDirect {
				return true
			}
		}
		return false
	})

	// A direct path means no relay was needed.
	if n := coord.relayCallCount(); n != 0 {
		t.Fatalf("requested a relay %d times despite a successful hole punch", n)
	}
	if coord.iceCallCount() == 0 {
		t.Fatal("expected ICE candidates to be exchanged")
	}
}

func TestFailedHolePunchFallsBackToRelay(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	p := testPeer("10.77.0.6")
	coord.registerResp.ExistingPeers = []*coordinationv1.Peer{p}

	// Start a stand-in relay so the proxy's BIND has somewhere to go.
	relayConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer relayConn.Close()
	relayAddr := relayConn.LocalAddr().(*net.UDPAddr)

	coord.relayResp = &coordinationv1.RequestRelayResponse{
		RelayId:      uuid.NewString(),
		Hostname:     relayAddr.IP.String(),
		RelayPort:    uint32(relayAddr.Port),
		SessionToken: "session-token-for-test",
	}

	// No handshake ever completes, so punching must time out.
	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	eventually(t, 5*time.Second, "relay fallback", func() bool {
		for _, ps := range tun.Status().Peers {
			if ps.PublicKey == p.GetPublicKey() && ps.Mode == ModeRelay {
				return true
			}
		}
		return false
	})

	if coord.relayCallCount() == 0 {
		t.Fatal("expected a relay to be requested")
	}

	// WireGuard must now point at the loopback proxy, with keepalives on:
	// a relayed path has no direct pinhole to maintain.
	cfg, ok := dev.peerConfig(p.GetPublicKey())
	if !ok {
		t.Fatal("peer missing")
	}
	if cfg.Endpoint == nil || !cfg.Endpoint.IP.IsLoopback() {
		t.Fatalf("endpoint = %v, want a loopback proxy address", cfg.Endpoint)
	}
	if cfg.PersistentKeepaliveSeconds == 0 {
		t.Fatal("expected persistent keepalive on a relayed peer")
	}

	// The relay should have received a BIND frame from the proxy.
	_ = relayConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 2048)
	n, _, err := relayConn.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("relay received no frame: %v", err)
	}
	if buf[0] != 0x01 {
		t.Fatalf("first frame type = 0x%02x, want 0x01 (BIND)", buf[0])
	}
	if string(buf[17:n]) != "session-token-for-test" {
		t.Fatalf("BIND carried token %q", string(buf[17:n]))
	}
}

func TestRelayFailureMarksPeerOffline(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	p := testPeer("10.77.0.7")
	coord.registerResp.ExistingPeers = []*coordinationv1.Peer{p}
	coord.relayErr = errors.New("no relays available")

	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	eventually(t, 5*time.Second, "offline marking", func() bool {
		for _, ps := range tun.Status().Peers {
			if ps.PublicKey == p.GetPublicKey() && ps.Mode == ModeOffline {
				return true
			}
		}
		return false
	})
}

func TestEndpointChangeFollowsPeerOnDirectPath(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	p := testPeer("10.77.0.8")
	coord.registerResp.ExistingPeers = []*coordinationv1.Peer{p}
	coord.iceResp = &coordinationv1.ICEExchangeResponse{
		PeerCandidates: []*coordinationv1.ICECandidate{{
			Endpoint: &coordinationv1.Endpoint{Ip: "203.0.113.10", Port: 51820, Protocol: "udp"},
			Type:     "srflx", Priority: 100,
		}},
	}
	dev.setHandshake(p.GetPublicKey(), time.Now().Add(time.Second))

	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	eventually(t, 3*time.Second, "direct connection", func() bool {
		for _, ps := range tun.Status().Peers {
			if ps.PublicKey == p.GetPublicKey() && ps.Mode == ModeDirect {
				return true
			}
		}
		return false
	})

	// The peer's NAT rebinds it to a new public endpoint.
	moved := &coordinationv1.Peer{
		DeviceId:   p.GetDeviceId(),
		DeviceName: p.GetDeviceName(),
		PublicKey:  p.GetPublicKey(),
		VirtualIp:  p.GetVirtualIp(),
		LastKnownEndpoint: &coordinationv1.Endpoint{
			Ip: "203.0.113.99", Port: 40000, Protocol: "udp",
		},
	}
	coord.stream.push(&coordinationv1.PeerUpdate{
		Type: coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_ENDPOINT_CHANGED,
		Peer: moved,
	})

	eventually(t, 3*time.Second, "endpoint follow", func() bool {
		cfg, ok := dev.peerConfig(p.GetPublicKey())
		return ok && cfg.Endpoint != nil && cfg.Endpoint.String() == "203.0.113.99:40000"
	})
}

func TestStreamReconnectsAfterFailure(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	tun := newTestTunnel(t, dev, coord)
	runTunnel(t, tun)

	// Let the first stream attach, then break it.
	eventually(t, 2*time.Second, "registration", func() bool { return tun.Status().DeviceID != "" })
	coord.stream.close()

	// A fresh stream should be established, and updates on it applied.
	coord.mu.Lock()
	coord.stream = newFakeStream()
	stream := coord.stream
	coord.mu.Unlock()

	eventually(t, 5*time.Second, "stream reconnect", func() bool {
		p := testPeer("10.77.0.9")
		select {
		case stream.updates <- &coordinationv1.PeerUpdate{
			Type: coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_JOINED,
			Peer: p,
		}:
		default:
		}
		return dev.peerCount() > 0
	})
}

func TestStatusReportsTunnelState(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	p := testPeer("10.77.0.10")
	coord.registerResp.ExistingPeers = []*coordinationv1.Peer{p}

	tun := newTestTunnel(t, dev, coord)
	tun.SetNetworkName("Home Lab")
	runTunnel(t, tun)

	eventually(t, 2*time.Second, "peer registration", func() bool {
		return len(tun.Status().Peers) == 1
	})

	status := tun.Status()
	if !status.Connected {
		t.Fatal("expected connected")
	}
	if status.NetworkName != "Home Lab" {
		t.Fatalf("network name = %q", status.NetworkName)
	}
	if status.VirtualIP != "10.77.0.2" {
		t.Fatalf("virtual IP = %q", status.VirtualIP)
	}
	if status.CIDR != "10.77.0.0/24" {
		t.Fatalf("cidr = %q", status.CIDR)
	}
	if status.InterfaceName != "nexus-test0" {
		t.Fatalf("interface = %q", status.InterfaceName)
	}
	if status.NATType != stun.NATPortRestrictedCone.String() {
		t.Fatalf("nat type = %q", status.NATType)
	}
	if status.PublicEndpoint != "198.51.100.5:51820" {
		t.Fatalf("public endpoint = %q", status.PublicEndpoint)
	}

	ps := status.Peers[0]
	if ps.VirtualIP != "10.77.0.10" {
		t.Fatalf("peer virtual IP = %q", ps.VirtualIP)
	}
	if ps.BytesSent != 100 || ps.BytesReceived != 200 {
		t.Fatalf("peer counters = %d/%d", ps.BytesSent, ps.BytesReceived)
	}
}

func TestShutdownIsClean(t *testing.T) {
	dev := newFakeDevice()
	coord := newFakeCoordinator()
	coord.registerResp.ExistingPeers = []*coordinationv1.Peer{testPeer("10.77.0.11")}

	tun := newTestTunnel(t, dev, coord)
	stop := runTunnel(t, tun)

	eventually(t, 2*time.Second, "peer install", func() bool { return dev.peerCount() == 1 })

	// Must return promptly on cancellation; runTunnel fails the test if not.
	stop()

	if len(tun.Status().Peers) != 0 {
		t.Fatal("peers should be released on shutdown")
	}
}

func TestStartTwiceIsRejected(t *testing.T) {
	tun := newTestTunnel(t, newFakeDevice(), newFakeCoordinator())
	runTunnel(t, tun)

	eventually(t, 2*time.Second, "first start", func() bool { return tun.Status().DeviceID != "" })

	if err := tun.Start(context.Background()); err == nil {
		t.Fatal("expected a second Start to be rejected")
	}
}

func TestAllowedIPsForRejectsBadInput(t *testing.T) {
	if _, err := allowedIPsFor(""); err == nil {
		t.Fatal("expected an error for an empty virtual IP")
	}
	if _, err := allowedIPsFor("not-an-ip"); err == nil {
		t.Fatal("expected an error for a malformed virtual IP")
	}

	v6, err := allowedIPsFor("fd00::1")
	if err != nil {
		t.Fatalf("IPv6: %v", err)
	}
	ones, bits := v6[0].Mask.Size()
	if ones != 128 || bits != 128 {
		t.Fatalf("IPv6 mask = /%d (of %d), want /128", ones, bits)
	}
}

func TestNATTypeMapping(t *testing.T) {
	cases := map[stun.NATType]coordinationv1.NATType{
		stun.NATOpen:               coordinationv1.NATType_NAT_TYPE_OPEN,
		stun.NATFullCone:           coordinationv1.NATType_NAT_TYPE_FULL_CONE,
		stun.NATRestrictedCone:     coordinationv1.NATType_NAT_TYPE_RESTRICTED_CONE,
		stun.NATPortRestrictedCone: coordinationv1.NATType_NAT_TYPE_PORT_RESTRICTED_CONE,
		stun.NATSymmetric:          coordinationv1.NATType_NAT_TYPE_SYMMETRIC,
		stun.NATUnknown:            coordinationv1.NATType_NAT_TYPE_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := natTypeToProto(in); got != want {
			t.Errorf("natTypeToProto(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestEndpointConversion(t *testing.T) {
	if endpointToProto(nil) != nil {
		t.Fatal("nil address should convert to a nil endpoint")
	}
	if endpointFromProto(nil) != nil {
		t.Fatal("nil endpoint should convert to a nil address")
	}
	if endpointFromProto(&coordinationv1.Endpoint{Ip: "bad", Port: 1}) != nil {
		t.Fatal("a malformed IP should convert to nil")
	}
	if endpointFromProto(&coordinationv1.Endpoint{Ip: "1.2.3.4", Port: 0}) != nil {
		t.Fatal("a zero port should convert to nil")
	}

	addr := endpointFromProto(&coordinationv1.Endpoint{Ip: "1.2.3.4", Port: 51820})
	if addr == nil || addr.String() != "1.2.3.4:51820" {
		t.Fatalf("round trip = %v", addr)
	}
}
