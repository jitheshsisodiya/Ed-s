package http

import (
	"net/http"

	"go.uber.org/zap"
)

// RouterConfig wires the handlers and cross-cutting concerns for the REST API.
type RouterConfig struct {
	Auth        *AuthHandler
	Networks    *NetworkHandler
	Devices     *DeviceHandler
	Logs        *LogsHandler
	Tokens      TokenParser
	Logger      *zap.Logger
	CORSOrigins []string
	// Relay, when non-nil, mounts the internal relay-fleet endpoints
	// (register/heartbeat), authenticated by the shared relay secret.
	Relay *RelayHandler
	// WSHandler, when non-nil, is mounted at /api/v1/ws for live presence and
	// peer updates. It performs its own authentication because browsers can't
	// set an Authorization header on a WebSocket handshake.
	WSHandler http.Handler
}

// NewRouter builds the complete REST API mux, applying authentication only to
// the routes that require it. Route patterns use Go 1.22+ method+wildcard
// syntax, which also gives us stable low-cardinality Prometheus labels.
func NewRouter(cfg RouterConfig) http.Handler {
	mux := http.NewServeMux()
	authed := Authenticate(cfg.Tokens)

	// Wraps a handler func with authentication.
	protect := func(h http.HandlerFunc) http.Handler { return authed(h) }

	const p = "/api/v1"

	// --- Public auth routes ---
	mux.HandleFunc("POST "+p+"/auth/register", cfg.Auth.register)
	mux.HandleFunc("POST "+p+"/auth/login", cfg.Auth.login)
	mux.HandleFunc("POST "+p+"/auth/refresh", cfg.Auth.refresh)
	mux.HandleFunc("POST "+p+"/auth/logout", cfg.Auth.logout)
	mux.HandleFunc("POST "+p+"/auth/password/forgot", cfg.Auth.forgotPassword)
	mux.HandleFunc("POST "+p+"/auth/password/reset", cfg.Auth.resetPassword)
	mux.HandleFunc("GET "+p+"/auth/oauth/{provider}/redirect", cfg.Auth.oauthRedirect)
	mux.HandleFunc("GET "+p+"/auth/oauth/{provider}/callback", cfg.Auth.oauthCallback)

	// --- Authenticated auth routes ---
	mux.Handle("POST "+p+"/auth/mfa/enable", protect(cfg.Auth.enableMFA))
	mux.Handle("POST "+p+"/auth/mfa/verify", protect(cfg.Auth.verifyMFA))

	// --- Networks ---
	mux.Handle("GET "+p+"/networks", protect(cfg.Networks.list))
	mux.Handle("POST "+p+"/networks", protect(cfg.Networks.create))
	mux.Handle("POST "+p+"/networks/join", protect(cfg.Networks.join))
	mux.Handle("GET "+p+"/networks/{networkId}", protect(cfg.Networks.get))
	mux.Handle("PATCH "+p+"/networks/{networkId}", protect(cfg.Networks.update))
	mux.Handle("DELETE "+p+"/networks/{networkId}", protect(cfg.Networks.delete))
	mux.Handle("POST "+p+"/networks/{networkId}/invite", protect(cfg.Networks.rotateInvite))
	mux.Handle("GET "+p+"/networks/{networkId}/members", protect(cfg.Networks.listMembers))
	mux.Handle("PATCH "+p+"/networks/{networkId}/members/{userId}", protect(cfg.Networks.updateMemberRole))
	mux.Handle("DELETE "+p+"/networks/{networkId}/members/{userId}", protect(cfg.Networks.removeMember))
	mux.Handle("GET "+p+"/networks/{networkId}/devices", protect(cfg.Networks.listDevices))
	mux.Handle("POST "+p+"/networks/{networkId}/devices", protect(cfg.Networks.registerDevice))

	// --- Devices ---
	mux.Handle("GET "+p+"/devices/{deviceId}", protect(cfg.Devices.get))
	mux.Handle("DELETE "+p+"/devices/{deviceId}", protect(cfg.Devices.delete))
	mux.Handle("POST "+p+"/devices/{deviceId}/heartbeat", protect(cfg.Devices.heartbeat))
	mux.Handle("GET "+p+"/devices/{deviceId}/peers", protect(cfg.Devices.listPeers))

	// --- Logs & dashboard ---
	mux.Handle("GET "+p+"/logs/audit", protect(cfg.Logs.auditLogs))
	mux.Handle("GET "+p+"/logs/connections", protect(cfg.Logs.connectionLogs))
	mux.Handle("GET "+p+"/dashboard/stats", protect(cfg.Logs.dashboardStats))

	// --- WebSocket signaling ---
	if cfg.WSHandler != nil {
		mux.Handle(p+"/ws", cfg.WSHandler)
	}

	// --- Internal relay-fleet endpoints (shared-secret auth, not JWT) ---
	if cfg.Relay != nil {
		mux.HandleFunc("POST /internal/relay/register", cfg.Relay.register)
		mux.HandleFunc("POST /internal/relay/heartbeat", cfg.Relay.heartbeat)
	}

	// --- Health ---
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
	})

	onPanic := func(rec any) {
		cfg.Logger.Error("panic_recovered", zap.Any("panic", rec))
	}

	return chain(mux,
		Recover(onPanic),
		CORS(cfg.CORSOrigins),
		Metrics(),
	)
}
