package tunnel

import (
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/client/internal/killswitch"
	"github.com/jitheshsisodiya/Ed-s/client/internal/netroute"
)

// fakeRouter records the order operations happen in, because with exit-node
// routing the order *is* the correctness: capturing the default before
// pinning the endpoint produces a tunnel that swallows its own transport
// and a machine that cannot reach anything.
type fakeRouter struct {
	mu    sync.Mutex
	calls []string

	snapshot    netroute.Default
	snapshotErr error
	pinErr      error
	captureErr  error

	pinned map[string]bool
}

func newFakeRouter() *fakeRouter {
	return &fakeRouter{
		snapshot: netroute.Default{
			Gateway:   net.ParseIP("192.0.2.1"),
			Interface: "eth0",
		},
		pinned: map[string]bool{},
	}
}

func (f *fakeRouter) record(s string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, s)
}

func (f *fakeRouter) order() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeRouter) Snapshot() (netroute.Default, error) {
	f.record("snapshot")
	return f.snapshot, f.snapshotErr
}

func (f *fakeRouter) PinEndpoint(ip net.IP, _ netroute.Default) error {
	f.record("pin " + ip.String())
	if f.pinErr != nil {
		return f.pinErr
	}
	f.mu.Lock()
	f.pinned[ip.String()] = true
	f.mu.Unlock()
	return nil
}

func (f *fakeRouter) UnpinEndpoint(ip net.IP) error {
	f.record("unpin " + ip.String())
	f.mu.Lock()
	delete(f.pinned, ip.String())
	f.mu.Unlock()
	return nil
}

func (f *fakeRouter) CaptureDefault(iface string) error {
	f.record("capture " + iface)
	return f.captureErr
}

func (f *fakeRouter) ReleaseDefault(iface string) error {
	f.record("release " + iface)
	return nil
}

func (f *fakeRouter) stillPinned() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for ip := range f.pinned {
		out = append(out, ip)
	}
	return out
}

type fakeSwitch struct {
	mu         sync.Mutex
	engaged    bool
	lastPolicy killswitch.Policy
	engageErr  error
	releases   int
}

func (f *fakeSwitch) Engage(p killswitch.Policy) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.engageErr != nil {
		return f.engageErr
	}
	f.engaged = true
	f.lastPolicy = p
	return nil
}

func (f *fakeSwitch) Release() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.engaged = false
	f.releases++
	return nil
}

func (f *fakeSwitch) Engaged() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.engaged, nil
}

func (f *fakeSwitch) policy() killswitch.Policy {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastPolicy
}

