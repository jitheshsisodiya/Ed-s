package usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// TokenIssuer is the subset of auth.TokenManager the use case layer needs.
type TokenIssuer interface {
	IssueAccessToken(userID uuid.UUID, email string) (string, time.Time, error)
	AccessTTLSeconds() int
	RefreshTTL() time.Duration
}

// PasswordHasher is the subset of auth.PasswordHasher used here.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(hash, password string) bool
}

// MFAProvider is the subset of auth.MFAManager used here.
type MFAProvider interface {
	GenerateSecret(accountEmail string) (secret, otpauthURL string, err error)
	Validate(secret, code string) bool
}

// GoogleOAuthProvider is the subset of auth.GoogleOAuthConfig used here.
type GoogleOAuthProvider interface {
	AuthCodeURL(state string) string
	Exchange(ctx context.Context, code string) (*auth.GoogleUserInfo, error)
}

// AuthResult is returned by login/refresh/oauth flows.
type AuthResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
	MFARequired  bool
}

// AuthService implements registration, login, token refresh, MFA and OAuth.
type AuthService struct {
	users     domain.UserRepository
	refresh   domain.RefreshTokenRepository
	resets    domain.PasswordResetRepository
	tokens    TokenIssuer
	passwords PasswordHasher
	mfa       MFAProvider
	google    GoogleOAuthProvider
	limiter   domain.RateLimiter
	audit     *AuditRecorder
	resetTTL  time.Duration
}

// NewAuthService builds an AuthService.
func NewAuthService(
	users domain.UserRepository,
	refresh domain.RefreshTokenRepository,
	resets domain.PasswordResetRepository,
	tokens TokenIssuer,
	passwords PasswordHasher,
	mfa MFAProvider,
	google GoogleOAuthProvider,
	limiter domain.RateLimiter,
	audit *AuditRecorder,
	resetTTL time.Duration,
) *AuthService {
	return &AuthService{
		users: users, refresh: refresh, resets: resets, tokens: tokens,
		passwords: passwords, mfa: mfa, google: google, limiter: limiter,
		audit: audit, resetTTL: resetTTL,
	}
}

// Register creates a new user account with a bcrypt-hashed password.
func (s *AuthService) Register(ctx context.Context, email, password, displayName string) (*domain.User, error) {
	existing, err := s.users.GetByEmail(ctx, email)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, domain.ErrAlreadyExists
	}

	hash, err := s.passwords.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	u := &domain.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
		Status:       domain.UserStatusActive,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return nil, err
	}

	s.audit.Record(ctx, &u.ID, nil, domain.AuditUserRegister, "user", u.ID.String(), "", nil)
	return u, nil
}

// Login authenticates a user by email/password (+ optional MFA code) and
// issues a new access/refresh token pair.
func (s *AuthService) Login(ctx context.Context, email, password, mfaCode, ipAddress, userAgent string) (*AuthResult, error) {
	if s.limiter != nil {
		allowed, err := s.limiter.Allow(ctx, "login:"+email, 10, time.Minute)
		if err == nil && !allowed {
			return nil, domain.ErrForbidden
		}
	}

	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			s.audit.Record(ctx, nil, nil, domain.AuditUserLoginFailed, "user", "", ipAddress, map[string]any{"email": email})
			return nil, domain.ErrInvalidCredentials
		}
		return nil, err
	}

	if u.Status == domain.UserStatusDisabled {
		return nil, domain.ErrForbidden
	}

	if !s.passwords.Verify(u.PasswordHash, password) {
		s.audit.Record(ctx, &u.ID, nil, domain.AuditUserLoginFailed, "user", u.ID.String(), ipAddress, nil)
		return nil, domain.ErrInvalidCredentials
	}

	if u.MFAEnabled {
		if mfaCode == "" {
			return &AuthResult{MFARequired: true}, nil
		}
		if !s.mfa.Validate(u.MFASecret, mfaCode) {
			s.audit.Record(ctx, &u.ID, nil, domain.AuditUserLoginFailed, "user", u.ID.String(), ipAddress, map[string]any{"reason": "bad_mfa"})
			return nil, domain.ErrInvalidMFACode
		}
	}

	result, err := s.issueTokenPair(ctx, u, ipAddress, userAgent)
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, &u.ID, nil, domain.AuditUserLogin, "user", u.ID.String(), ipAddress, nil)
	return result, nil
}

