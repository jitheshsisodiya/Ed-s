package apiclient

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// PinnedClient returns an http.Client that will talk to exactly one server:
// the one holding the certificate with this fingerprint.
//
// The control plane is a machine on somebody's desk. No certificate authority
// will vouch for 192.168.1.20 or a home IP address, and there is no domain to
// prove ownership of, so the usual arrangement is unavailable — not skipped.
//
// What replaces it is stronger for this shape of problem. The fingerprint
// arrives in the pairing QR code, over a channel no network attacker is on: a
// screen in the same room. From then on this client trusts exactly one key,
// rather than any of the hundreds of authorities a browser trusts.
//
// An empty fingerprint returns a client with ordinary verification, for a
// deployment that does have a real certificate.
func PinnedClient(fingerprint string, timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	want := normalizeFingerprint(fingerprint)
	if want == "" {
		return &http.Client{Timeout: timeout}
	}

	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// The name and the chain are not what is being trusted here,
				// so Go's own verification is turned off and replaced —
				// replaced, not dropped. VerifyPeerCertificate below runs on
				// every handshake and refuses anything that is not the exact
				// key this device was introduced to.
				//
				// A certificate for the right address signed by a public
				// authority would pass the standard check and fail this one,
				// which is the intended behaviour: the question is not "is
				// this a valid certificate" but "is this the machine I
				// paired with".
				InsecureSkipVerify: true, //nolint:gosec // replaced by the pin below
				MinVersion:         tls.VersionTLS12,
				VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
					if len(rawCerts) == 0 {
						return fmt.Errorf("apiclient: server presented no certificate")
					}
					sum := sha256.Sum256(rawCerts[0])
					got := hex.EncodeToString(sum[:])
					if got != want {
						return fmt.Errorf("apiclient: this is not the machine you paired with — "+
							"expected certificate %s, got %s", short(want), short(got))
					}
					return nil
				},
			},
		},
	}
}

// normalizeFingerprint accepts the forms a fingerprint is written in: lower
// or upper case, with or without the colons or spaces used to make it
// readable.
func normalizeFingerprint(fp string) string {
	var b strings.Builder
	for _, r := range fp {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
			b.WriteRune(r)
		case r >= 'A' && r <= 'F':
			b.WriteRune(r + ('a' - 'A'))
		}
	}
	out := b.String()
	// A SHA-256 is 64 hex characters. Anything else is not one, and treating
	// it as a pin would silently trust whatever it was.
	if len(out) != 64 {
		return ""
	}
	return out
}

// short abbreviates a fingerprint for an error message, where the whole thing
// is unreadable and the first bytes are enough to tell two apart.
func short(fp string) string {
	if len(fp) <= 16 {
		return fp
	}
	return fp[:16] + "…"
}
