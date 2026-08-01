package tunnel

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/client/internal/holepunch"
	"github.com/jitheshsisodiya/Ed-s/client/internal/relayproxy"
	"github.com/jitheshsisodiya/Ed-s/client/internal/wireguard"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/client/internal/coordination/gen"
)

// HandshakeStaleAfter is how long without a WireGuard handshake before a
// peer's path is treated as dead and renegotiated. WireGuard rekeys every
// ~2 minutes on an active session, so three minutes of silence means the
// path is genuinely broken rather than merely idle.
const HandshakeStaleAfter = 3 * time.Minute

// relayKeepalive keeps NAT mappings open on a relayed path, where there is
// no direct pinhole to maintain.
const relayKeepaliveSeconds = 25

// peer holds the orchestrator's per-peer connection state.
type peer struct {
	mu sync.Mutex

	deviceID   uuid.UUID
	deviceName string
	deviceOS   string
	exitNode   bool
	publicKey  string
	virtualIP  string

	// remote is the peer's last known public endpoint from the control plane.
	remote *net.UDPAddr
	// localCandidate is the peer's LAN address, if it advertised one.
	localCandidate *net.UDPAddr

	mode ConnectionMode
	// proxy is non-nil while this peer is relayed.
	proxy *relayproxy.Proxy
	// lastAttempt throttles renegotiation so a permanently unreachable peer
	// doesn't spin.
	lastAttempt time.Time
}

// snapshot builds a PeerStatus, merging live WireGuard counters and the
// most recent measured round-trip time.
func (p *peer) snapshot(dev WireGuardDevice, paths PathProber) PeerStatus {
	p.mu.Lock()
	status := PeerStatus{
		DeviceID:   p.deviceID.String(),
		DeviceName: p.deviceName,
		OS:         p.deviceOS,
		ExitNode:   p.exitNode,
		PublicKey:  p.publicKey,
		VirtualIP:  p.virtualIP,
		Mode:       p.mode,
		LatencyMs:  -1,
	}
	if p.remote != nil {
		status.Endpoint = p.remote.String()
	}
	p.mu.Unlock()

	// -1 means "not measured yet"; a real probe overwrites it.
	if paths != nil {
		if res, ok := paths.Result(p.deviceID); ok {
			status.LatencyMs = int(res.RTT.Milliseconds())
		}
	}

	if dev == nil {
		return status
	}
	stats, ok, err := dev.PeerStats(p.publicKey)
	if err != nil || !ok {
		return status
	}
	status.LastHandshake = stats.LastHandshake
	status.BytesSent = stats.TxBytes
	status.BytesReceived = stats.RxBytes
	if stats.Endpoint != "" {
		status.Endpoint = stats.Endpoint
	}
	return status
}

// close tears down any relay proxy held by the peer.
func (p *peer) close() {
	p.mu.Lock()
	proxy := p.proxy
	p.proxy = nil
	p.mu.Unlock()

	if proxy != nil {
		_ = proxy.Close()
	}
}

// addPeer installs a peer in WireGuard and starts negotiating a path to it.
func (t *Tunnel) addPeer(ctx context.Context, p *coordinationv1.Peer) error {
	deviceID, err := uuid.Parse(p.GetDeviceId())
	if err != nil {
		return fmt.Errorf("tunnel: malformed peer device id %q: %w", p.GetDeviceId(), err)
	}
	if p.GetPublicKey() == "" {
		return fmt.Errorf("tunnel: peer %s has no public key", deviceID)
	}

	allowed, err := allowedIPsFor(p.GetVirtualIp())
	if err != nil {
		return err
	}

	pr := &peer{
		deviceID:   deviceID,
		deviceName: p.GetDeviceName(),
		deviceOS:   p.GetOs(),
		exitNode:   p.GetExitNode(),
		publicKey:  p.GetPublicKey(),
		virtualIP:  p.GetVirtualIp(),
		remote:     endpointFromProto(p.GetLastKnownEndpoint()),
		mode:       ModeConnecting,
	}

	t.mu.Lock()
	if existing, ok := t.peers[deviceID]; ok {
		// Already known: refresh what we learned and let the existing
		// negotiation continue.
		t.mu.Unlock()
		existing.mu.Lock()
		existing.deviceName = p.GetDeviceName()
		if os := p.GetOs(); os != "" {
			existing.deviceOS = os
		}
		existing.exitNode = p.GetExitNode()
		if ep := endpointFromProto(p.GetLastKnownEndpoint()); ep != nil {
			existing.remote = ep
		}
		existing.mu.Unlock()
		return nil
	}
	t.peers[deviceID] = pr
	t.mu.Unlock()

	// Install the peer with its last known endpoint (if any) so WireGuard
	// can start handshaking immediately; the negotiation below upgrades or
	// replaces that path.
	cfg := wireguard.PeerConfig{
		PublicKeyBase64: pr.publicKey,
		Endpoint:        pr.remote,
		AllowedIPs:      allowed,
	}
	if err := t.dev.UpsertPeer(cfg); err != nil {
		return fmt.Errorf("tunnel: add wireguard peer: %w", err)
	}

	t.logf("peer %s (%s) added at %s", pr.deviceName, pr.virtualIP, endpointString(pr.remote))
	go t.negotiate(ctx, pr)
	return nil
}

