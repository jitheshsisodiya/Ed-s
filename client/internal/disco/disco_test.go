package disco

import (
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.zx2c4.com/wireguard/conn"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	sender := uuid.New()
	txID, err := NewTxID()
	if err != nil {
		t.Fatalf("txid: %v", err)
	}

	msg, err := Decode(Encode(TypePing, txID, sender))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if msg.Type != TypePing {
		t.Fatalf("type = %d", msg.Type)
	}
	if msg.TxID != txID {
		t.Fatal("transaction ID did not survive the round trip")
	}
	if msg.Sender != sender {
		t.Fatalf("sender = %s, want %s", msg.Sender, sender)
	}
}

// A disco packet must never be mistaken for WireGuard traffic, in either
// direction: the demultiplexer in Bind depends on it.
func TestDiscoIsDistinguishableFromWireGuard(t *testing.T) {
	// WireGuard message types 1-4, each followed by three reserved zeros.
	for msgType := byte(1); msgType <= 4; msgType++ {
		pkt := []byte{msgType, 0, 0, 0, 0xde, 0xad, 0xbe, 0xef}
		if IsDisco(pkt) {
			t.Fatalf("WireGuard message type %d was classified as disco", msgType)
		}
	}

	if !IsDisco(Encode(TypePing, TxID{}, uuid.New())) {
		t.Fatal("a disco packet was not recognised")
	}
}

func TestDecodeRejectsMalformed(t *testing.T) {
	cases := map[string][]byte{
		"empty":          {},
		"short":          {'N', 'X'},
		"wrong magic":    append([]byte("XXXXXX"), make([]byte, PacketLen)...),
		"truncated body": append(Magic[:], TypePing),
		"unknown type":   Encode(99, TxID{}, uuid.New()),
	}
	for name, pkt := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(pkt); err == nil {
				t.Fatal("expected decoding to fail")
			}
		})
	}
}

// fakeBind is a conn.Bind that loops packets to a partner bind, letting two
// Probers talk without real sockets.
type fakeBind struct {
	mu      sync.Mutex
	partner *fakeBind
	// deliver is set by Open to hand packets to the receive loop.
	inbox  chan inboundPacket
	closed bool
	// addr is the address this bind appears to send from.
	addr netip.AddrPort
}

type inboundPacket struct {
	payload []byte
	from    netip.AddrPort
}

func newFakeBind(addr netip.AddrPort) *fakeBind {
	return &fakeBind{inbox: make(chan inboundPacket, 32), addr: addr}
}

func (b *fakeBind) Open(uint16) ([]conn.ReceiveFunc, uint16, error) {
	return []conn.ReceiveFunc{b.receive}, b.addr.Port(), nil
}

func (b *fakeBind) receive(packets [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
	pkt, ok := <-b.inbox
	if !ok {
		return 0, net.ErrClosed
	}
	n := copy(packets[0], pkt.payload)
	sizes[0] = n
	eps[0] = &fakeEndpoint{addr: pkt.from}
	return 1, nil
}

func (b *fakeBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	b.mu.Lock()
	partner := b.partner
	b.mu.Unlock()
	if partner == nil {
		return nil
	}
	for _, buf := range bufs {
		payload := make([]byte, len(buf))
		copy(payload, buf)
		select {
		case partner.inbox <- inboundPacket{payload: payload, from: b.addr}:
		default:
		}
	}
	return nil
}

func (b *fakeBind) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		close(b.inbox)
	}
	return nil
}

func (b *fakeBind) SetMark(uint32) error { return nil }
func (b *fakeBind) BatchSize() int       { return 1 }

func (b *fakeBind) ParseEndpoint(s string) (conn.Endpoint, error) {
	addr, err := netip.ParseAddrPort(s)
	if err != nil {
		return nil, err
	}
	return &fakeEndpoint{addr: addr}, nil
}

type fakeEndpoint struct{ addr netip.AddrPort }

func (e *fakeEndpoint) ClearSrc()           {}
func (e *fakeEndpoint) SrcToString() string { return "" }
func (e *fakeEndpoint) DstToString() string { return e.addr.String() }
func (e *fakeEndpoint) DstToBytes() []byte  { return e.addr.Addr().AsSlice() }
func (e *fakeEndpoint) DstIP() netip.Addr   { return e.addr.Addr() }
func (e *fakeEndpoint) SrcIP() netip.Addr   { return netip.Addr{} }

// pairedProbers wires two Probers together over fake binds and starts their
// receive loops.
func pairedProbers(t *testing.T) (a, b *Prober, aID, bID uuid.UUID) {
	t.Helper()

	addrA := netip.MustParseAddrPort("198.51.100.1:51820")
	addrB := netip.MustParseAddrPort("203.0.113.1:51820")

	bindA, bindB := newFakeBind(addrA), newFakeBind(addrB)
	bindA.partner, bindB.partner = bindB, bindA

	discoA, discoB := NewBindWith(bindA), NewBindWith(bindB)
	aID, bID = uuid.New(), uuid.New()
	a, b = NewProber(discoA, aID), NewProber(discoB, bID)

	// Drive each side's receive loop, mirroring what wireguard-go does.
	for _, d := range []*Bind{discoA, discoB} {
		fns, _, err := d.Open(0)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		for _, fn := range fns {
			go func(fn conn.ReceiveFunc) {
				packets := [][]byte{make([]byte, 1500)}
				sizes := make([]int, 1)
				eps := make([]conn.Endpoint, 1)
				for {
					if _, err := fn(packets, sizes, eps); err != nil {
						return
					}
				}
			}(fn)
		}
	}

	t.Cleanup(func() {
		_ = bindA.Close()
		_ = bindB.Close()
	})
	return a, b, aID, bID
}