func (s *AuthService) issueTokenPair(ctx context.Context, u *domain.User, ipAddress, userAgent string) (*AuthResult, error) {
	access, _, err := s.tokens.IssueAccessToken(u.ID, u.Email)
	if err != nil {
		return nil, err
	}

	rawRefresh, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		return nil, err
	}
	rt := &domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    u.ID,
		TokenHash: auth.HashToken(rawRefresh),
		UserAgent: userAgent,
		IPAddress: ipAddress,
		ExpiresAt: time.Now().Add(s.tokens.RefreshTTL()),
	}
	if err := s.refresh.Create(ctx, rt); err != nil {
		return nil, err
	}

	return &AuthResult{
		AccessToken:  access,
		RefreshToken: rawRefresh,
		ExpiresIn:    s.tokens.AccessTTLSeconds(),
	}, nil
}

// Refresh rotates a refresh token: the old one is revoked and a new pair issued.
func (s *AuthService) Refresh(ctx context.Context, rawRefreshToken, ipAddress, userAgent string) (*AuthResult, error) {
	hash := auth.HashToken(rawRefreshToken)
	rt, err := s.refresh.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrTokenInvalid
		}
		return nil, err
	}
	if rt.RevokedAt != nil {
		return nil, domain.ErrTokenRevoked
	}
	if time.Now().After(rt.ExpiresAt) {
		return nil, domain.ErrTokenExpired
	}

	u, err := s.users.GetByID(ctx, rt.UserID)
	if err != nil {
		return nil, err
	}

	if err := s.refresh.Revoke(ctx, rt.ID); err != nil {
		return nil, err
	}

	return s.issueTokenPair(ctx, u, ipAddress, userAgent)
}

// Logout revokes a single refresh token.
func (s *AuthService) Logout(ctx context.Context, rawRefreshToken string) error {
	if rawRefreshToken == "" {
		return nil
	}
	hash := auth.HashToken(rawRefreshToken)
	rt, err := s.refresh.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	return s.refresh.Revoke(ctx, rt.ID)
}

// EnableMFA generates a new TOTP secret for the user (not yet enabled until
// VerifyMFA confirms possession of a valid code).
func (s *AuthService) EnableMFA(ctx context.Context, userID uuid.UUID) (secret, otpauthURL string, err error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	secret, otpauthURL, err = s.mfa.GenerateSecret(u.Email)
	if err != nil {
		return "", "", err
	}
	u.MFASecret = secret
	u.MFAEnabled = false
	if err := s.users.Update(ctx, u); err != nil {
		return "", "", err
	}
	return secret, otpauthURL, nil
}

// VerifyMFA validates a TOTP code against the pending secret and, on
// success, flips mfa_enabled on.
func (s *AuthService) VerifyMFA(ctx context.Context, userID uuid.UUID, code string) error {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return err
	}
	if u.MFASecret == "" {
		return domain.ErrInvalidInput
	}
	if !s.mfa.Validate(u.MFASecret, code) {
		return domain.ErrInvalidMFACode
	}
	u.MFAEnabled = true
	if err := s.users.Update(ctx, u); err != nil {
		return err
	}
	s.audit.Record(ctx, &u.ID, nil, domain.AuditUserMFAEnabled, "user", u.ID.String(), "", nil)
	return nil
}

