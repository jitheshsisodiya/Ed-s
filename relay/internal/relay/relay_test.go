package relay

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/relay/internal/token"
)

const testSecret = "relay-secret-for-tests"

// issueToken mints a relay session token the same way the control plane's
// auth.RelaySessionManager does, so the tests exercise the real signature
// path rather than a stub verifier.
func issueToken(t *testing.T, deviceID, relayID, networkID string, ttl time.Duration) string {
	t.Helper()
	now := time.Now()
	claims := token.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   deviceID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        uuid.NewString(),
		},
		DeviceID:  deviceID,
		RelayID:   relayID,
		NetworkID: networkID,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// testServer starts a relay on a loopback UDP port and returns its address.
func testServer(t *testing.T) (*net.UDPAddr, *SessionTable) {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	sessions := NewSessionTable(time.Minute, 100)
	srv := NewServer(Config{
		Conn:          conn,
		Verifier:      token.NewVerifier(testSecret, ""),
		Sessions:      sessions,
		Logger:        zap.NewNop(),
		EvictInterval: time.Hour, // the janitor is exercised directly instead
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = conn.Close()
	})

	return conn.LocalAddr().(*net.UDPAddr), sessions
}

// peer is a test client speaking the relay wire protocol.
type peer struct {
	conn     *net.UDPConn
	deviceID uuid.UUID
}

