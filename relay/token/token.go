// Package token verifies the relay session tokens issued by the NexusVPN
// control plane.
//
// The claim shape mirrors backend/internal/auth.RelaySessionClaims. It is
// duplicated here rather than imported so relay nodes stay a standalone
// module with no dependency on the control-plane codebase — the only thing
// shared between them is the HS256 signing secret.
package token

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims authorize one device to use one relay for a limited time.
type Claims struct {
	jwt.RegisteredClaims
	DeviceID  string `json:"device_id"`
	RelayID   string `json:"relay_id"`
	NetworkID string `json:"network_id"`
}

// ErrInvalid is returned for any token that fails verification.
var ErrInvalid = errors.New("invalid relay session token")

// Verifier validates relay session tokens against the shared secret.
type Verifier struct {
	secret []byte
	// relayID, when non-empty, additionally requires the token to be scoped
	// to this specific relay node, so a token minted for relay A cannot be
	// replayed against relay B.
	relayID string
}

// NewVerifier builds a Verifier. Pass an empty relayID to skip the
// per-relay scope check (useful before this node has registered itself).
func NewVerifier(secret, relayID string) *Verifier {
	return &Verifier{secret: []byte(secret), relayID: relayID}
}

// SetRelayID binds the verifier to a relay ID once registration assigns one.
func (v *Verifier) SetRelayID(id string) { v.relayID = id }

// Verify parses and validates a token, returning its claims.
func (v *Verifier) Verify(raw string) (*Claims, error) {
	if raw == "" {
		return nil, ErrInvalid
	}

	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return v.secret, nil
	})
	if err != nil || !parsed.Valid {
		return nil, ErrInvalid
	}
	if claims.DeviceID == "" || claims.NetworkID == "" {
		return nil, ErrInvalid
	}
	if v.relayID != "" && claims.RelayID != v.relayID {
		return nil, ErrInvalid
	}
	return claims, nil
}

// SessionKey identifies the relay session a token belongs to. Both peers in
// a session present tokens carrying the same network ID, so the network is
// the rendezvous key; each peer is then distinguished by its device ID.
func (c *Claims) SessionKey() string { return c.NetworkID }

// ExpiresAt returns the token's expiry, or the zero time if unset.
func (c *Claims) ExpiresAt() time.Time {
	if c.RegisteredClaims.ExpiresAt == nil {
		return time.Time{}
	}
	return c.RegisteredClaims.ExpiresAt.Time
}
