package disco

import (
	"net/netip"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.zx2c4.com/wireguard/conn"
)

// Handler is called for each inbound disco packet, with the address it
// arrived from.
type Handler func(msg Message, from netip.AddrPort)

// Bind wraps a WireGuard conn.Bind, intercepting disco packets before the
// WireGuard engine sees them and allowing disco packets to be sent from the
// same socket WireGuard transmits on.
//
// Because probes and tunnel traffic share one socket, they also share one
// NAT mapping: a probe that reaches a peer proves the exact path the tunnel
// will use, and the pinhole it opens is the one WireGuard needs.
type Bind struct {
	inner conn.Bind

	mu      sync.RWMutex
	handler Handler
}

// NewBind wraps the platform's default bind.
func NewBind() *Bind {
	return &Bind{inner: conn.NewDefaultBind()}
}

// NewBindWith wraps a specific bind (used in tests).
func NewBindWith(inner conn.Bind) *Bind {
	return &Bind{inner: inner}
}

// SetHandler installs the callback for inbound disco packets.
func (b *Bind) SetHandler(h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handler = h
}

// Open implements conn.Bind.
func (b *Bind) Open(port uint16) ([]conn.ReceiveFunc, uint16, error) {
	fns, actualPort, err := b.inner.Open(port)
	if err != nil {
		return nil, 0, err
	}
	wrapped := make([]conn.ReceiveFunc, len(fns))
	for i, fn := range fns {
		wrapped[i] = b.intercept(fn)
	}
	return wrapped, actualPort, nil
}

// intercept removes disco packets from a received batch, dispatching them to
// the handler, and compacts the remainder so WireGuard sees only its own
// traffic.
func (b *Bind) intercept(fn conn.ReceiveFunc) conn.ReceiveFunc {
	return func(packets [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		n, err := fn(packets, sizes, eps)
		if err != nil {
			return n, err
		}

		kept := 0
		for i := range n {
			pkt := packets[i][:sizes[i]]
			if IsDisco(pkt) {
				b.dispatch(pkt, eps[i])
				continue
			}
			if kept != i {
				// Compact in place: move this packet down over the gap left
				// by the disco packets already consumed.
				copy(packets[kept], pkt)
				sizes[kept] = sizes[i]
				eps[kept] = eps[i]
			}
			kept++
		}
		// Returning 0 is safe: wireguard-go simply calls the receive
		// function again, which blocks on the socket.
		return kept, nil
	}
}

// dispatch decodes a disco packet and hands it to the handler.
func (b *Bind) dispatch(pkt []byte, ep conn.Endpoint) {
	msg, err := Decode(pkt)
	if err != nil {
		return
	}

	b.mu.RLock()
	handler := b.handler
	b.mu.RUnlock()
	if handler == nil {
		return
	}

	var from netip.AddrPort
	if ep != nil {
		// For a received packet the endpoint's "destination" is the remote
		// peer, i.e. where a reply should be sent.
		if addr, err := netip.ParseAddrPort(ep.DstToString()); err == nil {
			from = addr
		}
	}
	handler(msg, from)
}

// SendDisco sends a disco packet to addr over WireGuard's socket.
func (b *Bind) SendDisco(payload []byte, addr netip.AddrPort) error {
	ep, err := b.inner.ParseEndpoint(addr.String())
	if err != nil {
		return err
	}
	return b.inner.Send([][]byte{payload}, ep)
}

// Close implements conn.Bind.
func (b *Bind) Close() error { return b.inner.Close() }

// SetMark implements conn.Bind.
func (b *Bind) SetMark(mark uint32) error { return b.inner.SetMark(mark) }

// Send implements conn.Bind.
func (b *Bind) Send(bufs [][]byte, ep conn.Endpoint) error { return b.inner.Send(bufs, ep) }

// ParseEndpoint implements conn.Bind.
func (b *Bind) ParseEndpoint(s string) (conn.Endpoint, error) { return b.inner.ParseEndpoint(s) }

// BatchSize implements conn.Bind.
func (b *Bind) BatchSize() int { return b.inner.BatchSize() }

// --- Probe tracking ---

// Prober sends disco pings and matches the pongs that come back, yielding a
// round-trip time per peer.
type Prober struct {
	bind   *Bind
	selfID uuid.UUID

	mu       sync.Mutex
	inflight map[uint64]*inflightPing
	// results holds the most recent successful probe per peer.
	results map[uuid.UUID]Result
}

type inflightPing struct {
	peer netip.AddrPort
	sent time.Time
}

// Result is the outcome of a successful probe.
type Result struct {
	// Addr is the address the pong came from — the address that actually
	// works, which may differ from the one probed if NAT rewrote it.
	Addr netip.AddrPort
	RTT  time.Duration
	At   time.Time
}

// NewProber builds a Prober bound to a socket.
func NewProber(bind *Bind, selfID uuid.UUID) *Prober {
	p := &Prober{
		bind:     bind,
		selfID:   selfID,
		inflight: make(map[uint64]*inflightPing),
		results:  make(map[uuid.UUID]Result),
	}
	bind.SetHandler(p.onPacket)
	return p
}

// SetSelfID sets this device's ID once registration assigns one, so peers
// can attribute the probes and replies they receive.
func (p *Prober) SetSelfID(id uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.selfID = id
}

// self returns this device's ID under lock.
func (p *Prober) self() uuid.UUID {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.selfID
}

// Ping sends a probe to addr. The reply, if any, arrives asynchronously and
// is recorded against the responding peer's device ID.
func (p *Prober) Ping(addr netip.AddrPort) error {
	txID, err := NewTxID()
	if err != nil {
		return err
	}

	p.mu.Lock()
	p.inflight[txIDKey(txID)] = &inflightPing{peer: addr, sent: time.Now()}
	p.mu.Unlock()

	return p.bind.SendDisco(Encode(TypePing, txID, p.self()), addr)
}

// onPacket handles an inbound disco packet.
func (p *Prober) onPacket(msg Message, from netip.AddrPort) {
	switch msg.Type {
	case TypePing:
		// Always answer a ping: replying is what lets the *other* side
		// discover this path, and it costs one small datagram.
		_ = p.bind.SendDisco(Encode(TypePong, msg.TxID, p.self()), from)

	case TypePong:
		p.mu.Lock()
		sent, ok := p.inflight[txIDKey(msg.TxID)]
		if ok {
			delete(p.inflight, txIDKey(msg.TxID))
			p.results[msg.Sender] = Result{
				Addr: from,
				RTT:  time.Since(sent.sent),
				At:   time.Now(),
			}
		}
		p.mu.Unlock()
	}
}

// Result returns the most recent probe result for a peer.
func (p *Prober) Result(peer uuid.UUID) (Result, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	r, ok := p.results[peer]
	return r, ok
}

// Forget drops a peer's recorded result (e.g. when it leaves the network).
func (p *Prober) Forget(peer uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.results, peer)
}

// ExpireInflight drops probes older than maxAge so a peer that never answers
// cannot leak memory.
func (p *Prober) ExpireInflight(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, ping := range p.inflight {
		if ping.sent.Before(cutoff) {
			delete(p.inflight, key)
		}
	}
}
