package apiclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLogin_StoresTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "user@example.com" {
			t.Fatalf("unexpected email: %s", body["email"])
		}
		json.NewEncoder(w).Encode(TokenPair{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresIn: 900})
	}))
	defer srv.Close()

	c := New(srv.URL, Options{})
	tokens, err := c.Login(context.Background(), "user@example.com", "hunter2", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tokens.AccessToken != "access-1" {
		t.Fatalf("AccessToken = %q, want access-1", tokens.AccessToken)
	}
	tok, _ := c.AccessToken(context.Background())
	if tok != "access-1" {
		t.Fatalf("stored access token = %q, want access-1", tok)
	}
}

func TestLogin_MFARequiredDoesNotStoreTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(TokenPair{MFARequired: true})
	}))
	defer srv.Close()

	c := New(srv.URL, Options{})
	tokens, err := c.Login(context.Background(), "user@example.com", "hunter2", "")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if !tokens.MFARequired {
		t.Fatalf("expected MFARequired true")
	}
	tok, _ := c.AccessToken(context.Background())
	if tok != "" {
		t.Fatalf("expected no access token to be stored when MFA required, got %q", tok)
	}
}

func TestLogin_InvalidCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]string{"code": "invalid_credentials", "message": "bad email or password"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL, Options{})
	_, err := c.Login(context.Background(), "user@example.com", "wrong", "")
	if err == nil {
		t.Fatalf("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized || apiErr.Code != "invalid_credentials" {
		t.Fatalf("unexpected APIError: %+v", apiErr)
	}
}

func TestAuthenticatedRequest_AttachesBearerToken(t *testing.T) {
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		json.NewEncoder(w).Encode([]Network{{ID: "net-1", Name: "Home"}})
	}))
	defer srv.Close()

	c := New(srv.URL, Options{})
	c.SetTokens("my-access-token", "my-refresh-token")

	networks, err := c.ListNetworks(context.Background())
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(networks) != 1 || networks[0].ID != "net-1" {
		t.Fatalf("unexpected networks: %+v", networks)
	}
	if gotAuth.Load().(string) != "Bearer my-access-token" {
		t.Fatalf("Authorization header = %q", gotAuth.Load())
	}
}

func TestAuthenticatedRequest_RefreshesOn401(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/networks":
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				// First call with the stale token: unauthorized.
				if r.Header.Get("Authorization") != "Bearer stale-token" {
					t.Fatalf("expected stale token on first call, got %q", r.Header.Get("Authorization"))
				}
				w.WriteHeader(http.StatusUnauthorized)
				json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "unauthorized"}})
				return
			}
			// Retried call after refresh should carry the new token.
			if r.Header.Get("Authorization") != "Bearer fresh-token" {
				t.Fatalf("expected fresh token on retry, got %q", r.Header.Get("Authorization"))
			}
			json.NewEncoder(w).Encode([]Network{{ID: "net-1"}})
		case "/auth/refresh":
			json.NewEncoder(w).Encode(TokenPair{AccessToken: "fresh-token", RefreshToken: "fresh-refresh"})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	var refreshedTokens TokenPair
	c := New(srv.URL, Options{OnTokenRefresh: func(tp TokenPair) { refreshedTokens = tp }})
	c.SetTokens("stale-token", "some-refresh-token")

	networks, err := c.ListNetworks(context.Background())
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(networks) != 1 {
		t.Fatalf("unexpected networks: %+v", networks)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls to /networks (original + retry), got %d", calls)
	}
	if refreshedTokens.AccessToken != "fresh-token" {
		t.Fatalf("OnTokenRefresh callback not invoked with new tokens: %+v", refreshedTokens)
	}
}

func TestJoinNetwork(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/networks/join" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["inviteCode"] != "ABC123" {
			t.Fatalf("unexpected invite code: %s", body["inviteCode"])
		}
		json.NewEncoder(w).Encode(Network{ID: "net-1", Name: "Home Lab", CIDR: "10.77.0.0/24"})
	}))
	defer srv.Close()

	c := New(srv.URL, Options{})
	c.SetTokens("tok", "ref")

	network, err := c.JoinNetwork(context.Background(), "ABC123")
	if err != nil {
		t.Fatalf("JoinNetwork: %v", err)
	}
	if network.ID != "net-1" || network.CIDR != "10.77.0.0/24" {
		t.Fatalf("unexpected network: %+v", network)
	}
}

func TestRegisterDevice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(Device{ID: "dev-1", VirtualIP: "10.77.0.9", PublicKey: "pubkey"})
	}))
	defer srv.Close()

	c := New(srv.URL, Options{})
	c.SetTokens("tok", "ref")

	device, err := c.RegisterDevice(context.Background(), "net-1", "laptop", "linux", "6.1", "pubkey")
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if device.VirtualIP != "10.77.0.9" {
		t.Fatalf("unexpected device: %+v", device)
	}
}
