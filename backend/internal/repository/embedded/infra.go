package embedded

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

/* ---------- presence ---------- */

// Presence returns the PresenceStore view.
func (s *Store) Presence() domain.PresenceStore { return presenceStore{s} }

type presenceStore struct{ s *Store }

func (p presenceStore) SetOnline(_ context.Context, networkID, deviceID uuid.UUID,
	ep domain.PresenceEndpoint, ttl time.Duration) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	p.s.presence[deviceID] = &presenceEntry{
		networkID: networkID,
		endpoint:  ep,
		expires:   time.Now().Add(ttl),
	}
	// Deliberately not persisted: presence is a claim about right now, and
	// restoring it from a file would report machines as online that have
	// not been heard from since before the restart.
	return nil
}

func (p presenceStore) Remove(_ context.Context, _, deviceID uuid.UUID) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	delete(p.s.presence, deviceID)
	return nil
}

func (p presenceStore) Get(_ context.Context, deviceID uuid.UUID) (*domain.PresenceEndpoint, error) {
	p.s.mu.RLock()
	defer p.s.mu.RUnlock()
	entry, ok := p.s.presence[deviceID]
	if !ok || time.Now().After(entry.expires) {
		return nil, nil
	}
	ep := entry.endpoint
	return &ep, nil
}

