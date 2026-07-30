package nexusvpn

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginAndListNetworks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
			json.NewEncoder(w).Encode(TokenPair{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresIn: 900})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/networks":
			if r.Header.Get("Authorization") != "Bearer access-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			json.NewEncoder(w).Encode([]Network{{ID: "n1", Name: "Home Lab"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := New(srv.URL + "/api/v1")
	if _, err := c.Login(t.Context(), "a@example.com", "pw", ""); err != nil {
		t.Fatalf("login: %v", err)
	}
	networks, err := c.ListNetworks(t.Context())
	if err != nil {
		t.Fatalf("list networks: %v", err)
	}
	if len(networks) != 1 || networks[0].Name != "Home Lab" {
		t.Fatalf("unexpected networks: %+v", networks)
	}
}

func TestRefreshOnUnauthorized(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/refresh":
			json.NewEncoder(w).Encode(TokenPair{AccessToken: "access-2", RefreshToken: "refresh-2"})
		case r.URL.Path == "/api/v1/networks":
			calls++
			if r.Header.Get("Authorization") == "Bearer access-2" {
				json.NewEncoder(w).Encode([]Network{{ID: "n1"}})
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer srv.Close()

	c := New(srv.URL+"/api/v1", WithTokens("stale-token", "refresh-1"))
	networks, err := c.ListNetworks(t.Context())
	if err != nil {
		t.Fatalf("expected success after refresh, got: %v", err)
	}
	if len(networks) != 1 {
		t.Fatalf("unexpected networks: %+v", networks)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls to /networks (fail then retry), got %d", calls)
	}
}