func newPeer(t *testing.T, serverAddr *net.UDPAddr) *peer {
	t.Helper()
	conn, err := net.DialUDP("udp", nil, serverAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &peer{conn: conn, deviceID: uuid.New()}
}

func (p *peer) send(t *testing.T, frame []byte) {
	t.Helper()
	if _, err := p.conn.Write(frame); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// recv reads one frame, failing the test if nothing arrives in time.
func (p *peer) recv(t *testing.T, timeout time.Duration) []byte {
	t.Helper()
	_ = p.conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, MaxPacketSize)
	n, err := p.conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return buf[:n]
}

// expectNothing asserts no frame arrives within the window.
func (p *peer) expectNothing(t *testing.T, window time.Duration) {
	t.Helper()
	_ = p.conn.SetReadDeadline(time.Now().Add(window))
	buf := make([]byte, MaxPacketSize)
	n, err := p.conn.Read(buf)
	if err == nil {
		t.Fatalf("expected no frame, got %d bytes (type 0x%02x)", n, buf[0])
	}
}

func (p *peer) bind(t *testing.T, networkID string) {
	t.Helper()
	tok := issueToken(t, p.deviceID.String(), "", networkID, time.Minute)
	p.send(t, EncodeBind(p.deviceID, tok))

	frame := p.recv(t, 2*time.Second)
	if frame[0] != FrameBindAck {
		reason, _ := DecodeError(frame)
		t.Fatalf("bind rejected: type 0x%02x %q", frame[0], reason)
	}
}

func TestBindAndRelayBetweenPeers(t *testing.T) {
	addr, sessions := testServer(t)
	networkID := uuid.NewString()

	a := newPeer(t, addr)
	b := newPeer(t, addr)
	a.bind(t, networkID)
	b.bind(t, networkID)

	// A sends an opaque payload addressed to B.
	payload := []byte("an encrypted wireguard datagram")
	a.send(t, EncodeData(b.deviceID, payload))

	frame := b.recv(t, 2*time.Second)
	if frame[0] != FrameData {
		t.Fatalf("expected DATA frame, got 0x%02x", frame[0])
	}

	from, got, err := DecodeData(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The relay rewrites the peer field to the sender, so B learns the origin.
	if from != a.deviceID {
		t.Fatalf("sender = %s, want %s", from, a.deviceID)
	}
	if string(got) != string(payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}

	// And the reverse direction works over the same session.
	reply := []byte("the response datagram")
	b.send(t, EncodeData(a.deviceID, reply))
	frame = a.recv(t, 2*time.Second)
	from, got, err = DecodeData(frame)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if from != b.deviceID {
		t.Fatalf("sender = %s, want %s", from, b.deviceID)
	}
	if string(got) != string(reply) {
		t.Fatalf("payload = %q, want %q", got, reply)
	}

	stats := sessions.Stats()
	if stats.Sessions != 1 {
		t.Fatalf("sessions = %d, want 1", stats.Sessions)
	}
	if stats.Bindings != 2 {
		t.Fatalf("bindings = %d, want 2", stats.Bindings)
	}
	if stats.PacketsRelayed != 2 {
		t.Fatalf("packets relayed = %d, want 2", stats.PacketsRelayed)
	}
	if stats.BytesRelayed == 0 {
		t.Fatal("expected non-zero relayed bytes")
	}
}

func TestPayloadIsForwardedByteForByte(t *testing.T) {
	addr, _ := testServer(t)
	networkID := uuid.NewString()

	a := newPeer(t, addr)
	b := newPeer(t, addr)
	a.bind(t, networkID)
	b.bind(t, networkID)

	// Binary payload including bytes that collide with frame type markers,
	// to prove the relay treats it as opaque.
	payload := []byte{0x01, 0x02, 0x03, 0x04, 0x00, 0xff, 0xfe, 0x05}
	a.send(t, EncodeData(b.deviceID, payload))

	_, got, err := DecodeData(b.recv(t, 2*time.Second))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(payload) {
		t.Fatalf("length = %d, want %d", len(got), len(payload))
	}
	for i := range payload {
		if got[i] != payload[i] {
			t.Fatalf("byte %d = 0x%02x, want 0x%02x", i, got[i], payload[i])
		}
	}
}

func TestUnauthenticatedPeerCannotRelay(t *testing.T) {
	addr, sessions := testServer(t)
	networkID := uuid.NewString()

	b := newPeer(t, addr)
	b.bind(t, networkID)

	// An unbound sender tries to push traffic at B.
	attacker := newPeer(t, addr)
	attacker.send(t, EncodeData(b.deviceID, []byte("injected")))

	// It gets an error frame, and B receives nothing.
	frame := attacker.recv(t, 2*time.Second)
	if frame[0] != FrameError {
		t.Fatalf("expected ERROR frame, got 0x%02x", frame[0])
	}
	b.expectNothing(t, 300*time.Millisecond)

	if sessions.Stats().PacketsRelayed != 0 {
		t.Fatal("no packet should have been relayed")
	}
}

func TestBindRejectsInvalidToken(t *testing.T) {
	addr, sessions := testServer(t)

	p := newPeer(t, addr)
	p.send(t, EncodeBind(p.deviceID, "not-a-valid-token"))

	frame := p.recv(t, 2*time.Second)
	if frame[0] != FrameError {
		t.Fatalf("expected ERROR frame, got 0x%02x", frame[0])
	}
	if sessions.Stats().Bindings != 0 {
		t.Fatal("an invalid token must not create a binding")
	}
}

func TestBindRejectsTokenSignedWithWrongSecret(t *testing.T) {
	addr, _ := testServer(t)

	claims := token.Claims{
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute))},
		DeviceID:         uuid.NewString(),
		NetworkID:        uuid.NewString(),
	}
	forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("a-different-secret"))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	p := newPeer(t, addr)
	p.send(t, EncodeBind(p.deviceID, forged))

	if frame := p.recv(t, 2*time.Second); frame[0] != FrameError {
		t.Fatalf("expected ERROR frame for a forged token, got 0x%02x", frame[0])
	}
}

func TestBindRejectsExpiredToken(t *testing.T) {
	addr, _ := testServer(t)

	p := newPeer(t, addr)
	expired := issueToken(t, p.deviceID.String(), "", uuid.NewString(), -time.Minute)
	p.send(t, EncodeBind(p.deviceID, expired))

	if frame := p.recv(t, 2*time.Second); frame[0] != FrameError {
		t.Fatalf("expected ERROR frame for an expired token, got 0x%02x", frame[0])
	}
}

func TestBindRejectsDeviceIDMismatch(t *testing.T) {
	addr, sessions := testServer(t)

	p := newPeer(t, addr)
	// A valid token, but claiming to be a different device in the frame.
	tok := issueToken(t, uuid.NewString(), "", uuid.NewString(), time.Minute)
	p.send(t, EncodeBind(p.deviceID, tok))

	if frame := p.recv(t, 2*time.Second); frame[0] != FrameError {
		t.Fatalf("expected ERROR frame for a device mismatch, got 0x%02x", frame[0])
	}
	if sessions.Stats().Bindings != 0 {
		t.Fatal("a mismatched bind must not create a binding")
	}
}

