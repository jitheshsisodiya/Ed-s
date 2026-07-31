package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// PasswordResetRepo is a pgx-backed implementation of domain.PasswordResetRepository.
type PasswordResetRepo struct {
	db Querier
}

// NewPasswordResetRepo builds a PasswordResetRepo.
func NewPasswordResetRepo(db Querier) *PasswordResetRepo {
	return &PasswordResetRepo{db: db}
}

const passwordResetColumns = `id, user_id, token_hash, expires_at, used_at, created_at`

func scanPasswordReset(row interface{ Scan(dest ...any) error }) (*domain.PasswordReset, error) {
	var pr domain.PasswordReset
	if err := row.Scan(&pr.ID, &pr.UserID, &pr.TokenHash, &pr.ExpiresAt, &pr.UsedAt, &pr.CreatedAt); err != nil {
		return nil, translateErr(err)
	}
	return &pr, nil
}

// Create inserts a new password reset token row.
func (r *PasswordResetRepo) Create(ctx context.Context, pr *domain.PasswordReset) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO password_resets (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
		RETURNING `+passwordResetColumns,
		orNewID(pr.ID), pr.UserID, pr.TokenHash, pr.ExpiresAt,
	)
	saved, err := scanPasswordReset(row)
	if err != nil {
		return err
	}
	*pr = *saved
	return nil
}

// GetByHash fetches a password reset token by its stored hash.
func (r *PasswordResetRepo) GetByHash(ctx context.Context, tokenHash string) (*domain.PasswordReset, error) {
	row := r.db.QueryRow(ctx, `SELECT `+passwordResetColumns+` FROM password_resets WHERE token_hash = $1`, tokenHash)
	return scanPasswordReset(row)
}

// MarkUsed marks a password reset token as consumed (single-use).
func (r *PasswordResetRepo) MarkUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE password_resets SET used_at = now() WHERE id = $1 AND used_at IS NULL`, id)
	return translateErr(err)
}
