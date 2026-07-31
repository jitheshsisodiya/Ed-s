package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// DeviceStatus mirrors the Postgres device_status enum.
type DeviceStatus string

const (
	DeviceStatusOnline  DeviceStatus = "online"
	DeviceStatusOffline DeviceStatus = "offline"
	DeviceStatusUnknown DeviceStatus = "unknown"
)

// DeviceOS mirrors the Postgres device_os enum.
type DeviceOS string

const (
	DeviceOSWindows       DeviceOS = "windows"
	DeviceOSWindowsServer DeviceOS = "windows_server"
	DeviceOSLinux         DeviceOS = "linux"
	DeviceOSMacOS         DeviceOS = "macos"
	DeviceOSAndroid       DeviceOS = "android"
	DeviceOSIOS           DeviceOS = "ios"
	DeviceOSUnknown       DeviceOS = "unknown"
)

// NormalizeDeviceOS maps an arbitrary client-supplied OS string onto the
// closest known device_os enum value, defaulting to "unknown".
func NormalizeDeviceOS(s string) DeviceOS {
	switch DeviceOS(s) {
	case DeviceOSWindows, DeviceOSWindowsServer, DeviceOSLinux, DeviceOSMacOS, DeviceOSAndroid, DeviceOSIOS:
		return DeviceOS(s)
	default:
		return DeviceOSUnknown
	}
}

// Device is a registered endpoint (client agent) belonging to a user on a network.
type Device struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	NetworkID       uuid.UUID
	Name            string
	OS              DeviceOS
	OSVersion       string
	PublicKey       string
	VirtualIP       string
	LastPublicIP    *string
	LastPrivateIP   *string
	NATType         *string
	Status          DeviceStatus
	LastSeenAt      *time.Time
	LastHandshakeAt *time.Time
	BytesSent       int64
	BytesReceived   int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// DeviceRepository persists Device aggregates.
type DeviceRepository interface {
	Create(ctx context.Context, d *Device) error
	GetByID(ctx context.Context, id uuid.UUID) (*Device, error)
	GetByPublicKey(ctx context.Context, publicKey string) (*Device, error)
	GetByNetworkAndUser(ctx context.Context, networkID, userID uuid.UUID) (*Device, error)
	ListByNetwork(ctx context.Context, networkID uuid.UUID) ([]*Device, error)
	ListByUser(ctx context.Context, userID uuid.UUID) ([]*Device, error)
	ListPeers(ctx context.Context, deviceID uuid.UUID) ([]*Device, error)
	Update(ctx context.Context, d *Device) error
	UpdateHeartbeat(ctx context.Context, id uuid.UUID, status DeviceStatus, lastPublicIP, lastPrivateIP, natType *string, bytesSent, bytesReceived int64) error
	Delete(ctx context.Context, id uuid.UUID) error
	// NextFreeVirtualIP returns the lowest unused host address inside cidr
	// that is not already assigned to a device on networkID.
	NextFreeVirtualIP(ctx context.Context, networkID uuid.UUID, cidr string) (string, error)
	// CountDistinctActiveUsers returns the number of distinct users with at
	// least one device seen since the given time.
	CountDistinctActiveUsers(ctx context.Context, since time.Time) (int, error)
	// CountDistinctActiveNetworks returns the number of distinct networks
	// with at least one device seen since the given time.
	CountDistinctActiveNetworks(ctx context.Context, since time.Time) (int, error)
	CountTotal(ctx context.Context) (int, error)
}

// RelayServer is a relay/TURN-like node the control plane can allocate.
type RelayServer struct {
	ID              uuid.UUID
	Region          string
	Hostname        string
	PublicKey       string
	ControlPort     int
	RelayPort       int
	Capacity        int
	CurrentLoad     int
	Status          string
	LastHeartbeatAt *time.Time
	CreatedAt       time.Time
}

// RelayServerRepository persists RelayServer rows.
type RelayServerRepository interface {
	Create(ctx context.Context, r *RelayServer) error
	// Upsert registers a relay node idempotently, keyed on its public
	// (hostname, relay_port) endpoint, so a restarting relay reclaims its
	// existing row instead of creating a duplicate.
	Upsert(ctx context.Context, r *RelayServer) error
	GetByID(ctx context.Context, id uuid.UUID) (*RelayServer, error)
	ListActive(ctx context.Context) ([]*RelayServer, error)
	PickLeastLoaded(ctx context.Context, preferredRegion string) (*RelayServer, error)
	IncrementLoad(ctx context.Context, id uuid.UUID, delta int) error
	Heartbeat(ctx context.Context, id uuid.UUID, currentLoad int) error
}