func TestPeersInDifferentNetworksAreIsolated(t *testing.T) {
	addr, _ := testServer(t)

	a := newPeer(t, addr)
	b := newPeer(t, addr)
	a.bind(t, uuid.NewString()) // network 1
	b.bind(t, uuid.NewString()) // network 2

	// A addresses B, but they share no session, so nothing is forwarded.
	a.send(t, EncodeData(b.deviceID, []byte("cross-network")))
	b.expectNothing(t, 300*time.Millisecond)
}

func TestTrafficToUnboundPeerIsDropped(t *testing.T) {
	addr, sessions := testServer(t)
	networkID := uuid.NewString()

	a := newPeer(t, addr)
	a.bind(t, networkID)

	// The intended peer never bound.
	a.send(t, EncodeData(uuid.New(), []byte("into the void")))

	a.expectNothing(t, 300*time.Millisecond)
	if sessions.Stats().PacketsRelayed != 0 {
		t.Fatal("nothing should have been relayed")
	}
	if sessions.Stats().PacketsDropped == 0 {
		t.Fatal("the drop should have been counted")
	}
}

func TestRebindFromNewAddressUpdatesRoute(t *testing.T) {
	addr, sessions := testServer(t)
	networkID := uuid.NewString()

	a := newPeer(t, addr)
	b := newPeer(t, addr)
	a.bind(t, networkID)
	b.bind(t, networkID)

	// B's NAT rebinds it to a fresh source port; it re-binds with the same
	// device ID from the new address.
	bMoved := newPeer(t, addr)
	bMoved.deviceID = b.deviceID
	bMoved.bind(t, networkID)

	// Traffic now follows B to its new address.
	a.send(t, EncodeData(b.deviceID, []byte("after rebind")))

	_, got, err := DecodeData(bMoved.recv(t, 2*time.Second))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(got) != "after rebind" {
		t.Fatalf("payload = %q", got)
	}

	// The stale address was replaced, not duplicated.
	if bindings := sessions.Stats().Bindings; bindings != 2 {
		t.Fatalf("bindings = %d, want 2 after rebind", bindings)
	}
}

func TestKeepaliveRefreshesLiveness(t *testing.T) {
	addr, sessions := testServer(t)
	networkID := uuid.NewString()

	p := newPeer(t, addr)
	p.bind(t, networkID)

	time.Sleep(50 * time.Millisecond)
	p.send(t, EncodeKeepalive())
	// Give the server a moment to process it.
	time.Sleep(100 * time.Millisecond)

	// A keepalive keeps the binding alive past an eviction sweep whose
	// timeout is longer than the idle period so far.
	if removed := sessions.EvictIdle(time.Now()); removed != 0 {
		t.Fatalf("session was evicted despite a keepalive (removed %d)", removed)
	}
	if sessions.Stats().Bindings != 1 {
		t.Fatal("binding should still be present")
	}
}

func TestGarbageTrafficIsIgnoredSilently(t *testing.T) {
	addr, sessions := testServer(t)

	p := newPeer(t, addr)
	// Unknown frame type: internet background noise on an open UDP port.
	p.send(t, []byte{0x7f, 0xde, 0xad, 0xbe, 0xef})

	// No reply at all, so the node can't be used as a reflector.
	p.expectNothing(t, 300*time.Millisecond)
	if sessions.Stats().PacketsDropped == 0 {
		t.Fatal("the drop should have been counted")
	}
}

func TestSessionCapacityEnforced(t *testing.T) {
	sessions := NewSessionTable(time.Minute, 2)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}

	if err := sessions.Bind("net-1", uuid.New(), addr, time.Time{}); err != nil {
		t.Fatalf("first bind: %v", err)
	}
	if err := sessions.Bind("net-2", uuid.New(), addr, time.Time{}); err != nil {
		t.Fatalf("second bind: %v", err)
	}

	err := sessions.Bind("net-3", uuid.New(), addr, time.Time{})
	var capacityErr *ErrTooManySessions
	if err == nil {
		t.Fatal("expected the third session to be refused")
	}
	if !asErrTooManySessions(err, &capacityErr) {
		t.Fatalf("expected ErrTooManySessions, got %v", err)
	}

	// An existing session still accepts more devices.
	if err := sessions.Bind("net-1", uuid.New(), addr, time.Time{}); err != nil {
		t.Fatalf("binding into an existing session: %v", err)
	}
}

