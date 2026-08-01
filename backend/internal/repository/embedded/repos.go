package embedded

import (
	"context"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

/* ---------- users ---------- */

// Users returns the UserRepository view of the store.
func (s *Store) Users() domain.UserRepository { return userRepo{s} }

type userRepo struct{ s *Store }

func (r userRepo) Create(_ context.Context, u *domain.User) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	for _, existing := range r.s.users {
		if strings.EqualFold(existing.Email, u.Email) {
			return domain.ErrAlreadyExists
		}
	}
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	now := time.Now().UTC()
	u.CreatedAt, u.UpdatedAt = now, now
	r.s.users[u.ID] = clone(u)
	r.s.touch()
	return nil
}

func (r userRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.User, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	u, ok := r.s.users[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(u), nil
}

func (r userRepo) GetByEmail(_ context.Context, email string) (*domain.User, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, u := range r.s.users {
		if strings.EqualFold(u.Email, email) {
			return clone(u), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r userRepo) GetByOAuthSubject(_ context.Context, provider, subject string) (*domain.User, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, u := range r.s.users {
		if u.OAuthProvider != nil && *u.OAuthProvider == provider &&
			u.OAuthSubject != nil && *u.OAuthSubject == subject {
			return clone(u), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r userRepo) Update(_ context.Context, u *domain.User) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.users[u.ID]; !ok {
		return domain.ErrNotFound
	}
	u.UpdatedAt = time.Now().UTC()
	r.s.users[u.ID] = clone(u)
	r.s.touch()
	return nil
}

/* ---------- refresh tokens ---------- */

// RefreshTokens returns the RefreshTokenRepository view.
func (s *Store) RefreshTokens() domain.RefreshTokenRepository { return refreshRepo{s} }

type refreshRepo struct{ s *Store }

func (r refreshRepo) Create(_ context.Context, rt *domain.RefreshToken) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if rt.ID == uuid.Nil {
		rt.ID = uuid.New()
	}
	rt.CreatedAt = time.Now().UTC()
	r.s.refresh[rt.ID] = clone(rt)
	r.s.touch()
	return nil
}

func (r refreshRepo) GetByHash(_ context.Context, hash string) (*domain.RefreshToken, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, rt := range r.s.refresh {
		if rt.TokenHash == hash {
			return clone(rt), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r refreshRepo) Revoke(_ context.Context, id uuid.UUID) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	rt, ok := r.s.refresh[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	rt.RevokedAt = &now
	r.s.touch()
	return nil
}

func (r refreshRepo) RevokeAllForUser(_ context.Context, userID uuid.UUID) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	now := time.Now().UTC()
	for _, rt := range r.s.refresh {
		if rt.UserID == userID && rt.RevokedAt == nil {
			revoked := now
			rt.RevokedAt = &revoked
		}
	}
	r.s.touch()
	return nil
}

/* ---------- password resets ---------- */

// PasswordResets returns the PasswordResetRepository view.
func (s *Store) PasswordResets() domain.PasswordResetRepository { return resetRepo{s} }

type resetRepo struct{ s *Store }

func (r resetRepo) Create(_ context.Context, pr *domain.PasswordReset) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if pr.ID == uuid.Nil {
		pr.ID = uuid.New()
	}
	pr.CreatedAt = time.Now().UTC()
	r.s.resets[pr.ID] = clone(pr)
	r.s.touch()
	return nil
}

func (r resetRepo) GetByHash(_ context.Context, hash string) (*domain.PasswordReset, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, pr := range r.s.resets {
		if pr.TokenHash == hash {
			return clone(pr), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r resetRepo) MarkUsed(_ context.Context, id uuid.UUID) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	pr, ok := r.s.resets[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	pr.UsedAt = &now
	r.s.touch()
	return nil
}

/* ---------- networks ---------- */

// Networks returns the NetworkRepository view.
func (s *Store) Networks() domain.NetworkRepository { return networkRepo{s} }

type networkRepo struct{ s *Store }

func (r networkRepo) Create(_ context.Context, n *domain.Network) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, existing := range r.s.networks {
		if existing.InviteCode == n.InviteCode {
			return domain.ErrAlreadyExists
		}
	}
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	now := time.Now().UTC()
	n.CreatedAt, n.UpdatedAt = now, now
	r.s.networks[n.ID] = clone(n)
	r.s.touch()
	return nil
}

func (r networkRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Network, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	n, ok := r.s.networks[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(n), nil
}

func (r networkRepo) GetByInviteCode(_ context.Context, code string) (*domain.Network, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, n := range r.s.networks {
		if !strings.EqualFold(n.InviteCode, code) {
			continue
		}
		// An expired code is not a code. Reporting "not found" rather than
		// "expired" keeps this endpoint from confirming which codes ever
		// existed.
		if n.InviteCodeExpiresAt != nil && time.Now().After(*n.InviteCodeExpiresAt) {
			return nil, domain.ErrNotFound
		}
		return clone(n), nil
	}
	return nil, domain.ErrNotFound
}

func (r networkRepo) ListForUser(_ context.Context, userID uuid.UUID) ([]*domain.Network, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()

	var out []*domain.Network
	for key, m := range r.s.members {
		if m.UserID != userID {
			continue
		}
		if n, ok := r.s.networks[key.network]; ok {
			out = append(out, clone(n))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (r networkRepo) Update(_ context.Context, n *domain.Network) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.networks[n.ID]; !ok {
		return domain.ErrNotFound
	}
	n.UpdatedAt = time.Now().UTC()
	r.s.networks[n.ID] = clone(n)
	r.s.touch()
	return nil
}

func (r networkRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.networks[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.s.networks, id)

	// Postgres does this with ON DELETE CASCADE; here it is explicit, and
	// leaving it out would strand memberships and devices pointing at a
	// network that no longer exists.
	for key := range r.s.members {
		if key.network == id {
			delete(r.s.members, key)
		}
	}
	for devID, d := range r.s.devices {
		if d.NetworkID == id {
			delete(r.s.devices, devID)
		}
	}
	r.s.touch()
	return nil
}

func (r networkRepo) RotateInviteCode(_ context.Context, id uuid.UUID, code string, expires *time.Time) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	n, ok := r.s.networks[id]
	if !ok {
		return domain.ErrNotFound
	}
	n.InviteCode = code
	n.InviteCodeExpiresAt = expires
	n.UpdatedAt = time.Now().UTC()
	r.s.touch()
	return nil
}

func (r networkRepo) CountMembers(_ context.Context, networkID uuid.UUID) (int, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	count := 0
	for key := range r.s.members {
		if key.network == networkID {
			count++
		}
	}
	return count, nil
}

func (r networkRepo) CountDevices(_ context.Context, networkID uuid.UUID) (int, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	count := 0
	for _, d := range r.s.devices {
		if d.NetworkID == networkID {
			count++
		}
	}
	return count, nil
}

/* ---------- members ---------- */

// Members returns the NetworkMemberRepository view.
func (s *Store) Members() domain.NetworkMemberRepository { return memberRepo{s} }

type memberRepo struct{ s *Store }

func (r memberRepo) Create(_ context.Context, m *domain.NetworkMember) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	key := memberKey{m.NetworkID, m.UserID}
	if _, ok := r.s.members[key]; ok {
		return domain.ErrAlreadyExists
	}
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	m.JoinedAt = time.Now().UTC()
	r.s.members[key] = clone(m)
	r.s.touch()
	return nil
}

func (r memberRepo) Get(_ context.Context, networkID, userID uuid.UUID) (*domain.NetworkMember, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	m, ok := r.s.members[memberKey{networkID, userID}]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(m), nil
}

func (r memberRepo) List(_ context.Context, networkID uuid.UUID) ([]*domain.NetworkMember, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []*domain.NetworkMember
	for key, m := range r.s.members {
		if key.network == networkID {
			out = append(out, clone(m))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JoinedAt.Before(out[j].JoinedAt) })
	return out, nil
}

func (r memberRepo) ListNetworkIDsForUser(_ context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []uuid.UUID
	for key, m := range r.s.members {
		if m.UserID == userID {
			out = append(out, key.network)
		}
	}
	return out, nil
}

func (r memberRepo) UpdateRole(_ context.Context, networkID, userID uuid.UUID, role domain.NetworkRole) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	m, ok := r.s.members[memberKey{networkID, userID}]
	if !ok {
		return domain.ErrNotFound
	}
	m.Role = role
	r.s.touch()
	return nil
}

func (r memberRepo) Delete(_ context.Context, networkID, userID uuid.UUID) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	key := memberKey{networkID, userID}
	if _, ok := r.s.members[key]; !ok {
		return domain.ErrNotFound
	}
	delete(r.s.members, key)

	// A member who leaves takes their devices on that network with them,
	// or those devices keep an address on a network their owner can no
	// longer see.
	for id, d := range r.s.devices {
		if d.NetworkID == networkID && d.UserID == userID {
			delete(r.s.devices, id)
		}
	}
	r.s.touch()
	return nil
}

func (r memberRepo) CountByRole(_ context.Context, networkID uuid.UUID, role domain.NetworkRole) (int, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	count := 0
	for key, m := range r.s.members {
		if key.network == networkID && m.Role == role {
			count++
		}
	}
	return count, nil
}

/* ---------- devices ---------- */

// Devices returns the DeviceRepository view.
func (s *Store) Devices() domain.DeviceRepository { return deviceRepo{s} }

type deviceRepo struct{ s *Store }

func (r deviceRepo) Create(_ context.Context, d *domain.Device) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, existing := range r.s.devices {
		if existing.PublicKey == d.PublicKey {
			return domain.ErrAlreadyExists
		}
	}
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	now := time.Now().UTC()
	d.CreatedAt, d.UpdatedAt = now, now
	r.s.devices[d.ID] = clone(d)
	r.s.touch()
	return nil
}

func (r deviceRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	d, ok := r.s.devices[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(d), nil
}

func (r deviceRepo) GetByPublicKey(_ context.Context, key string) (*domain.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, d := range r.s.devices {
		if d.PublicKey == key {
			return clone(d), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r deviceRepo) GetByNetworkAndUser(_ context.Context, networkID, userID uuid.UUID) (*domain.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	for _, d := range r.s.devices {
		if d.NetworkID == networkID && d.UserID == userID {
			return clone(d), nil
		}
	}
	return nil, domain.ErrNotFound
}

func (r deviceRepo) ListByNetwork(_ context.Context, networkID uuid.UUID) ([]*domain.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return r.s.listDevicesLocked(func(d *domain.Device) bool { return d.NetworkID == networkID }), nil
}

func (r deviceRepo) ListByUser(_ context.Context, userID uuid.UUID) ([]*domain.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return r.s.listDevicesLocked(func(d *domain.Device) bool { return d.UserID == userID }), nil
}

func (r deviceRepo) ListPeers(_ context.Context, deviceID uuid.UUID) ([]*domain.Device, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	self, ok := r.s.devices[deviceID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return r.s.listDevicesLocked(func(d *domain.Device) bool {
		return d.NetworkID == self.NetworkID && d.ID != deviceID
	}), nil
}

func (s *Store) listDevicesLocked(match func(*domain.Device) bool) []*domain.Device {
	var out []*domain.Device
	for _, d := range s.devices {
		if match(d) {
			out = append(out, clone(d))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (r deviceRepo) Update(_ context.Context, d *domain.Device) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.devices[d.ID]; !ok {
		return domain.ErrNotFound
	}
	d.UpdatedAt = time.Now().UTC()
	r.s.devices[d.ID] = clone(d)
	r.s.touch()
	return nil
}

func (r deviceRepo) UpdateHeartbeat(_ context.Context, id uuid.UUID, status domain.DeviceStatus,
	publicIP, privateIP, natType *string, sent, received int64) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	d, ok := r.s.devices[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	d.Status = status
	d.LastSeenAt = &now
	// Nil and empty both mean "no new information", matching the
	// COALESCE(NULLIF(...)) the Postgres statement uses: a heartbeat that
	// omits an endpoint must not erase the one already known.
	if publicIP != nil && *publicIP != "" {
		d.LastPublicIP = publicIP
	}
	if privateIP != nil && *privateIP != "" {
		d.LastPrivateIP = privateIP
	}
	if natType != nil && *natType != "" {
		d.NATType = natType
	}
	d.BytesSent, d.BytesReceived = sent, received
	d.UpdatedAt = now
	r.s.touch()
	return nil
}

func (r deviceRepo) Delete(_ context.Context, id uuid.UUID) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.devices[id]; !ok {
		return domain.ErrNotFound
	}
	delete(r.s.devices, id)
	r.s.touch()
	return nil
}

// NextFreeVirtualIP walks the network's range and returns the first address
// nobody holds.
//
// Linear from the bottom rather than random: on a network of six machines
// the addresses come out as .2, .3, .4 rather than scattered across a /16,
// and people read and type these.
func (r deviceRepo) NextFreeVirtualIP(_ context.Context, networkID uuid.UUID, cidr string) (string, error) {
	_, subnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return "", domain.ErrInvalidInput
	}

	r.s.mu.RLock()
	taken := map[string]bool{}
	for _, d := range r.s.devices {
		if d.NetworkID == networkID {
			taken[d.VirtualIP] = true
		}
	}
	r.s.mu.RUnlock()

	ip := make(net.IP, len(subnet.IP))
	copy(ip, subnet.IP)

	// .0 is the network address and the first host is .1, conventionally the
	// gateway, so allocation starts at .2 to match what people expect from
	// every other LAN they have used.
	for i := 0; i < 2; i++ {
		incrementIP(ip)
	}

	for subnet.Contains(ip) {
		candidate := ip.String()
		if !taken[candidate] && !isBroadcast(ip, subnet) {
			return candidate, nil
		}
		incrementIP(ip)
	}
	return "", domain.ErrNetworkFull
}

func incrementIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			return
		}
	}
}

// isBroadcast reports whether ip is the all-ones address of its subnet,
// which is not assignable to a host.
func isBroadcast(ip net.IP, subnet *net.IPNet) bool {
	for i := range ip {
		if ip[i]|subnet.Mask[i] != 0xff {
			return false
		}
	}
	return true
}

func (r deviceRepo) CountDistinctActiveUsers(_ context.Context, since time.Time) (int, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	seen := map[uuid.UUID]bool{}
	for _, d := range r.s.devices {
		if d.LastSeenAt != nil && d.LastSeenAt.After(since) {
			seen[d.UserID] = true
		}
	}
	return len(seen), nil
}

func (r deviceRepo) CountDistinctActiveNetworks(_ context.Context, since time.Time) (int, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	seen := map[uuid.UUID]bool{}
	for _, d := range r.s.devices {
		if d.LastSeenAt != nil && d.LastSeenAt.After(since) {
			seen[d.NetworkID] = true
		}
	}
	return len(seen), nil
}

func (r deviceRepo) CountTotal(_ context.Context) (int, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return len(r.s.devices), nil
}
