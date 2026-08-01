package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// PairingHandler enrols a device from a code shown on one that is already
// signed in.
type PairingHandler struct {
	pairing *usecase.PairingService
}

// NewPairingHandler builds a PairingHandler.
func NewPairingHandler(p *usecase.PairingService) *PairingHandler {
	return &PairingHandler{pairing: p}
}

type startPairingRequest struct {
	// NetworkID is optional: with it, the paired device knows which network
	// to connect to and needs no further choice; without it, it is signed in
	// and left to pick.
	NetworkID string `json:"networkId,omitempty"`
}

type pairingDTO struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
	ExpiresIn int       `json:"expiresIn"`
}

// start issues a pairing code. Authenticated: making one of these requires
// already being signed in somewhere.
func (h *PairingHandler) start(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req startPairingRequest
	// An empty body is a valid request for a code with no network attached,
	// so a decode failure is only an error when something was actually sent.
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
			return
		}
	}

	p, err := h.pairing.Start(r.Context(), userID, strings.TrimSpace(req.NetworkID))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pairingDTO{
		Token:     p.Token,
		ExpiresAt: p.ExpiresAt,
		ExpiresIn: p.ExpiresIn,
	})
}

type claimPairingRequest struct {
	Token string `json:"token"`
}

type claimedPairingDTO struct {
	tokenPairDTO
	NetworkID string `json:"networkId,omitempty"`
	Email     string `json:"email"`
}

// claim spends a pairing code and returns a session. Public by necessity —
// the device calling it has no credentials yet, which is the entire point —
// and safe because the code is single use, short lived, and only exists
// because somebody already signed in asked for it.
func (h *PairingHandler) claim(w http.ResponseWriter, r *http.Request) {
	var req claimPairingRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	req.Token = strings.TrimSpace(req.Token)
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "token is required")
		return
	}

	claimed, err := h.pairing.Claim(r.Context(), req.Token, clientIP(r), r.UserAgent())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, claimedPairingDTO{
		tokenPairDTO: toTokenPairDTO(claimed.Auth),
		NetworkID:    claimed.NetworkID,
		Email:        claimed.Email,
	})
}
