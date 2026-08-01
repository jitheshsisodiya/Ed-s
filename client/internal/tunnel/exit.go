package tunnel

import (
	"fmt"
	"net"
	"net/netip"
	"sync"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/client/internal/killswitch"
	"github.com/jitheshsisodiya/Ed-s/client/internal/netroute"
	"github.com/jitheshsisodiya/Ed-s/client/internal/wireguard"
)

// exitAllowedIPs is what an exit node's peer entry is widened to: the whole
// address space, in both families.
//
// WireGuard treats AllowedIPs as a routing table and a filter at once, so
// this is the line that makes the peer eligible to carry everything. It has
// to be paired with the host routes netroute installs — one without the
// other gives you a tunnel that accepts every packet and never receives
// one, or a routing table that points at a peer refusing to decrypt.
var exitAllowedIPs = []string{"0.0.0.0/0", "::/0"}

// exitState is everything the tunnel has to undo when exit-node mode ends.
//
// It is recorded rather than recomputed because teardown runs on paths
// where the world has already changed — the peer may be gone, the default
// route may have moved, the network may be down. Undoing what was actually
// done is the only reliable option.
type exitState struct {
	mu sync.Mutex

	// deviceID is the peer currently carrying the default route.
	deviceID uuid.UUID
	// priorDefault is the host's routing as it was before we touched it.
	priorDefault netroute.Default
	// pinned are the endpoint addresses routed around the tunnel.
	pinned []net.IP
	// captured records that the default route is currently ours, so
	// release is not attempted against a route we never installed.
	captured bool
	// locked records that the kill switch is engaged.
	locked bool
}

// ExitNodeOptions configures exit-node mode.
type ExitNodeOptions struct {
	// KillSwitch blocks unprotected traffic while the tunnel is down.
	// Off by default: it is a promise to break someone's internet on
	// their behalf, and that has to be asked for.
	KillSwitch bool
	// AllowLAN keeps the local subnet reachable under the kill switch.
	// Defaults on, because cutting a user off from their own printer
	// protects them from nothing.
	AllowLAN bool
}

// SetExitNode routes all of this machine's traffic through one peer.
//
// The order is not arbitrary and cannot be rearranged. The peer's entry is
// widened first, so the tunnel can carry general traffic before anything
// depends on it; then the peer's real endpoint is pinned around the tunnel,
// because the moment after that the default route moves and un-pinned
// transport would be swallowed; then the default is captured. If any step
// fails the ones before it are undone, because a half-applied exit node is
// a machine with no working route.
func (t *Tunnel) SetExitNode(deviceID uuid.UUID, opts ExitNodeOptions) error {
	if t.router == nil {
		return fmt.Errorf("tunnel: exit-node mode needs a router")
	}

	t.mu.Lock()
	pr, ok := t.peers[deviceID]
	iface := t.dev.Name()
	t.mu.Unlock()
	if !ok {
		return fmt.Errorf("tunnel: %s is not a peer on this network", deviceID)
	}

	// Anything already in place is torn down first: switching exit nodes is
	// a normal thing to do, and leaving the old one's routes behind would
	// send traffic to a peer that is no longer carrying it.
	if err := t.ClearExitNode(); err != nil {
		return err
	}

	prior, err := t.router.Snapshot()
	if err != nil {
		return fmt.Errorf("tunnel: exit-node mode needs a working default route: %w", err)
	}

	if err := t.widenPeer(pr); err != nil {
		return err
	}

	// Everything this host sends to the internet on the tunnel's behalf:
	// the exit node's own endpoint, plus every other peer's, because those
	// paths have to survive too. A relayed peer's real destination is the
	// relay, not the loopback address WireGuard was handed.
	endpoints := t.transportEndpoints()

	var pinned []net.IP
	for _, ip := range endpoints {
		if err := t.router.PinEndpoint(ip, prior); err != nil {
			t.unpinAll(pinned)
			t.narrowPeer(pr)
			return fmt.Errorf("tunnel: pin %s around the tunnel: %w", ip, err)
		}
		pinned = append(pinned, ip)
	}

	if err := t.router.CaptureDefault(iface); err != nil {
		t.unpinAll(pinned)
		t.narrowPeer(pr)
		return fmt.Errorf("tunnel: capture the default route: %w", err)
	}

	t.exit.mu.Lock()
	t.exit.deviceID = deviceID
	t.exit.priorDefault = prior
	t.exit.pinned = pinned
	t.exit.captured = true
	t.exit.mu.Unlock()

	t.logf("exit node: all traffic now leaves through %s", pr.deviceName)

	if opts.KillSwitch {
		if err := t.engageKillSwitch(iface, endpoints, opts.AllowLAN); err != nil {
			// The route is up and working; only the safety net failed.
			// Tearing the tunnel down over that would be a worse outcome
			// than reporting it, so the caller decides.
			return fmt.Errorf("tunnel: exit node is up but the kill switch did not engage: %w", err)
		}
	}
	return nil
}