// exitFixture builds a tunnel with one peer already established, which is
// the state exit-node mode is entered from.
func exitFixture(t *testing.T) (*Tunnel, *fakeRouter, *fakeSwitch, uuid.UUID) {
	t.Helper()

	dev := newFakeDevice()
	router := newFakeRouter()
	ks := &fakeSwitch{}

	tun, err := New(Options{
		Device:      dev,
		Coordinator: newFakeCoordinator(),
		NetworkID:   uuid.NewString(),
		PublicKey:   "dGVzdC1wdWJsaWMta2V5LWZvci11bml0LXRlc3RzMTI=",
		Router:      router,
		KillSwitch:  ks,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	id := uuid.New()
	tun.peers[id] = &peer{
		deviceID:   id,
		deviceName: "exit-box",
		publicKey:  "cGVlci1wdWJsaWMta2V5LWZvci11bml0LXRlc3RzMTI=",
		virtualIP:  "100.84.0.9",
		remote:     &net.UDPAddr{IP: net.ParseIP("203.0.113.7"), Port: 51820},
		mode:       ModeDirect,
	}
	return tun, router, ks, id
}

// The single most important property in the package: the endpoint is routed
// around the tunnel *before* the tunnel takes the default route. Reversed,
// the WireGuard packets to the exit node would be routed into the tunnel
// they are trying to establish.
func TestExitNodePinsEndpointBeforeCapturingDefault(t *testing.T) {
	tun, router, _, id := exitFixture(t)

	if err := tun.SetExitNode(id, ExitNodeOptions{}); err != nil {
		t.Fatalf("SetExitNode: %v", err)
	}

	order := router.order()
	pinAt, captureAt := -1, -1
	for i, call := range order {
		if strings.HasPrefix(call, "pin ") && pinAt < 0 {
			pinAt = i
		}
		if strings.HasPrefix(call, "capture ") {
			captureAt = i
		}
	}
	if pinAt < 0 {
		t.Fatalf("the exit node's endpoint was never pinned: %v", order)
	}
	if captureAt < 0 {
		t.Fatalf("the default route was never captured: %v", order)
	}
	if pinAt > captureAt {
		t.Fatalf("the default was captured before the endpoint was pinned, so the "+
			"tunnel would carry its own transport: %v", order)
	}
}

// A half-applied exit node is a machine with no working route, so a failure
// part-way through has to leave nothing behind.
func TestExitNodeUnwindsWhenCapturingFails(t *testing.T) {
	tun, router, _, id := exitFixture(t)
	router.captureErr = errors.New("routing table is busy")

	if err := tun.SetExitNode(id, ExitNodeOptions{}); err == nil {
		t.Fatal("SetExitNode reported success despite the capture failing")
	}

	if pins := router.stillPinned(); len(pins) != 0 {
		t.Fatalf("pins survived a failed setup: %v", pins)
	}
	if _, ok := tun.ExitNode(); ok {
		t.Fatal("the tunnel still thinks it has an exit node after the setup failed")
	}
}

func TestExitNodeUnwindsWhenPinningFails(t *testing.T) {
	tun, router, _, id := exitFixture(t)
	router.pinErr = errors.New("no route to host")

	if err := tun.SetExitNode(id, ExitNodeOptions{}); err == nil {
		t.Fatal("SetExitNode reported success despite the pin failing")
	}
	for _, call := range router.order() {
		if strings.HasPrefix(call, "capture ") {
			t.Fatalf("the default route was captured after pinning failed: %v", router.order())
		}
	}
}

// Teardown has to undo everything in the reverse order, and unblocking has
// to come first: a user left with a kill switch engaged and no tunnel has
// no way to fix it.
func TestClearExitNodeReleasesTheLockBeforeTheRoute(t *testing.T) {
	tun, router, ks, id := exitFixture(t)

	if err := tun.SetExitNode(id, ExitNodeOptions{KillSwitch: true}); err != nil {
		t.Fatalf("SetExitNode: %v", err)
	}
	if !tun.KillSwitchEngaged() {
		t.Fatal("the kill switch did not engage")
	}

	if err := tun.ClearExitNode(); err != nil {
		t.Fatalf("ClearExitNode: %v", err)
	}

	if engaged, _ := ks.Engaged(); engaged {
		t.Fatal("the kill switch outlived the exit node")
	}
	if pins := router.stillPinned(); len(pins) != 0 {
		t.Fatalf("route pins outlived the exit node: %v", pins)
	}
	if _, ok := tun.ExitNode(); ok {
		t.Fatal("the exit node is still recorded after being cleared")
	}

	// And doing it twice must be harmless: teardown runs on paths that may
	// already have torn down.
	if err := tun.ClearExitNode(); err != nil {
		t.Fatalf("ClearExitNode is not idempotent: %v", err)
	}
}

// The kill switch has to permit the very endpoint the tunnel needs to come
// back, or the lock can never be lifted from the locked machine.
func TestKillSwitchPolicyPermitsTheWayBack(t *testing.T) {
	tun, _, ks, id := exitFixture(t)

	if err := tun.SetExitNode(id, ExitNodeOptions{KillSwitch: true, AllowLAN: true}); err != nil {
		t.Fatalf("SetExitNode: %v", err)
	}

	policy := ks.policy()
	if policy.Interface == "" {
		t.Fatal("the policy does not name the tunnel interface")
	}
	want := netip.MustParseAddrPort("203.0.113.7:51820")
	found := false
	for _, ep := range policy.Endpoints {
		if ep == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("the exit node's endpoint %s is not permitted, so the lock could "+
			"never be lifted: %v", want, policy.Endpoints)
	}
	if !policy.AllowLAN {
		t.Fatal("AllowLAN was requested but not passed through")
	}
}

// Turning the kill switch off must not silently leave it engaged.
func TestExitNodeWithoutKillSwitchDoesNotBlockAnything(t *testing.T) {
	tun, _, ks, id := exitFixture(t)

	if err := tun.SetExitNode(id, ExitNodeOptions{KillSwitch: false}); err != nil {
		t.Fatalf("SetExitNode: %v", err)
	}
	if engaged, _ := ks.Engaged(); engaged {
		t.Fatal("traffic was blocked without the kill switch being asked for")
	}
	if tun.KillSwitchEngaged() {
		t.Fatal("the tunnel reports a lock it never engaged")
	}
}

// Exit-node mode is opt-in machinery; a client that never asks for it must
// never touch the host's routing table.
func TestExitNodeRefusedWithoutARouter(t *testing.T) {
	dev := newFakeDevice()
	tun, err := New(Options{
		Device:      dev,
		Coordinator: newFakeCoordinator(),
		NetworkID:   uuid.NewString(),
		PublicKey:   "dGVzdC1wdWJsaWMta2V5LWZvci11bml0LXRlc3RzMTI=",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := tun.SetExitNode(uuid.New(), ExitNodeOptions{}); err == nil {
		t.Fatal("exit-node mode was accepted on a tunnel with no router")
	}
}

// A relayed exit node's real destination is the relay, not the loopback
// address WireGuard was handed. Pinning loopback would leave the relay's
// traffic inside the tunnel and the connection would never come up.
func TestTransportEndpointsIgnoreLoopback(t *testing.T) {
	tun, _, _, _ := exitFixture(t)

	id := uuid.New()
	tun.peers[id] = &peer{
		deviceID:   id,
		deviceName: "loopback-only",
		publicKey:  "b3RoZXItcHVibGljLWtleS1mb3ItdW5pdC10ZXN0czE=",
		virtualIP:  "100.84.0.10",
		remote:     &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 40000},
		mode:       ModeRelay,
	}

	for _, ip := range tun.transportEndpoints() {
		if ip.IsLoopback() {
			t.Fatalf("loopback %s was treated as a transport endpoint", ip)
		}
	}
}
