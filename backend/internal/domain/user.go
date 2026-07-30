package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// UserStatus mirrors the Postgres user_status enum.
type UserStatus string

const (
	UserStatusActive              UserStatus = "active"
	UserStatusDisabled            UserStatus = "disabled"
	UserStatusPendingVerification UserStatus = "pending_verification"
)

// User is the domain entity for a NexusVPN account.
type User struct {
	ID              uuid.UUID
	Email           string
	PasswordHash    string
	DisplayName     string
	Status          UserStatus
	MFASecret       string
	MFAEnabled      bool
	OAuthProvider   *string
	OAuthSubject    *string
	EmailVerifiedAt *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// UserRepository persists and retrieves User aggregates.
type UserRepository interface {
	Create(ctx context.Context, u *User) error
	GetByID(ctx context.Context, id uuid.UUID) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetByOAuthSubject(ctx context.Context, provider, subject string) (*User, error)
	Update(ctx context.Context, u *User) error
}

// RefreshToken represents a rotatable, revocable refresh token stored hashed.
type RefreshToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	UserAgent string
	IPAddress string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// RefreshTokenRepository persists refresh tokens.
type RefreshTokenRepository interface {
	Create(ctx context.Context, rt *RefreshToken) error
	GetByHash(ctx context.Context, tokenHash string) (*RefreshToken, error)
	Revoke(ctx context.Context, id uuid.UUID) error
	RevokeAllForUser(ctx context.Context, userID uuid.UUID) error
}

// PasswordReset is a single-use, expiring password reset token stored hashed.
type PasswordReset struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

// PasswordResetRepository persists password reset tokens.
type PasswordResetRepository interface {
	Create(ctx context.Context, pr *PasswordReset) error
	GetByHash(ctx context.Context, tokenHash string) (*PasswordReset, error)
	MarkUsed(ctx context.Context, id uuid.UUID) error
}
