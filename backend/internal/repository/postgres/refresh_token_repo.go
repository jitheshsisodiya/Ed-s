package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// RefreshTokenRepo is a pgx-backed implementation of domain.RefreshTokenRepository.
type RefreshTokenRepo struct {
	db Querier
}

// NewRefreshTokenRepo builds a RefreshTokenRepo.
func NewRefreshTokenRepo(db Querier) *RefreshTokenRepo {
	return &RefreshTokenRepo{db: db}
}

const refreshTokenColumns = `id, user_id, token_hash, user_agent, ip_address, expires_at, revoked_at, created_at`

func scanRefreshToken(row interface{ Scan(dest ...any) error }) (*domain.RefreshToken, error) {
	var rt domain.RefreshToken
	var ip *string
	err := row.Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.UserAgent, &ip, &rt.ExpiresAt, &rt.RevokedAt, &rt.CreatedAt)
	if err != nil {
		return nil, translateErr(err)
	}
	if ip != nil {
		rt.IPAddress = *ip
	}
	return &rt, nil
}

// Create inserts a new refresh token row.
func (r *RefreshTokenRepo) Create(ctx context.Context, rt *domain.RefreshToken) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO refresh_tokens (id, user_id, token_hash, user_agent, ip_address, expires_at)
		VALUES ($1, $2, $3, $4, NULLIF($5, '')::inet, $6)
		RETURNING `+refreshTokenColumns,
		orNewID(rt.ID), rt.UserID, rt.TokenHash, rt.UserAgent, rt.IPAddress, rt.ExpiresAt,
	)
	saved, err := scanRefreshToken(row)
	if err != nil {
		return err
	}
	*rt = *saved
	return nil
}

// GetByHash fetches a refresh token by its stored hash.
func (r *RefreshTokenRepo) GetByHash(ctx context.Context, tokenHash string) (*domain.RefreshToken, error) {
	row := r.db.QueryRow(ctx, `SELECT `+refreshTokenColumns+` FROM refresh_tokens WHERE token_hash = $1`, tokenHash)
	return scanRefreshToken(row)
}

// Revoke marks a single refresh token as revoked.
func (r *RefreshTokenRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	return translateErr(err)
}

// RevokeAllForUser revokes every non-revoked refresh token for a user
// (used on password reset).
func (r *RefreshTokenRepo) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	_, err := r.db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	return translateErr(err)
}
