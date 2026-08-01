// Package embedded is a self-contained implementation of every repository
// and infrastructure interface the control plane needs, backed by memory and
// a single JSON file on disk.
//
// It exists so NexusVPN can run on one person's desktop with nothing else
// installed. The Postgres and Redis implementations are the right answer for
// a deployment serving an organisation; they are the wrong answer for
// somebody who wants their three machines on one network and does not want
// to learn Docker to get there.
//
// The tradeoffs are deliberate and worth stating plainly:
//
//   - One process. There is no coordination between replicas because there
//     are no replicas. Anything that exists to fan out across instances —
//     the pub/sub bus, the presence store — collapses to an in-process
//     structure here, which is strictly simpler and strictly faster.
//   - Everything is held in memory. The whole dataset for a household or a
//     small office is a few hundred rows; the file on disk exists so it
//     survives a restart, not because it is ever too big to hold.
//   - Writes are durable at the granularity of a snapshot, not a
//     transaction. A crash between snapshots can lose the last few seconds.
//     For "which machines are on my network" that is an acceptable trade;
//     for an organisation's audit log it is not, which is the line where
//     you should be running Postgres instead.
package embedded

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// Store holds every entity, guarded by one lock.
//
// One lock rather than one per collection: the operations here are
// microseconds long and frequently touch two collections at once (creating a
// network also creates its owner's membership), and a single lock makes
// those atomic without any ordering discipline to get wrong. Contention is
// not a concern at this scale — it would be at the scale where you should
// have moved to Postgres anyway.
type Store struct {
	mu sync.RWMutex

	users     map[uuid.UUID]*domain.User
	networks  map[uuid.UUID]*domain.Network
	members   map[memberKey]*domain.NetworkMember
	devices   map[uuid.UUID]*domain.Device
	refresh   map[uuid.UUID]*domain.RefreshToken
	resets    map[uuid.UUID]*domain.PasswordReset
	relays    map[uuid.UUID]*domain.RelayServer
	connLogs  []*domain.ConnectionLog
	auditLogs []*domain.AuditLog
	errLogs   []*domain.ErrorLog

	// Ephemeral state, never persisted: presence and ICE candidates are
	// meaningless across a restart, because every client has to
	// re-handshake anyway.
	presence map[uuid.UUID]*presenceEntry
	ice      map[iceKey]*iceEntry
	limits   map[string]*limitEntry

	bus *bus

	path  string
	dirty bool
	stop  chan struct{}
	once  sync.Once
}

type memberKey struct{ network, user uuid.UUID }

type iceKey struct{ from, to uuid.UUID }

type presenceEntry struct {
	networkID uuid.UUID
	endpoint  domain.PresenceEndpoint
	expires   time.Time
}

type iceEntry struct {
	candidates []domain.ICECandidate
	expires    time.Time
}

type limitEntry struct {
	count   int
	resets  time.Time
	limitAt int
}

// Open loads a store from path, creating it if it does not exist.
//
// The directory is created 0700 and the file 0600: it holds password hashes
// and refresh tokens, and on a shared desktop the other accounts have no
// business reading them.
func Open(path string) (*Store, error) {
	s := &Store{
		users:    map[uuid.UUID]*domain.User{},
		networks: map[uuid.UUID]*domain.Network{},
		members:  map[memberKey]*domain.NetworkMember{},
		devices:  map[uuid.UUID]*domain.Device{},
		refresh:  map[uuid.UUID]*domain.RefreshToken{},
		resets:   map[uuid.UUID]*domain.PasswordReset{},
		relays:   map[uuid.UUID]*domain.RelayServer{},
		presence: map[uuid.UUID]*presenceEntry{},
		ice:      map[iceKey]*iceEntry{},
		limits:   map[string]*limitEntry{},
		bus:      newBus(),
		path:     path,
		stop:     make(chan struct{}),
	}

	if path != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("embedded: create data directory: %w", err)
		}
		if err := s.load(); err != nil {
			return nil, err
		}
		go s.snapshotLoop()
	}
	return s, nil
}

// Close flushes any pending change and stops the snapshot loop.
func (s *Store) Close() error {
	var err error
	s.once.Do(func() {
		close(s.stop)
		err = s.Flush()
	})
	return err
}

// Flush writes the current state to disk if anything has changed.
func (s *Store) Flush() error {
	s.mu.Lock()
	if !s.dirty || s.path == "" {
		s.mu.Unlock()
		return nil
	}
	snap := s.snapshotLocked()
	s.dirty = false
	s.mu.Unlock()

	blob, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return fmt.Errorf("embedded: encode snapshot: %w", err)
	}

	// Written to a sibling and renamed, so a crash mid-write leaves the
	// previous good file rather than a truncated one. Rename is atomic on
	// every filesystem this runs on.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return fmt.Errorf("embedded: write snapshot: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("embedded: replace snapshot: %w", err)
	}
	return nil
}

