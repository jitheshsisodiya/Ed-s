package auth

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// ErrPairingTokenUsed means a pairing token has already been redeemed.
var ErrPairingTokenUsed = errors.New("auth: pairing token has already been used")

// PairingClaims authorise one device to be signed in as a user, once,
// without that user's password.
type PairingClaims struct {
	jwt.RegisteredClaims
	UserID    string `json:"uid"`
	NetworkID string `json:"nid,omitempty"`
}

// PairingManager issues and redeems pairing tokens.
//
// A pairing token is what a QR code on an already-signed-in screen carries,
// so a phone can be enrolled by pointing a camera at a laptop instead of
// typing a server address, an email and a password on a touchscreen. Every
// one of those three is a chance to get it wrong, and the address is not
// something most people can be expected to know.
//
// It is therefore a bearer credential for somebody's whole account, and is
// treated like one:
//
//   - It is only ever issued to a session that is already authenticated, so
//     making one requires already being signed in somewhere.
//   - It lives for minutes, not hours. Long enough to walk across a room.
//   - It is single use. A photograph of the screen taken over somebody's
//     shoulder is worth nothing once the phone it was meant for has claimed
//     it, and the legitimate owner finds out immediately, because their own
//     scan fails.
type PairingManager struct {
	secret []byte
	ttl    time.Duration

	mu sync.Mutex
	// used maps a token's unique id to the moment it stops being worth
	// remembering, which is when the token would have expired anyway.
	//
	// In process, which is correct for the desktop app running its own
	// control plane — the case this feature exists for — and for any
	// single-instance deployment. A hosted deployment behind more than one
	// replica needs this in shared storage, or a token becomes usable once
	// per replica.
	used map[string]time.Time
}

// NewPairingManager builds a PairingManager. A ttl of zero means five
// minutes.
//
// Anything under a second is raised to a second: JWT expiry is expressed in
// whole seconds, so a shorter lifetime cannot be written down and produces a
// token that is already expired when it is handed over.
func NewPairingManager(secret string, ttl time.Duration) *PairingManager {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if ttl < time.Second {
		ttl = time.Second
	}
	return &PairingManager{
		secret: []byte(secret),
		ttl:    ttl,
		used:   map[string]time.Time{},
	}
}

// TTL is how long an issued token stays valid, so a caller can show it.
func (m *PairingManager) TTL() time.Duration { return m.ttl }

// Issue creates a pairing token for a user, optionally naming the network
// the new device should end up on.
func (m *PairingManager) Issue(userID uuid.UUID, networkID string) (string, time.Time, error) {
	now := time.Now()
	expires := now.Add(m.ttl)
	claims := PairingClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
			ID:        uuid.NewString(),
		},
		UserID:    userID.String(),
		NetworkID: networkID,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expires, nil
}

// Consume validates a pairing token and spends it. A second call with the
// same token fails, whoever makes it.
func (m *PairingManager) Consume(tokenStr string) (*PairingClaims, error) {
	claims := &PairingClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, errors.New("auth: invalid pairing token")
	}
	if claims.ID == "" {
		// Without an id there is nothing to remember, so the token could be
		// redeemed forever. Nothing this package issues lacks one.
		return nil, errors.New("auth: pairing token cannot be tracked")
	}

	expires := time.Now().Add(m.ttl)
	if claims.ExpiresAt != nil {
		expires = claims.ExpiresAt.Time
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sweepLocked()
	if _, spent := m.used[claims.ID]; spent {
		return nil, ErrPairingTokenUsed
	}
	m.used[claims.ID] = expires

	return claims, nil
}

// sweepLocked drops tokens that have expired, so the set stays the size of
// however many pairings are in flight rather than growing forever.
func (m *PairingManager) sweepLocked() {
	now := time.Now()
	for id, expires := range m.used {
		if now.After(expires) {
			delete(m.used, id)
		}
	}
}
