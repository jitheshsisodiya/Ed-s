package udprelay

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// binding is one device attached to a session from a particular UDP address.
type binding struct {
	DeviceID uuid.UUID
	Addr     *net.UDPAddr
	LastSeen time.Time
	// ExpiresAt is the session token's expiry. A binding is dropped once its
	// authorization lapses, so a revoked or aged-out token cannot keep
	// relaying traffic indefinitely.
	ExpiresAt time.Time
}

// session groups the devices of one virtual network that are relaying
// through this node. Peers address each other by device ID within it.
type session struct {
	NetworkID  string
	bindings   map[uuid.UUID]*binding
	lastActive time.Time
}

// SessionTable tracks live relay sessions. It is safe for concurrent use by
// the reader goroutine and the janitor.
//
// The table is the node's only mutable state, and it is derived entirely
// from tokens the control plane signed. Nothing is persisted and no session
// is shared between relay nodes, which is what lets the fleet scale
// horizontally behind a plain UDP load balancer or per-node DNS.
type SessionTable struct {
	mu       sync.Mutex
	sessions map[string]*session
	// addrIndex maps a peer's UDP address to the session/device it bound as.
	addrIndex map[string]addrBinding

	idleTimeout time.Duration
	maxSessions int

	// Counters, read by the metrics exporter.
	bytesRelayed   uint64
	packetsRelayed uint64
	packetsDropped uint64
}

type addrBinding struct {
	NetworkID string
	DeviceID  uuid.UUID
}

// NewSessionTable builds a SessionTable. idleTimeout bounds how long a
// session survives without traffic; maxSessions caps memory use (0 = no cap).
func NewSessionTable(idleTimeout time.Duration, maxSessions int) *SessionTable {
	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute
	}
	return &SessionTable{
		sessions:    make(map[string]*session),
		addrIndex:   make(map[string]addrBinding),
		idleTimeout: idleTimeout,
		maxSessions: maxSessions,
	}
}

// ErrTooManySessions is returned when the node is at capacity.
type ErrTooManySessions struct{ Max int }

func (e *ErrTooManySessions) Error() string {
	return fmt.Sprintf("relay: session capacity reached (max %d)", e.Max)
}

// Bind attaches deviceID to the session for networkID at addr, replacing any
// previous binding for that device (which is how a peer's address change
// after a NAT rebind is absorbed).
func (t *SessionTable) Bind(networkID string, deviceID uuid.UUID, addr *net.UDPAddr, expiresAt time.Time) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	s, ok := t.sessions[networkID]
	if !ok {
		if t.maxSessions > 0 && len(t.sessions) >= t.maxSessions {
			return &ErrTooManySessions{Max: t.maxSessions}
		}
		s = &session{NetworkID: networkID, bindings: make(map[uuid.UUID]*binding)}
		t.sessions[networkID] = s
	}

	// Drop the device's previous address from the index so a stale entry
	// can't keep routing traffic to an address the peer has moved away from.
	if prev, ok := s.bindings[deviceID]; ok && prev.Addr.String() != addr.String() {
		delete(t.addrIndex, prev.Addr.String())
	}

	s.bindings[deviceID] = &binding{
		DeviceID:  deviceID,
		Addr:      addr,
		LastSeen:  now,
		ExpiresAt: expiresAt,
	}
	s.lastActive = now
	t.addrIndex[addr.String()] = addrBinding{NetworkID: networkID, DeviceID: deviceID}
	return nil
}

// Lookup resolves the sender of a datagram to its session binding.
func (t *SessionTable) Lookup(addr *net.UDPAddr) (networkID string, deviceID uuid.UUID, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	ab, found := t.addrIndex[addr.String()]
	if !found {
		return "", uuid.Nil, false
	}
	return ab.NetworkID, ab.DeviceID, true
}

// Route resolves the destination address for a datagram sent by senderID to
// peerID within networkID, refreshing the sender's liveness. It reports
// ok=false when the peer has not bound (or has expired), in which case the
// caller drops the packet: the relay never forwards to an unauthenticated
// address.
func (t *SessionTable) Route(networkID string, senderID, peerID uuid.UUID) (dest *net.UDPAddr, ok bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, found := t.sessions[networkID]
	if !found {
		return nil, false
	}

	now := time.Now()
	sender, found := s.bindings[senderID]
	if !found {
		return nil, false
	}
	if !sender.ExpiresAt.IsZero() && now.After(sender.ExpiresAt) {
		// The sender's authorization lapsed; force it to re-bind.
		delete(t.addrIndex, sender.Addr.String())
		delete(s.bindings, senderID)
		return nil, false
	}
	sender.LastSeen = now
	s.lastActive = now

	peer, found := s.bindings[peerID]
	if !found {
		return nil, false
	}
	if !peer.ExpiresAt.IsZero() && now.After(peer.ExpiresAt) {
		delete(t.addrIndex, peer.Addr.String())
		delete(s.bindings, peerID)
		return nil, false
	}
	return peer.Addr, true
}

// Touch refreshes liveness for a bound address (used by keepalives).
func (t *SessionTable) Touch(networkID string, deviceID uuid.UUID) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s, ok := t.sessions[networkID]
	if !ok {
		return
	}
	now := time.Now()
	if b, ok := s.bindings[deviceID]; ok {
		b.LastSeen = now
	}
	s.lastActive = now
}

// RecordRelayed accounts a successfully forwarded datagram.
func (t *SessionTable) RecordRelayed(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.bytesRelayed += uint64(n)
	t.packetsRelayed++
}

// RecordDropped accounts a datagram the relay refused to forward.
func (t *SessionTable) RecordDropped() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.packetsDropped++
}

// Stats is a point-in-time snapshot of the node's activity.
type Stats struct {
	Sessions       int
	Bindings       int
	BytesRelayed   uint64
	PacketsRelayed uint64
	PacketsDropped uint64
}

// Stats returns a snapshot for the metrics exporter and control-plane
// heartbeat.
func (t *SessionTable) Stats() Stats {
	t.mu.Lock()
	defer t.mu.Unlock()

	bindings := 0
	for _, s := range t.sessions {
		bindings += len(s.bindings)
	}
	return Stats{
		Sessions:       len(t.sessions),
		Bindings:       bindings,
		BytesRelayed:   t.bytesRelayed,
		PacketsRelayed: t.packetsRelayed,
		PacketsDropped: t.packetsDropped,
	}
}

// EvictIdle drops bindings that have been silent past the idle timeout or
// whose authorization expired, and removes sessions left empty. It returns
// the number of sessions removed.
func (t *SessionTable) EvictIdle(now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	removed := 0
	for networkID, s := range t.sessions {
		for deviceID, b := range s.bindings {
			idle := now.Sub(b.LastSeen) > t.idleTimeout
			expired := !b.ExpiresAt.IsZero() && now.After(b.ExpiresAt)
			if idle || expired {
				delete(t.addrIndex, b.Addr.String())
				delete(s.bindings, deviceID)
			}
		}
		if len(s.bindings) == 0 {
			delete(t.sessions, networkID)
			removed++
		}
	}
	return removed
}
