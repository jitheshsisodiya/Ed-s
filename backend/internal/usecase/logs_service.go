package usecase

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// LogsService lists audit and connection logs, offset-paginated and
// optionally filtered by network.
//
// Pagination: callers pass limit (page size, default 50, max 200) and
// offset (rows to skip, default 0). Offset pagination is used rather than a
// cursor because audit/connection log volumes for a self-hosted deployment
// are modest and offset pagination keeps the API simple to consume from the
// admin panel and SDKs; revisit with keyset pagination if a deployment's
// log volume grows past what offset scans handle comfortably.
type LogsService struct {
	audit    domain.AuditLogRepository
	connLogs domain.ConnectionLogRepository
	members  domain.NetworkMemberRepository
}

// NewLogsService builds a LogsService.
func NewLogsService(audit domain.AuditLogRepository, connLogs domain.ConnectionLogRepository, members domain.NetworkMemberRepository) *LogsService {
	return &LogsService{audit: audit, connLogs: connLogs, members: members}
}

const (
	defaultLogLimit = 50
	maxLogLimit     = 200
)

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultLogLimit
	}
	if limit > maxLogLimit {
		return maxLogLimit
	}
	return limit
}

// AuditLogs returns audit log entries, optionally scoped to a network the
// caller must belong to.
func (s *LogsService) AuditLogs(ctx context.Context, userID uuid.UUID, networkID *uuid.UUID, limit, offset int) ([]*domain.AuditLog, error) {
	limit = clampLimit(limit)
	if offset < 0 {
		offset = 0
	}
	if networkID != nil {
		if _, err := s.members.Get(ctx, *networkID, userID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, domain.ErrForbidden
			}
			return nil, err
		}
		return s.audit.ListByNetwork(ctx, *networkID, limit, offset)
	}
	return s.audit.List(ctx, limit, offset)
}

// ConnectionLogs returns connection log entries, optionally scoped to a
// network the caller must belong to.
func (s *LogsService) ConnectionLogs(ctx context.Context, userID uuid.UUID, networkID *uuid.UUID, limit, offset int) ([]*domain.ConnectionLog, error) {
	limit = clampLimit(limit)
	if offset < 0 {
		offset = 0
	}
	if networkID != nil {
		if _, err := s.members.Get(ctx, *networkID, userID); err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil, domain.ErrForbidden
			}
			return nil, err
		}
		return s.connLogs.ListByNetwork(ctx, *networkID, limit, offset)
	}
	return s.connLogs.List(ctx, limit, offset)
}
