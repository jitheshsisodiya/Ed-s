package ws

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
)

// TokenParser validates access tokens presented on the WS handshake.
type TokenParser interface {
	ParseAccessToken(tokenStr string) (*auth.AccessClaims, error)
}

// TokenAuthenticator implements Authenticator using JWT access tokens.
//
// Browsers cannot set an Authorization header on a WebSocket handshake, so an
// `access_token` query parameter is accepted as well. That is safe here
// because the connection is TLS-terminated in production and access tokens
// are short-lived; still, prefer the header where the client allows it.
type TokenAuthenticator struct {
	Tokens TokenParser
}

// AuthenticateRequest validates the request's access token.
func (a TokenAuthenticator) AuthenticateRequest(r *http.Request) (uuid.UUID, error) {
	raw := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		raw = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if raw == "" {
		raw = r.URL.Query().Get("access_token")
	}
	if raw == "" {
		return uuid.Nil, errors.New("missing access token")
	}

	claims, err := a.Tokens.ParseAccessToken(raw)
	if err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(claims.UserID)
}