// ClearExitNode restores ordinary split-tunnel routing.
//
// Every step is attempted even if an earlier one failed: a machine left
// with a captured default and no tunnel is a machine with no internet, so
// the priority is undoing as much as possible, not stopping at the first
// error.
func (t *Tunnel) ClearExitNode() error {
	t.exit.mu.Lock()
	state := exitState{
		deviceID:     t.exit.deviceID,
		priorDefault: t.exit.priorDefault,
		pinned:       t.exit.pinned,
		captured:     t.exit.captured,
		locked:       t.exit.locked,
	}
	t.exit.deviceID = uuid.Nil
	t.exit.pinned = nil
	t.exit.captured = false
	t.exit.locked = false
	t.exit.mu.Unlock()

	if state.deviceID == uuid.Nil && !state.captured && !state.locked {
		return nil
	}

	var firstErr error
	note := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	// The kill switch goes first. Releasing it early means that if a later
	// step fails, the user is left with an intact original route and an
	// unblocked machine rather than a locked one.
	if state.locked && t.killSwitch != nil {
		note(t.killSwitch.Release())
	}

	if state.captured && t.router != nil {
		note(t.router.ReleaseDefault(t.dev.Name()))
	}
	if t.router != nil {
		t.unpinAll(state.pinned)
	}

	if state.deviceID != uuid.Nil {
		t.mu.Lock()
		pr := t.peers[state.deviceID]
		t.mu.Unlock()
		if pr != nil {
			note(t.narrowPeer(pr))
		}
	}

	if state.captured {
		t.logf("exit node: traffic is back on the ordinary route")
	}
	return firstErr
}

// ExitNode reports the peer currently carrying the default route, if any.
func (t *Tunnel) ExitNode() (uuid.UUID, bool) {
	t.exit.mu.Lock()
	defer t.exit.mu.Unlock()
	return t.exit.deviceID, t.exit.deviceID != uuid.Nil
}

// KillSwitchEngaged reports whether traffic is currently being blocked.
func (t *Tunnel) KillSwitchEngaged() bool {
	t.exit.mu.Lock()
	defer t.exit.mu.Unlock()
	return t.exit.locked
}

// widenPeer gives a peer the whole address space.
func (t *Tunnel) widenPeer(pr *peer) error {
	allowed, err := parseAllowedIPs(exitAllowedIPs)
	if err != nil {
		return err
	}
	return t.updatePeerAllowedIPs(pr, allowed)
}

// narrowPeer puts a peer back to its own single address.
func (t *Tunnel) narrowPeer(pr *peer) error {
	pr.mu.Lock()
	virtualIP := pr.virtualIP
	pr.mu.Unlock()

	allowed, err := allowedIPsFor(virtualIP)
	if err != nil {
		return err
	}
	return t.updatePeerAllowedIPs(pr, allowed)
}

