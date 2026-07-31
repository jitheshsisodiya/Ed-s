package usecase

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

type authFixture struct {
	svc     *AuthService
	users   *fakeUserRepo
	refresh *fakeRefreshRepo
	resets  *fakeResetRepo
	audit   *fakeAuditRepo
	tokens  *auth.TokenManager
}

func newAuthFixture(t *testing.T, mfaCode string) *authFixture {
	t.Helper()
	users := newFakeUserRepo()
	refresh := newFakeRefreshRepo()
	resets := newFakeResetRepo()
	auditRepo := &fakeAuditRepo{}
	tokens := auth.NewTokenManager("access-secret-for-tests", "refresh-secret-for-tests", "nexusvpn-test", 15*time.Minute, 24*time.Hour)

	svc := NewAuthService(
		users, refresh, resets, tokens, realHasher(),
		stubMFA{validCode: mfaCode}, nil, &fakeRateLimiter{allow: true},
		NewAuditRecorder(auditRepo, zap.NewNop()), time.Hour,
	)
	return &authFixture{svc: svc, users: users, refresh: refresh, resets: resets, audit: auditRepo, tokens: tokens}
}

func TestRegisterThenLogin(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	u, err := f.svc.Register(ctx, "alice@example.com", "correct-horse-battery", "Alice")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if u.PasswordHash == "correct-horse-battery" {
		t.Fatal("password was stored in plaintext")
	}

	result, err := f.svc.Login(ctx, "alice@example.com", "correct-horse-battery", "", "1.2.3.4", "test-agent")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatal("expected both access and refresh tokens")
	}

	claims, err := f.tokens.ParseAccessToken(result.AccessToken)
	if err != nil {
		t.Fatalf("issued access token does not validate: %v", err)
	}
	if claims.UserID != u.ID.String() {
		t.Fatalf("token subject = %s, want %s", claims.UserID, u.ID)
	}
}

func TestRegisterRejectsDuplicateEmail(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	if _, err := f.svc.Register(ctx, "dup@example.com", "correct-horse-battery", "First"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	_, err := f.svc.Register(ctx, "dup@example.com", "another-password-here", "Second")
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestLoginWrongPasswordIsRejectedAndAudited(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	if _, err := f.svc.Register(ctx, "bob@example.com", "correct-horse-battery", "Bob"); err != nil {
		t.Fatalf("register: %v", err)
	}

	_, err := f.svc.Login(ctx, "bob@example.com", "wrong-password-entry", "", "", "")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}

	f.audit.waitForAction(t, domain.AuditUserLoginFailed)
}

func TestLoginUnknownEmailDoesNotLeakExistence(t *testing.T) {
	f := newAuthFixture(t, "")

	_, err := f.svc.Login(t.Context(), "ghost@example.com", "correct-horse-battery", "", "", "")
	if !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for unknown account, got %v", err)
	}
}

func TestLoginRateLimited(t *testing.T) {
	users := newFakeUserRepo()
	svc := NewAuthService(
		users, newFakeRefreshRepo(), newFakeResetRepo(),
		auth.NewTokenManager("a", "b", "iss", time.Minute, time.Hour),
		realHasher(), stubMFA{}, nil,
		&fakeRateLimiter{allow: false}, // limiter denies
		NewAuditRecorder(&fakeAuditRepo{}, zap.NewNop()), time.Hour,
	)

	_, err := svc.Login(t.Context(), "someone@example.com", "password", "", "", "")
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden when rate limited, got %v", err)
	}
}

