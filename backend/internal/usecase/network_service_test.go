package usecase

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

type networkFixture struct {
	svc      *NetworkService
	networks *fakeNetworkRepo
	members  *fakeMemberRepo
	users    *fakeUserRepo
	devices  *fakeDeviceRepo
	audit    *fakeAuditRepo
}

func newNetworkFixture(t *testing.T) *networkFixture {
	t.Helper()
	networks := newFakeNetworkRepo()
	members := newFakeMemberRepo()
	devices := newFakeDeviceRepo()
	networks.members = members
	networks.devices = devices
	users := newFakeUserRepo()
	auditRepo := &fakeAuditRepo{}

	return &networkFixture{
		svc:      NewNetworkService(networks, members, users, NewAuditRecorder(auditRepo, zap.NewNop())),
		networks: networks,
		members:  members,
		users:    users,
		devices:  devices,
		audit:    auditRepo,
	}
}

// seedUser inserts a user directly so network tests don't depend on AuthService.
func (f *networkFixture) seedUser(t *testing.T, email string) uuid.UUID {
	t.Helper()
	u := &domain.User{ID: uuid.New(), Email: email, DisplayName: email, Status: domain.UserStatusActive}
	if err := f.users.Create(t.Context(), u); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return u.ID
}

func TestCreateNetworkMakesCallerOwner(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")

	n, err := f.svc.Create(ctx, owner, "Home Lab", "my lab", "10.77.0.0/24", []string{"1.1.1.1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if n.OwnerID != owner {
		t.Fatalf("owner = %s, want %s", n.OwnerID, owner)
	}
	if n.InviteCode == "" {
		t.Fatal("expected an invite code to be generated")
	}
	if n.CallerRole != domain.NetworkRoleOwner {
		t.Fatalf("caller role = %s, want owner", n.CallerRole)
	}

	m, err := f.members.Get(ctx, n.ID, owner)
	if err != nil {
		t.Fatalf("owner membership row missing: %v", err)
	}
	if m.Role != domain.NetworkRoleOwner {
		t.Fatalf("membership role = %s, want owner", m.Role)
	}
}

func TestCreateNetworkRejectsBadCIDR(t *testing.T) {
	f := newNetworkFixture(t)
	owner := f.seedUser(t, "owner@example.com")

	if _, err := f.svc.Create(t.Context(), owner, "Bad", "", "not-a-cidr", nil); err == nil {
		t.Fatal("expected an error for a malformed CIDR")
	}
}

func TestJoinNetworkByInviteCode(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	joiner := f.seedUser(t, "joiner@example.com")

	n, err := f.svc.Create(ctx, owner, "Team", "", "10.80.0.0/24", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	joined, err := f.svc.Join(ctx, joiner, n.InviteCode)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	if joined.ID != n.ID {
		t.Fatalf("joined network = %s, want %s", joined.ID, n.ID)
	}
	if joined.CallerRole != domain.NetworkRoleMember {
		t.Fatalf("joiner role = %s, want member", joined.CallerRole)
	}
	f.audit.waitForAction(t, domain.AuditNetworkMemberJoined)
}

func TestJoinIsIdempotent(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	joiner := f.seedUser(t, "joiner@example.com")

	n, _ := f.svc.Create(ctx, owner, "Team", "", "10.80.0.0/24", nil)
	if _, err := f.svc.Join(ctx, joiner, n.InviteCode); err != nil {
		t.Fatalf("first join: %v", err)
	}
	if _, err := f.svc.Join(ctx, joiner, n.InviteCode); err != nil {
		t.Fatalf("second join should be idempotent, got: %v", err)
	}

	members, err := f.members.List(ctx, n.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 2 { // owner + joiner, not duplicated
		t.Fatalf("member count = %d, want 2", len(members))
	}
}

func TestJoinWithBadInviteCodeRejected(t *testing.T) {
	f := newNetworkFixture(t)
	joiner := f.seedUser(t, "joiner@example.com")

	if _, err := f.svc.Join(t.Context(), joiner, "NOTAREALCODE"); !errors.Is(err, domain.ErrInviteInvalid) {
		t.Fatalf("expected ErrInviteInvalid, got %v", err)
	}
}

func TestRotateInviteInvalidatesOldCode(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	joiner := f.seedUser(t, "joiner@example.com")

	n, _ := f.svc.Create(ctx, owner, "Team", "", "10.80.0.0/24", nil)
	oldCode := n.InviteCode

	newCode, _, err := f.svc.RotateInvite(ctx, n.ID, owner)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if newCode == oldCode {
		t.Fatal("invite code was not rotated")
	}

	if _, err := f.svc.Join(ctx, joiner, oldCode); !errors.Is(err, domain.ErrInviteInvalid) {
		t.Fatalf("expected old code to be invalid, got %v", err)
	}
	if _, err := f.svc.Join(ctx, joiner, newCode); err != nil {
		t.Fatalf("new code should work: %v", err)
	}
}

func TestMemberCannotRotateInvite(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	member := f.seedUser(t, "member@example.com")

	n, _ := f.svc.Create(ctx, owner, "Team", "", "10.80.0.0/24", nil)
	if _, err := f.svc.Join(ctx, member, n.InviteCode); err != nil {
		t.Fatalf("join: %v", err)
	}

	if _, _, err := f.svc.RotateInvite(ctx, n.ID, member); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a plain member, got %v", err)
	}
}

func TestNonMemberCannotReadNetwork(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	outsider := f.seedUser(t, "outsider@example.com")

	n, _ := f.svc.Create(ctx, owner, "Private", "", "10.90.0.0/24", nil)

	if _, err := f.svc.Get(ctx, n.ID, outsider); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for a non-member, got %v", err)
	}
}

func TestOnlyOwnerCanDeleteNetwork(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	admin := f.seedUser(t, "admin@example.com")

	n, _ := f.svc.Create(ctx, owner, "Team", "", "10.80.0.0/24", nil)
	if _, err := f.svc.Join(ctx, admin, n.InviteCode); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := f.svc.UpdateMemberRole(ctx, n.ID, owner, admin, domain.NetworkRoleAdmin); err != nil {
		t.Fatalf("promote to admin: %v", err)
	}

	// Even an admin cannot delete the network.
	if err := f.svc.Delete(ctx, n.ID, admin); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden for admin delete, got %v", err)
	}
	if err := f.svc.Delete(ctx, n.ID, owner); err != nil {
		t.Fatalf("owner delete: %v", err)
	}
	if _, err := f.networks.GetByID(ctx, n.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("network should be gone, got %v", err)
	}
}