func (p presenceStore) OnlineInNetwork(_ context.Context, networkID uuid.UUID) ([]uuid.UUID, error) {
	p.s.mu.RLock()
	defer p.s.mu.RUnlock()
	now := time.Now()
	var out []uuid.UUID
	for id, entry := range p.s.presence {
		if entry.networkID == networkID && now.Before(entry.expires) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (p presenceStore) CountOnline(_ context.Context) (int, error) {
	p.s.mu.RLock()
	defer p.s.mu.RUnlock()
	now := time.Now()
	count := 0
	for _, entry := range p.s.presence {
		if now.Before(entry.expires) {
			count++
		}
	}
	return count, nil
}

/* ---------- peer event bus ---------- */

// Bus returns the PeerEventBus view.
func (s *Store) Bus() domain.PeerEventBus { return s.bus }

// bus fans peer events out to in-process subscribers.
//
// The Redis implementation exists so events reach clients attached to a
// different backend replica. With one process there are no other replicas,
// so this is a slice of channels — which is not a simplification of the
// Redis version so much as what the Redis version is emulating.
type bus struct {
	mu   sync.Mutex
	subs map[*subscription]struct{}
}

func newBus() *bus { return &bus{subs: map[*subscription]struct{}{}} }

type subscription struct {
	bus      *bus
	networks map[uuid.UUID]bool
	ch       chan domain.PeerEvent
	once     sync.Once
}

func (b *bus) Publish(_ context.Context, event domain.PeerEvent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for sub := range b.subs {
		if !sub.networks[event.NetworkID] {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			// A subscriber that is not reading is dropped from rather than
			// blocking the publisher. Topology events are refreshed by the
			// next heartbeat anyway, so a missed one costs a few seconds of
			// staleness; a blocked publisher would stall every other
			// client's connect.
		}
	}
	return nil
}

func (b *bus) Subscribe(ctx context.Context, networkIDs []uuid.UUID) (domain.PeerEventSubscription, error) {
	sub := &subscription{
		bus:      b,
		networks: make(map[uuid.UUID]bool, len(networkIDs)),
		ch:       make(chan domain.PeerEvent, 64),
	}
	for _, id := range networkIDs {
		sub.networks[id] = true
	}

	b.mu.Lock()
	b.subs[sub] = struct{}{}
	b.mu.Unlock()

	// A caller that goes away without closing still has its subscription
	// reclaimed, so a client dropping its gRPC stream cannot leak a channel.
	go func() {
		<-ctx.Done()
		_ = sub.Close()
	}()

	return sub, nil
}

func (s *subscription) Events() <-chan domain.PeerEvent { return s.ch }

func (s *subscription) Close() error {
	s.once.Do(func() {
		s.bus.mu.Lock()
		delete(s.bus.subs, s)
		s.bus.mu.Unlock()
		close(s.ch)
	})
	return nil
}

/* ---------- ICE rendezvous ---------- */

// ICE returns the ICEStore view.
func (s *Store) ICE() domain.ICEStore { return iceStore{s} }

type iceStore struct{ s *Store }

func (i iceStore) Put(_ context.Context, from, to uuid.UUID,
	candidates []domain.ICECandidate, ttl time.Duration) error {
	i.s.mu.Lock()
	defer i.s.mu.Unlock()
	i.s.ice[iceKey{from: from, to: to}] = &iceEntry{
		candidates: append([]domain.ICECandidate(nil), candidates...),
		expires:    time.Now().Add(ttl),
	}
	return nil
}

func (i iceStore) Get(_ context.Context, forDevice, fromDevice uuid.UUID) ([]domain.ICECandidate, error) {
	i.s.mu.RLock()
	defer i.s.mu.RUnlock()
	entry, ok := i.s.ice[iceKey{from: fromDevice, to: forDevice}]
	if !ok || time.Now().After(entry.expires) {
		return nil, nil
	}
	return append([]domain.ICECandidate(nil), entry.candidates...), nil
}

/* ---------- rate limiting ---------- */

// Limiter returns the RateLimiter view.
func (s *Store) Limiter() domain.RateLimiter { return limiter{s} }

type limiter struct{ s *Store }

func (l limiter) Allow(_ context.Context, key string, limit int, window time.Duration) (bool, error) {
	l.s.mu.Lock()
	defer l.s.mu.Unlock()

	now := time.Now()
	entry, ok := l.s.limits[key]
	if !ok || now.After(entry.resets) {
		entry = &limitEntry{resets: now.Add(window)}
		l.s.limits[key] = entry
	}
	entry.count++
	entry.limitAt = limit
	return entry.count <= limit, nil
}

/* ---------- logs ---------- */

// ConnectionLogs returns the ConnectionLogRepository view.
func (s *Store) ConnectionLogs() domain.ConnectionLogRepository { return connLogRepo{s} }

type connLogRepo struct{ s *Store }

func (r connLogRepo) Create(_ context.Context, l *domain.ConnectionLog) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	l.CreatedAt = time.Now().UTC()
	r.s.connLogs = capLog(append(r.s.connLogs, clone(l)))
	r.s.touch()
	return nil
}

func (r connLogRepo) ListByNetwork(_ context.Context, networkID uuid.UUID, limit, offset int) ([]*domain.ConnectionLog, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return page(r.s.connLogs, limit, offset, func(l *domain.ConnectionLog) bool {
		return l.NetworkID == networkID
	}), nil
}

func (r connLogRepo) List(_ context.Context, limit, offset int) ([]*domain.ConnectionLog, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return page(r.s.connLogs, limit, offset, func(*domain.ConnectionLog) bool { return true }), nil
}

func (r connLogRepo) SumBytesByEventType(_ context.Context, eventType domain.ConnectionEventType,
	since time.Time) (int64, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var total int64
	for _, l := range r.s.connLogs {
		if l.EventType == eventType && l.CreatedAt.After(since) {
			total += l.BytesSent + l.BytesReceived
		}
	}
	return total, nil
}

func (r connLogRepo) CountByEventType(_ context.Context, since time.Time) (map[domain.ConnectionEventType]int64, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	counts := map[domain.ConnectionEventType]int64{}
	for _, l := range r.s.connLogs {
		if l.CreatedAt.After(since) {
			counts[l.EventType]++
		}
	}
	return counts, nil
}

// AuditLogs returns the AuditLogRepository view.
func (s *Store) AuditLogs() domain.AuditLogRepository { return auditLogRepo{s} }

type auditLogRepo struct{ s *Store }

func (r auditLogRepo) Create(_ context.Context, l *domain.AuditLog) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	l.CreatedAt = time.Now().UTC()
	r.s.auditLogs = capLog(append(r.s.auditLogs, clone(l)))
	r.s.touch()
	return nil
}

func (r auditLogRepo) ListByNetwork(_ context.Context, networkID uuid.UUID, limit, offset int) ([]*domain.AuditLog, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return page(r.s.auditLogs, limit, offset, func(l *domain.AuditLog) bool {
		return l.NetworkID != nil && *l.NetworkID == networkID
	}), nil
}

func (r auditLogRepo) List(_ context.Context, limit, offset int) ([]*domain.AuditLog, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	return page(r.s.auditLogs, limit, offset, func(*domain.AuditLog) bool { return true }), nil
}

// ErrorLogs returns the ErrorLogRepository view.
func (s *Store) ErrorLogs() domain.ErrorLogRepository { return errLogRepo{s} }

type errLogRepo struct{ s *Store }

func (r errLogRepo) Create(_ context.Context, l *domain.ErrorLog) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	l.CreatedAt = time.Now().UTC()
	r.s.errLogs = capLog(append(r.s.errLogs, clone(l)))
	r.s.touch()
	return nil
}

