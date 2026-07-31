package usecase

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

type deviceFixture struct {
	svc      *DeviceService
	networks *NetworkService
	devices  *fakeDeviceRepo
	presence *fakePresence
	bus      *fakeBus
	users    *fakeUserRepo
	audit    *fakeAuditRepo
}

func newDeviceFixture(t *testing.T) *deviceFixture {
	t.Helper()
	networkRepo := newFakeNetworkRepo()
	memberRepo := newFakeMemberRepo()
	deviceRepo := newFakeDeviceRepo()
	networkRepo.members = memberRepo
	networkRepo.devices = deviceRepo
	users := newFakeUserRepo()
	presence := newFakePresence()
	bus := &fakeBus{}
	auditRepo := &fakeAuditRepo{}
	recorder := NewAuditRecorder(auditRepo, zap.NewNop())

	return &deviceFixture{
		svc:      NewDeviceService(deviceRepo, networkRepo, memberRepo, presence, bus, recorder),
		networks: NewNetworkService(networkRepo, memberRepo, users, recorder),
		devices:  deviceRepo,
		presence: presence,
		bus:      bus,
		users:    users,
		audit:    auditRepo,
	}
}

func (f *deviceFixture) seedUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	u := &domain.User{ID: uuid.New(), Email: email, DisplayName: email, Status: domain.UserStatusActive}
	if err := f.users.Create(t.Context(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

func TestRegisterDeviceAssignsVirtualIP(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")

	n, err := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)
	if err != nil {
		t.Fatalf("create network: %v", err)
	}

	d, err := f.svc.Register(ctx, owner, n.ID, "laptop", "linux", "6.1", "pubkey-aaa")
	if err != nil {
		t.Fatalf("register device: %v", err)
	}
	if d.VirtualIP == "" {
		t.Fatal("expected a virtual IP to be assigned")
	}
	if d.OS != domain.DeviceOSLinux {
		t.Fatalf("os = %s, want linux", d.OS)
	}
	f.audit.waitForAction(t, domain.AuditDeviceRegistered)
}

func TestRegisterDeviceAssignsDistinctIPs(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	other := f.seedUser(t, "other@example.com")

	n, _ := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)
	if _, err := f.networks.Join(ctx, other, n.InviteCode); err != nil {
		t.Fatalf("join: %v", err)
	}

	d1, err := f.svc.Register(ctx, owner, n.ID, "one", "linux", "", "pubkey-1")
	if err != nil {
		t.Fatalf("register first: %v", err)
	}
	d2, err := f.svc.Register(ctx, other, n.ID, "two", "windows", "", "pubkey-2")
	if err != nil {
		t.Fatalf("register second: %v", err)
	}
	if d1.VirtualIP == d2.VirtualIP {
		t.Fatalf("two devices got the same virtual IP: %s", d1.VirtualIP)
	}
}

func TestRegisterDeviceRequiresMembership(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	outsider := f.seedUser(t, "outsider@example.com")

	n, _ := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)

	_, err := f.svc.Register(ctx, outsider, n.ID, "sneaky", "linux", "", "pubkey-x")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a non-member, got %v", err)
	}
}

func TestRegisterDeviceRejectsEmptyFields(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	n, _ := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)

	if _, err := f.svc.Register(ctx, owner, n.ID, "", "linux", "", "pubkey"); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty name, got %v", err)
	}
	if _, err := f.svc.Register(ctx, owner, n.ID, "name", "linux", "", ""); !errors.Is(err, domain.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for empty public key, got %v", err)
	}
}

func TestHeartbeatUpdatesPresenceAndCounters(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	n, _ := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)

	d, err := f.svc.Register(ctx, owner, n.ID, "laptop", "linux", "", "pubkey-hb")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	publicIP := "203.0.113.9"
	natType := "port_restricted_cone"
	if err := f.svc.Heartbeat(ctx, d.ID, owner, &publicIP, nil, &natType, 1024, 2048); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}

	updated, err := f.devices.GetByID(ctx, d.ID)
	if err != nil {
		t.Fatalf("get device: %v", err)
	}
	if updated.Status != domain.DeviceStatusOnline {
		t.Fatalf("status = %s, want online", updated.Status)
	}
	if updated.BytesSent != 1024 || updated.BytesReceived != 2048 {
		t.Fatalf("counters = %d/%d, want 1024/2048", updated.BytesSent, updated.BytesReceived)
	}
	if updated.LastPublicIP == nil || *updated.LastPublicIP != publicIP {
		t.Fatalf("public IP not recorded: %v", updated.LastPublicIP)
	}

	ep, err := f.presence.Get(ctx, d.ID)
	if err != nil {
		t.Fatalf("presence get: %v", err)
	}
	if ep == nil {
		t.Fatal("expected the device to be marked online in the presence store")
	}
}

func TestHeartbeatFromAnotherUserRejected(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	attacker := f.seedUser(t, "attacker@example.com")
	n, _ := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)

	d, _ := f.svc.Register(ctx, owner, n.ID, "laptop", "linux", "", "pubkey-hb2")

	err := f.svc.Heartbeat(ctx, d.ID, attacker, nil, nil, nil, 0, 0)
	if err == nil {
		t.Fatal("expected another user's heartbeat to be rejected")
	}
	if !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("expected ErrForbidden/ErrNotFound, got %v", err)
	}
}

func TestListPeersExcludesSelf(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	other := f.seedUser(t, "other@example.com")

	n, _ := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)
	if _, err := f.networks.Join(ctx, other, n.InviteCode); err != nil {
		t.Fatalf("join: %v", err)
	}

	mine, _ := f.svc.Register(ctx, owner, n.ID, "mine", "linux", "", "pubkey-self")
	theirs, _ := f.svc.Register(ctx, other, n.ID, "theirs", "macos", "", "pubkey-peer")

	peers, err := f.svc.ListPeers(ctx, mine.ID, owner)
	if err != nil {
		t.Fatalf("list peers: %v", err)
	}
	if len(peers) != 1 {
		t.Fatalf("peer count = %d, want 1", len(peers))
	}
	if peers[0].ID != theirs.ID {
		t.Fatalf("peer = %s, want %s", peers[0].ID, theirs.ID)
	}
}

func TestDeleteDeviceRemovesIt(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	n, _ := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)

	d, _ := f.svc.Register(ctx, owner, n.ID, "laptop", "linux", "", "pubkey-del")

	if err := f.svc.Delete(ctx, d.ID, owner); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := f.devices.GetByID(ctx, d.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("device should be gone, got %v", err)
	}
	f.audit.waitForAction(t, domain.AuditDeviceRemoved)
}

func TestListForNetworkRequiresMembership(t *testing.T) {
	f := newDeviceFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	outsider := f.seedUser(t, "outsider@example.com")
	n, _ := f.networks.Create(ctx, owner, "Lab", "", "10.77.0.0/24", nil)

	if _, err := f.svc.ListForNetwork(ctx, n.ID, outsider); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}
}