func TestRemoveMember(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	member := f.seedUser(t, "member@example.com")

	n, _ := f.svc.Create(ctx, owner, "Team", "", "10.80.0.0/24", nil)
	if _, err := f.svc.Join(ctx, member, n.InviteCode); err != nil {
		t.Fatalf("join: %v", err)
	}

	if err := f.svc.RemoveMember(ctx, n.ID, owner, member); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	if _, err := f.members.Get(ctx, n.ID, member); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("membership should be gone, got %v", err)
	}
	f.audit.waitForAction(t, domain.AuditNetworkMemberRemoved)
}

func TestOwnerCannotBeRemoved(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	owner := f.seedUser(t, "owner@example.com")
	admin := f.seedUser(t, "admin@example.com")

	n, _ := f.svc.Create(ctx, owner, "Team", "", "10.80.0.0/24", nil)
	if _, err := f.svc.Join(ctx, admin, n.InviteCode); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := f.svc.UpdateMemberRole(ctx, n.ID, owner, admin, domain.NetworkRoleAdmin); err != nil {
		t.Fatalf("promote: %v", err)
	}

	err := f.svc.RemoveMember(ctx, n.ID, admin, owner)
	if err == nil {
		t.Fatal("expected removing the owner to be rejected")
	}
	if !errors.Is(err, domain.ErrForbidden) && !errors.Is(err, domain.ErrLastOwner) {
		t.Fatalf("expected ErrForbidden/ErrLastOwner, got %v", err)
	}
}

func TestListForUserOnlyReturnsOwnNetworks(t *testing.T) {
	f := newNetworkFixture(t)
	ctx := t.Context()
	alice := f.seedUser(t, "alice@example.com")
	bob := f.seedUser(t, "bob@example.com")

	if _, err := f.svc.Create(ctx, alice, "Alice Net", "", "10.1.0.0/24", nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := f.svc.Create(ctx, bob, "Bob Net", "", "10.2.0.0/24", nil); err != nil {
		t.Fatalf("create: %v", err)
	}

	aliceNets, err := f.svc.ListForUser(ctx, alice)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(aliceNets) != 1 || aliceNets[0].Name != "Alice Net" {
		t.Fatalf("alice should see exactly her own network, got %d", len(aliceNets))
	}
}
