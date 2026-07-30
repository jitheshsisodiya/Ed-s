package usecase

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// AuditRecorder writes audit_logs rows for mutating actions. Writes are
// best-effort: a failure to write an audit row never fails the calling
// request, it is only logged.
type AuditRecorder struct {
	repo   domain.AuditLogRepository
	logger *zap.Logger
}

// NewAuditRecorder builds an AuditRecorder.
func NewAuditRecorder(repo domain.AuditLogRepository, logger *zap.Logger) *AuditRecorder {
	return &AuditRecorder{repo: repo, logger: logger}
}

// Record writes an audit log entry. actorUserID, networkID, targetType,
// targetID and ipAddress may be nil/empty when not applicable.
func (a *AuditRecorder) Record(ctx context.Context, actorUserID *uuid.UUID, networkID *uuid.UUID, action domain.AuditAction, targetType, targetID, ipAddress string, metadata map[string]any) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	log := &domain.AuditLog{
		ID:          uuid.New(),
		ActorUserID: actorUserID,
		NetworkID:   networkID,
		Action:      action,
		Metadata:    metadata,
	}
	if targetType != "" {
		log.TargetType = &targetType
	}
	if targetID != "" {
		log.TargetID = &targetID
	}
	if ipAddress != "" {
		log.IPAddress = &ipAddress
	}
	// Fire-and-forget: audit logging must not block or fail the primary
	// request path.
	go func() {
		bgCtx := context.WithoutCancel(ctx)
		if err := a.repo.Create(bgCtx, log); err != nil && a.logger != nil {
			a.logger.Error("failed to write audit log", zap.Error(err), zap.String("action", string(action)))
		}
	}()
}