// asErrTooManySessions is a tiny errors.As wrapper kept local to the test
// to keep the assertion above readable.
func asErrTooManySessions(err error, target **ErrTooManySessions) bool {
	e, ok := err.(*ErrTooManySessions)
	if ok {
		*target = e
	}
	return ok
}

func TestEvictIdleRemovesSilentSessions(t *testing.T) {
	sessions := NewSessionTable(50*time.Millisecond, 0)
	addr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 5555}
	networkID := "net-idle"
	deviceID := uuid.New()

	if err := sessions.Bind(networkID, deviceID, addr, time.Time{}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if sessions.Stats().Sessions != 1 {
		t.Fatal("expected one session")
	}

	// Nothing evicted while fresh.
	if removed := sessions.EvictIdle(time.Now()); removed != 0 {
		t.Fatalf("evicted %d fresh sessions", removed)
	}

	// Past the idle timeout, the session is reaped and its address index
	// entry released.
	if removed := sessions.EvictIdle(time.Now().Add(time.Second)); removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if sessions.Stats().Sessions != 0 {
		t.Fatal("session should be gone")
	}
	if _, _, ok := sessions.Lookup(addr); ok {
		t.Fatal("address index should have been cleaned up")
	}
}

func TestExpiredBindingStopsRouting(t *testing.T) {
	sessions := NewSessionTable(time.Hour, 0)
	networkID := "net-exp"
	a, b := uuid.New(), uuid.New()
	addrA := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1111}
	addrB := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 2222}

	// B's authorization already lapsed.
	if err := sessions.Bind(networkID, a, addrA, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("bind a: %v", err)
	}
	if err := sessions.Bind(networkID, b, addrB, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("bind b: %v", err)
	}

	if _, ok := sessions.Route(networkID, a, b); ok {
		t.Fatal("routing to an expired binding must fail")
	}
	// The expired binding was dropped, forcing a re-bind.
	if _, _, ok := sessions.Lookup(addrB); ok {
		t.Fatal("expired binding should have been removed from the index")
	}
}

func TestRouteUnknownSession(t *testing.T) {
	sessions := NewSessionTable(time.Minute, 0)
	if _, ok := sessions.Route("no-such-network", uuid.New(), uuid.New()); ok {
		t.Fatal("expected routing in an unknown session to fail")
	}
}

func TestProtocolEncodeDecodeRoundTrip(t *testing.T) {
	deviceID := uuid.New()

	gotID, gotToken, err := DecodeBind(EncodeBind(deviceID, "tok-123"))
	if err != nil {
		t.Fatalf("decode bind: %v", err)
	}
	if gotID != deviceID || gotToken != "tok-123" {
		t.Fatalf("bind round trip = %s/%q", gotID, gotToken)
	}

	peerID := uuid.New()
	payload := []byte("payload")
	gotPeer, gotPayload, err := DecodeData(EncodeData(peerID, payload))
	if err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if gotPeer != peerID || string(gotPayload) != string(payload) {
		t.Fatalf("data round trip = %s/%q", gotPeer, gotPayload)
	}

	reason, err := DecodeError(EncodeError("nope"))
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if reason != "nope" {
		t.Fatalf("error round trip = %q", reason)
	}
}

func TestProtocolRejectsTruncatedFrames(t *testing.T) {
	if _, _, err := DecodeBind([]byte{FrameBind, 0x01}); err == nil {
		t.Fatal("expected a truncated BIND frame to be rejected")
	}
	if _, _, err := DecodeData([]byte{FrameData}); err == nil {
		t.Fatal("expected a truncated DATA frame to be rejected")
	}
	if _, _, err := DecodeBind(EncodeData(uuid.New(), nil)); err == nil {
		t.Fatal("expected DecodeBind to reject a DATA frame")
	}
}
