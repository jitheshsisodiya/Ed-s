// Package holepunch implements simultaneous UDP hole punching between two
// WireGuard peers: given a set of local and peer candidate endpoints
// (typically produced by internal/stun and exchanged via
// CoordinationService.ExchangeICECandidates), it fires WireGuard-shaped
// probe packets at every peer candidate concurrently while the caller's
// WireGuard engine listens on the same UDP socket, then watches for a
// fresh WireGuard handshake timestamp to detect success.
//
// The actual WireGuard handshake is performed by the wireguard-go engine
// itself once it has a peer configured with a candidate endpoint — this
// package's job is (a) to keep NAT/firewall mappings open long enough for
// that handshake to land by sending punch packets to every candidate, and
// (b) to decide, within a bounded timeout, whether direct connectivity
// succeeded so the tunnel orchestrator can fall back to a relay if not.
package holepunch

import (
	"context"
	"fmt"
	"net"
	"sort"
	"sync"
	"time"
)

// Candidate is a single ICE-style endpoint to attempt, mirroring
// coordination.ICECandidate.
type Candidate struct {
	Addr *net.UDPAddr
	// Type is "host" (local interface address), "srflx" (server
	// reflexive, i.e. STUN-discovered public endpoint), or "relay".
	Type string
	// Priority ranks candidates when choosing which to try first/report
	// as primary; higher wins. Host candidates should generally outrank
	// server-reflexive ones on a shared LAN, and both outrank relay.
	Priority uint32
}

// DefaultPunchInterval is how often probe packets are (re)sent to each
// candidate while waiting for a handshake.
const DefaultPunchInterval = 200 * time.Millisecond

// DefaultTimeout bounds how long Punch waits for a fresh handshake before
// giving up and letting the caller fall back to relay.
const DefaultTimeout = 5 * time.Second

// HandshakeChecker reports the most recent WireGuard handshake time for a
// peer (by public key), so Punch can detect success without needing to
// know anything about WireGuard internals itself.
// internal/wireguard.Device.PeerStats satisfies the shape needed here.
type HandshakeChecker func() (lastHandshake time.Time, err error)

// Prober sends a single UDP probe datagram to addr. In production this is
// backed by the same *net.UDPConn the WireGuard engine's conn.Bind uses
// (see internal/wireguard), so probes and real WireGuard handshake
// packets share one socket/NAT mapping. Tests inject a fake.
type Prober func(addr *net.UDPAddr) error

// Result is the outcome of a Punch attempt.
type Result struct {
	// Success is true if a fresh handshake was observed within the
	// timeout.
	Success bool
	// Elapsed is how long it took to detect success (zero if it failed).
	Elapsed time.Duration
	// AttemptedCandidates lists the candidates probes were sent to, in
	// priority order.
	AttemptedCandidates []Candidate
}

// Punch fires concurrent probe packets at every candidate (highest
// priority first, but all are tried — cheap UDP sends) on a repeating
// interval, polling checkHandshake to detect a fresh handshake, until
// either success or ctx/timeout expires.
//
// before is the handshake baseline: Punch only counts a handshake as
// "fresh" (i.e. caused by this punching attempt, not a stale prior
// session) if the checker reports a timestamp strictly after `before`.
// Pass the peer's current last-handshake time (zero if none yet).
func Punch(ctx context.Context, before time.Time, candidates []Candidate, probe Prober, checkHandshake HandshakeChecker, opts ...Option) Result {
	cfg := config{interval: DefaultPunchInterval, timeout: DefaultTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}

	ordered := make([]Candidate, len(candidates))
	copy(ordered, candidates)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority > ordered[j].Priority })

	result := Result{AttemptedCandidates: ordered}
	if len(ordered) == 0 {
		return result
	}

	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()

	start := time.Now()
	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	sendAll := func() {
		var wg sync.WaitGroup
		for _, c := range ordered {
			c := c
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = probe(c.Addr) // best-effort; unreachable candidates just never respond
			}()
		}
		wg.Wait()
	}

	sendAll() // fire immediately, don't wait for the first tick
	if fresh, ok := pollFresh(before, checkHandshake); ok && fresh {
		result.Success = true
		result.Elapsed = time.Since(start)
		return result
	}

	for {
		select {
		case <-ctx.Done():
			return result
		case <-ticker.C:
			sendAll()
			if fresh, ok := pollFresh(before, checkHandshake); ok && fresh {
				result.Success = true
				result.Elapsed = time.Since(start)
				return result
			}
		}
	}
}

func pollFresh(before time.Time, checkHandshake HandshakeChecker) (fresh bool, ok bool) {
	if checkHandshake == nil {
		return false, false
	}
	ts, err := checkHandshake()
	if err != nil {
		return false, false
	}
	return ts.After(before), true
}

type config struct {
	interval time.Duration
	timeout  time.Duration
}

// Option configures Punch.
type Option func(*config)

// WithInterval overrides DefaultPunchInterval.
func WithInterval(d time.Duration) Option {
	return func(c *config) { c.interval = d }
}

// WithTimeout overrides DefaultTimeout.
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// UDPProber returns a Prober that sends a minimal WireGuard-shaped punch
// packet from conn. WireGuard peers silently drop packets they can't
// parse as a valid message type, but the UDP datagram still opens/refreshes
// the NAT mapping on its way out and (crucially) primes the peer's NAT to
// accept the real handshake initiation that follows on the same 5-tuple —
// which is the entire point of simultaneous hole punching. A single zero
// byte is sufficient and matches what other userspace WireGuard mesh
// implementations send as a "punch" packet.
func UDPProber(conn *net.UDPConn) Prober {
	punch := []byte{0}
	return func(addr *net.UDPAddr) error {
		if addr == nil {
			return fmt.Errorf("holepunch: nil candidate address")
		}
		_, err := conn.WriteToUDP(punch, addr)
		return err
	}
}
