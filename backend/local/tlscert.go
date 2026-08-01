package local

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// identity is this installation's TLS certificate and the fingerprint that
// identifies it.
type identity struct {
	cert        tls.Certificate
	fingerprint string
}

// loadOrCreateIdentity returns this installation's certificate, generating one
// on first run.
//
// Self-signed, and that is the right answer here rather than a compromise. A
// public certificate authority will not issue for 192.168.1.20 or for a home
// IP address, and there is no domain name to prove ownership of. What makes
// this safe is not who signed it but that the device pairing with it is told
// the exact fingerprint to expect, over a channel — a QR code on the screen
// in front of you — that no network attacker is on.
//
// That is stronger than the usual arrangement, not weaker: a public CA
// vouches for any of thousands of issuers, whereas this trusts exactly one
// key, the one shown on the screen at the moment of pairing.
func loadOrCreateIdentity(dir string) (*identity, error) {
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err == nil {
		fp, err := fingerprintOf(cert)
		if err == nil && !expiringSoon(cert) {
			return &identity{cert: cert, fingerprint: fp}, nil
		}
		// Unreadable or nearly expired: replaced rather than limped along
		// with. A device that pairs against a certificate about to expire
		// would stop trusting this server days later for no visible reason.
	}

	cert, err = generateSelfSigned(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	fp, err := fingerprintOf(cert)
	if err != nil {
		return nil, err
	}
	return &identity{cert: cert, fingerprint: fp}, nil
}

// certLifetime is deliberately long. This certificate is pinned by devices
// that may not be switched on for months, and an expiry is a day on which
// every one of them stops working at once, for a reason none of them can
// explain to their owner.
const certLifetime = 10 * 365 * 24 * time.Hour

func generateSelfSigned(certPath, keyPath string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, err
	}

	now := time.Now()
	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "NexusVPN"},
		// Backdated an hour so a device whose clock runs slow — which on a
		// phone that has just been reset is normal — does not reject a
		// certificate issued seconds ago as not yet valid.
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		// Every address this machine might be reached at, because the client
		// checks the name it dialled against these. The set is recorded at
		// generation time and the address can change afterwards, which is
		// precisely why the client pins the fingerprint instead of relying
		// on this list.
		DNSNames:    []string{"nexusvpn", "nexusvpn.local", "localhost"},
		IPAddresses: localIPs(),
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	if err := os.MkdirAll(filepath.Dir(certPath), 0o700); err != nil {
		return tls.Certificate{}, err
	}
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("local: write %s: %w", certPath, err)
	}
	// The private key is what lets something be this server, so it is owner
	// only, like the signing secrets beside it.
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("local: write %s: %w", keyPath, err)
	}

	return tls.X509KeyPair(certPEM, keyPEM)
}

// fingerprintOf returns the SHA-256 of the certificate, lower-case hex, which
// is what a pairing code carries and a phone compares against.
func fingerprintOf(cert tls.Certificate) (string, error) {
	if len(cert.Certificate) == 0 {
		return "", fmt.Errorf("local: certificate has no contents")
	}
	sum := sha256.Sum256(cert.Certificate[0])
	return hex.EncodeToString(sum[:]), nil
}

func expiringSoon(cert tls.Certificate) bool {
	leaf := cert.Leaf
	if leaf == nil {
		parsed, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			return true
		}
		leaf = parsed
	}
	return time.Now().Add(30 * 24 * time.Hour).After(leaf.NotAfter)
}

// localIPs lists every address this machine currently has, so the certificate
// names them. Loopback is included because the app on this machine reaches
// its own server that way.
func localIPs() []net.IP {
	out := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	ifaces, err := net.Interfaces()
	if err != nil {
		return out
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if n, ok := addr.(*net.IPNet); ok && !n.IP.IsLoopback() {
				out = append(out, n.IP)
			}
		}
	}
	return out
}

// FormatFingerprint groups a fingerprint for a person to compare by eye, in
// the rare case they have to. Machines compare the raw form.
func FormatFingerprint(fp string) string {
	var b strings.Builder
	for i := 0; i < len(fp); i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		end := min(i+4, len(fp))
		b.WriteString(strings.ToUpper(fp[i:end]))
	}
	return b.String()
}
