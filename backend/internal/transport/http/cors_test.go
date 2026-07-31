package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// corsHeaders runs a request through the CORS middleware and returns the
// resulting response headers.
func corsHeaders(t *testing.T, allowedOrigins []string, origin string) http.Header {
	t.Helper()
	handler := CORS(allowedOrigins)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/networks", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Header()
}

// Credentialed CORS must never be granted to an origin that was not
// explicitly allowlisted. Echoing back an arbitrary Origin together with
// Access-Control-Allow-Credentials would let any website make credentialed
// cross-origin calls to the API.
func TestWildcardCORSNeverGrantsCredentials(t *testing.T) {
	h := corsHeaders(t, []string{"*"}, "https://evil.example.com")

	if got := h.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Allow-Credentials = %q under a wildcard policy; must be unset", got)
	}
	if got := h.Get("Access-Control-Allow-Origin"); got == "https://evil.example.com" {
		t.Fatal("wildcard policy echoed the request origin back; must reply with a literal *")
	}
	if got := h.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Allow-Origin = %q, want *", got)
	}
}

func TestExplicitOriginGetsCredentials(t *testing.T) {
	h := corsHeaders(t, []string{"https://admin.example.com"}, "https://admin.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("Allow-Origin = %q", got)
	}
	if got := h.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("Allow-Credentials = %q, want true for an allowlisted origin", got)
	}
	// Responses vary by origin, so caches must not share them across origins.
	if got := h.Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want Origin", got)
	}
}

func TestUnlistedOriginGetsNoCORSHeaders(t *testing.T) {
	h := corsHeaders(t, []string{"https://admin.example.com"}, "https://evil.example.com")

	if got := h.Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Allow-Origin = %q for an unlisted origin; must be unset", got)
	}
	if got := h.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Allow-Credentials = %q for an unlisted origin; must be unset", got)
	}
}

// A mixed policy must still refuse credentials to origins that only match
// via the wildcard entry.
func TestWildcardAlongsideExplicitOriginsStaysSafe(t *testing.T) {
	allowed := []string{"*", "https://admin.example.com"}

	explicit := corsHeaders(t, allowed, "https://admin.example.com")
	if got := explicit.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("allowlisted origin lost its credentials grant: %q", got)
	}

	other := corsHeaders(t, allowed, "https://evil.example.com")
	if got := other.Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("Allow-Credentials = %q for a wildcard-only match; must be unset", got)
	}
}
