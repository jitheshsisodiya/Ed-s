package local

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

// TestPairingEnrolsADeviceWithoutItsOwnPassword drives the flow a phone
// actually takes, against a real server: a signed-in machine asks for a code,
// something with no credentials at all redeems it, and comes away with a
// working session.
//
// Written at this level on purpose. The parts were each unit tested and each
// worked; what this checks is that they are wired to one another and reachable
// over HTTP, which is where the bugs in this feature would live.
func TestPairingEnrolsADeviceWithoutItsOwnPassword(t *testing.T) {
	srv := start(t)
	base := srv.BaseURL

	// 1. Somebody creates an account on the machine that runs the server.
	var registered map[string]any
	postJSON(t, srv, base+"/api/v1/auth/register", "", map[string]string{
		"email":       "someone@example.com",
		"password":    "a-long-enough-password",
		"displayName": "Someone",
	}, &registered)

	var session map[string]any
	postJSON(t, srv, base+"/api/v1/auth/login", "", map[string]string{
		"email":    "someone@example.com",
		"password": "a-long-enough-password",
	}, &session)
	access, _ := session["accessToken"].(string)
	if access == "" {
		t.Fatal("login returned no access token")
	}

	// 2. That session asks for a pairing code.
	var pairing map[string]any
	postJSON(t, srv, base+"/api/v1/auth/pair", access, map[string]string{}, &pairing)
	token, _ := pairing["token"].(string)
	if token == "" {
		t.Fatal("pairing returned no token")
	}
	if secs, _ := pairing["expiresIn"].(float64); secs <= 0 {
		t.Fatalf("expiresIn = %v, want a positive lifetime", pairing["expiresIn"])
	}

	// 3. A device with no credentials whatsoever redeems it.
	var claimed map[string]any
	postJSON(t, srv, base+"/api/v1/auth/pair/claim", "", map[string]string{"token": token}, &claimed)
	newAccess, _ := claimed["accessToken"].(string)
	if newAccess == "" {
		t.Fatal("claiming returned no access token")
	}
	if got, _ := claimed["email"].(string); got != "someone@example.com" {
		t.Fatalf("claimed as %q, want someone@example.com", got)
	}

	// 4. And the session it got actually works.
	req, err := http.NewRequest(http.MethodGet, base+"/api/v1/networks", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+newAccess)
	resp, err := pinnedClient(t, srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the paired session could not list networks: %s", resp.Status)
	}
}

// TestPairingCodeIsSpentOnFirstUse: a code is shown on a screen, where anyone
// in the room can photograph it. Single use is what makes that acceptable.
func TestPairingCodeIsSpentOnFirstUse(t *testing.T) {
	srv := start(t)
	base := srv.BaseURL

	postJSON(t, srv, base+"/api/v1/auth/register", "", map[string]string{
		"email": "someone@example.com", "password": "a-long-enough-password",
		"displayName": "Someone",
	}, nil)
	var session map[string]any
	postJSON(t, srv, base+"/api/v1/auth/login", "", map[string]string{
		"email": "someone@example.com", "password": "a-long-enough-password",
	}, &session)
	access, _ := session["accessToken"].(string)

	var pairing map[string]any
	postJSON(t, srv, base+"/api/v1/auth/pair", access, map[string]string{}, &pairing)
	token, _ := pairing["token"].(string)

	postJSON(t, srv, base+"/api/v1/auth/pair/claim", "", map[string]string{"token": token}, nil)

	// The second attempt is the one an onlooker would make.
	status := postJSONStatus(t, srv, base+"/api/v1/auth/pair/claim", "", map[string]string{"token": token})
	if status == http.StatusOK {
		t.Fatal("a pairing code was redeemed twice")
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("second claim returned %d, want 401", status)
	}
}

// TestPairingCannotBeStartedWithoutASession: if anyone could mint one of
// these, it would be a way to sign in as anybody.
func TestPairingCannotBeStartedWithoutASession(t *testing.T) {
	srv := start(t)
	base := srv.BaseURL

	if got := postJSONStatus(t, srv, base+"/api/v1/auth/pair", "", map[string]string{}); got != http.StatusUnauthorized {
		t.Fatalf("unauthenticated pairing request returned %d, want 401", got)
	}
}

func TestClaimingRubbishIsRefused(t *testing.T) {
	srv := start(t)
	base := srv.BaseURL

	for _, token := range []string{"not-a-token", "a.b.c", ""} {
		status := postJSONStatus(t, srv, base+"/api/v1/auth/pair/claim", "", map[string]string{"token": token})
		if status == http.StatusOK {
			t.Fatalf("claiming %q succeeded", token)
		}
	}
}

/* ---------- helpers ---------- */

// postJSON is the authenticated, body-decoding sibling of the package's
// existing post helper, which takes a raw string and returns the response.
func postJSON(t *testing.T, srv *Server, url, bearer string, body any, out any) {
	t.Helper()
	status, blob := doPostJSON(t, srv, url, bearer, body)
	if status < 200 || status > 299 {
		t.Fatalf("POST %s returned %d: %s", url, status, blob)
	}
	if out != nil {
		if err := json.Unmarshal(blob, out); err != nil {
			t.Fatalf("POST %s returned unreadable JSON: %v", url, err)
		}
	}
}

func postJSONStatus(t *testing.T, srv *Server, url, bearer string, body any) int {
	t.Helper()
	status, _ := doPostJSON(t, srv, url, bearer, body)
	return status
}

func doPostJSON(t *testing.T, srv *Server, url, bearer string, body any) (int, []byte) {
	t.Helper()
	blob, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(blob))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := pinnedClient(t, srv).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := new(bytes.Buffer)
	_, _ = out.ReadFrom(resp.Body)
	return resp.StatusCode, out.Bytes()
}

// pinnedClient trusts exactly the certificate this server is serving.
//
// Built from srv.Fingerprint rather than by disabling verification, so these
// tests also check that the fingerprint the server reports is the one it
// actually presents — a mismatch there would ship a pairing code nothing
// could use, and no other test would notice.
func pinnedClient(t *testing.T, srv *Server) *http.Client {
	t.Helper()
	want, err := hex.DecodeString(srv.Fingerprint)
	if err != nil || len(want) != sha256.Size {
		t.Fatalf("server reported an unusable fingerprint %q", srv.Fingerprint)
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec // replaced by the check below
				MinVersion:         tls.VersionTLS12,
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					if len(rawCerts) == 0 {
						return errors.New("no certificate")
					}
					got := sha256.Sum256(rawCerts[0])
					if !bytes.Equal(got[:], want) {
						return errors.New("served a different certificate than it reported")
					}
					return nil
				},
			},
		},
	}
}
