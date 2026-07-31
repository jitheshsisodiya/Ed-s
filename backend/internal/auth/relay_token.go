package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// RelaySessionClaims authorize a device to open a session on a specific
// relay node for a limited time. The relay daemon validates these using
// the same shared secret.
type RelaySessionClaims struct {
	jwt.RegisteredClaims
	DeviceID  string `json:"device_id"`
	RelayID   string `json:"relay_id"`
	NetworkID string `json:"network_id"`
}

// RelaySessionManager issues and verifies short-lived relay session tokens.
type RelaySessionManager struct {
	secret []byte
	ttl    time.Duration
}

// NewRelaySessionManager builds a RelaySessionManager.
func NewRelaySessionManager(secret string, ttl time.Duration) *RelaySessionManager {
	return &RelaySessionManager{secret: []byte(secret), ttl: ttl}
}

// Issue creates a signed session token scoped to deviceID+relayID+networkID.
func (m *RelaySessionManager) Issue(deviceID, relayID, networkID uuid.UUID) (string, error) {
	now := time.Now()
	claims := RelaySessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   deviceID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			ID:        uuid.NewString(),
		},
		DeviceID:  deviceID.String(),
		RelayID:   relayID.String(),
		NetworkID: networkID.String(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(m.secret)
}

// Verify validates a relay session token and returns its claims. Used by
// the relay daemon (via the same shared secret) to authorize sessions.
func (m *RelaySessionManager) Verify(tokenStr string) (*RelaySessionClaims, error) {
	claims := &RelaySessionClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !token.Valid {
		return nil, fmt.Errorf("invalid relay session token")
	}
	return claims, nil
}
