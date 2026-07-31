package http

import (
	"net/http"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// DeviceHandler serves the /devices/* endpoints from api/openapi.yaml.
type DeviceHandler struct {
	devices *usecase.DeviceService
}

// NewDeviceHandler builds a DeviceHandler.
func NewDeviceHandler(devices *usecase.DeviceService) *DeviceHandler {
	return &DeviceHandler{devices: devices}
}

func (h *DeviceHandler) get(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	deviceID, ok := pathUUID(w, r, "deviceId")
	if !ok {
		return
	}

	d, err := h.devices.Get(r.Context(), deviceID, userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDeviceDTO(d))
}

func (h *DeviceHandler) delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	deviceID, ok := pathUUID(w, r, "deviceId")
	if !ok {
		return
	}

	if err := h.devices.Delete(r.Context(), deviceID, userID); err != nil {
		writeDomainError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// heartbeat serves POST /devices/{deviceId}/heartbeat. This is the lightweight
// REST equivalent of the gRPC CoordinationService.Heartbeat RPC, for clients
// that don't maintain a gRPC connection.
func (h *DeviceHandler) heartbeat(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	deviceID, ok := pathUUID(w, r, "deviceId")
	if !ok {
		return
	}

	var req heartbeatRequest
	// The body is optional: a bare heartbeat still refreshes presence.
	_ = decodeJSON(r, &req)

	// If the client didn't report a public IP, fall back to the source address
	// the request actually arrived from.
	if req.PublicIP == nil {
		ip := clientIP(r)
		if ip != "" {
			req.PublicIP = &ip
		}
	}

	if err := h.devices.Heartbeat(r.Context(), deviceID, userID, req.PublicIP, req.PrivateIP, req.NATType, req.BytesSent, req.BytesReceived); err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *DeviceHandler) listPeers(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	deviceID, ok := pathUUID(w, r, "deviceId")
	if !ok {
		return
	}

	peers, err := h.devices.ListPeers(r.Context(), deviceID, userID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDeviceDTOs(peers))
}
