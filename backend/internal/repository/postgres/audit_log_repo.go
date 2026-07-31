package postgres

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// AuditLogRepo is a pgx-backed implementation of domain.AuditLogRepository.
type AuditLogRepo struct {
	db Querier
}

// NewAuditLogRepo builds an AuditLogRepo.
func NewAuditLogRepo(db Querier) *AuditLogRepo {
	return &AuditLogRepo{db: db}
}

// Create inserts a new audit_logs row.
func (r *AuditLogRepo) Create(ctx context.Context, l *domain.AuditLog) error {
	meta, err := json.Marshal(l.Metadata)
	if err != nil {
		return err
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO audit_logs (id, actor_user_id, network_id, action, target_type, target_id, ip_address, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::inet, $8)
		RETURNING id, created_at`,
		orNewID(l.ID), l.ActorUserID, l.NetworkID, l.Action, l.TargetType, l.TargetID, derefOrEmpty(l.IPAddress), meta,
	)
	return translateErr(row.Scan(&l.ID, &l.CreatedAt))
}

const auditLogColumns = `id, actor_user_id, network_id, action, target_type, target_id, ip_address::text, metadata, created_at`

func scanAuditLog(rows interface{ Scan(dest ...any) error }) (*domain.AuditLog, error) {
	var l domain.AuditLog
	var meta []byte
	var ip *string
	if err := rows.Scan(&l.ID, &l.ActorUserID, &l.NetworkID, &l.Action, &l.TargetType, &l.TargetID, &ip, &meta, &l.CreatedAt); err != nil {
		return nil, translateErr(err)
	}
	l.IPAddress = ip
	if len(meta) > 0 {
		_ = json.Unmarshal(meta, &l.Metadata)
	}
	return &l, nil
}

// ListByNetwork returns audit logs for a network, newest first, paginated.
func (r *AuditLogRepo) ListByNetwork(ctx context.Context, networkID uuid.UUID, limit, offset int) ([]*domain.AuditLog, error) {
	rows, err := r.db.Query(ctx, `SELECT `+auditLogColumns+` FROM audit_logs WHERE network_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, networkID, limit, offset)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	var out []*domain.AuditLog
	for rows.Next() {
		l, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// List returns audit logs across all networks, newest first, paginated.
func (r *AuditLogRepo) List(ctx context.Context, limit, offset int) ([]*domain.AuditLog, error) {
	rows, err := r.db.Query(ctx, `SELECT `+auditLogColumns+` FROM audit_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, translateErr(err)
	}
	defer rows.Close()
	var out []*domain.AuditLog
	for rows.Next() {
		l, err := scanAuditLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