// snapshotLoop persists on a timer rather than on every write.
//
// A desktop that is idle writes nothing; a desktop under load writes once a
// second instead of once per request. The dirty flag means an idle process
// does no I/O at all.
func (s *Store) snapshotLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			_ = s.Flush()
			s.expire()
		}
	}
}

// expire drops presence, ICE and rate-limit entries that have aged out.
func (s *Store) expire() {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	for id, p := range s.presence {
		if now.After(p.expires) {
			delete(s.presence, id)
		}
	}
	for k, e := range s.ice {
		if now.After(e.expires) {
			delete(s.ice, k)
		}
	}
	for k, l := range s.limits {
		if now.After(l.resets) {
			delete(s.limits, k)
		}
	}
}

// touch marks the store as needing a snapshot. Callers hold the write lock.
func (s *Store) touch() { s.dirty = true }

/* ---------- persistence format ---------- */

// snapshot is the on-disk shape. It is a plain struct of slices rather than
// the live maps so the file is stable, diffable and obviously inspectable —
// somebody should be able to open it and see their own data.
type snapshot struct {
	Version   int                     `json:"version"`
	Users     []*domain.User          `json:"users"`
	Networks  []*domain.Network       `json:"networks"`
	Members   []*domain.NetworkMember `json:"members"`
	Devices   []*domain.Device        `json:"devices"`
	Refresh   []*domain.RefreshToken  `json:"refreshTokens"`
	Resets    []*domain.PasswordReset `json:"passwordResets"`
	Relays    []*domain.RelayServer   `json:"relays"`
	ConnLogs  []*domain.ConnectionLog `json:"connectionLogs"`
	AuditLogs []*domain.AuditLog      `json:"auditLogs"`
	ErrLogs   []*domain.ErrorLog      `json:"errorLogs"`
}

// snapshotVersion is bumped when the on-disk shape changes incompatibly.
const snapshotVersion = 1

func (s *Store) snapshotLocked() snapshot {
	snap := snapshot{Version: snapshotVersion}
	for _, u := range s.users {
		snap.Users = append(snap.Users, u)
	}
	for _, n := range s.networks {
		snap.Networks = append(snap.Networks, n)
	}
	for _, m := range s.members {
		snap.Members = append(snap.Members, m)
	}
	for _, d := range s.devices {
		snap.Devices = append(snap.Devices, d)
	}
	for _, r := range s.refresh {
		snap.Refresh = append(snap.Refresh, r)
	}
	for _, r := range s.resets {
		snap.Resets = append(snap.Resets, r)
	}
	for _, r := range s.relays {
		snap.Relays = append(snap.Relays, r)
	}
	// Logs are capped on the way in, so this is bounded.
	snap.ConnLogs = s.connLogs
	snap.AuditLogs = s.auditLogs
	snap.ErrLogs = s.errLogs
	return snap
}

func (s *Store) load() error {
	blob, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("embedded: read %s: %w", s.path, err)
	}
	if len(blob) == 0 {
		return nil
	}

	var snap snapshot
	if err := json.Unmarshal(blob, &snap); err != nil {
		// Refusing to start is the right response: silently continuing with
		// an empty store would look like every account had been deleted,
		// and the next snapshot would overwrite the file that still has
		// them in it.
		return fmt.Errorf("embedded: %s is not readable as a NexusVPN data file "+
			"(move it aside to start fresh): %w", s.path, err)
	}
	if snap.Version > snapshotVersion {
		return fmt.Errorf("embedded: %s was written by a newer version of NexusVPN "+
			"(file version %d, this build understands %d)", s.path, snap.Version, snapshotVersion)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range snap.Users {
		s.users[u.ID] = u
	}
	for _, n := range snap.Networks {
		s.networks[n.ID] = n
	}
	for _, m := range snap.Members {
		s.members[memberKey{m.NetworkID, m.UserID}] = m
	}
	for _, d := range snap.Devices {
		s.devices[d.ID] = d
	}
	for _, r := range snap.Refresh {
		s.refresh[r.ID] = r
	}
	for _, r := range snap.Resets {
		s.resets[r.ID] = r
	}
	for _, r := range snap.Relays {
		s.relays[r.ID] = r
	}
	s.connLogs = snap.ConnLogs
	s.auditLogs = snap.AuditLogs
	s.errLogs = snap.ErrLogs
	return nil
}

// IsEmpty reports whether any account exists yet, which is how the desktop
// app knows to offer "create an account" instead of "sign in".
func (s *Store) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) == 0
}

/* ---------- helpers ---------- */

// maxLogRows caps each log so a long-running desktop cannot grow its data
// file without bound. Logs are diagnostic here, not an audit record anyone
// is retaining for compliance.
const maxLogRows = 2000

func capLog[T any](rows []T) []T {
	if len(rows) <= maxLogRows {
		return rows
	}
	return rows[len(rows)-maxLogRows:]
}

// clone returns a copy so callers cannot mutate stored state by holding a
// pointer, which the Postgres implementation gets for free by scanning into
// a fresh struct every time.
func clone[T any](v *T) *T {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}
