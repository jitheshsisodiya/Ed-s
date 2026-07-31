package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// PresenceEndpoint is the last-known network endpoint of a device, mirrored
// into the fast ephemeral presence store (Redis) so peer lookups don't need
// to hit Postgres on every heartbeat.
type PresenceEndpoint struct {
	PublicIP    string
	PublicPort  uint32
	PrivateIP   string
	PrivatePort uint32
	NATType     string
	UpdatedAt   time.Time
}

// PresenceStore tracks which devices are currently online, backed by an
// expiring key so a crashed client is automatically reaped.
type PresenceStore interface {
	// SetOnline marks deviceID online with the given endpoint, refreshing its
	// TTL. Also adds deviceID to networkID's online-device set.
	SetOnline(ctx context.Context, networkID, deviceID uuid.UUID, ep PresenceEndpoint, ttl time.Duration) error
	// Remove marks a device offline immediately (e.g. explicit disconnect).
	Remove(ctx context.Context, networkID, deviceID uuid.UUID) error
	// Get returns the last known presence endpoint for a device, or nil if offline.
	Get(ctx context.Context, deviceID uuid.UUID) (*PresenceEndpoint, error)
	// OnlineInNetwork returns the set of online device IDs for a network.
	OnlineInNetwork(ctx context.Context, networkID uuid.UUID) ([]uuid.UUID, error)
	// CountOnline returns the total number of online devices across all networks.
	CountOnline(ctx context.Context) (int, error)
}

// PeerEventType mirrors the gRPC PeerUpdateType enum.
type PeerEventType string

const (
	PeerEventJoined          PeerEventType = "joined"
	PeerEventLeft            PeerEventType = "left"
	PeerEventEndpointChanged PeerEventType = "endpoint_changed"
	PeerEventStatusChanged   PeerEventType = "status_changed"
)

// PeerEvent is a topology change broadcast to everyone watching a network.
type PeerEvent struct {
	Type      PeerEventType
	NetworkID uuid.UUID
	DeviceID  uuid.UUID
}

// PeerEventSubscription is a live subscription to PeerEvents for one or more networks.
type PeerEventSubscription interface {
	// Events yields PeerEvents until the subscription's context is done.
	Events() <-chan PeerEvent
	Close() error
}

// PeerEventBus publishes and subscribes to peer topology change events,
// backed by Redis pubsub so it fans out across backend replicas.
type PeerEventBus interface {
	Publish(ctx context.Context, event PeerEvent) error
	Subscribe(ctx context.Context, networkIDs []uuid.UUID) (PeerEventSubscription, error)
}

// ICECandidate mirrors the gRPC ICECandidate message.
type ICECandidate struct {
	IP       string
	Port     uint32
	Protocol string
	Type     string
	Priority uint32
}

// ICEStore is a short-lived rendezvous point for ICE candidate exchange
// between two devices negotiating a direct connection.
type ICEStore interface {
	// Put stores the candidates fromDevice sent, addressed to toDevice.
	Put(ctx context.Context, fromDevice, toDevice uuid.UUID, candidates []ICECandidate, ttl time.Duration) error
	// Get retrieves candidates forDevice previously received from fromDevice, if any.
	Get(ctx context.Context, forDevice, fromDevice uuid.UUID) ([]ICECandidate, error)
}

// RateLimiter provides a simple fixed-window rate limit, used to slow down
// brute-force login attempts.
type RateLimiter interface {
	// Allow reports whether the action identified by key is permitted, given
	// at most limit occurrences per window. It increments the counter as a
	// side effect.
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}
