package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	m := NewTokenManager("access-secret", "refresh-secret", "nexusvpn", 15*time.Minute, time.Hour)
	userID := uuid.New()

	token, expiresAt, err := m.IssueAccessToken(userID, "user@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatal("expiry should be in the future")
	}

	claims, err := m.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.UserID != userID.String() {
		t.Fatalf("uid = %s, want %s", claims.UserID, userID)
	}
	if claims.Email != "user@example.com" {
		t.Fatalf("email = %s", claims.Email)
	}
	if claims.Type != TokenTypeAccess {
		t.Fatalf("type = %s, want access", claims.Type)
	}
}

func TestAccessTokenRejectsWrongSecret(t *testing.T) {
	issuer := NewTokenManager("secret-a", "r", "nexusvpn", time.Hour, time.Hour)
	verifier := NewTokenManager("secret-b", "r", "nexusvpn", time.Hour, time.Hour)

	token, _, err := issuer.IssueAccessToken(uuid.New(), "user@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := verifier.ParseAccessToken(token); err == nil {
		t.Fatal("expected a token signed with a different secret to be rejected")
	}
}

func TestAccessTokenRejectsWrongIssuer(t *testing.T) {
	issuer := NewTokenManager("s", "r", "some-other-service", time.Hour, time.Hour)
	verifier := NewTokenManager("s", "r", "nexusvpn", time.Hour, time.Hour)

	token, _, err := issuer.IssueAccessToken(uuid.New(), "user@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := verifier.ParseAccessToken(token); err == nil {
		t.Fatal("expected a token from a different issuer to be rejected")
	}
}

func TestExpiredAccessTokenRejected(t *testing.T) {
	m := NewTokenManager("s", "r", "nexusvpn", -time.Minute, time.Hour)

	token, _, err := m.IssueAccessToken(uuid.New(), "user@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := m.ParseAccessToken(token); err == nil {
		t.Fatal("expected an expired token to be rejected")
	}
}

func TestAccessTTLSeconds(t *testing.T) {
	m := NewTokenManager("s", "r", "nexusvpn", 15*time.Minute, time.Hour)
	if got := m.AccessTTLSeconds(); got != 900 {
		t.Fatalf("AccessTTLSeconds = %d, want 900", got)
	}
}

func TestOpaqueRefreshTokensAreUniqueAndHashed(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		tok, err := NewOpaqueRefreshToken()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if seen[tok] {
			t.Fatal("generated a duplicate refresh token")
		}
		seen[tok] = true

		hash := HashToken(tok)
		if hash == tok {
			t.Fatal("hash must differ from the raw token")
		}
		if len(hash) != 64 { // hex-encoded SHA-256
			t.Fatalf("hash length = %d, want 64", len(hash))
		}
		if HashToken(tok) != hash {
			t.Fatal("hashing must be deterministic")
		}
	}
}

func TestPasswordHashing(t *testing.T) {
	h := NewPasswordHasher(4) // low cost keeps the test fast

	hash, err := h.Hash("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if strings.Contains(hash, "correct-horse") {
		t.Fatal("hash must not contain the plaintext password")
	}
	if !h.Verify(hash, "correct-horse-battery-staple") {
		t.Fatal("correct password should verify")
	}
	if h.Verify(hash, "wrong-password") {
		t.Fatal("wrong password must not verify")
	}
	if h.Verify("not-a-valid-bcrypt-hash", "correct-horse-battery-staple") {
		t.Fatal("a malformed hash must not verify")
	}
}

func TestPasswordHashesAreSalted(t *testing.T) {
	h := NewPasswordHasher(4)

	a, err := h.Hash("same-password-here")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	b, err := h.Hash("same-password-here")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if a == b {
		t.Fatal("identical passwords must produce different hashes (salting)")
	}
}

func TestRelaySessionTokenRoundTrip(t *testing.T) {
	m := NewRelaySessionManager("relay-secret", time.Minute)
	deviceID, relayID, networkID := uuid.New(), uuid.New(), uuid.New()

	token, err := m.Issue(deviceID, relayID, networkID)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	claims, err := m.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.DeviceID != deviceID.String() {
		t.Fatalf("device = %s, want %s", claims.DeviceID, deviceID)
	}
	if claims.RelayID != relayID.String() {
		t.Fatalf("relay = %s, want %s", claims.RelayID, relayID)
	}
	if claims.NetworkID != networkID.String() {
		t.Fatalf("network = %s, want %s", claims.NetworkID, networkID)
	}
}

func TestRelaySessionTokenRejectsForeignSecret(t *testing.T) {
	issuer := NewRelaySessionManager("relay-secret", time.Minute)
	verifier := NewRelaySessionManager("different-secret", time.Minute)

	token, err := issuer.Issue(uuid.New(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := verifier.Verify(token); err == nil {
		t.Fatal("expected verification against a different secret to fail")
	}
}

func TestExpiredRelaySessionTokenRejected(t *testing.T) {
	m := NewRelaySessionManager("relay-secret", -time.Minute)

	token, err := m.Issue(uuid.New(), uuid.New(), uuid.New())
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := m.Verify(token); err == nil {
		t.Fatal("expected an expired relay session token to be rejected")
	}
}

func TestMFASecretAndValidation(t *testing.T) {
	m := NewMFAManager("NexusVPN")

	secret, otpauthURL, err := m.GenerateSecret("user@example.com")
	if err != nil {
		t.Fatalf("generate secret: %v", err)
	}
	if secret == "" {
		t.Fatal("expected a non-empty secret")
	}
	if !strings.HasPrefix(otpauthURL, "otpauth://totp/") {
		t.Fatalf("otpauth URL = %q", otpauthURL)
	}
	if !strings.Contains(otpauthURL, "user@example.com") {
		t.Fatalf("otpauth URL should identify the account: %q", otpauthURL)
	}

	// An obviously wrong code must not validate.
	if m.Validate(secret, "000000") && m.Validate(secret, "111111") {
		t.Fatal("arbitrary codes should not validate against a fresh secret")
	}
}
