package postgres

import (
	"context"
	"encoding/json"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// ErrorLogRepo is a pgx-backed implementation of domain.ErrorLogRepository.
// Wrap it with ErrorSinkAdapter to satisfy logging.ErrorSink.
type ErrorLogRepo struct {
	db Querier
}

// NewErrorLogRepo builds an ErrorLogRepo.
func NewErrorLogRepo(db Querier) *ErrorLogRepo {
	return &ErrorLogRepo{db: db}
}

// Create inserts a new error_logs row.
func (r *ErrorLogRepo) Create(ctx context.Context, l *domain.ErrorLog) error {
	meta, err := json.Marshal(l.Metadata)
	if err != nil {
		return err
	}
	row := r.db.QueryRow(ctx, `
		INSERT INTO error_logs (id, service, level, message, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		orNewID(l.ID), l.Service, l.Level, l.Message, meta,
	)
	return translateErr(row.Scan(&l.ID, &l.CreatedAt))
}

// ErrorSinkAdapter adapts an ErrorLogRepo to the logging.ErrorSink
// interface (which uses positional fields rather than a domain.ErrorLog),
// so it can be wired into logging.SetErrorSink at startup.
type ErrorSinkAdapter struct {
	Repo *ErrorLogRepo
}

// Create implements logging.ErrorSink.
func (a ErrorSinkAdapter) Create(ctx context.Context, service, level, message string, metadata map[string]any) error {
	return a.Repo.Create(ctx, &domain.ErrorLog{Service: service, Level: level, Message: message, Metadata: metadata})
}