// page filters newest-first and applies limit/offset, which is what every
// log listing wants: the most recent entry is the one being looked for.
func page[T any](rows []*T, limit, offset int, match func(*T) bool) []*T {
	if limit <= 0 || limit > maxLogRows {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	out := make([]*T, 0, limit)
	skipped := 0
	for i := len(rows) - 1; i >= 0 && len(out) < limit; i-- {
		if !match(rows[i]) {
			continue
		}
		if skipped < offset {
			skipped++
			continue
		}
		out = append(out, clone(rows[i]))
	}
	return out
}

/* ---------- relay servers ---------- */

// Relays returns the RelayServerRepository view.
//
// A local install normally has no relay at all: every machine is on the
// same network or reachable directly, and a relay only earns its place when
// two peers cannot see each other. The repository exists so the same code
// paths run, and stays empty until somebody registers one.
func (s *Store) Relays() domain.RelayServerRepository { return relayRepo{s} }

type relayRepo struct{ s *Store }

func (r relayRepo) Create(_ context.Context, rs *domain.RelayServer) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if rs.ID == uuid.Nil {
		rs.ID = uuid.New()
	}
	rs.CreatedAt = time.Now().UTC()
	r.s.relays[rs.ID] = clone(rs)
	r.s.touch()
	return nil
}

// Upsert keys on the public endpoint so a relay that restarts reclaims its
// row instead of leaving a dead duplicate behind for the allocator to pick.
func (r relayRepo) Upsert(_ context.Context, rs *domain.RelayServer) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()

	for _, existing := range r.s.relays {
		if existing.Hostname == rs.Hostname && existing.RelayPort == rs.RelayPort {
			rs.ID = existing.ID
			rs.CreatedAt = existing.CreatedAt
			r.s.relays[existing.ID] = clone(rs)
			r.s.touch()
			return nil
		}
	}
	if rs.ID == uuid.Nil {
		rs.ID = uuid.New()
	}
	rs.CreatedAt = time.Now().UTC()
	r.s.relays[rs.ID] = clone(rs)
	r.s.touch()
	return nil
}

func (r relayRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.RelayServer, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	rs, ok := r.s.relays[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return clone(rs), nil
}

func (r relayRepo) ListActive(_ context.Context) ([]*domain.RelayServer, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()
	var out []*domain.RelayServer
	for _, rs := range r.s.relays {
		if isRelayHealthy(rs) {
			out = append(out, clone(rs))
		}
	}
	return out, nil
}

func (r relayRepo) PickLeastLoaded(_ context.Context, preferredRegion string) (*domain.RelayServer, error) {
	r.s.mu.RLock()
	defer r.s.mu.RUnlock()

	var best *domain.RelayServer
	for _, rs := range r.s.relays {
		if !isRelayHealthy(rs) || rs.CurrentLoad >= rs.Capacity {
			continue
		}
		// The preferred region wins outright; among equals, the emptiest
		// node. Load is compared as a fraction so a small relay and a large
		// one are judged on how full they are, not how many sessions they
		// happen to hold.
		if best == nil ||
			(rs.Region == preferredRegion && best.Region != preferredRegion) ||
			(((rs.Region == preferredRegion) == (best.Region == preferredRegion)) &&
				loadFraction(rs) < loadFraction(best)) {
			best = rs
		}
	}
	if best == nil {
		return nil, domain.ErrNoRelayAvailable
	}
	return clone(best), nil
}

func (r relayRepo) IncrementLoad(_ context.Context, id uuid.UUID, delta int) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	rs, ok := r.s.relays[id]
	if !ok {
		return domain.ErrNotFound
	}
	rs.CurrentLoad += delta
	if rs.CurrentLoad < 0 {
		rs.CurrentLoad = 0
	}
	r.s.touch()
	return nil
}

func (r relayRepo) Heartbeat(_ context.Context, id uuid.UUID, load int) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	rs, ok := r.s.relays[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now().UTC()
	rs.CurrentLoad = load
	rs.LastHeartbeatAt = &now
	r.s.touch()
	return nil
}

// relayStaleAfter is how long a relay may go without a heartbeat before it
// stops being offered. Allocating to a node that died two minutes ago sends
// a client somewhere that will never answer.
const relayStaleAfter = 90 * time.Second

func isRelayHealthy(rs *domain.RelayServer) bool {
	if rs.Status != "" && rs.Status != "active" && rs.Status != "healthy" {
		return false
	}
	return rs.LastHeartbeatAt != nil && time.Since(*rs.LastHeartbeatAt) < relayStaleAfter
}

func loadFraction(rs *domain.RelayServer) float64 {
	if rs.Capacity <= 0 {
		return 1
	}
	return float64(rs.CurrentLoad) / float64(rs.Capacity)
}
