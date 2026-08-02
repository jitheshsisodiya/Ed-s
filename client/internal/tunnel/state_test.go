package tunnel

import (
	"testing"

	"github.com/google/uuid"
)

// TestASleepingPeerIsNotABrokenTunnel is what a user reported: a machine
// showing "linking" indefinitely, and reporting itself dropped, because the
// only other device on the network was a phone in somebody's pocket.
//
// The interface is up and the device is registered. Whether some other
// machine is awake is that machine's business, and the device list already
// says so per peer. Crying wolf about the ordinary state leaves nothing to
// say when something is genuinely wrong.
func TestASleepingPeerIsNotABrokenTunnel(t *testing.T) {
	tun := &Tunnel{peers: map[uuid.UUID]*peer{}}
	tun.state = StateHandshaking
	sleeping := uuid.New()
	tun.peers[sleeping] = &peer{deviceID: sleeping, mode: ModeOffline}

	tun.observeHealth()

	if got := tun.State(); got != StateActive {
		t.Fatalf("state = %s, want active — an offline peer is not a broken tunnel", got)
	}

	// And it must not go on to report a drop either.
	tun.observeHealth()
	if got := tun.State(); got != StateActive {
		t.Fatalf("state = %s after a second look, want active", got)
	}
}

// With an exit node the calculus is the opposite: that peer carries
// everything, so its absence means traffic the user believes is tunnelled is
// about to leave in the clear. That is the case worth calling a drop.
func TestAnUnreachableExitNodeIsADrop(t *testing.T) {
	tun := &Tunnel{peers: map[uuid.UUID]*peer{}}
	tun.state = StateActive
	exit := uuid.New()
	tun.exit.deviceID = exit
	tun.peers[exit] = &peer{deviceID: exit, mode: ModeOffline}

	tun.observeHealth()

	if got := tun.State(); got != StateDropped {
		t.Fatalf("state = %s, want dropped — the exit node carries everything", got)
	}
}

func TestAReachableExitNodeIsActive(t *testing.T) {
	tun := &Tunnel{peers: map[uuid.UUID]*peer{}}
	tun.state = StateHandshaking
	exit := uuid.New()
	tun.exit.deviceID = exit
	tun.peers[exit] = &peer{deviceID: exit, mode: ModeDirect}
	// Another peer being offline must not affect this.
	other := uuid.New()
	tun.peers[other] = &peer{deviceID: other, mode: ModeOffline}

	tun.observeHealth()

	if got := tun.State(); got != StateActive {
		t.Fatalf("state = %s, want active", got)
	}
}

// An exit node chosen and then removed from the network leaves traffic with
// nowhere to go, which is a drop even though there is no peer left to inspect.
func TestAnExitNodeThatVanishedIsADrop(t *testing.T) {
	tun := &Tunnel{peers: map[uuid.UUID]*peer{}}
	tun.state = StateActive
	tun.exit.deviceID = uuid.New()

	tun.observeHealth()

	if got := tun.State(); got != StateDropped {
		t.Fatalf("state = %s, want dropped", got)
	}
}
