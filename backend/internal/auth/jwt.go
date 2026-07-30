// Package auth provides JWT issuing/verification, password hashing, TOTP
// MFA and OAuth2 helpers for the NexusVPN control plane.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// TokenType distinguishes access tokens from refresh tokens in the JWT claims.
type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

// AccessClaims are the custom claims embedded in a NexusVPN access token.
type AccessClaims struct {
	jwt.RegisteredClaims
	UserID string    `json:"uid"`
	Email  string    `json:"email"`
	Type   TokenType `json:"type"`
}

// TokenManager issues and verifies access/refresh JWTs.
type TokenManager struct {
	accessSecret  []byte
	refreshSecret []byte
	issuer        string
	accessTTL     time.Duration
	refreshTTL    time.Duration
}

// NewTokenManager builds a TokenManager from raw secret strings.
func NewTokenManager(accessSecret, refreshSecret, issuer string, accessTTL, refreshTTL time.Duration) *TokenManager {
	return &TokenManager{
		accessSecret:  []byte(accessSecret),
		refreshSecret: []byte(refreshSecret),
		issuer:        issuer,
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
	}
}

// IssueAccessToken creates a signed, short-lived access token for userID.
func (m *TokenManager) IssueAccessToken(userID uuid.UUID, email string) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(m.accessTTL)
	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			ID:        uuid.NewString(),
		},
		UserID: userID.String(),
		Email:  email,
		Type:   TokenTypeAccess,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.accessSecret)
	return signed, expiresAt, err
}

// AccessTTLSeconds returns the configured access token lifetime in seconds
// (used to populate TokenPair.expiresIn).
func (m *TokenManager) AccessTTLSeconds() int {
	return int(m.accessTTL.Seconds())
}

// RefreshTTL returns the configured refresh token lifetime.
func (m *TokenManager) RefreshTTL() time.Duration {
	return m.refreshTTL
}

// ParseAccessToken validates and parses an access token, returning its claims.
func (m *TokenManager) ParseAccessToken(tokenStr string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.accessSecret, nil
	}, jwt.WithIssuer(m.issuer))
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.Type != TokenTypeAccess {
		return nil, errors.New("not an access token")
	}
	return claims, nil
}

// NewOpaqueRefreshToken generates a cryptographically random opaque refresh
// token string. The raw value is returned to the client; only its SHA-256
// hash is persisted (see HashToken), matching refresh_tokens.token_hash.
func NewOpaqueRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// HashToken returns the hex-encoded SHA-256 digest of an opaque token, used
// to store refresh tokens and password reset tokens hashed at rest.
func HashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