// removePeer drops a peer from WireGuard and releases its resources.
func (t *Tunnel) removePeer(deviceID uuid.UUID) {
	t.mu.Lock()
	pr, ok := t.peers[deviceID]
	if ok {
		delete(t.peers, deviceID)
	}
	t.mu.Unlock()
	if !ok {
		return
	}

	pr.close()
	if err := t.dev.RemovePeer(pr.publicKey); err != nil {
		t.logf("remove wireguard peer %s: %v", pr.deviceName, err)
	}
	t.logf("peer %s removed", pr.deviceName)
}

// negotiate drives one peer from "connecting" to either a direct or relayed
// path, per the strategy in docs/architecture.md: exchange ICE candidates,
// attempt a simultaneous UDP hole punch, and fall back to a relay if no
// handshake completes in time.
func (t *Tunnel) negotiate(ctx context.Context, pr *peer) {
	pr.mu.Lock()
	// Throttle: don't renegotiate the same peer more than once per interval.
	if !pr.lastAttempt.IsZero() && time.Since(pr.lastAttempt) < t.renegotiateInterval {
		pr.mu.Unlock()
		return
	}
	pr.lastAttempt = time.Now()
	pr.mu.Unlock()

	if t.tryDirect(ctx, pr) {
		return
	}
	if ctx.Err() != nil {
		return
	}
	t.fallBackToRelay(ctx, pr)
}

// tryDirect exchanges candidates and attempts a hole punch. It reports
// whether a direct path was established.
func (t *Tunnel) tryDirect(ctx context.Context, pr *peer) bool {
	candidates, err := t.exchangeCandidates(ctx, pr)
	if err != nil {
		t.logf("peer %s: candidate exchange failed: %v", pr.deviceName, err)
	}

	// Even with no candidates from the peer yet, the last known endpoint
	// from the control plane is worth punching at.
	pr.mu.Lock()
	if pr.remote != nil {
		candidates = append(candidates, holepunch.Candidate{
			Addr: pr.remote, Type: "srflx", Priority: 100,
		})
	}
	pr.mu.Unlock()

	if len(candidates) == 0 {
		return false
	}

	// Baseline: only a handshake *after* this counts as caused by punching.
	var before time.Time
	if stats, ok, err := t.dev.PeerStats(pr.publicKey); err == nil && ok {
		before = stats.LastHandshake
	}

	result := holepunch.Punch(ctx, before, candidates, t.prober, func() (time.Time, error) {
		stats, ok, err := t.dev.PeerStats(pr.publicKey)
		if err != nil || !ok {
			return time.Time{}, err
		}
		return stats.LastHandshake, nil
	}, holepunch.WithTimeout(t.punchTimeout))

	if !result.Success {
		t.logf("peer %s: direct connection failed after %d candidates", pr.deviceName, len(result.AttemptedCandidates))
		return false
	}

	// Pin the endpoint WireGuard actually completed a handshake on.
	pr.mu.Lock()
	pr.mode = ModeDirect
	// Any relay path is now redundant.
	proxy := pr.proxy
	pr.proxy = nil
	pr.mu.Unlock()
	if proxy != nil {
		_ = proxy.Close()
	}

	t.logf("peer %s: direct connection established in %s", pr.deviceName, result.Elapsed.Round(time.Millisecond))
	return true
}

// exchangeCandidates publishes this device's candidates to a peer via the
// control plane and returns the peer's candidates.
func (t *Tunnel) exchangeCandidates(ctx context.Context, pr *peer) ([]holepunch.Candidate, error) {
	t.mu.Lock()
	discovery := t.discovery
	deviceID := t.deviceID
	t.mu.Unlock()

	var mine []*coordinationv1.ICECandidate
	if discovery.PublicEndpoint != nil {
		mine = append(mine, &coordinationv1.ICECandidate{
			Endpoint: endpointToProto(discovery.PublicEndpoint),
			Type:     "srflx",
			Priority: 100,
		})
	}
	if discovery.LocalEndpoint != nil {
		// Host candidates outrank server-reflexive ones: two peers on the
		// same LAN should talk directly rather than hairpin through the NAT.
		mine = append(mine, &coordinationv1.ICECandidate{
			Endpoint: endpointToProto(discovery.LocalEndpoint),
			Type:     "host",
			Priority: 200,
		})
	}

	rpcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	resp, err := t.coord.ExchangeICECandidates(rpcCtx, &coordinationv1.ICEExchangeRequest{
		DeviceId:       deviceID,
		TargetDeviceId: pr.deviceID.String(),
		Candidates:     mine,
	})
	if err != nil {
		return nil, err
	}

	var out []holepunch.Candidate
	for _, c := range resp.GetPeerCandidates() {
		addr := endpointFromProto(c.GetEndpoint())
		if addr == nil {
			continue
		}
		priority := c.GetPriority()
		if priority == 0 {
			priority = 50
		}
		out = append(out, holepunch.Candidate{Addr: addr, Type: c.GetType(), Priority: priority})

		if c.GetType() == "host" {
			pr.mu.Lock()
			pr.localCandidate = addr
			pr.mu.Unlock()
		}
	}
	return out, nil
}