// ConnectionEventType mirrors the Postgres connection_event_type enum.
type ConnectionEventType string

const (
	ConnEventConnect            ConnectionEventType = "connect"
	ConnEventDisconnect         ConnectionEventType = "disconnect"
	ConnEventP2PEstablished     ConnectionEventType = "p2p_established"
	ConnEventRelayFallback      ConnectionEventType = "relay_fallback"
	ConnEventNATTraversalFailed ConnectionEventType = "nat_traversal_failed"
	ConnEventReconnect          ConnectionEventType = "reconnect"
)

// ConnectionLog is an immutable record of a connectivity event.
type ConnectionLog struct {
	ID            uuid.UUID
	NetworkID     uuid.UUID
	DeviceID      uuid.UUID
	PeerDeviceID  *uuid.UUID
	RelayServerID *uuid.UUID
	EventType     ConnectionEventType
	LatencyMs     *int
	BytesSent     int64
	BytesReceived int64
	Metadata      map[string]any
	CreatedAt     time.Time
}

// ConnectionLogRepository persists ConnectionLog rows.
type ConnectionLogRepository interface {
	Create(ctx context.Context, l *ConnectionLog) error
	ListByNetwork(ctx context.Context, networkID uuid.UUID, limit, offset int) ([]*ConnectionLog, error)
	List(ctx context.Context, limit, offset int) ([]*ConnectionLog, error)
	SumBytesByEventType(ctx context.Context, eventType ConnectionEventType, since time.Time) (int64, error)
	CountByEventType(ctx context.Context, since time.Time) (map[ConnectionEventType]int64, error)
}

// AuditAction mirrors the Postgres audit_action enum.
type AuditAction string

const (
	AuditUserRegister         AuditAction = "user.register"
	AuditUserLogin            AuditAction = "user.login"
	AuditUserLoginFailed      AuditAction = "user.login_failed"
	AuditUserPasswordReset    AuditAction = "user.password_reset"
	AuditUserMFAEnabled       AuditAction = "user.mfa_enabled"
	AuditUserMFADisabled      AuditAction = "user.mfa_disabled"
	AuditNetworkCreate        AuditAction = "network.create"
	AuditNetworkUpdate        AuditAction = "network.update"
	AuditNetworkDelete        AuditAction = "network.delete"
	AuditNetworkInviteCreated AuditAction = "network.invite_created"
	AuditNetworkInviteRotated AuditAction = "network.invite_rotated"
	AuditNetworkMemberJoined  AuditAction = "network.member_joined"
	AuditNetworkMemberRemoved AuditAction = "network.member_removed"
	AuditNetworkMemberRoleChg AuditAction = "network.member_role_changed"
	AuditDeviceRegistered     AuditAction = "device.registered"
	AuditDeviceRemoved        AuditAction = "device.removed"
	AuditDeviceKeyRotated     AuditAction = "device.key_rotated"
)

// AuditLog is an immutable record of a mutating action.
type AuditLog struct {
	ID          uuid.UUID
	ActorUserID *uuid.UUID
	NetworkID   *uuid.UUID
	Action      AuditAction
	TargetType  *string
	TargetID    *string
	IPAddress   *string
	Metadata    map[string]any
	CreatedAt   time.Time
}

// AuditLogRepository persists AuditLog rows.
type AuditLogRepository interface {
	Create(ctx context.Context, l *AuditLog) error
	ListByNetwork(ctx context.Context, networkID uuid.UUID, limit, offset int) ([]*AuditLog, error)
	List(ctx context.Context, limit, offset int) ([]*AuditLog, error)
}

// ErrorLog is a record written for level>=error application log entries.
type ErrorLog struct {
	ID        uuid.UUID
	Service   string
	Level     string
	Message   string
	Metadata  map[string]any
	CreatedAt time.Time
}

// ErrorLogRepository persists ErrorLog rows.
type ErrorLogRepository interface {
	Create(ctx context.Context, l *ErrorLog) error
}
