// Package ws implements the WebSocket signaling endpoint mounted at
// /api/v1/ws. It streams live peer/presence topology changes to browser and
// lightweight clients that don't maintain a gRPC connection.
//
// Fanout is backed by the Redis-backed domain.PeerEventBus, so every backend
// replica delivers events for the networks its own connections care about —
// no sticky sessions or in-process shared state required.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/metrics"
)

// NetworkLister returns the network IDs a user belongs to.
type NetworkLister interface {
	ListNetworkIDsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
}

// DeviceLookup resolves a device so events can carry useful detail.
type DeviceLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Device, error)
}

// Authenticator validates the access token supplied on the WS handshake.
type Authenticator interface {
	// AuthenticateRequest returns the authenticated user's ID for a request,
	// or an error if the request is unauthenticated.
	AuthenticateRequest(r *http.Request) (uuid.UUID, error)
}

// Config wires the Hub's dependencies.
type Config struct {
	Bus           domain.PeerEventBus
	Presence      domain.PresenceStore
	Members       NetworkLister
	Devices       DeviceLookup
	Auth          Authenticator
	Logger        *zap.Logger
	// AllowedOrigins is passed to the websocket accept options. Empty means
	// same-origin only; include the admin panel's origin in development.
	AllowedOrigins []string
	// PingInterval controls the keepalive ping cadence.
	PingInterval time.Duration
}

// Hub serves WebSocket connections.
type Hub struct {
	cfg Config
}

// NewHub builds a Hub.
func NewHub(cfg Config) *Hub {
	if cfg.PingInterval <= 0 {
		cfg.PingInterval = 30 * time.Second
	}
	return &Hub{cfg: cfg}
}

// outboundEvent is the JSON envelope pushed to WebSocket clients.
type outboundEvent struct {
	Type      string     `json:"type"`
	NetworkID string     `json:"networkId"`
	DeviceID  string     `json:"deviceId"`
	Device    *deviceMsg `json:"device,omitempty"`
	Timestamp time.Time  `json:"timestamp"`
}

type deviceMsg struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	OS        string `json:"os"`
	VirtualIP string `json:"virtualIp"`
	Status    string `json:"status"`
	PublicIP  string `json:"publicIp,omitempty"`
	NATType   string `json:"natType,omitempty"`
}

// ServeHTTP upgrades the connection and streams peer events until the client
// disconnects or the request context is cancelled.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, err := h.cfg.Auth.AuthenticateRequest(r)
	if err != nil {
		http.Error(w, `{"error":{"code":"unauthorized","message":"invalid or missing access token"}}`, http.StatusUnauthorized)
		return
	}

	networkIDs, err := h.cfg.Members.ListNetworkIDsForUser(r.Context(), userID)
	if err != nil {
		h.cfg.Logger.Error("ws_list_networks_failed", zap.Error(err), zap.String("user_id", userID.String()))
		http.Error(w, `{"error":{"code":"internal_error","message":"could not resolve networks"}}`, http.StatusInternalServerError)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: h.cfg.AllowedOrigins,
	})
	if err != nil {
		h.cfg.Logger.Warn("ws_accept_failed", zap.Error(err))
		return
	}
	defer conn.CloseNow()

	metrics.ActiveWebSocketConnections.Inc()
	defer metrics.ActiveWebSocketConnections.Dec()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// A user with no networks still gets a (quiet) connection, so the client
	// doesn't have to special-case the empty state with a reconnect loop.
	sub, err := h.cfg.Bus.Subscribe(ctx, networkIDs)
	if err != nil {
		h.cfg.Logger.Error("ws_subscribe_failed", zap.Error(err))
		_ = conn.Close(websocket.StatusInternalError, "subscribe failed")
		return
	}
	defer sub.Close()

	// Detect client-side disconnects: any read error (including a close
	// frame) cancels the context and tears down the writer loop below.
	go func() {
		defer cancel()
		for {
			if _, _, err := conn.Read(ctx); err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(h.cfg.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case <-ticker.C:
			pingCtx, pingCancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pingCtx)
			pingCancel()
			if err != nil {
				return
			}

		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			msg := h.buildEvent(ctx, ev)
			payload, err := json.Marshal(msg)
			if err != nil {
				h.cfg.Logger.Error("ws_marshal_failed", zap.Error(err))
				continue
			}
			writeCtx, writeCancel := context.WithTimeout(ctx, 10*time.Second)
			err = conn.Write(writeCtx, websocket.MessageText, payload)
			writeCancel()
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					h.cfg.Logger.Debug("ws_write_failed", zap.Error(err))
				}
				return
			}
		}
	}
}

// buildEvent enriches a raw PeerEvent with device detail where available.
func (h *Hub) buildEvent(ctx context.Context, ev domain.PeerEvent) outboundEvent {
	out := outboundEvent{
		Type:      string(ev.Type),
		NetworkID: ev.NetworkID.String(),
		DeviceID:  ev.DeviceID.String(),
		Timestamp: time.Now().UTC(),
	}

	// A "left" event may refer to an already-deleted device, so a lookup
	// failure here is expected and non-fatal.
	d, err := h.cfg.Devices.GetByID(ctx, ev.DeviceID)
	if err != nil || d == nil {
		return out
	}

	dm := &deviceMsg{
		ID:        d.ID.String(),
		Name:      d.Name,
		OS:        string(d.OS),
		VirtualIP: d.VirtualIP,
		Status:    string(d.Status),
	}
	if d.LastPublicIP != nil {
		dm.PublicIP = *d.LastPublicIP
	}
	if d.NATType != nil {
		dm.NATType = *d.NATType
	}
	out.Device = dm
	return out
}
