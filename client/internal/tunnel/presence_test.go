package tunnel

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/client/internal/relayproxy"
	"github.com/jitheshsisodiya/Ed-s/client/internal/wireguard"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/protogen/coordination/v1"
)

// presenceTunnel builds a tunnel holding one peer already installed in the
// device, which is the state observePeerModes runs against.
func presenceTunnel(t *testing.T, mode ConnectionMode) (*Tunnel, *fakeDevice, *peer) {
	t.Helper()

	dev := newFakeDevice()
	const key = "peer-key"
	if err := dev.UpsertPeer(wireguard.PeerConfig{PublicKeyBase64: key}); err != nil {
		t.Fatal(err)
	}

	// A peer that has just been installed has received nothing. The fake's
	// default counter is non-zero for the benefit of other tests, and here
	// that would read as traffic that never happened.
	dev.mu.Lock()
	dev.rx[key] = 0
	dev.mu.Unlock()

	pr := &peer{deviceID: uuid.New(), deviceName: "phone", publicKey: key, mode: mode}
	tun := &Tunnel{dev: dev, peers: map[uuid.UUID]*peer{pr.deviceID: pr}}
	return tun, dev, pr
}

func modeOf(pr *peer) ConnectionMode {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	return pr.mode
}

// A peer whose keepalives are arriving is present, and it is present within
// one observation rather than within a handshake interval.
func TestAPeerThatKeepsTalkingIsPresent(t *testing.T) {
	tun, dev, pr := presenceTunnel(t, ModeOffline)

	// Nothing has ever arrived: silence is not presence, and an unheard-from
	// peer must not be promoted by the mere act of looking at it.
	tun.observePeerModes()
	if got := modeOf(pr); got != ModeOffline {
		t.Fatalf("mode = %s before anything arrived, want offline", got)
	}

	dev.receive(pr.publicKey, 32)
	tun.observePeerModes()
	if got := modeOf(pr); got != ModeDirect {
		t.Fatalf("mode = %s after a keepalive arrived, want direct", got)
	}
}

// The complaint this exists for: a machine that has gone away goes on being
// listed as online. Presence has to follow the last thing actually received,
// not the handshake clock, which only ticks every couple of minutes.
func TestAPeerThatWentSilentGoesOffline(t *testing.T) {
	tun, dev, pr := presenceTunnel(t, ModeDirect)

	dev.receive(pr.publicKey, 32)
	tun.observePeerModes()
	if got := modeOf(pr); got != ModeDirect {
		t.Fatalf("mode = %s while it was talking, want direct", got)
	}

	// It stops sending. A handshake from a minute ago is not evidence it is
	// still there — that is exactly the stale signal being replaced.
	dev.mu.Lock()
	dev.handshake[pr.publicKey] = time.Now().Add(-time.Minute)
	dev.mu.Unlock()

	pr.mu.Lock()
	pr.lastRxMove = time.Now().Add(-PeerSilentAfter - time.Second)
	pr.mu.Unlock()

	tun.observePeerModes()
	if got := modeOf(pr); got != ModeOffline {
		t.Fatalf("mode = %s after %s of silence, want offline", got, PeerSilentAfter)
	}
}

// One dropped keepalive on a mobile network is not a machine going away.
func TestABriefGapIsNotADeparture(t *testing.T) {
	tun, dev, pr := presenceTunnel(t, ModeDirect)

	dev.receive(pr.publicKey, 32)
	tun.observePeerModes()

	pr.mu.Lock()
	pr.lastRxMove = time.Now().Add(-2 * keepaliveSeconds * time.Second)
	pr.mu.Unlock()

	tun.observePeerModes()
	if got := modeOf(pr); got != ModeDirect {
		t.Fatalf("mode = %s after two missed keepalives, want direct — "+
			"a lost packet is not a disconnection", got)
	}
}

// A peer that comes back on a relayed path must come back as relayed. Saying
// "direct" about a path running through a relay misreports both the latency
// somebody is seeing and whose bandwidth is paying for it.
func TestAReturningRelayedPeerIsStillRelayed(t *testing.T) {
	tun, dev, pr := presenceTunnel(t, ModeOffline)
	// Non-nil is the whole signal: a peer holding a proxy is on a relay.
	pr.proxy = &relayproxy.Proxy{}

	dev.receive(pr.publicKey, 32)
	tun.observePeerModes()

	if got := modeOf(pr); got != ModeRelay {
		t.Fatalf("mode = %s, want relay", got)
	}
}

// A peer still negotiating has never been heard from. Calling that "offline"
// describes a connection attempt in progress as a failure.
func TestANegotiatingPeerIsLeftAlone(t *testing.T) {
	tun, _, pr := presenceTunnel(t, ModeConnecting)

	tun.observePeerModes()

	if got := modeOf(pr); got != ModeConnecting {
		t.Fatalf("mode = %s during negotiation, want connecting", got)
	}
}

// Every peer carries a keepalive, not only the relayed ones.
//
// Without it two machines that punched a hole through their NATs and then had
// nothing to say lose the hole: the router forgets the mapping, and the next
// packet arrives at a closed door. That was being reported as a disconnection
// minutes after a connection that had been working.
func TestEveryPeerGetsAKeepalive(t *testing.T) {
	dev := newFakeDevice()
	tun := newTestTunnel(t, dev, newFakeCoordinator())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := tun.addPeer(ctx, &coordinationv1.Peer{
		DeviceId:   uuid.NewString(),
		DeviceName: "phone",
		PublicKey:  "peer-key",
		VirtualIp:  "10.77.0.5",
	})
	if err != nil {
		t.Fatal(err)
	}

	dev.mu.Lock()
	defer dev.mu.Unlock()
	for key, cfg := range dev.peers {
		if cfg.PersistentKeepaliveSeconds != keepaliveSeconds {
			t.Fatalf("peer %s installed with keepalive %d, want %d — "+
				"a silent direct path loses its NAT mapping",
				key, cfg.PersistentKeepaliveSeconds, keepaliveSeconds)
		}
	}
}
