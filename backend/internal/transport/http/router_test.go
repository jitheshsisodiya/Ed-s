package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
)

// The router tests exercise the transport layer end to end (routing, auth
// middleware, request validation, status codes and the error envelope)
// against the real chain, with the use-case layer's dependencies faked at
// the repository boundary in package usecase's own tests.
//
// Here we focus on what the transport layer itself owns: authentication,
// method/path routing, malformed input handling and response shape.

func newTestTokens() *auth.TokenManager {
	return auth.NewTokenManager("access-secret-test", "refresh-secret-test", "nexusvpn-test", 15*time.Minute, time.Hour)
}

// newMinimalRouter builds a router with nil use-case services. It is only
// used for tests that never reach a handler body (auth rejections, 404s,
// method mismatches), which the middleware short-circuits first.
func newMinimalRouter(t *testing.T) (http.Handler, *auth.TokenManager) {
	t.Helper()
	tokens := newTestTokens()
	r := NewRouter(RouterConfig{
		Auth:        NewAuthHandler(nil, zap.NewNop(), false),
		Networks:    NewNetworkHandler(nil, nil),
		Devices:     NewDeviceHandler(nil),
		Logs:        NewLogsHandler(nil, nil),
		Tokens:      tokens,
		Logger:      zap.NewNop(),
		CORSOrigins: []string{"*"},
	})
	return r, tokens
}

func TestHealthzIsPublic(t *testing.T) {
	r, _ := newMinimalRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body = %v", body)
	}
}

func TestProtectedRouteRejectsMissingToken(t *testing.T) {
	r, _ := newMinimalRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/networks", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}

	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if env.Error.Code != "unauthorized" {
		t.Fatalf("error code = %q, want unauthorized", env.Error.Code)
	}
}

func TestProtectedRouteRejectsGarbageToken(t *testing.T) {
	r, _ := newMinimalRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env errorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "token_invalid" {
		t.Fatalf("error code = %q, want token_invalid", env.Error.Code)
	}
}

func TestProtectedRouteRejectsExpiredToken(t *testing.T) {
	r, _ := newMinimalRouter(t)
	// A manager whose access tokens expired an hour ago.
	expiredTokens := auth.NewTokenManager("access-secret-test", "refresh-secret-test", "nexusvpn-test", -time.Hour, time.Hour)
	token, _, err := expiredTokens.IssueAccessToken(uuid.New(), "expired@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for an expired token", rec.Code)
	}
}

func TestTokenSignedWithWrongSecretRejected(t *testing.T) {
	r, _ := newMinimalRouter(t)
	foreign := auth.NewTokenManager("someone-elses-secret", "x", "nexusvpn-test", time.Hour, time.Hour)
	token, _, err := foreign.IssueAccessToken(uuid.New(), "mallory@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/networks", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for a foreign-signed token", rec.Code)
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	r, _ := newMinimalRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/does-not-exist", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestWrongMethodIsNotRouted(t *testing.T) {
	r, _ := newMinimalRouter(t)

	// /auth/login is POST-only.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/login", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 405/404 for a wrong-method request", rec.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	// An explicit allowlist is the realistic deployment shape: it is the
	// only configuration that grants credentials (see cors_test.go).
	tokens := newTestTokens()
	r := NewRouter(RouterConfig{
		Auth:        NewAuthHandler(nil, zap.NewNop(), false),
		Networks:    NewNetworkHandler(nil, nil),
		Devices:     NewDeviceHandler(nil),
		Logs:        NewLogsHandler(nil, nil),
		Tokens:      tokens,
		Logger:      zap.NewNop(),
		CORSOrigins: []string{"http://localhost:3000"},
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/networks", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 for a preflight", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("expected Access-Control-Allow-Methods to be set")
	}
}

func TestCORSDisallowedOriginGetsNoHeader(t *testing.T) {
	tokens := newTestTokens()
	r := NewRouter(RouterConfig{
		Auth:        NewAuthHandler(nil, zap.NewNop(), false),
		Networks:    NewNetworkHandler(nil, nil),
		Devices:     NewDeviceHandler(nil),
		Logs:        NewLogsHandler(nil, nil),
		Tokens:      tokens,
		Logger:      zap.NewNop(),
		CORSOrigins: []string{"https://admin.example.com"},
	})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no allow-origin header for a disallowed origin, got %q", got)
	}
}

func TestMalformedJSONRejected(t *testing.T) {
	r, _ := newMinimalRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var env errorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "invalid_input" {
		t.Fatalf("error code = %q, want invalid_input", env.Error.Code)
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	r, _ := newMinimalRouter(t)

	cases := []struct {
		name string
		body map[string]string
	}{
		{"missing email", map[string]string{"password": "correct-horse-battery", "displayName": "X"}},
		{"bad email", map[string]string{"email": "nope", "password": "correct-horse-battery", "displayName": "X"}},
		{"short password", map[string]string{"email": "a@example.com", "password": "short", "displayName": "X"}},
		{"missing display name", map[string]string{"email": "a@example.com", "password": "correct-horse-battery"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", bytes.NewReader(body))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
		})
	}
}

func TestMalformedUUIDPathParamRejected(t *testing.T) {
	r, tokens := newMinimalRouter(t)
	token, _, err := tokens.IssueAccessToken(uuid.New(), "user@example.com")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/networks/not-a-uuid", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a malformed UUID", rec.Code)
	}
}