// ForgotPassword issues a single-use password reset token if the account
// exists. It never reveals whether the email is registered.
func (s *AuthService) ForgotPassword(ctx context.Context, email string) (rawToken string, err error) {
	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", nil
		}
		return "", err
	}

	rawToken, err = auth.NewOpaqueRefreshToken()
	if err != nil {
		return "", err
	}
	pr := &domain.PasswordReset{
		ID:        uuid.New(),
		UserID:    u.ID,
		TokenHash: auth.HashToken(rawToken),
		ExpiresAt: time.Now().Add(s.resetTTL),
	}
	if err := s.resets.Create(ctx, pr); err != nil {
		return "", err
	}
	return rawToken, nil
}

// ResetPassword consumes a single-use reset token and sets a new password.
func (s *AuthService) ResetPassword(ctx context.Context, rawToken, newPassword string) error {
	hash := auth.HashToken(rawToken)
	pr, err := s.resets.GetByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrTokenInvalid
		}
		return err
	}
	if pr.UsedAt != nil {
		return domain.ErrTokenInvalid
	}
	if time.Now().After(pr.ExpiresAt) {
		return domain.ErrTokenExpired
	}

	u, err := s.users.GetByID(ctx, pr.UserID)
	if err != nil {
		return err
	}
	newHash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return err
	}
	u.PasswordHash = newHash
	if err := s.users.Update(ctx, u); err != nil {
		return err
	}
	if err := s.resets.MarkUsed(ctx, pr.ID); err != nil {
		return err
	}
	if err := s.refresh.RevokeAllForUser(ctx, u.ID); err != nil {
		return err
	}
	s.audit.Record(ctx, &u.ID, nil, domain.AuditUserPasswordReset, "user", u.ID.String(), "", nil)
	return nil
}

// GoogleAuthCodeURL returns the URL to send the browser to for Google login,
// or an error if OAuth isn't configured.
func (s *AuthService) GoogleAuthCodeURL(state string) (string, error) {
	if s.google == nil {
		return "", domain.ErrInvalidInput
	}
	return s.google.AuthCodeURL(state), nil
}

// GoogleCallback exchanges an OAuth code for a Google profile, finds or
// creates the corresponding user, and issues a token pair.
func (s *AuthService) GoogleCallback(ctx context.Context, code, ipAddress, userAgent string) (*AuthResult, error) {
	if s.google == nil {
		return nil, domain.ErrInvalidInput
	}
	info, err := s.google.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("google exchange: %w", err)
	}

	u, err := s.users.GetByOAuthSubject(ctx, "google", info.Sub)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}
	if u == nil {
		if existing, err := s.users.GetByEmail(ctx, info.Email); err == nil {
			u = existing
			provider := "google"
			u.OAuthProvider = &provider
			u.OAuthSubject = &info.Sub
			if err := s.users.Update(ctx, u); err != nil {
				return nil, err
			}
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
	}
	if u == nil {
		provider := "google"
		randomPassword, err := auth.NewOpaqueRefreshToken()
		if err != nil {
			return nil, err
		}
		hash, err := s.passwords.Hash(randomPassword)
		if err != nil {
			return nil, err
		}
		now := time.Now()
		u = &domain.User{
			ID:              uuid.New(),
			Email:           info.Email,
			PasswordHash:    hash,
			DisplayName:     info.Name,
			Status:          domain.UserStatusActive,
			OAuthProvider:   &provider,
			OAuthSubject:    &info.Sub,
			EmailVerifiedAt: &now,
		}
		if err := s.users.Create(ctx, u); err != nil {
			return nil, err
		}
		s.audit.Record(ctx, &u.ID, nil, domain.AuditUserRegister, "user", u.ID.String(), ipAddress, map[string]any{"via": "google"})
	}

	result, err := s.issueTokenPair(ctx, u, ipAddress, userAgent)
	if err != nil {
		return nil, err
	}
	s.audit.Record(ctx, &u.ID, nil, domain.AuditUserLogin, "user", u.ID.String(), ipAddress, map[string]any{"via": "google"})
	return result, nil
}
