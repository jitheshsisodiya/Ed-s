package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// NetworkRole mirrors the Postgres network_role enum.
type NetworkRole string

const (
	NetworkRoleOwner  NetworkRole = "owner"
	NetworkRoleAdmin  NetworkRole = "admin"
	NetworkRoleMember NetworkRole = "member"
)

// Network is a virtual LAN / mesh network with a CIDR address space.
type Network struct {
	ID                  uuid.UUID
	Name                string
	Description         string
	CIDR                string
	CIDRv6              *string
	DNSServers          []string
	OwnerID             uuid.UUID
	InviteCode          string
	InviteCodeExpiresAt *time.Time
	InviteEnabled       bool
	AllowBroadcast      bool
	CreatedAt           time.Time
	UpdatedAt           time.Time

	// Populated by joined queries, not persisted directly on this table.
	MemberCount int
	DeviceCount int
	// CallerRole is the requesting user's role in this network, populated
	// by queries that join against network_members for "my networks" views.
	CallerRole NetworkRole
}

// NetworkMember links a user to a network with a role.
type NetworkMember struct {
	ID        uuid.UUID
	NetworkID uuid.UUID
	UserID    uuid.UUID
	Role      NetworkRole
	JoinedAt  time.Time

	// Populated by joined queries.
	Email       string
	DisplayName string
}

// NetworkRepository persists Network aggregates.
type NetworkRepository interface {
	Create(ctx context.Context, n *Network) error
	GetByID(ctx context.Context, id uuid.UUID) (*Network, error)
	GetByInviteCode(ctx context.Context, code string) (*Network, error)
	ListForUser(ctx context.Context, userID uuid.UUID) ([]*Network, error)
	Update(ctx context.Context, n *Network) error
	Delete(ctx context.Context, id uuid.UUID) error
	RotateInviteCode(ctx context.Context, id uuid.UUID, newCode string, expiresAt *time.Time) error
	CountMembers(ctx context.Context, networkID uuid.UUID) (int, error)
	CountDevices(ctx context.Context, networkID uuid.UUID) (int, error)
}

// NetworkMemberRepository persists network membership rows.
type NetworkMemberRepository interface {
	Create(ctx context.Context, m *NetworkMember) error
	Get(ctx context.Context, networkID, userID uuid.UUID) (*NetworkMember, error)
	List(ctx context.Context, networkID uuid.UUID) ([]*NetworkMember, error)
	ListNetworkIDsForUser(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	UpdateRole(ctx context.Context, networkID, userID uuid.UUID, role NetworkRole) error
	Delete(ctx context.Context, networkID, userID uuid.UUID) error
	CountByRole(ctx context.Context, networkID uuid.UUID, role NetworkRole) (int, error)
}
