package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// ConnectionLogRepo is a pgx-backed implementation of domain.ConnectionLogRepository.
type ConnectionLogRepo struct {
	db Querier
}

// NewConnectionLogRepo builds a ConnectionLogRepo.
func NewConnectionLogRepo(db Querier) *ConnectionLogRepo {
	return &ConnectionLogRepo{db: db}
}

// Create inserts a new connection_logs row.
func (r *ConnectionLogRepo) Create(ctx context.Context, l *domain.ConnectionLog) error {
	meta, err := json.Marshal(l.Metadata)
	if err != nil {
		return err
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO connection_logs (id, network_id, device_id, peer_device_id, relay_server_id, event_type, latency_ms, bytes_sent, bytes_received, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`,
		orNewID(l.ID), l.NetworkID, l.DeviceID, l.PeerDeviceID, l.RelayServerID, l.EventType, l.LatencyMs, l.BytesSent, l.BytesReceived, meta,
	)
	return translateErr(row.Scan(&l.ID, &l.CreatedAt))
}

func scanConnectionLog(rows interface {
	Scan(dest ...any) error
}) (*domain.ConnectionLog, error) {
	var l domain.ConnectionLog
	var meta []byte
	if err := rows.Scan(&l.ID, &l.NetworkID, &l.DeviceID, &l.PeerDeviceID, &l.RelayServerID, &l.EventType, &l.LatencyMs, &l.BytesSent, &l.BytesReceived, &meta, &l.CreatedAt); err != nil {
		return nil, translateErr(err)
	}
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &l.Metadata)
	}
	return &l, nil
}

const connectionLogColumns = `id, network_id, device_id, peer_device_id, relay_server_id, event_type, latency_ms, bytes_sent, bytes_received, metadata, created_at`

// ListByNetwork returns connection logs for a network, newest first, paginated.
func (r *ConnectionLogRepo) ListByNetwork(ctx context.Context, networkID uuid.UUID, limit, offset int) ([]*domain.ConnectionLog, error) {
	rows, err := r.db.Query(ctx, `SELECT `+connectionLogColumns+` FROM connection_logs WHERE network_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, networkID, limit, offset)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	var out []*domain.ConnectionLog
	for rows.Next() {
		l, err := scanConnectionLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// List returns connection logs across all networks, newest first, paginated.
func (r *ConnectionLogRepo) List(ctx context.Context, limit, offset int) ([]*domain.ConnectionLog, error) {
	rows, err := r.db.Query(ctx, `SELECT `+connectionLogColumns+` FROM connection_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	var out []*domain.ConnectionLog
	for rows.Next() {
		l, err := scanConnectionLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SumBytesByEventType sums bytes_sent+bytes_received for rows of a given
// event type created at or after since (zero value means "all time").
func (r *ConnectionLogRepo) SumBytesByEventType(ctx context.Context, eventType domain.ConnectionEventType, since time.Time) (int64, error) {
	var total int64
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(bytes_sent + bytes_received), 0) FROM connection_logs
		WHERE event_type = $1 AND created_at >= $2`, eventType, since).Scan(&total)
	return total, translateErr(err)
}

// CountByEventType returns a count of connection log rows grouped by event
// type, created at or after since (zero value means "all time").
func (r *ConnectionLogRepo) CountByEventType(ctx context.Context, since time.Time) (map[domain.ConnectionEventType]int64, error) {
	rows, err := r.db.Query(ctx, `SELECT event_type, COUNT(*) FROM connection_logs WHERE created_at >= $1 GROUP BY event_type`, since)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	out := map[domain.ConnectionEventType]int64{}
	for rows.Next() {
		var et domain.ConnectionEventType
		var n int64
		if err := rows.Scan(&et, &n); err != nil {
			return nil, translateErr(err)
		}
		out[et] = n
	}
	return out, rows.Err()
}
