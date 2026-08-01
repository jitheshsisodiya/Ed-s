package apiclient

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These run against real TLS servers rather than a fake transport, because
// what is being tested is a TLS handshake callback. A test that stubbed the
// transport would exercise none of it and pass regardless.

func TestPinnedClientAcceptsTheServerItWasIntroducedTo(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello")
	}))
	defer srv.Close()

	client := PinnedClient(fingerprintOfServer(t, srv), 5*time.Second)
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("refused the server it was pinned to: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("got %q", body)
	}
}

// The whole point. Something else answering on that address — a different
// machine, or somebody in the middle with a certificate of their own — must
// not be talked to, however valid its certificate is in the ordinary sense.
func TestPinnedClientRefusesADifferentServer(t *testing.T) {
	ours := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer ours.Close()

	// A certificate generated here, not another httptest server: httptest
	// hands every server it makes the same built-in certificate, so two of
	// them are indistinguishable to a fingerprint and this test would pass
	// while proving nothing. It did, until this was noticed.
	theirs := serverWithItsOwnCertificate(t)
	defer theirs.Close()

	client := PinnedClient(fingerprintOfServer(t, ours), 5*time.Second)
	resp, err := client.Get(theirs.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("talked to a server it was never introduced to")
	}
	if !strings.Contains(err.Error(), "not the machine you paired with") {
		t.Fatalf("the refusal does not say why: %v", err)
	}
}

// An unreadable pin must not quietly become "trust anything". That is the
// failure that looks like it works.
func TestUnusableFingerprintDoesNotBecomeTrustEverything(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	for _, bad := range []string{
		"not-a-fingerprint",
		"abcd",                   // too short
		strings.Repeat("ab", 40), // too long
		strings.Repeat("zz", 32), // right length, not hex
	} {
		client := PinnedClient(bad, 5*time.Second)
		resp, err := client.Get(srv.URL)
		if err == nil {
			resp.Body.Close()
			t.Errorf("fingerprint %q was discarded and the server trusted anyway", bad)
		}
	}
}

func TestFingerprintFormsAreAllAccepted(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	raw := fingerprintOfServer(t, srv)

	// The same value written the ways a person or another tool might write it.
	colons := strings.Join(split2(raw), ":")
	spaced := strings.Join(split2(raw), " ")
	for _, form := range []string{raw, strings.ToUpper(raw), colons, spaced} {
		client := PinnedClient(form, 5*time.Second)
		resp, err := client.Get(srv.URL)
		if err != nil {
			t.Errorf("form %q was rejected: %v", short(form), err)
			continue
		}
		resp.Body.Close()
	}
}

// An empty fingerprint means "no pin", for a deployment with a real
// certificate. It must still verify normally rather than trust anything —
// httptest's certificate is not one any authority signed, so a properly
// verifying client refuses it.
func TestNoFingerprintStillVerifiesNormally(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()

	client := PinnedClient("", 5*time.Second)
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("with no pin, an unsigned certificate was accepted")
	}
}

// serverWithItsOwnCertificate stands in for somebody else's machine: a real
// TLS server holding a key this client has never been introduced to.
func serverWithItsOwnCertificate(t *testing.T) *httptest.Server {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "somebody else"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "your password please")
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	}
	srv.StartTLS()
	return srv
}

func fingerprintOfServer(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	if srv.Certificate() == nil {
		t.Fatal("test server has no certificate")
	}
	sum := sha256.Sum256(srv.Certificate().Raw)
	return hex.EncodeToString(sum[:])
}

func split2(s string) []string {
	var out []string
	for i := 0; i < len(s); i += 2 {
		out = append(out, s[i:i+2])
	}
	return out
}