func TestMFARequiredThenAccepted(t *testing.T) {
	f := newAuthFixture(t, "123456")
	ctx := t.Context()

	u, err := f.svc.Register(ctx, "mfa@example.com", "correct-horse-battery", "MFA User")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, _, err := f.svc.EnableMFA(ctx, u.ID); err != nil {
		t.Fatalf("enable mfa: %v", err)
	}
	// Not enabled until a valid code is confirmed.
	if err := f.svc.VerifyMFA(ctx, u.ID, "000000"); !errors.Is(err, domain.ErrInvalidMFACode) {
		t.Fatalf("expected ErrInvalidMFACode, got %v", err)
	}
	if err := f.svc.VerifyMFA(ctx, u.ID, "123456"); err != nil {
		t.Fatalf("verify mfa: %v", err)
	}

	// Login without a code now reports mfaRequired instead of issuing tokens.
	result, err := f.svc.Login(ctx, "mfa@example.com", "correct-horse-battery", "", "", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if !result.MFARequired || result.AccessToken != "" {
		t.Fatalf("expected mfaRequired with no tokens, got %+v", result)
	}

	// Wrong code is rejected.
	if _, err := f.svc.Login(ctx, "mfa@example.com", "correct-horse-battery", "999999", "", ""); !errors.Is(err, domain.ErrInvalidMFACode) {
		t.Fatalf("expected ErrInvalidMFACode, got %v", err)
	}

	// Correct code completes login.
	result, err = f.svc.Login(ctx, "mfa@example.com", "correct-horse-battery", "123456", "", "")
	if err != nil {
		t.Fatalf("login with mfa: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatal("expected an access token after successful MFA")
	}
}

func TestRefreshRotatesAndRevokesOldToken(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	if _, err := f.svc.Register(ctx, "rot@example.com", "correct-horse-battery", "Rot"); err != nil {
		t.Fatalf("register: %v", err)
	}
	first, err := f.svc.Login(ctx, "rot@example.com", "correct-horse-battery", "", "", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	second, err := f.svc.Refresh(ctx, first.RefreshToken, "", "")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}

	// Reusing the old (now revoked) refresh token must fail.
	if _, err := f.svc.Refresh(ctx, first.RefreshToken, "", ""); !errors.Is(err, domain.ErrTokenRevoked) {
		t.Fatalf("expected ErrTokenRevoked on reuse, got %v", err)
	}
}

func TestRefreshRejectsUnknownToken(t *testing.T) {
	f := newAuthFixture(t, "")

	if _, err := f.svc.Refresh(t.Context(), "not-a-real-token", "", ""); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestLogoutRevokesRefreshToken(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	if _, err := f.svc.Register(ctx, "out@example.com", "correct-horse-battery", "Out"); err != nil {
		t.Fatalf("register: %v", err)
	}
	result, err := f.svc.Login(ctx, "out@example.com", "correct-horse-battery", "", "", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if err := f.svc.Logout(ctx, result.RefreshToken); err != nil {
		t.Fatalf("logout: %v", err)
	}
	if _, err := f.svc.Refresh(ctx, result.RefreshToken, "", ""); !errors.Is(err, domain.ErrTokenRevoked) {
		t.Fatalf("expected ErrTokenRevoked after logout, got %v", err)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	if _, err := f.svc.Register(ctx, "reset@example.com", "correct-horse-battery", "Reset"); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Issue a refresh token that should be revoked by the reset.
	login, err := f.svc.Login(ctx, "reset@example.com", "correct-horse-battery", "", "", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	token, err := f.svc.ForgotPassword(ctx, "reset@example.com")
	if err != nil {
		t.Fatalf("forgot password: %v", err)
	}
	if token == "" {
		t.Fatal("expected a reset token")
	}

	if err := f.svc.ResetPassword(ctx, token, "brand-new-password-1"); err != nil {
		t.Fatalf("reset password: %v", err)
	}

	// Old password no longer works; new one does.
	if _, err := f.svc.Login(ctx, "reset@example.com", "correct-horse-battery", "", "", ""); !errors.Is(err, domain.ErrInvalidCredentials) {
		t.Fatalf("expected old password to be rejected, got %v", err)
	}
	if _, err := f.svc.Login(ctx, "reset@example.com", "brand-new-password-1", "", "", ""); err != nil {
		t.Fatalf("login with new password: %v", err)
	}

	// Reset tokens are single use.
	if err := f.svc.ResetPassword(ctx, token, "yet-another-password"); !errors.Is(err, domain.ErrTokenInvalid) {
		t.Fatalf("expected single-use reset token, got %v", err)
	}

	// Sessions issued before the reset were revoked.
	if _, err := f.svc.Refresh(ctx, login.RefreshToken, "", ""); !errors.Is(err, domain.ErrTokenRevoked) {
		t.Fatalf("expected pre-reset refresh tokens to be revoked, got %v", err)
	}
}

func TestForgotPasswordUnknownEmailIsSilent(t *testing.T) {
	f := newAuthFixture(t, "")

	token, err := f.svc.ForgotPassword(t.Context(), "nobody@example.com")
	if err != nil {
		t.Fatalf("expected no error for unknown email, got %v", err)
	}
	if token != "" {
		t.Fatal("expected no reset token for an unknown account")
	}
}

func TestExpiredRefreshTokenRejected(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	u, err := f.svc.Register(ctx, "exp@example.com", "correct-horse-battery", "Exp")
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	raw, err := auth.NewOpaqueRefreshToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	expired := &domain.RefreshToken{
		ID:        uuid.New(),
		UserID:    u.ID,
		TokenHash: auth.HashToken(raw),
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	if err := f.refresh.Create(ctx, expired); err != nil {
		t.Fatalf("seed expired token: %v", err)
	}

	if _, err := f.svc.Refresh(ctx, raw, "", ""); !errors.Is(err, domain.ErrTokenExpired) {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}
