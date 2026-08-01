package embedded

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "server.json"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// Addresses are read aloud and typed by hand, so they are handed out from
// the bottom of the range in order rather than scattered.
func TestVirtualIPsAreAllocatedInOrderFromTwo(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	networkID := uuid.New()

	for i, want := range []string{"100.84.0.2", "100.84.0.3", "100.84.0.4"} {
		got, err := s.Devices().NextFreeVirtualIP(ctx, networkID, "100.84.0.0/16")
		if err != nil {
			t.Fatalf("allocation %d: %v", i, err)
		}
		if got != want {
			t.Fatalf("allocation %d = %s, want %s", i, got, want)
		}
		mustCreateDevice(t, s, networkID, got)
	}
}

// A device that goes away releases its address, or a network churning
// machines slowly exhausts a range nobody is using.
func TestDeletedDeviceReleasesItsAddress(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	networkID := uuid.New()

	first := mustCreateDevice(t, s, networkID, "100.84.0.2")
	mustCreateDevice(t, s, networkID, "100.84.0.3")

	if err := s.Devices().Delete(ctx, first.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got, err := s.Devices().NextFreeVirtualIP(ctx, networkID, "100.84.0.0/16")
	if err != nil {
		t.Fatalf("NextFreeVirtualIP: %v", err)
	}
	if got != "100.84.0.2" {
		t.Fatalf("got %s, want the freed 100.84.0.2", got)
	}
}

// Two networks are separate address spaces. Sharing the allocator's view
// across them would hand the second network's first machine a .3.
func TestAddressesAreScopedToTheirNetwork(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	a, b := uuid.New(), uuid.New()
	mustCreateDevice(t, s, a, "100.84.0.2")

	got, err := s.Devices().NextFreeVirtualIP(ctx, b, "100.84.0.0/16")
	if err != nil {
		t.Fatalf("NextFreeVirtualIP: %v", err)
	}
	if got != "100.84.0.2" {
		t.Fatalf("got %s, want 100.84.0.2 — the other network's devices should not count", got)
	}
}

// A /30 holds exactly one usable host under this scheme (.0 network,
// .1 gateway, .2 host, .3 broadcast), so the second request must fail
// cleanly rather than hand out the broadcast address.
func TestAFullNetworkIsReportedRatherThanOverAllocated(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	networkID := uuid.New()

	first, err := s.Devices().NextFreeVirtualIP(ctx, networkID, "10.9.9.0/30")
	if err != nil {
		t.Fatalf("first allocation: %v", err)
	}
	if first != "10.9.9.2" {
		t.Fatalf("first = %s, want 10.9.9.2", first)
	}
	mustCreateDevice(t, s, networkID, first)

	if _, err := s.Devices().NextFreeVirtualIP(ctx, networkID, "10.9.9.0/30"); err != domain.ErrNetworkFull {
		t.Fatalf("second allocation returned %v, want ErrNetworkFull", err)
	}
}

// Deleting a network has to take its memberships and devices with it. In
// Postgres that is ON DELETE CASCADE; here it is code, and code can forget.
func TestDeletingANetworkRemovesWhatDependedOnIt(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	networkID, userID := uuid.New(), uuid.New()
	if err := s.Members().Create(ctx, &domain.NetworkMember{
		NetworkID: networkID, UserID: userID, Role: domain.NetworkRoleOwner,
	}); err != nil {
		t.Fatalf("Members.Create: %v", err)
	}
	device := mustCreateDevice(t, s, networkID, "100.84.0.2")

	if err := s.Networks().Create(ctx, &domain.Network{
		ID: networkID, Name: "Home", CIDR: "100.84.0.0/16", InviteCode: "AAAA",
	}); err != nil {
		t.Fatalf("Networks.Create: %v", err)
	}
	if err := s.Networks().Delete(ctx, networkID); err != nil {
		t.Fatalf("Networks.Delete: %v", err)
	}

	if _, err := s.Members().Get(ctx, networkID, userID); err != domain.ErrNotFound {
		t.Fatalf("membership survived its network: %v", err)
	}
	if _, err := s.Devices().GetByID(ctx, device.ID); err != domain.ErrNotFound {
		t.Fatalf("device survived its network: %v", err)
	}
}

// An expired invite is not an invite, and must not report itself as one.
func TestExpiredInviteCodesDoNotResolve(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour)
	if err := s.Networks().Create(ctx, &domain.Network{
		ID: uuid.New(), Name: "Old", CIDR: "10.0.0.0/24",
		InviteCode: "EXPIRED", InviteCodeExpiresAt: &past,
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := s.Networks().GetByInviteCode(ctx, "EXPIRED"); err != domain.ErrNotFound {
		t.Fatalf("an expired code resolved: %v", err)
	}
}

// A heartbeat that omits an endpoint must not erase the one already known —
// the Postgres statement uses COALESCE(NULLIF(...)) for exactly this.
func TestHeartbeatWithoutAnEndpointKeepsTheKnownOne(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	device := mustCreateDevice(t, s, uuid.New(), "100.84.0.2")

	known := "203.0.113.7"
	if err := s.Devices().UpdateHeartbeat(ctx, device.ID, domain.DeviceStatusOnline,
		&known, nil, nil, 10, 20); err != nil {
		t.Fatalf("first heartbeat: %v", err)
	}

	empty := ""
	if err := s.Devices().UpdateHeartbeat(ctx, device.ID, domain.DeviceStatusOnline,
		&empty, nil, nil, 30, 40); err != nil {
		t.Fatalf("second heartbeat: %v", err)
	}

	got, err := s.Devices().GetByID(ctx, device.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.LastPublicIP == nil || *got.LastPublicIP != known {
		t.Fatalf("public IP is %v, want it preserved as %s", got.LastPublicIP, known)
	}
	if got.BytesSent != 30 {
		t.Fatalf("counters did not update: %d", got.BytesSent)
	}
}

// Everything has to survive a restart, which is the entire reason the file
// exists.
func TestDataSurvivesAReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.json")
	ctx := context.Background()

	first, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	user := &domain.User{ID: uuid.New(), Email: "a@example.com", PasswordHash: "hash"}
	if err := first.Users().Create(ctx, user); err != nil {
		t.Fatalf("Users.Create: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	got, err := second.Users().GetByEmail(ctx, "a@example.com")
	if err != nil {
		t.Fatalf("the account did not survive the restart: %v", err)
	}
	if got.PasswordHash != "hash" {
		t.Fatalf("password hash was not preserved")
	}
	if second.IsEmpty() {
		t.Fatal("IsEmpty reported true with an account on disk")
	}
}

// A corrupt file must stop the process, not silently present as an empty
// store — which would look like every account had been deleted, and the next
// snapshot would overwrite the file that still had them in it.
func TestACorruptFileRefusesToLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("a corrupt data file loaded as if it were empty")
	}
}

// The bus is what tells a connected client that somebody joined.
func TestPeerEventsReachSubscribersOfThatNetworkOnly(t *testing.T) {
	s := newStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mine, theirs := uuid.New(), uuid.New()
	sub, err := s.Bus().Subscribe(ctx, []uuid.UUID{mine})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer sub.Close()

	if err := s.Bus().Publish(ctx, domain.PeerEvent{
		Type: domain.PeerEventJoined, NetworkID: theirs, DeviceID: uuid.New(),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	want := uuid.New()
	if err := s.Bus().Publish(ctx, domain.PeerEvent{
		Type: domain.PeerEventJoined, NetworkID: mine, DeviceID: want,
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case got := <-sub.Events():
		if got.NetworkID != mine || got.DeviceID != want {
			t.Fatalf("received the wrong event: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event arrived")
	}
}

// Presence is a claim about right now, so it has to lapse on its own.
func TestPresenceExpires(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	networkID, deviceID := uuid.New(), uuid.New()

	if err := s.Presence().SetOnline(ctx, networkID, deviceID,
		domain.PresenceEndpoint{PublicIP: "203.0.113.7"}, 40*time.Millisecond); err != nil {
		t.Fatalf("SetOnline: %v", err)
	}
	if ep, _ := s.Presence().Get(ctx, deviceID); ep == nil {
		t.Fatal("a device marked online reported as offline")
	}

	time.Sleep(80 * time.Millisecond)
	if ep, _ := s.Presence().Get(ctx, deviceID); ep != nil {
		t.Fatal("presence outlived its TTL")
	}
	online, _ := s.Presence().OnlineInNetwork(ctx, networkID)
	if len(online) != 0 {
		t.Fatalf("expired presence still counted: %v", online)
	}
}

func TestRateLimiterBoundsAKeyWithinItsWindow(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		allowed, err := s.Limiter().Allow(ctx, "login:a@example.com", 3, time.Minute)
		if err != nil || !allowed {
			t.Fatalf("attempt %d was refused inside the budget (err %v)", i, err)
		}
	}
	allowed, err := s.Limiter().Allow(ctx, "login:a@example.com", 3, time.Minute)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if allowed {
		t.Fatal("the fourth attempt was allowed past a budget of three")
	}

	// A different key has its own budget.
	if allowed, _ := s.Limiter().Allow(ctx, "login:b@example.com", 3, time.Minute); !allowed {
		t.Fatal("one key's budget was charged to another")
	}
}

func mustCreateDevice(t *testing.T, s *Store, networkID uuid.UUID, ip string) *domain.Device {
	t.Helper()
	d := &domain.Device{
		ID:        uuid.New(),
		UserID:    uuid.New(),
		NetworkID: networkID,
		Name:      "device-" + ip,
		PublicKey: "key-" + ip,
		VirtualIP: ip,
		Status:    domain.DeviceStatusUnknown,
	}
	if err := s.Devices().Create(context.Background(), d); err != nil {
		t.Fatalf("Devices.Create(%s): %v", ip, err)
	}
	return d
}