// fallBackToRelay allocates a relay and repoints the peer's WireGuard
// endpoint at a local proxy that speaks the relay protocol.
func (t *Tunnel) fallBackToRelay(ctx context.Context, pr *peer) {
	t.mu.Lock()
	deviceID := t.deviceID
	networkID := t.networkID
	region := t.preferredRegion
	t.mu.Unlock()

	rpcCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	alloc, err := t.coord.RequestRelay(rpcCtx, &coordinationv1.RequestRelayRequest{
		DeviceId:        deviceID,
		NetworkId:       networkID,
		PreferredRegion: region,
	})
	if err != nil {
		t.logf("peer %s: relay allocation failed: %v", pr.deviceName, err)
		t.setMode(pr, ModeOffline)
		return
	}

	relayAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(alloc.GetHostname(), fmt.Sprint(alloc.GetRelayPort())))
	if err != nil {
		t.logf("peer %s: resolve relay %s: %v", pr.deviceName, alloc.GetHostname(), err)
		t.setMode(pr, ModeOffline)
		return
	}

	selfID, err := uuid.Parse(deviceID)
	if err != nil {
		t.logf("relay fallback: malformed local device id: %v", err)
		t.setMode(pr, ModeOffline)
		return
	}

	proxy, err := relayproxy.New(relayproxy.Options{
		RelayAddr:    relayAddr,
		SessionToken: alloc.GetSessionToken(),
		SelfDeviceID: selfID,
		PeerDeviceID: pr.deviceID,
		Logf:         t.logf,
	})
	if err != nil {
		t.logf("peer %s: relay proxy setup failed: %v", pr.deviceName, err)
		t.setMode(pr, ModeOffline)
		return
	}
	if err := proxy.Start(ctx); err != nil {
		_ = proxy.Close()
		t.logf("peer %s: relay proxy start failed: %v", pr.deviceName, err)
		t.setMode(pr, ModeOffline)
		return
	}

	// Point WireGuard at the loopback proxy and enable keepalives, since a
	// relayed path has no direct pinhole to maintain.
	allowed, err := allowedIPsFor(pr.virtualIP)
	if err != nil {
		_ = proxy.Close()
		t.logf("peer %s: %v", pr.deviceName, err)
		return
	}
	err = t.dev.UpsertPeer(wireguard.PeerConfig{
		PublicKeyBase64:            pr.publicKey,
		Endpoint:                   proxy.LocalEndpoint(),
		AllowedIPs:                 allowed,
		PersistentKeepaliveSeconds: relayKeepaliveSeconds,
	})
	if err != nil {
		_ = proxy.Close()
		t.logf("peer %s: repoint to relay failed: %v", pr.deviceName, err)
		t.setMode(pr, ModeOffline)
		return
	}

	pr.mu.Lock()
	old := pr.proxy
	pr.proxy = proxy
	pr.mode = ModeRelay
	pr.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}

	t.logf("peer %s: relaying via %s:%d", pr.deviceName, alloc.GetHostname(), alloc.GetRelayPort())
}

// setMode updates a peer's connection mode under its lock.
func (t *Tunnel) setMode(pr *peer, mode ConnectionMode) {
	pr.mu.Lock()
	pr.mode = mode
	pr.mu.Unlock()
}

// allowedIPsFor builds the AllowedIPs for a peer: just its single virtual
// address, which is what makes this a mesh of /32s rather than a
// default-route VPN.
func allowedIPsFor(virtualIP string) ([]net.IPNet, error) {
	if virtualIP == "" {
		return nil, fmt.Errorf("tunnel: peer has no virtual IP")
	}
	ip := net.ParseIP(virtualIP)
	if ip == nil {
		return nil, fmt.Errorf("tunnel: malformed virtual IP %q", virtualIP)
	}
	if v4 := ip.To4(); v4 != nil {
		return []net.IPNet{{IP: v4, Mask: net.CIDRMask(32, 32)}}, nil
	}
	return []net.IPNet{{IP: ip, Mask: net.CIDRMask(128, 128)}}, nil
}

// endpointString renders an endpoint for logs, tolerating nil.
func endpointString(addr *net.UDPAddr) string {
	if addr == nil {
		return "unknown"
	}
	return addr.String()
}
