package http

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// LogsHandler serves /logs/* and /dashboard/* endpoints.
type LogsHandler struct {
	logs      *usecase.LogsService
	dashboard *usecase.DashboardService
}

// NewLogsHandler builds a LogsHandler.
func NewLogsHandler(logs *usecase.LogsService, dashboard *usecase.DashboardService) *LogsHandler {
	return &LogsHandler{logs: logs, dashboard: dashboard}
}

const (
	defaultPageLimit = 50
	maxPageLimit     = 500
)

// pagination reads limit/offset query params with sane bounds.
func pagination(r *http.Request) (limit, offset int) {
	limit = defaultPageLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = min(n, maxPageLimit)
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}

// optionalNetworkID parses the optional networkId query filter.
func optionalNetworkID(w http.ResponseWriter, r *http.Request) (*uuid.UUID, bool) {
	raw := r.URL.Query().Get("networkId")
	if raw == "" {
		return nil, true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_input", "malformed networkId")
		return nil, false
	}
	return &id, true
}

func (h *LogsHandler) auditLogs(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := optionalNetworkID(w, r)
	if !ok {
		return
	}
	limit, offset := pagination(r)

	logs, err := h.logs.AuditLogs(r.Context(), userID, networkID, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAuditLogDTOs(logs))
}

func (h *LogsHandler) connectionLogs(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUser(w, r)
	if !ok {
		return
	}
	networkID, ok := optionalNetworkID(w, r)
	if !ok {
		return
	}
	limit, offset := pagination(r)

	logs, err := h.logs.ConnectionLogs(r.Context(), userID, networkID, limit, offset)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toConnectionLogDTOs(logs))
}

func (h *LogsHandler) dashboardStats(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok {
		return
	}

	stats, err := h.dashboard.Stats(r.Context())
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDashboardStatsDTO(stats))
}
