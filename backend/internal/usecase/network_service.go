package usecase

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// inviteCodeAlphabet avoids ambiguous characters (0/O, 1/I/L).
var inviteCodeEncoding = base32.NewEncoding("ABCDEFGHJKMNPQRSTUVWXYZ23456789").WithPadding(base32.NoPadding)

func generateInviteCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return inviteCodeEncoding.EncodeToString(b), nil
}

// NetworkService implements network CRUD, invites and membership management.
type NetworkService struct {
	networks domain.NetworkRepository
	members  domain.NetworkMemberRepository
	users    domain.UserRepository
	audit    *AuditRecorder
}

// NewNetworkService builds a NetworkService.
func NewNetworkService(networks domain.NetworkRepository, members domain.NetworkMemberRepository, users domain.UserRepository, audit *AuditRecorder) *NetworkService {
	return &NetworkService{networks: networks, members: members, users: users, audit: audit}
}

// Create validates the CIDR, generates an invite code and creates the
// network with the caller as owner (and an implicit owner network_members row).
func (s *NetworkService) Create(ctx context.Context, ownerID uuid.UUID, name, description, cidr string, dnsServers []string) (*domain.Network, error) {
	if strings.TrimSpace(name) == "" {
		return nil, domain.ErrInvalidInput
	}
	if _, _, err := net.ParseCIDR(cidr); err != nil {
		return nil, domain.ErrInvalidInput
	}
	code, err := generateInviteCode()
	if err != nil {
		return nil, err
	}

	n := &domain.Network{
		ID:             uuid.New(),
		Name:           name,
		Description:    description,
		CIDR:           cidr,
		DNSServers:     dnsServers,
		OwnerID:        ownerID,
		InviteCode:     code,
		InviteEnabled:  true,
		AllowBroadcast: true,
	}
	if err := s.networks.Create(ctx, n); err != nil {
		return nil, err
	}

	member := &domain.NetworkMember{
		ID:        uuid.New(),
		NetworkID: n.ID,
		UserID:    ownerID,
		Role:      domain.NetworkRoleOwner,
	}
	if err := s.members.Create(ctx, member); err != nil {
		return nil, err
	}

	n.CallerRole = domain.NetworkRoleOwner
	n.MemberCount = 1
	n.DeviceCount = 0

	s.audit.Record(ctx, &ownerID, &n.ID, domain.AuditNetworkCreate, "network", n.ID.String(), "", nil)
	return n, nil
}

// ListForUser returns every network the user belongs to, annotated with
// their role and member/device counts.
func (s *NetworkService) ListForUser(ctx context.Context, userID uuid.UUID) ([]*domain.Network, error) {
	networks, err := s.networks.ListForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, n := range networks {
		mc, err := s.networks.CountMembers(ctx, n.ID)
		if err == nil {
			n.MemberCount = mc
		}
		dc, err := s.networks.CountDevices(ctx, n.ID)
		if err == nil {
			n.DeviceCount = dc
		}
	}
	return networks, nil
}

// requireMembership fetches the caller's membership row or returns ErrForbidden.
func (s *NetworkService) requireMembership(ctx context.Context, networkID, userID uuid.UUID) (*domain.NetworkMember, error) {
	m, err := s.members.Get(ctx, networkID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrForbidden
		}
		return nil, err
	}
	return m, nil
}

func requireRole(role domain.NetworkRole, allowed ...domain.NetworkRole) error {
	for _, a := range allowed {
		if role == a {
			return nil
		}
	}
	return domain.ErrForbidden
}

// Get returns a network's details for a member, including their role.
func (s *NetworkService) Get(ctx context.Context, networkID, userID uuid.UUID) (*domain.Network, error) {
	member, err := s.requireMembership(ctx, networkID, userID)
	if err != nil {
		return nil, err
	}
	n, err := s.networks.GetByID(ctx, networkID)
	if err != nil {
		return nil, err
	}
	n.CallerRole = member.Role
	if mc, err := s.networks.CountMembers(ctx, networkID); err == nil {
		n.MemberCount = mc
	}
	if dc, err := s.networks.CountDevices(ctx, networkID); err == nil {
		n.DeviceCount = dc
	}
	return n, nil
}

// Update applies a partial update to a network. Only owner/admin may update.
func (s *NetworkService) Update(ctx context.Context, networkID, userID uuid.UUID, name, description, cidr *string, dnsServers []string) (*domain.Network, error) {
	member, err := s.requireMembership(ctx, networkID, userID)
	if err != nil {
		return nil, err
	}
	if err := requireRole(member.Role, domain.NetworkRoleOwner, domain.NetworkRoleAdmin); err != nil {
		return nil, err
	}
	n, err := s.networks.GetByID(ctx, networkID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		n.Name = *name
	}
	if description != nil {
		n.Description = *description
	}
	if cidr != nil {
		if _, _, err := net.ParseCIDR(*cidr); err != nil {
			return nil, domain.ErrInvalidInput
		}
		n.CIDR = *cidr
	}
	if dnsServers != nil {
		n.DNSServers = dnsServers
	}
	if err := s.networks.Update(ctx, n); err != nil {
		return nil, err
	}
	n.CallerRole = member.Role
	s.audit.Record(ctx, &userID, &n.ID, domain.AuditNetworkUpdate, "network", n.ID.String(), "", nil)
	return n, nil
}

// Delete removes a network. Only the owner may delete.
func (s *NetworkService) Delete(ctx context.Context, networkID, userID uuid.UUID) error {
	member, err := s.requireMembership(ctx, networkID, userID)
	if err != nil {
		return err
	}
	if err := requireRole(member.Role, domain.NetworkRoleOwner); err != nil {
		return err
	}
	if err := s.networks.Delete(ctx, networkID); err != nil {
		return err
	}
	s.audit.Record(ctx, &userID, &networkID, domain.AuditNetworkDelete, "network", networkID.String(), "", nil)
	return nil
}