func TestAuthenticateInjectsUserIntoContext(t *testing.T) {
	tokens := newTestTokens()
	wantID := uuid.New()
	token, _, err := tokens.IssueAccessToken(wantID, "ctx@example.com")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	var gotID uuid.UUID
	var gotEmail string
	handler := Authenticate(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID, _ = UserIDFromContext(r.Context())
		gotEmail, _ = EmailFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/whatever", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if gotID != wantID {
		t.Fatalf("user id = %s, want %s", gotID, wantID)
	}
	if gotEmail != "ctx@example.com" {
		t.Fatalf("email = %q", gotEmail)
	}
}

func TestAuthenticateAcceptsQueryParamToken(t *testing.T) {
	tokens := newTestTokens()
	token, _, err := tokens.IssueAccessToken(uuid.New(), "ws@example.com")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	handler := Authenticate(tokens)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/whatever?access_token="+token, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (query-param token should be accepted)", rec.Code)
	}
}

func TestRecoverConvertsPanicTo500(t *testing.T) {
	var recovered bool
	handler := Recover(func(any) { recovered = true })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !recovered {
		t.Fatal("expected the panic hook to fire")
	}
	var env errorEnvelope
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	if env.Error.Code != "internal_error" {
		t.Fatalf("error code = %q, want internal_error", env.Error.Code)
	}
	// The panic message must not leak to the client.
	if bytes.Contains(rec.Body.Bytes(), []byte("boom")) {
		t.Fatal("panic detail leaked into the response body")
	}
}

func TestClientIPPrefersXForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:5555"
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")

	if got := clientIP(req); got != "203.0.113.7" {
		t.Fatalf("clientIP = %q, want 203.0.113.7", got)
	}
}

func TestClientIPFallsBackToRemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "198.51.100.4:9999"

	if got := clientIP(req); got != "198.51.100.4" {
		t.Fatalf("clientIP = %q, want 198.51.100.4", got)
	}
}

// An unauthenticated endpoint that reads an unbounded body is a memory
// exhaustion primitive: one connection carrying a gigabyte-long string field
// costs a gigabyte of allocation before any credential has been checked.
func TestDecodeJSONRefusesAnOversizedBody(t *testing.T) {
	var body bytes.Buffer
	body.WriteString(`{"email":"`)
	body.Write(bytes.Repeat([]byte("a"), maxRequestBody+1024))
	body.WriteString(`"}`)

	req := httptest.NewRequest(http.MethodPost, "/auth/login", &body)
	var dst struct {
		Email string `json:"email"`
	}

	if err := decodeJSON(req, &dst); err == nil {
		t.Fatal("a body past the limit was accepted")
	}
}

// The limit must not be so tight that ordinary requests trip it.
func TestDecodeJSONAcceptsAnOrdinaryBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login",
		strings.NewReader(`{"email":"alice@example.com"}`))
	var dst struct {
		Email string `json:"email"`
	}

	if err := decodeJSON(req, &dst); err != nil {
		t.Fatalf("an ordinary body was rejected: %v", err)
	}
	if dst.Email != "alice@example.com" {
		t.Fatalf("Email = %q, want alice@example.com", dst.Email)
	}
}
