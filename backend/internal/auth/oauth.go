package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// GoogleOAuthConfig wraps golang.org/x/oauth2's Google endpoint config.
type GoogleOAuthConfig struct {
	cfg *oauth2.Config
}

// NewGoogleOAuthConfig builds a GoogleOAuthConfig. Returns nil if the
// client ID/secret aren't configured (OAuth login simply won't be offered).
func NewGoogleOAuthConfig(clientID, clientSecret, redirectURL string) *GoogleOAuthConfig {
	if clientID == "" || clientSecret == "" {
		return nil
	}
	return &GoogleOAuthConfig{cfg: &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}}
}

// NewState generates a random CSRF-protection state parameter.
func NewState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// AuthCodeURL returns the URL to redirect the browser to for consent.
func (g *GoogleOAuthConfig) AuthCodeURL(state string) string {
	return g.cfg.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

// GoogleUserInfo is the subset of Google's userinfo response we care about.
type GoogleUserInfo struct {
	Sub           string `json:"sub"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// Exchange trades an authorization code for tokens and fetches the user's
// profile from Google's userinfo endpoint.
func (g *GoogleOAuthConfig) Exchange(ctx context.Context, code string) (*GoogleUserInfo, error) {
	tok, err := g.cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oauth exchange: %w", err)
	}
	client := g.cfg.Client(ctx, tok)
	resp, err := client.Get("https://www.googleapis.com/oauth2/v3/userinfo")
	if err != nil {
		return nil, fmt.Errorf("fetch userinfo: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("userinfo status %d: %s", resp.StatusCode, string(body))
	}
	var info GoogleUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	if info.Sub == "" || info.Email == "" {
		return nil, errors.New("incomplete userinfo response")
	}
	return &info, nil
}