// Join adds the caller as a member of the network identified by inviteCode.
func (s *NetworkService) Join(ctx context.Context, userID uuid.UUID, inviteCode string) (*domain.Network, error) {
	n, err := s.networks.GetByInviteCode(ctx, inviteCode)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrInviteInvalid
		}
		return nil, err
	}
	if !n.InviteEnabled {
		return nil, domain.ErrInviteInvalid
	}
	if n.InviteCodeExpiresAt != nil && time.Now().After(*n.InviteCodeExpiresAt) {
		return nil, domain.ErrInviteInvalid
	}

	if existing, err := s.members.Get(ctx, n.ID, userID); err == nil && existing != nil {
		existing.Role = existing.Role
		n.CallerRole = existing.Role
		return n, nil
	}

	member := &domain.NetworkMember{
		ID:        uuid.New(),
		NetworkID: n.ID,
		UserID:    userID,
		Role:      domain.NetworkRoleMember,
	}
	if err := s.members.Create(ctx, member); err != nil {
		return nil, err
	}
	n.CallerRole = domain.NetworkRoleMember
	if mc, err := s.networks.CountMembers(ctx, n.ID); err == nil {
		n.MemberCount = mc
	}
	if dc, err := s.networks.CountDevices(ctx, n.ID); err == nil {
		n.DeviceCount = dc
	}

	s.audit.Record(ctx, &userID, &n.ID, domain.AuditNetworkMemberJoined, "network_member", userID.String(), "", nil)
	return n, nil
}

// RotateInvite regenerates a network's invite code. Only owner/admin may do this.
func (s *NetworkService) RotateInvite(ctx context.Context, networkID, userID uuid.UUID) (code string, expiresAt *time.Time, err error) {
	member, err := s.requireMembership(ctx, networkID, userID)
	if err != nil {
		return "", nil, err
	}
	if err := requireRole(member.Role, domain.NetworkRoleOwner, domain.NetworkRoleAdmin); err != nil {
		return "", nil, err
	}
	newCode, err := generateInviteCode()
	if err != nil {
		return "", nil, err
	}
	if err := s.networks.RotateInviteCode(ctx, networkID, newCode, nil); err != nil {
		return "", nil, err
	}
	s.audit.Record(ctx, &userID, &networkID, domain.AuditNetworkInviteRotated, "network", networkID.String(), "", nil)
	return newCode, nil, nil
}

// ListMembers returns every member of a network. Caller must be a member.
func (s *NetworkService) ListMembers(ctx context.Context, networkID, userID uuid.UUID) ([]*domain.NetworkMember, error) {
	if _, err := s.requireMembership(ctx, networkID, userID); err != nil {
		return nil, err
	}
	return s.members.List(ctx, networkID)
}

// UpdateMemberRole changes a member's role. Only the owner may promote to
// admin or demote an admin; admins may not modify other admins' roles.
func (s *NetworkService) UpdateMemberRole(ctx context.Context, networkID, callerID, targetUserID uuid.UUID, newRole domain.NetworkRole) error {
	caller, err := s.requireMembership(ctx, networkID, callerID)
	if err != nil {
		return err
	}
	if newRole != domain.NetworkRoleAdmin && newRole != domain.NetworkRoleMember {
		return domain.ErrInvalidInput
	}
	target, err := s.members.Get(ctx, networkID, targetUserID)
	if err != nil {
		return err
	}
	if target.Role == domain.NetworkRoleOwner {
		return domain.ErrLastOwner
	}
	// Only the owner can grant/revoke admin. Admins may only manage members.
	if newRole == domain.NetworkRoleAdmin || target.Role == domain.NetworkRoleAdmin {
		if err := requireRole(caller.Role, domain.NetworkRoleOwner); err != nil {
			return err
		}
	} else if err := requireRole(caller.Role, domain.NetworkRoleOwner, domain.NetworkRoleAdmin); err != nil {
		return err
	}

	if err := s.members.UpdateRole(ctx, networkID, targetUserID, newRole); err != nil {
		return err
	}
	s.audit.Record(ctx, &callerID, &networkID, domain.AuditNetworkMemberRoleChg, "network_member", targetUserID.String(), "", map[string]any{"new_role": string(newRole)})
	return nil
}

// RemoveMember removes a member from a network. Owner cannot be removed;
// members can only remove themselves, admins can remove members, owner can
// remove anyone except themselves (they must delete/transfer the network instead).
func (s *NetworkService) RemoveMember(ctx context.Context, networkID, callerID, targetUserID uuid.UUID) error {
	caller, err := s.requireMembership(ctx, networkID, callerID)
	if err != nil {
		return err
	}
	target, err := s.members.Get(ctx, networkID, targetUserID)
	if err != nil {
		return err
	}
	if target.Role == domain.NetworkRoleOwner {
		return domain.ErrLastOwner
	}
	if callerID != targetUserID {
		if err := requireRole(caller.Role, domain.NetworkRoleOwner, domain.NetworkRoleAdmin); err != nil {
			return err
		}
		// Admins cannot remove other admins, only the owner can.
		if target.Role == domain.NetworkRoleAdmin && caller.Role != domain.NetworkRoleOwner {
			return domain.ErrForbidden
		}
	}
	if err := s.members.Delete(ctx, networkID, targetUserID); err != nil {
		return err
	}
	s.audit.Record(ctx, &callerID, &networkID, domain.AuditNetworkMemberRemoved, "network_member", targetUserID.String(), "", nil)
	return nil
}