func TestPingPongMeasuresRoundTrip(t *testing.T) {
	a, _, _, bID := pairedProbers(t)

	if err := a.Ping(netip.MustParseAddrPort("203.0.113.1:51820")); err != nil {
		t.Fatalf("ping: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if res, ok := a.Result(bID); ok {
			if res.RTT <= 0 {
				t.Fatalf("RTT = %s, want a positive duration", res.RTT)
			}
			if res.Addr.String() != "203.0.113.1:51820" {
				t.Fatalf("responder address = %s", res.Addr)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("no pong was recorded within the deadline")
}

// A pong is only recorded against a ping we actually sent, so a stray or
// forged pong cannot fabricate a latency measurement.
func TestUnsolicitedPongIsIgnored(t *testing.T) {
	a, b, _, bID := pairedProbers(t)

	unknownTx, err := NewTxID()
	if err != nil {
		t.Fatalf("txid: %v", err)
	}
	if err := b.bind.SendDisco(Encode(TypePong, unknownTx, bID), netip.MustParseAddrPort("198.51.100.1:51820")); err != nil {
		t.Fatalf("send: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	if _, ok := a.Result(bID); ok {
		t.Fatal("an unsolicited pong should not produce a result")
	}
}

func TestBindPassesWireGuardTrafficThrough(t *testing.T) {
	addrA := netip.MustParseAddrPort("198.51.100.1:51820")
	addrB := netip.MustParseAddrPort("203.0.113.1:51820")
	bindA, bindB := newFakeBind(addrA), newFakeBind(addrB)
	bindA.partner, bindB.partner = bindB, bindA

	discoA := NewBindWith(bindA)
	var discoSeen int
	discoA.SetHandler(func(Message, netip.AddrPort) { discoSeen++ })

	fns, _, err := discoA.Open(0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	// One disco packet followed by a WireGuard packet.
	_ = bindB.Send([][]byte{Encode(TypePing, TxID{}, uuid.New())}, &fakeEndpoint{addr: addrA})
	wgPacket := []byte{1, 0, 0, 0, 0xaa, 0xbb}
	_ = bindB.Send([][]byte{wgPacket}, &fakeEndpoint{addr: addrA})

	packets := [][]byte{make([]byte, 1500)}
	sizes := make([]int, 1)
	eps := make([]conn.Endpoint, 1)

	// The disco packet is consumed, so this batch yields nothing.
	n, err := fns[0](packets, sizes, eps)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if n != 0 {
		t.Fatalf("disco packet leaked to WireGuard (n=%d)", n)
	}
	if discoSeen != 1 {
		t.Fatalf("disco handler called %d times, want 1", discoSeen)
	}

	// The WireGuard packet passes through untouched.
	n, err = fns[0](packets, sizes, eps)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if n != 1 {
		t.Fatalf("WireGuard packet was dropped (n=%d)", n)
	}
	if got := packets[0][:sizes[0]]; string(got) != string(wgPacket) {
		t.Fatalf("WireGuard payload was altered: %v", got)
	}
}

func TestExpireInflightDropsStaleProbes(t *testing.T) {
	bind := NewBindWith(newFakeBind(netip.MustParseAddrPort("198.51.100.1:51820")))
	p := NewProber(bind, uuid.New())

	if err := p.Ping(netip.MustParseAddrPort("203.0.113.1:51820")); err != nil {
		t.Fatalf("ping: %v", err)
	}

	p.mu.Lock()
	inflight := len(p.inflight)
	p.mu.Unlock()
	if inflight != 1 {
		t.Fatalf("inflight = %d, want 1", inflight)
	}

	// Nothing expires while fresh.
	p.ExpireInflight(time.Hour)
	p.mu.Lock()
	inflight = len(p.inflight)
	p.mu.Unlock()
	if inflight != 1 {
		t.Fatal("a fresh probe was expired")
	}

	// A zero max-age expires everything, so an unanswered probe can't leak.
	p.ExpireInflight(0)
	p.mu.Lock()
	inflight = len(p.inflight)
	p.mu.Unlock()
	if inflight != 0 {
		t.Fatalf("inflight = %d after expiry, want 0", inflight)
	}
}

func TestForgetDropsResult(t *testing.T) {
	a, _, _, bID := pairedProbers(t)

	if err := a.Ping(netip.MustParseAddrPort("203.0.113.1:51820")); err != nil {
		t.Fatalf("ping: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := a.Result(bID); ok {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	a.Forget(bID)
	if _, ok := a.Result(bID); ok {
		t.Fatal("result should have been forgotten")
	}
}
