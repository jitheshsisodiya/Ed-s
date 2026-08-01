package local

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The whole point of this package is that it comes up with nothing
// installed, so the test is the same thing a user does: start it, create an
// account, sign in.
func TestLocalServerRunsWithNothingInstalled(t *testing.T) {
	srv := start(t)

	if !srv.IsFirstRun() {
		t.Fatal("a fresh install did not report itself as a first run")
	}

	body := `{"email":"a@example.com","password":"correct-horse-battery","displayName":"A"}`
	resp := post(t, srv, srv.BaseURL+"/api/v1/auth/register", body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("register returned %d", resp.StatusCode)
	}
	resp.Body.Close()

	if srv.IsFirstRun() {
		t.Fatal("still reported as a first run after an account was created")
	}

	resp = post(t, srv, srv.BaseURL+"/api/v1/auth/login",
		`{"email":"a@example.com","password":"correct-horse-battery"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login returned %d", resp.StatusCode)
	}

	var tokens struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		t.Fatalf("decode tokens: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("login succeeded without issuing tokens")
	}
}

// Signing keys are generated per installation. A shipped default would mean
// every copy of the app signs tokens the same way, and anyone with the
// binary could mint a session against anyone else's server.
func TestSigningKeysAreUniquePerInstallation(t *testing.T) {
	a := start(t)
	b := start(t)

	if a.BaseURL == b.BaseURL {
		t.Fatal("both servers claimed the same port")
	}

	secretsOf := func(s *Server) string {
		t.Helper()
		sec, err := loadOrCreateSecrets(s.dataDir + "/secrets.json")
		if err != nil {
			t.Fatalf("read secrets: %v", err)
		}
		return sec.AccessSecret + sec.RefreshSecret + sec.RelaySecret
	}

	if secretsOf(a) == secretsOf(b) {
		t.Fatal("two installations generated identical signing keys")
	}
	if len(secretsOf(a)) < 96 {
		t.Fatal("signing keys are shorter than 32 bytes each")
	}
}

// Restarting must not sign everyone out, which means the keys have to be
// the ones already on disk rather than fresh ones.
func TestRestartingKeepsTheSameKeys(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/secrets.json"

	first, err := loadOrCreateSecrets(path)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := loadOrCreateSecrets(path)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.AccessSecret != second.AccessSecret {
		t.Fatal("reopening generated new keys, which would sign every device out")
	}
}

// A port already in use has to be reported to the caller, not swallowed by
// a background goroutine after Start has claimed success.
func TestAPortInUseIsReported(t *testing.T) {
	first := start(t)

	_, err := Start(context.Background(), Options{
		DataDir:  t.TempDir(),
		Host:     "127.0.0.1",
		HTTPPort: first.httpPort,
		GRPCPort: first.grpcPort + 1000,
	})
	if err == nil {
		t.Fatal("starting on a taken port reported success")
	}
	if !strings.Contains(err.Error(), "already using it") {
		t.Fatalf("the error does not explain the problem: %v", err)
	}
}

/* ---------- helpers ---------- */

var nextPort = 18080

func start(t *testing.T) *Server {
	t.Helper()
	nextPort += 2
	srv, err := Start(context.Background(), Options{
		DataDir: t.TempDir(),
		// Loopback in tests: binding every interface on a build machine is
		// rude and, in CI, sometimes refused.
		Host:     "127.0.0.1",
		HTTPPort: nextPort,
		GRPCPort: nextPort + 1,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(srv.Stop)
	return srv
}

func post(t *testing.T, srv *Server, url, body string) *http.Response {
	t.Helper()
	resp, err := pinnedClient(t, srv).Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}
