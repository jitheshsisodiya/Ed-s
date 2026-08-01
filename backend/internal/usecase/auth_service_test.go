package usecase

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// testIP stands in for the caller's address. Anything keyed by address is
// keyed by this, so a test that means to exhaust an address budget can.
const testIP = "198.51.100.7"

type authFixture struct {
	svc     *AuthService
	users   *fakeUserRepo
	refresh *fakeRefreshRepo
	resets  *fakeResetRepo
	audit   *fakeAuditRepo
	tokens  *auth.TokenManager
	limiter *fakeRateLimiter
}

func newAuthFixture(t *testing.T, mfaCode string) *authFixture {
	t.Helper()
	users := newFakeUserRepo()
	refresh := newFakeRefreshRepo()
	resets := newFakeResetRepo()
	auditRepo := &fakeAuditRepo{}
	tokens := auth.NewTokenManager("access-secret-for-tests", "refresh-secret-for-tests", "nexusvpn-test", 15*time.Minute, 24*time.Hour)

	limiter := &fakeRateLimiter{allow: true}
	svc := NewAuthService(
		users, refresh, resets, tokens, realHasher(),
		stubMFA{validCode: mfaCode}, nil, limiter,
		NewAuditRecorder(auditRepo, zap.NewNop()), time.Hour,
	)
	return &authFixture{
		svc: svc, users: users, refresh: refresh, resets: resets,
		audit: auditRepo, tokens: tokens, limiter: limiter,
	}
}

func TestRegisterThenLogin(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	u, err := f.svc.Register(ctx, "alice@example.com", "correct-horse-battery", "Alice", testIP)
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

	if _, err := f.svc.Register(ctx, "dup@example.com", "correct-horse-battery", "First", testIP); err != nil {
		t.Fatalf("first register: %v", err)
	}
	_, err := f.svc.Register(ctx, "dup@example.com", "another-password-here", "Second", testIP)
	if !errors.Is(err, domain.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}
}

func TestLoginWrongPasswordIsRejectedAndAudited(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	if _, err := f.svc.Register(ctx, "bob@example.com", "correct-horse-battery", "Bob", testIP); err != nil {
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

	u, err := f.svc.Register(ctx, "mfa@example.com", "correct-horse-battery", "MFA User", testIP)
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

	if _, err := f.svc.Register(ctx, "rot@example.com", "correct-horse-battery", "Rot", testIP); err != nil {
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

	if _, err := f.svc.Register(ctx, "out@example.com", "correct-horse-battery", "Out", testIP); err != nil {
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

	if _, err := f.svc.Register(ctx, "reset@example.com", "correct-horse-battery", "Reset", testIP); err != nil {
		t.Fatalf("register: %v", err)
	}
	// Issue a refresh token that should be revoked by the reset.
	login, err := f.svc.Login(ctx, "reset@example.com", "correct-horse-battery", "", "", "")
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	token, err := f.svc.ForgotPassword(ctx, "reset@example.com", testIP)
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

	token, err := f.svc.ForgotPassword(t.Context(), "nobody@example.com", testIP)
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

	u, err := f.svc.Register(ctx, "exp@example.com", "correct-horse-battery", "Exp", testIP)
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

// A per-account limit alone lets an attacker spray one password across many
// accounts unbounded, which is the attack that actually happens. The
// per-address budget is what stops it.
func TestLoginIsLimitedPerAddressAcrossDifferentAccounts(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	var lastErr error
	for i := 0; i < loginPerAddress+1; i++ {
		// A different account every time, so the per-account budget is
		// never the thing doing the stopping.
		email := fmt.Sprintf("victim%d@example.com", i)
		_, lastErr = f.svc.Login(ctx, email, "guess", "", testIP, "ua")
	}

	if !errors.Is(lastErr, domain.ErrForbidden) {
		t.Fatalf("spraying %d accounts from one address was not stopped: %v",
			loginPerAddress+1, lastErr)
	}
	if got := f.limiter.count("login-ip:" + testIP); got != loginPerAddress+1 {
		t.Fatalf("address budget charged %d times, want %d", got, loginPerAddress+1)
	}
}

// Someone else's careless colleague must not lock a whole office out, so
// the address budget is looser than the account one.
func TestAddressBudgetIsLooserThanAccountBudget(t *testing.T) {
	if loginPerAddress <= loginPerAccount {
		t.Fatalf("per-address budget %d must exceed per-account budget %d",
			loginPerAddress, loginPerAccount)
	}
}

func TestRegisterIsLimitedPerAddress(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	for i := 0; i < registerPerAddress; i++ {
		email := fmt.Sprintf("new%d@example.com", i)
		if _, err := f.svc.Register(ctx, email, "correct-horse-battery", "New", testIP); err != nil {
			t.Fatalf("registration %d should have been allowed: %v", i, err)
		}
	}

	_, err := f.svc.Register(ctx, "toomany@example.com", "correct-horse-battery", "Too Many", testIP)
	if !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("expected ErrForbidden past the budget, got %v", err)
	}
	if _, err := f.users.GetByEmail(ctx, "toomany@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("a rejected registration must not have created the account")
	}
}

// Reset mail lands in somebody else's inbox, so an unbounded endpoint is a
// mail-flooding tool. Crucially the refusal must be silent: an error would
// tell an attacker which addresses are worth grinding, which is exactly what
// this endpoint refuses to reveal.
func TestForgotPasswordIsLimitedWithoutRevealingAnything(t *testing.T) {
	f := newAuthFixture(t, "")
	ctx := t.Context()

	if _, err := f.svc.Register(ctx, "target@example.com", "correct-horse-battery", "Target", testIP); err != nil {
		t.Fatalf("register: %v", err)
	}

	issued := 0
	for i := 0; i < resetPerAccount+3; i++ {
		token, err := f.svc.ForgotPassword(ctx, "target@example.com", testIP)
		if err != nil {
			t.Fatalf("attempt %d returned an error, which leaks that the "+
				"account exists: %v", i, err)
		}
		if token != "" {
			issued++
		}
	}

	if issued != resetPerAccount {
		t.Fatalf("issued %d reset tokens, want %d", issued, resetPerAccount)
	}
}

// Redis falling over must not lock every user out of their own account.
func TestLimiterFailureFailsOpen(t *testing.T) {
	f := newAuthFixture(t, "")
	f.limiter.err = errors.New("redis is down")
	ctx := t.Context()

	// Registration still has to work, and so does the login that follows.
	if _, err := f.svc.Register(ctx, "open@example.com", "correct-horse-battery", "Open", testIP); err != nil {
		t.Fatalf("a limiter outage locked out registration: %v", err)
	}
	if _, err := f.svc.Login(ctx, "open@example.com", "correct-horse-battery", "", testIP, "ua"); err != nil {
		t.Fatalf("a limiter outage locked out login: %v", err)
	}
}
