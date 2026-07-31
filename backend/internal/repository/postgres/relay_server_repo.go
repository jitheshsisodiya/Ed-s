package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// RelayServerRepo is a pgx-backed implementation of domain.RelayServerRepository.
type RelayServerRepo struct {
	db Querier
}

// NewRelayServerRepo builds a RelayServerRepo.
func NewRelayServerRepo(db Querier) *RelayServerRepo {
	return &RelayServerRepo{db: db}
}

const relayServerColumns = `id, region, hostname, public_key, control_port, relay_port, capacity, current_load, status, last_heartbeat_at, created_at`

func scanRelayServer(row interface{ Scan(dest ...any) error }) (*domain.RelayServer, error) {
	var s domain.RelayServer
	if err := row.Scan(&s.ID, &s.Region, &s.Hostname, &s.PublicKey, &s.ControlPort, &s.RelayPort, &s.Capacity, &s.CurrentLoad, &s.Status, &s.LastHeartbeatAt, &s.CreatedAt); err != nil {
		return nil, translateErr(err)
	}
	return &s, nil
}

// Create inserts a new relay_servers row.
func (r *RelayServerRepo) Create(ctx context.Context, s *domain.RelayServer) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO relay_servers (id, region, hostname, public_key, control_port, relay_port, capacity, current_load, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+relayServerColumns,
		orNewID(s.ID), s.Region, s.Hostname, s.PublicKey, s.ControlPort, s.RelayPort, s.Capacity, s.CurrentLoad, s.Status,
	)
	saved, err := scanRelayServer(row)
	if err != nil {
		return err
	}
	*s = *saved
	return nil
}

// Upsert registers a relay node idempotently. On conflict with an existing
// (hostname, relay_port) row the mutable fields are refreshed, the row is
// re-marked active, and its heartbeat timestamp reset; current_load is
// deliberately NOT overwritten so in-flight allocations survive a relay
// restart race.
func (r *RelayServerRepo) Upsert(ctx context.Context, s *domain.RelayServer) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO relay_servers (id, region, hostname, public_key, control_port, relay_port, capacity, current_load, status, last_heartbeat_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', now())
		ON CONFLICT (hostname, relay_port) DO UPDATE SET
			region = EXCLUDED.region,
			public_key = EXCLUDED.public_key,
			control_port = EXCLUDED.control_port,
			capacity = EXCLUDED.capacity,
			status = 'active',
			last_heartbeat_at = now()
		RETURNING `+relayServerColumns,
		orNewID(s.ID), s.Region, s.Hostname, s.PublicKey, s.ControlPort, s.RelayPort, s.Capacity, s.CurrentLoad,
	)
	saved, err := scanRelayServer(row)
	if err != nil {
		return err
	}
	*s = *saved
	return nil
}

// GetByID fetches a relay server by primary key.
func (r *RelayServerRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.RelayServer, error) {
	row := r.db.QueryRow(ctx, `SELECT `+relayServerColumns+` FROM relay_servers WHERE id = $1`, id)
	return scanRelayServer(row)
}

// ListActive returns every relay server with status = 'active'.
func (r *RelayServerRepo) ListActive(ctx context.Context) ([]*domain.RelayServer, error) {
	rows, err := r.db.Query(ctx, `SELECT `+relayServerColumns+` FROM relay_servers WHERE status = 'active' ORDER BY hostname`)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	var out []*domain.RelayServer
	for rows.Next() {
		var s domain.RelayServer
		if err := rows.Scan(&s.ID, &s.Region, &s.Hostname, &s.PublicKey, &s.ControlPort, &s.RelayPort, &s.Capacity, &s.CurrentLoad, &s.Status, &s.LastHeartbeatAt, &s.CreatedAt); err != nil {
			return nil, translateErr(err)
		}
		out = append(out, &s)
	}
	return out, rows.Err()
}

// PickLeastLoaded returns the active relay server with the lowest
// current_load/capacity ratio, optionally preferring a region. Returns
// (nil, nil) if no active relay servers exist.
func (r *RelayServerRepo) PickLeastLoaded(ctx context.Context, preferredRegion string) (*domain.RelayServer, error) {
	if preferredRegion != "" {
		row := r.db.QueryRow(ctx, `
			SELECT `+relayServerColumns+` FROM relay_servers
			WHERE status = 'active' AND region = $1 AND current_load < capacity
			ORDER BY (current_load::float / GREATEST(capacity, 1)) ASC
			LIMIT 1`, preferredRegion)
		s, err := scanRelayServer(row)
		if err == nil {
			return s, nil
		}
		if err != domain.ErrNotFound {
			return nil, err
		}
		// fall through to any-region search
	}

	row := r.db.QueryRow(ctx, `
		SELECT `+relayServerColumns+` FROM relay_servers
		WHERE status = 'active' AND current_load < capacity
		ORDER BY (current_load::float / GREATEST(capacity, 1)) ASC
		LIMIT 1`)
	s, err := scanRelayServer(row)
	if err != nil {
		if err == domain.ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return s, nil
}

// IncrementLoad adjusts a relay server's current_load by delta (can be negative).
func (r *RelayServerRepo) IncrementLoad(ctx context.Context, id uuid.UUID, delta int) error {
	_, err := r.db.Exec(ctx, `UPDATE relay_servers SET current_load = GREATEST(0, current_load + $2) WHERE id = $1`, id, delta)
	return translateErr(err)
}

// Heartbeat updates a relay server's load and last_heartbeat_at, called
// periodically by the relay daemon itself.
func (r *RelayServerRepo) Heartbeat(ctx context.Context, id uuid.UUID, currentLoad int) error {
	_, err := r.db.Exec(ctx, `UPDATE relay_servers SET current_load = $2, last_heartbeat_at = now() WHERE id = $1`, id, currentLoad)
	return translateErr(err)
}
