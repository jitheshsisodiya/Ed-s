package http

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// AuthHandler serves the /auth/* endpoints from api/openapi.yaml.
type AuthHandler struct {
	auth   *usecase.AuthService
	logger *zap.Logger
	// devExposeResetToken, when true, returns the raw password-reset token in
	// the forgot-password response. This is intended for local development
	// and integration tests where no SMTP server is configured; it must stay
	// false in production, where the token is delivered by email only.
	devExposeResetToken bool
}

// NewAuthHandler builds an AuthHandler.
func NewAuthHandler(auth *usecase.AuthService, logger *zap.Logger, devExposeResetToken bool) *AuthHandler {
	return &AuthHandler{auth: auth, logger: logger, devExposeResetToken: devExposeResetToken}
}

func (h *AuthHandler) register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		writeError(w, http.StatusBadRequest, "invalid_input", "a valid email is required")
		return
	}
	if len(req.Password) < 12 {
		writeError(w, http.StatusBadRequest, "invalid_input", "password must be at least 12 characters")
		return
	}
	if strings.TrimSpace(req.DisplayName) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "displayName is required")
		return
	}

	u, err := h.auth.Register(r.Context(), req.Email, req.Password, req.DisplayName, clientIP(r))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toUserDTO(u))
}

func (h *AuthHandler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "email and password are required")
		return
	}

	result, err := h.auth.Login(r.Context(), req.Email, req.Password, req.MFACode, clientIP(r), r.UserAgent())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTokenPairDTO(result))
}

func (h *AuthHandler) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "refreshToken is required")
		return
	}

	result, err := h.auth.Refresh(r.Context(), req.RefreshToken, clientIP(r), r.UserAgent())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTokenPairDTO(result))
}

func (h *AuthHandler) logout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	// A body is optional here: clients that only hold an access token can
	// still call logout, it simply has nothing server-side to revoke.
	_ = decodeJSON(r, &req)

	if err := h.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AuthHandler) enableMFA(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	secret, otpauthURL, err := h.auth.EnableMFA(r.Context(), userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"secret":     secret,
		"otpauthUrl": otpauthURL,
	})
}

func (h *AuthHandler) verifyMFA(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req mfaVerifyRequest
	if err := decodeJSON(r, &req); err != nil || req.Code == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "code is required")
		return
	}

	if err := h.auth.VerifyMFA(r.Context(), userID, req.Code); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"mfaEnabled": true})
}

func (h *AuthHandler) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var req forgotPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}

	token, err := h.auth.ForgotPassword(r.Context(), strings.TrimSpace(strings.ToLower(req.Email)), clientIP(r))
	if err != nil {
		// Deliberately do not surface repository errors here — the response
		// must not reveal whether the account exists.
		h.logger.Error("forgot_password_failed", zap.Error(err))
	}

	// Always 202, whether or not the account exists, so this endpoint cannot
	// be used to enumerate registered email addresses.
	resp := map[string]string{"status": "accepted"}
	if h.devExposeResetToken && token != "" {
		resp["resetToken"] = token
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (h *AuthHandler) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req resetPasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	if req.Token == "" || len(req.NewPassword) < 12 {
		writeError(w, http.StatusBadRequest, "invalid_input", "token and a newPassword of at least 12 characters are required")
		return
	}

	if err := h.auth.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_updated"})
}

// oauthState generates an anti-CSRF state parameter for the OAuth redirect.
func oauthState() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const oauthStateCookie = "nexusvpn_oauth_state"

func (h *AuthHandler) oauthRedirect(w http.ResponseWriter, r *http.Request) {
	if provider := r.PathValue("provider"); provider != "google" {
		writeError(w, http.StatusBadRequest, "invalid_input", "unsupported OAuth provider")
		return
	}

	state, err := oauthState()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not start OAuth flow")
		return
	}
	url, err := h.auth.GoogleAuthCodeURL(state)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "OAuth is not configured on this server")
		return
	}

	// Bind the state to the browser so the callback can verify it.
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookie,
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	http.Redirect(w, r, url, http.StatusFound)
}

func (h *AuthHandler) oauthCallback(w http.ResponseWriter, r *http.Request) {
	if provider := r.PathValue("provider"); provider != "google" {
		writeError(w, http.StatusBadRequest, "invalid_input", "unsupported OAuth provider")
		return
	}

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "code and state are required")
		return
	}

	cookie, err := r.Cookie(oauthStateCookie)
	if err != nil || cookie.Value == "" || cookie.Value != state {
		writeError(w, http.StatusBadRequest, "invalid_input", "OAuth state mismatch")
		return
	}
	// Clear the one-time state cookie.
	http.SetCookie(w, &http.Cookie{Name: oauthStateCookie, Value: "", Path: "/", MaxAge: -1})

	result, err := h.auth.GoogleCallback(r.Context(), code, clientIP(r), r.UserAgent())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toTokenPairDTO(result))
}