func (t *Tunnel) updatePeerAllowedIPs(pr *peer, allowed []net.IPNet) error {
	pr.mu.Lock()
	cfg := wireguard.PeerConfig{
		PublicKeyBase64: pr.publicKey,
		Endpoint:        pr.remote,
		AllowedIPs:      allowed,
	}
	if pr.proxy != nil {
		// A relayed peer keeps its keepalive: the relay's NAT mapping has
		// to stay warm, and widening AllowedIPs must not quietly turn that
		// off and let the path go cold.
		cfg.Endpoint = pr.proxy.LocalEndpoint()
		cfg.PersistentKeepaliveSeconds = relayKeepaliveSeconds
	}
	pr.mu.Unlock()

	if err := t.dev.UpsertPeer(cfg); err != nil {
		return fmt.Errorf("tunnel: update allowed IPs for %s: %w", pr.deviceName, err)
	}
	return nil
}

// transportEndpoints lists every real address this host sends tunnel
// traffic to. A relayed peer contributes the relay's address, not the
// loopback one WireGuard was given, because loopback is not where the
// packet ends up.
func (t *Tunnel) transportEndpoints() []net.IP {
	t.mu.Lock()
	peers := make([]*peer, 0, len(t.peers))
	for _, pr := range t.peers {
		peers = append(peers, pr)
	}
	t.mu.Unlock()

	seen := map[string]bool{}
	var out []net.IP
	add := func(addr *net.UDPAddr) {
		if addr == nil || addr.IP == nil || addr.IP.IsLoopback() {
			return
		}
		if key := addr.IP.String(); !seen[key] {
			seen[key] = true
			out = append(out, addr.IP)
		}
	}

	for _, pr := range peers {
		pr.mu.Lock()
		remote, proxy := pr.remote, pr.proxy
		pr.mu.Unlock()
		if proxy != nil {
			add(proxy.RemoteEndpoint())
			continue
		}
		add(remote)
	}
	return out
}

// transportAddrPorts is transportEndpoints with the ports attached, which
// is what a firewall rule needs.
func (t *Tunnel) transportAddrPorts() []netip.AddrPort {
	t.mu.Lock()
	peers := make([]*peer, 0, len(t.peers))
	for _, pr := range t.peers {
		peers = append(peers, pr)
	}
	t.mu.Unlock()

	seen := map[netip.AddrPort]bool{}
	var out []netip.AddrPort
	add := func(addr *net.UDPAddr) {
		if addr == nil || addr.IP == nil || addr.IP.IsLoopback() {
			return
		}
		ip, ok := netip.AddrFromSlice(addr.IP)
		if !ok {
			return
		}
		ap := netip.AddrPortFrom(ip.Unmap(), uint16(addr.Port))
		if !seen[ap] {
			seen[ap] = true
			out = append(out, ap)
		}
	}

	for _, pr := range peers {
		pr.mu.Lock()
		remote, proxy := pr.remote, pr.proxy
		pr.mu.Unlock()
		if proxy != nil {
			add(proxy.RemoteEndpoint())
			continue
		}
		add(remote)
	}
	return out
}

func (t *Tunnel) engageKillSwitch(iface string, _ []net.IP, allowLAN bool) error {
	if t.killSwitch == nil {
		return fmt.Errorf("tunnel: no kill switch is available on this platform build")
	}

	policy := killswitch.Policy{
		Interface: iface,
		Endpoints: t.transportAddrPorts(),
		AllowLAN:  allowLAN,
	}
	if allowLAN {
		local, err := killswitch.DefaultLocalNetworks(iface)
		if err != nil {
			return err
		}
		policy.LocalNetworks = local
	}

	if err := t.killSwitch.Engage(policy); err != nil {
		return err
	}

	t.exit.mu.Lock()
	t.exit.locked = true
	t.exit.mu.Unlock()
	t.logf("kill switch: traffic outside the tunnel is now blocked")
	return nil
}

func (t *Tunnel) unpinAll(ips []net.IP) {
	for _, ip := range ips {
		if err := t.router.UnpinEndpoint(ip); err != nil {
			t.logf("exit node: could not remove the route pinned for %s: %v", ip, err)
		}
	}
}

func parseAllowedIPs(cidrs []string) ([]net.IPNet, error) {
	out := make([]net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		n, err := wireguard.ParseAllowedIP(c)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}
