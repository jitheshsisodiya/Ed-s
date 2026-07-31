package http

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// NetworkHandler serves the /networks/* endpoints from api/openapi.yaml.
// Authorization (owner/admin/member) is enforced inside NetworkService, which
// re-checks membership and role for every operation.
type NetworkHandler struct {
	networks *usecase.NetworkService
	devices  *usecase.DeviceService
}

// NewNetworkHandler builds a NetworkHandler.
func NewNetworkHandler(networks *usecase.NetworkService, devices *usecase.DeviceService) *NetworkHandler {
	return &NetworkHandler{networks: networks, devices: devices}
}

// requireUser resolves the authenticated caller, writing a 401 if absent.
func requireUser(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return uuid.Nil, false
	}
	return userID, true
}

// pathUUID parses a UUID path parameter, writing a 400 if malformed.
func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := r.PathValue(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed "+name)
		return uuid.Nil, false
	}
	return id, true
}

func (h *NetworkHandler) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networks, err := h.networks.ListForUser(r.Context(), userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNetworkDTOs(networks))
}

func (h *NetworkHandler) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req networkCreateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "name is required")
		return
	}

	n, err := h.networks.Create(r.Context(), userID, req.Name, req.Description, req.CIDR, req.DNSServers)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toNetworkDTO(n))
}

func (h *NetworkHandler) join(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	var req joinNetworkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	if strings.TrimSpace(req.InviteCode) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "inviteCode is required")
		return
	}

	n, err := h.networks.Join(r.Context(), userID, strings.TrimSpace(req.InviteCode))
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNetworkDTO(n))
}

func (h *NetworkHandler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}

	n, err := h.networks.Get(r.Context(), networkID, userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNetworkDTO(n))
}

func (h *NetworkHandler) update(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}
	var req networkUpdateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}

	n, err := h.networks.Update(r.Context(), networkID, userID, req.Name, req.Description, req.CIDR, req.DNSServers)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toNetworkDTO(n))
}

func (h *NetworkHandler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}

	if err := h.networks.Delete(r.Context(), networkID, userID); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *NetworkHandler) rotateInvite(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}

	code, expiresAt, err := h.networks.RotateInvite(r.Context(), networkID, userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	resp := map[string]any{"inviteCode": code}
	if expiresAt != nil {
		resp["expiresAt"] = *expiresAt
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *NetworkHandler) listMembers(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}

	members, err := h.networks.ListMembers(r.Context(), networkID, userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMemberDTOs(members))
}

func (h *NetworkHandler) updateMemberRole(w http.ResponseWriter, r *http.Request) {
	callerID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}
	targetUserID, ok := pathUUID(w, r, "userId")
	if !ok {
		return
	}

	var req memberRoleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	role := domain.NetworkRole(req.Role)
	if role != domain.NetworkRoleAdmin && role != domain.NetworkRoleMember {
		writeError(w, http.StatusBadRequest, "invalid_input", "role must be 'admin' or 'member'")
		return
	}

	if err := h.networks.UpdateMemberRole(r.Context(), networkID, callerID, targetUserID, role); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"role": req.Role})
}

func (h *NetworkHandler) removeMember(w http.ResponseWriter, r *http.Request) {
	callerID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}
	targetUserID, ok := pathUUID(w, r, "userId")
	if !ok {
		return
	}

	if err := h.networks.RemoveMember(r.Context(), networkID, callerID, targetUserID); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// listDevices serves GET /networks/{networkId}/devices.
func (h *NetworkHandler) listDevices(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}

	devices, err := h.devices.ListForNetwork(r.Context(), networkID, userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDeviceDTOs(devices))
}

// registerDevice serves POST /networks/{networkId}/devices.
func (h *NetworkHandler) registerDevice(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := pathUUID(w, r, "networkId")
	if !ok {
		return
	}

	var req deviceRegisterRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed JSON body")
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.PublicKey) == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "name and publicKey are required")
		return
	}

	d, err := h.devices.Register(r.Context(), userID, networkID, req.Name, req.OS, req.OSVersion, req.PublicKey)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toDeviceDTO(d))
}
