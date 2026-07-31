package postgres

import (
	"context"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// UserRepo is a pgx-backed implementation of domain.UserRepository.
type UserRepo struct {
	db Querier
}

// NewUserRepo builds a UserRepo.
func NewUserRepo(db Querier) *UserRepo {
	return &UserRepo{db: db}
}

func scanUser(row interface {
	Scan(dest ...any) error
}) (*domain.User, error) {
	var u domain.User
	err := row.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &u.DisplayName, &u.Status,
		&u.MFASecret, &u.MFAEnabled, &u.OAuthProvider, &u.OAuthSubject,
		&u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, translateErr(err)
	}
	return &u, nil
}

const userColumns = `id, email, password_hash, display_name, status, mfa_secret, mfa_enabled, oauth_provider, oauth_subject, email_verified_at, created_at, updated_at`

// Create inserts a new user row.
func (r *UserRepo) Create(ctx context.Context, u *domain.User) error {
	row := r.db.QueryRow(ctx, `
		INSERT INTO users (id, email, password_hash, display_name, status, mfa_secret, mfa_enabled, oauth_provider, oauth_subject, email_verified_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8, $9, $10)
		RETURNING `+userColumns,
		orNewID(u.ID), u.Email, u.PasswordHash, u.DisplayName, u.Status, u.MFASecret, u.MFAEnabled,
		u.OAuthProvider, u.OAuthSubject, u.EmailVerifiedAt,
	)
	saved, err := scanUser(row)
	if err != nil {
		return translateErr(err)
	}
	*u = *saved
	return nil
}

func orNewID(id uuid.UUID) uuid.UUID {
	if id == uuid.Nil {
		return uuid.New()
	}
	return id
}

// GetByID fetches a user by primary key.
func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	row := r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	return scanUser(row)
}

// GetByEmail fetches a user by (case-insensitive, via citext) email.
func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email)
	return scanUser(row)
}

// GetByOAuthSubject fetches a user linked to an OAuth provider identity.
func (r *UserRepo) GetByOAuthSubject(ctx context.Context, provider, subject string) (*domain.User, error) {
	row := r.db.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE oauth_provider = $1 AND oauth_subject = $2`, provider, subject)
	return scanUser(row)
}

// Update persists changes to an existing user row.
func (r *UserRepo) Update(ctx context.Context, u *domain.User) error {
	row := r.db.QueryRow(ctx, `
		UPDATE users SET
			email = $2, password_hash = $3, display_name = $4, status = $5,
			mfa_secret = $6, mfa_enabled = $7, oauth_provider = $8, oauth_subject = $9,
			email_verified_at = $10
		WHERE id = $1
		RETURNING `+userColumns,
		u.ID, u.Email, u.PasswordHash, u.DisplayName, u.Status,
		u.MFASecret, u.MFAEnabled, u.OAuthProvider, u.OAuthSubject, u.EmailVerifiedAt,
	)
	saved, err := scanUser(row)
	if err != nil {
		return err
	}
	*u = *saved
	return nil
}
