package usecase

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// In-memory fakes for the repository interfaces, so the use-case layer can be
// tested without a live Postgres/Redis.

type fakeUserRepo struct {
	mu    sync.Mutex
	byID  map[uuid.UUID]*domain.User
	calls int
}

func newFakeUserRepo() *fakeUserRepo {
	return &fakeUserRepo{byID: map[uuid.UUID]*domain.User{}}
}

func (r *fakeUserRepo) Create(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.byID {
		if strings.EqualFold(existing.Email, u.Email) {
			return domain.ErrAlreadyExists
		}
	}
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	u.CreatedAt = time.Now()
	copied := *u
	r.byID[u.ID] = &copied
	return nil
}

func (r *fakeUserRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *u
	return &copied, nil
}

func (r *fakeUserRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	for _, u := range r.byID {
		if strings.EqualFold(u.Email, email) {
			copied := *u
			return &copied, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *fakeUserRepo) GetByOAuthSubject(_ context.Context, provider, subject string) (*domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, u := range r.byID {
		if u.OAuthProvider != nil && *u.OAuthProvider == provider && u.OAuthSubject != nil && *u.OAuthSubject == subject {
			copied := *u
			return &copied, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *fakeUserRepo) Update(_ context.Context, u *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[u.ID]; !ok {
		return domain.ErrNotFound
	}
	copied := *u
	r.byID[u.ID] = &copied
	return nil
}

type fakeRefreshRepo struct {
	mu     sync.Mutex
	byHash map[string]*domain.RefreshToken
}

func newFakeRefreshRepo() *fakeRefreshRepo {
	return &fakeRefreshRepo{byHash: map[string]*domain.RefreshToken{}}
}

func (r *fakeRefreshRepo) Create(_ context.Context, rt *domain.RefreshToken) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *rt
	r.byHash[rt.TokenHash] = &copied
	return nil
}

func (r *fakeRefreshRepo) GetByHash(_ context.Context, hash string) (*domain.RefreshToken, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rt, ok := r.byHash[hash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *rt
	return &copied, nil
}

func (r *fakeRefreshRepo) Revoke(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, rt := range r.byHash {
		if rt.ID == id {
			rt.RevokedAt = &now
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *fakeRefreshRepo) RevokeAllForUser(_ context.Context, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, rt := range r.byHash {
		if rt.UserID == userID {
			rt.RevokedAt = &now
		}
	}
	return nil
}

type fakeResetRepo struct {
	mu     sync.Mutex
	byHash map[string]*domain.PasswordReset
}

func newFakeResetRepo() *fakeResetRepo {
	return &fakeResetRepo{byHash: map[string]*domain.PasswordReset{}}
}

func (r *fakeResetRepo) Create(_ context.Context, pr *domain.PasswordReset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *pr
	r.byHash[pr.TokenHash] = &copied
	return nil
}

func (r *fakeResetRepo) GetByHash(_ context.Context, hash string) (*domain.PasswordReset, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pr, ok := r.byHash[hash]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *pr
	return &copied, nil
}

func (r *fakeResetRepo) MarkUsed(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for _, pr := range r.byHash {
		if pr.ID == id {
			pr.UsedAt = &now
			return nil
		}
	}
	return domain.ErrNotFound
}

type fakeNetworkRepo struct {
	mu       sync.Mutex
	byID     map[uuid.UUID]*domain.Network
	byInvite map[string]uuid.UUID
	members  *fakeMemberRepo
	devices  *fakeDeviceRepo
}

func newFakeNetworkRepo() *fakeNetworkRepo {
	return &fakeNetworkRepo{byID: map[uuid.UUID]*domain.Network{}, byInvite: map[string]uuid.UUID{}}
}

func (r *fakeNetworkRepo) Create(_ context.Context, n *domain.Network) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	n.CreatedAt = time.Now()
	copied := *n
	r.byID[n.ID] = &copied
	r.byInvite[n.InviteCode] = n.ID
	return nil
}

func (r *fakeNetworkRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Network, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.byID[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *n
	return &copied, nil
}

func (r *fakeNetworkRepo) GetByInviteCode(_ context.Context, code string) (*domain.Network, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byInvite[code]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *r.byID[id]
	return &copied, nil
}

func (r *fakeNetworkRepo) ListForUser(ctx context.Context, userID uuid.UUID) ([]*domain.Network, error) {
	ids, err := r.members.ListNetworkIDsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*domain.Network, 0, len(ids))
	for _, id := range ids {
		if n, ok := r.byID[id]; ok {
			copied := *n
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeNetworkRepo) Update(_ context.Context, n *domain.Network) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.byID[n.ID]; !ok {
		return domain.ErrNotFound
	}
	copied := *n
	r.byID[n.ID] = &copied
	return nil
}

func (r *fakeNetworkRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	delete(r.byInvite, n.InviteCode)
	delete(r.byID, id)
	return nil
}

func (r *fakeNetworkRepo) RotateInviteCode(_ context.Context, id uuid.UUID, newCode string, expiresAt *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, ok := r.byID[id]
	if !ok {
		return domain.ErrNotFound
	}
	delete(r.byInvite, n.InviteCode)
	n.InviteCode = newCode
	n.InviteCodeExpiresAt = expiresAt
	r.byInvite[newCode] = id
	return nil
}

func (r *fakeNetworkRepo) CountMembers(ctx context.Context, networkID uuid.UUID) (int, error) {
	ms, err := r.members.List(ctx, networkID)
	return len(ms), err
}

func (r *fakeNetworkRepo) CountDevices(ctx context.Context, networkID uuid.UUID) (int, error) {
	if r.devices == nil {
		return 0, nil
	}
	ds, err := r.devices.ListByNetwork(ctx, networkID)
	return len(ds), err
}

type fakeMemberRepo struct {
	mu      sync.Mutex
	members []*domain.NetworkMember
}

func newFakeMemberRepo() *fakeMemberRepo { return &fakeMemberRepo{} }

func (r *fakeMemberRepo) Create(_ context.Context, m *domain.NetworkMember) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.JoinedAt = time.Now()
	copied := *m
	r.members = append(r.members, &copied)
	return nil
}

func (r *fakeMemberRepo) Get(_ context.Context, networkID, userID uuid.UUID) (*domain.NetworkMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.members {
		if m.NetworkID == networkID && m.UserID == userID {
			copied := *m
			return &copied, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *fakeMemberRepo) List(_ context.Context, networkID uuid.UUID) ([]*domain.NetworkMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.NetworkMember
	for _, m := range r.members {
		if m.NetworkID == networkID {
			copied := *m
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeMemberRepo) ListNetworkIDsForUser(_ context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []uuid.UUID
	for _, m := range r.members {
		if m.UserID == userID {
			out = append(out, m.NetworkID)
		}
	}
	return out, nil
}

func (r *fakeMemberRepo) UpdateRole(_ context.Context, networkID, userID uuid.UUID, role domain.NetworkRole) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range r.members {
		if m.NetworkID == networkID && m.UserID == userID {
			m.Role = role
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *fakeMemberRepo) Delete(_ context.Context, networkID, userID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, m := range r.members {
		if m.NetworkID == networkID && m.UserID == userID {
			r.members = append(r.members[:i], r.members[i+1:]...)
			return nil
		}
	}
	return domain.ErrNotFound
}

func (r *fakeMemberRepo) CountByRole(_ context.Context, networkID uuid.UUID, role domain.NetworkRole) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, m := range r.members {
		if m.NetworkID == networkID && m.Role == role {
			count++
		}
	}
	return count, nil
}

type fakeDeviceRepo struct {
	mu      sync.Mutex
	devices map[uuid.UUID]*domain.Device
}

func newFakeDeviceRepo() *fakeDeviceRepo {
	return &fakeDeviceRepo{devices: map[uuid.UUID]*domain.Device{}}
}

func (r *fakeDeviceRepo) Create(_ context.Context, d *domain.Device) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.devices {
		if existing.PublicKey == d.PublicKey {
			return domain.ErrAlreadyExists
		}
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	d.CreatedAt = time.Now()
	copied := *d
	r.devices[d.ID] = &copied
	return nil
}

func (r *fakeDeviceRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	copied := *d
	return &copied, nil
}

func (r *fakeDeviceRepo) GetByPublicKey(_ context.Context, publicKey string) (*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.devices {
		if d.PublicKey == publicKey {
			copied := *d
			return &copied, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *fakeDeviceRepo) GetByNetworkAndUser(_ context.Context, networkID, userID uuid.UUID) (*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.devices {
		if d.NetworkID == networkID && d.UserID == userID {
			copied := *d
			return &copied, nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r *fakeDeviceRepo) ListByNetwork(_ context.Context, networkID uuid.UUID) ([]*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Device
	for _, d := range r.devices {
		if d.NetworkID == networkID {
			copied := *d
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeDeviceRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Device
	for _, d := range r.devices {
		if d.UserID == userID {
			copied := *d
			out = append(out, &copied)
		}
	}
	return out, nil
}

func (r *fakeDeviceRepo) ListPeers(ctx context.Context, deviceID uuid.UUID) ([]*domain.Device, error) {
	self, err := r.GetByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	all, err := r.ListByNetwork(ctx, self.NetworkID)
	if err != nil {
		return nil, err
	}
	out := make([]*domain.Device, 0, len(all))
	for _, d := range all {
		if d.ID != deviceID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (r *fakeDeviceRepo) Update(_ context.Context, d *domain.Device) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.devices[d.ID]; !ok {
		return domain.ErrNotFound
	}
	copied := *d
	r.devices[d.ID] = &copied
	return nil
}

func (r *fakeDeviceRepo) UpdateHeartbeat(_ context.Context, id uuid.UUID, status domain.DeviceStatus, publicIP, privateIP, natType *string, sent, received int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now()
	d.Status = status
	d.LastSeenAt = &now
	d.LastPublicIP = publicIP
	d.LastPrivateIP = privateIP
	d.NATType = natType
	d.BytesSent = sent
	d.BytesReceived = received
	return nil
}

func (r *fakeDeviceRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.devices[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.devices, id)
	return nil
}

// NextFreeVirtualIP mimics the real allocator: lowest unused host address in
// the /24 the tests use.
func (r *fakeDeviceRepo) NextFreeVirtualIP(_ context.Context, networkID uuid.UUID, cidr string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	used := map[string]bool{}
	for _, d := range r.devices {
		if d.NetworkID == networkID {
			used[d.VirtualIP] = true
		}
	}
	base := strings.TrimSuffix(cidr, "/24")
	parts := strings.Split(base, ".")
	if len(parts) != 4 {
		return "", domain.ErrInvalidInput
	}
	prefix := parts[0] + "." + parts[1] + "." + parts[2] + "."
	for i := 2; i < 255; i++ {
		candidate := prefix + itoa(i)
		if !used[candidate] {
			return candidate, nil
		}
	}
	return "", domain.ErrNetworkFull
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [4]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}

func (r *fakeDeviceRepo) CountDistinctActiveUsers(_ context.Context, since time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	for _, d := range r.devices {
		if d.LastSeenAt != nil && d.LastSeenAt.After(since) {
			seen[d.UserID] = true
		}
	}
	return len(seen), nil
}

func (r *fakeDeviceRepo) CountDistinctActiveNetworks(_ context.Context, since time.Time) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[uuid.UUID]bool{}
	for _, d := range r.devices {
		if d.LastSeenAt != nil && d.LastSeenAt.After(since) {
			seen[d.NetworkID] = true
		}
	}
	return len(seen), nil
}

func (r *fakeDeviceRepo) CountTotal(_ context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.devices), nil
}

type fakeAuditRepo struct {
	mu   sync.Mutex
	logs []*domain.AuditLog
}

func (r *fakeAuditRepo) Create(_ context.Context, l *domain.AuditLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copied := *l
	r.logs = append(r.logs, &copied)
	return nil
}

func (r *fakeAuditRepo) ListByNetwork(_ context.Context, networkID uuid.UUID, limit, offset int) ([]*domain.AuditLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.AuditLog
	for _, l := range r.logs {
		if l.NetworkID != nil && *l.NetworkID == networkID {
			out = append(out, l)
		}
	}
	return out, nil
}

func (r *fakeAuditRepo) List(_ context.Context, limit, offset int) ([]*domain.AuditLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.logs, nil
}

// actions returns the recorded audit actions, for assertions.
func (r *fakeAuditRepo) actions() []domain.AuditAction {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.AuditAction, 0, len(r.logs))
	for _, l := range r.logs {
		out = append(out, l.Action)
	}
	return out
}

// waitForAction polls until the given audit action has been recorded, or
// fails the test. AuditRecorder.Record writes asynchronously by design, so
// assertions on audit output have to await the write rather than read it
// immediately after the call under test returns.
func (r *fakeAuditRepo) waitForAction(t *testing.T, want domain.AuditAction) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, a := range r.actions() {
			if a == want {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("audit action %q was never recorded (recorded: %v)", want, r.actions())
}

type fakeRateLimiter struct {
	allow bool
}

func (f *fakeRateLimiter) Allow(_ context.Context, _ string, _ int, _ time.Duration) (bool, error) {
	return f.allow, nil
}

type fakePresence struct {
	mu      sync.Mutex
	online  map[uuid.UUID]domain.PresenceEndpoint
	network map[uuid.UUID][]uuid.UUID
}

func newFakePresence() *fakePresence {
	return &fakePresence{online: map[uuid.UUID]domain.PresenceEndpoint{}, network: map[uuid.UUID][]uuid.UUID{}}
}

func (p *fakePresence) SetOnline(_ context.Context, networkID, deviceID uuid.UUID, ep domain.PresenceEndpoint, _ time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.online[deviceID] = ep
	for _, id := range p.network[networkID] {
		if id == deviceID {
			return nil
		}
	}
	p.network[networkID] = append(p.network[networkID], deviceID)
	return nil
}

func (p *fakePresence) Remove(_ context.Context, networkID, deviceID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.online, deviceID)
	ids := p.network[networkID]
	for i, id := range ids {
		if id == deviceID {
			p.network[networkID] = append(ids[:i], ids[i+1:]...)
			break
		}
	}
	return nil
}

func (p *fakePresence) Get(_ context.Context, deviceID uuid.UUID) (*domain.PresenceEndpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ep, ok := p.online[deviceID]
	if !ok {
		return nil, nil
	}
	return &ep, nil
}

func (p *fakePresence) OnlineInNetwork(_ context.Context, networkID uuid.UUID) ([]uuid.UUID, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.network[networkID], nil
}

func (p *fakePresence) CountOnline(_ context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.online), nil
}

type fakeBus struct {
	mu     sync.Mutex
	events []domain.PeerEvent
}

func (b *fakeBus) Publish(_ context.Context, ev domain.PeerEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.events = append(b.events, ev)
	return nil
}

func (b *fakeBus) Subscribe(_ context.Context, _ []uuid.UUID) (domain.PeerEventSubscription, error) {
	return &fakeSubscription{ch: make(chan domain.PeerEvent)}, nil
}

func (b *fakeBus) published() []domain.PeerEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]domain.PeerEvent(nil), b.events...)
}

type fakeSubscription struct {
	ch chan domain.PeerEvent
}

func (s *fakeSubscription) Events() <-chan domain.PeerEvent { return s.ch }
func (s *fakeSubscription) Close() error                    { close(s.ch); return nil }

// realHasher uses the production bcrypt hasher so password round-trips are
// exercised end to end (bcrypt at min cost keeps the tests fast).
func realHasher() PasswordHasher { return auth.NewPasswordHasher(4) }

type stubMFA struct {
	validCode string
}

func (s stubMFA) GenerateSecret(_ string) (string, string, error) {
	return "STUBSECRET", "otpauth://totp/NexusVPN:test?secret=STUBSECRET", nil
}

func (s stubMFA) Validate(_, code string) bool { return code == s.validCode }
