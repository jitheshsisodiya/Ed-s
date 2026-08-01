package tunnel

import "time"

// State is the connection phase, in the four values any frontend has to
// tell apart.
//
// The names are deliberately about the tunnel and not about the user's
// safety. This is a mesh: with no exit node configured, not being connected
// means you cannot reach your own machines, and nothing more. Calling that
// "vulnerable" would be borrowing a commercial VPN's marketing for a
// situation where it is simply untrue — and a product that cries wolf about
// the ordinary state has nothing left to say when something is actually
// wrong. StateDropped is that something.
type State string

const (
	// StateIdle is no tunnel: the app is open and nothing is running.
	StateIdle State = "idle"

	// StateHandshaking is the window between asking to connect and having
	// a usable path — registering, discovering the public endpoint,
	// exchanging keys, punching. It is the only state with a genuinely
	// unknown duration, so it is the one that has to look alive.
	StateHandshaking State = "handshaking"

	// StateActive is a tunnel that is up and passing traffic.
	StateActive State = "active"

	// StateDropped is a tunnel that was active and stopped being so
	// without anyone asking. This is the state that matters: with an exit
	// node configured it means traffic the user believed was tunnelled is
	// about to leave in the clear, which is what the kill switch exists
	// for. Reaching it always means something to say to the user.
	StateDropped State = "dropped"
)

// String satisfies fmt.Stringer.
func (s State) String() string { return string(s) }

// setState records a phase transition and logs the ones worth logging.
func (t *Tunnel) setState(next State) {
	t.stateMu.Lock()
	prev := t.state
	if prev == next {
		t.stateMu.Unlock()
		return
	}
	t.state = next
	if next == StateActive {
		t.activeSince = time.Now()
	}
	t.stateMu.Unlock()

	switch next {
	case StateActive:
		t.logf("tunnel is up")
	case StateDropped:
		// Worth a line at any log level: this is the transition that
		// silently exposes traffic when an exit node is configured.
		t.logf("tunnel dropped while it was up")
	}
}

// State reports the current phase.
func (t *Tunnel) State() State {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	return t.state
}

// ActiveSince reports when the tunnel last became active, zero if it never
// has. Frontends show it as a session duration.
func (t *Tunnel) ActiveSince() time.Time {
	t.stateMu.Lock()
	defer t.stateMu.Unlock()
	if t.state != StateActive {
		return time.Time{}
	}
	return t.activeSince
}

// observeHealth moves the tunnel between active and dropped based on
// whether any peer is currently reachable.
//
// "Any peer" rather than "all peers" is the right test for a mesh: one
// unreachable machine is that machine's problem, while zero reachable
// machines when there were some a moment ago is a tunnel problem. With an
// exit node the question collapses to that one peer, which is exactly the
// case where a drop is dangerous.
func (t *Tunnel) observeHealth() {
	exitID, hasExit := t.ExitNode()

	t.mu.Lock()
	peers := make([]*peer, 0, len(t.peers))
	for _, pr := range t.peers {
		peers = append(peers, pr)
	}
	t.mu.Unlock()

	if len(peers) == 0 {
		// A network with no other devices is not a broken tunnel. There is
		// nothing to be reachable, so the interface being up is all the
		// health there is.
		if t.State() == StateHandshaking {
			t.setState(StateActive)
		}
		return
	}

	reachable := false
	for _, pr := range peers {
		pr.mu.Lock()
		id, mode := pr.deviceID, pr.mode
		pr.mu.Unlock()

		if hasExit && id != exitID {
			// Only the exit node's health decides, because it is the only
			// peer carrying anything that matters.
			continue
		}
		if mode == ModeDirect || mode == ModeRelay {
			reachable = true
			break
		}
	}

	switch {
	case reachable:
		t.setState(StateActive)
	case t.State() == StateActive:
		t.setState(StateDropped)
	}
}
