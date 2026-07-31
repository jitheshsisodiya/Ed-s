package http

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// RelayHandler serves the internal endpoints relay nodes use to register
// themselves and report load. These are NOT part of the public OpenAPI
// surface: they are authenticated by the shared relay secret (the same
// secret that signs relay session tokens), presented as a bearer token.
//
// The relay fleet stays stateless and holds no database credentials — the
// control plane is the only writer of relay_servers rows.
type RelayHandler struct {
	relays domain.RelayServerRepository
	secret string
}

// NewRelayHandler builds a RelayHandler.
func NewRelayHandler(relays domain.RelayServerRepository, secret string) *RelayHandler {
	return &RelayHandler{relays: relays, secret: secret}
}

// authorize checks the shared-secret bearer token in constant time.
func (h *RelayHandler) authorize(r *http.Request) bool {
	if h.secret == "" {
		return false
	}
	raw := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	return subtle.ConstantTimeCompare([]byte(raw), []byte(h.secret)) == 1
}

type relayRegisterRequest struct {
	Region      string `json:"region"`
	Hostname    string `json:"hostname"`
	PublicKey   string `json:"publicKey"`
	ControlPort int    `json:"controlPort"`
	RelayPort   int    `json:"relayPort"`
	Capacity    int    `json:"capacity"`
}

// register serves POST /internal/relay/register: idempotent upsert keyed on
// (hostname, relayPort), returning the relay's assigned ID.
func (h *RelayHandler) register(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid relay secret")
		return
	}

	var req relayRegisterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	if req.Hostname == "" || req.RelayPort <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_input", "hostname and relayPort are required")
		return
	}
	if req.Capacity <= 0 {
		req.Capacity = 1000
	}
	if req.ControlPort <= 0 {
		req.ControlPort = 3478
	}

	relay := &domain.RelayServer{
		Region:      req.Region,
		Hostname:    req.Hostname,
		PublicKey:   req.PublicKey,
		ControlPort: req.ControlPort,
		RelayPort:   req.RelayPort,
		Capacity:    req.Capacity,
		Status:      "active",
	}
	if err := h.relays.Upsert(r.Context(), relay); err != nil {
		writeDomainError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"relayId": relay.ID.String()})
}

type relayHeartbeatRequest struct {
	RelayID     string `json:"relayId"`
	CurrentLoad int    `json:"currentLoad"`
}

// heartbeat serves POST /internal/relay/heartbeat: refreshes liveness and
// reconciles current_load with the relay's actual active-session count.
func (h *RelayHandler) heartbeat(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid relay secret")
		return
	}

	var req relayHeartbeatRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	relayID, err := uuid.Parse(req.RelayID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed relayId")
		return
	}
	if req.CurrentLoad < 0 {
		req.CurrentLoad = 0
	}

	if err := h.relays.Heartbeat(r.Context(), relayID, req.CurrentLoad); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
