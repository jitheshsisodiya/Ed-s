package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// DeviceService implements device registration, listing, heartbeat and peer discovery.
type DeviceService struct {
	devices  domain.DeviceRepository
	networks domain.NetworkRepository
	members  domain.NetworkMemberRepository
	presence domain.PresenceStore
	bus      domain.PeerEventBus
	audit    *AuditRecorder
}

// NewDeviceService builds a DeviceService.
func NewDeviceService(
	devices domain.DeviceRepository,
	networks domain.NetworkRepository,
	members domain.NetworkMemberRepository,
	presence domain.PresenceStore,
	bus domain.PeerEventBus,
	audit *AuditRecorder,
) *DeviceService {
	return &DeviceService{devices: devices, networks: networks, members: members, presence: presence, bus: bus, audit: audit}
}

// Register creates a new device on a network for the caller, assigning the
// next free virtual IP inside the network's CIDR. The caller must already
// be a member of the network.
func (s *DeviceService) Register(ctx context.Context, userID, networkID uuid.UUID, name, os, osVersion, publicKey string) (*domain.Device, error) {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(publicKey) == "" {
		return nil, domain.ErrInvalidInput
	}
	if _, err := s.members.Get(ctx, networkID, userID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrForbidden
		}
		return nil, err
	}

	n, err := s.networks.GetByID(ctx, networkID)
	if err != nil {
		return nil, err
	}

	if existing, err := s.devices.GetByPublicKey(ctx, publicKey); err == nil && existing != nil {
		return nil, domain.ErrAlreadyExists
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	ip, err := s.devices.NextFreeVirtualIP(ctx, networkID, n.CIDR)
	if err != nil {
		return nil, err
	}

	d := &domain.Device{
		ID:        uuid.New(),
		UserID:    userID,
		NetworkID: networkID,
		Name:      name,
		OS:        domain.NormalizeDeviceOS(os),
		OSVersion: osVersion,
		PublicKey: publicKey,
		VirtualIP: ip,
		Status:    domain.DeviceStatusUnknown,
	}
	if err := s.devices.Create(ctx, d); err != nil {
		return nil, err
	}

	s.audit.Record(ctx, &userID, &networkID, domain.AuditDeviceRegistered, "device", d.ID.String(), "", map[string]any{"virtual_ip": ip})
	return d, nil
}

// ListForNetwork returns every device on a network the caller belongs to,
// with presence-store status merged in where fresher than the DB row.
func (s *DeviceService) ListForNetwork(ctx context.Context, networkID, userID uuid.UUID) ([]*domain.Device, error) {
	if _, err := s.members.Get(ctx, networkID, userID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrForbidden
		}
		return nil, err
	}
	devices, err := s.devices.ListByNetwork(ctx, networkID)
	if err != nil {
		return nil, err
	}
	s.mergePresence(ctx, devices)
	return devices, nil
}

func (s *DeviceService) mergePresence(ctx context.Context, devices []*domain.Device) {
	if s.presence == nil {
		return
	}
	for _, d := range devices {
		if ep, err := s.presence.Get(ctx, d.ID); err == nil && ep != nil {
			d.Status = domain.DeviceStatusOnline
			if ep.PublicIP != "" {
				pub := ep.PublicIP
				d.LastPublicIP = &pub
			}
			if ep.NATType != "" {
				nt := ep.NATType
				d.NATType = &nt
			}
		}
	}
}

// authorizeDeviceAccess returns the device if the caller is either the
// device's own user, or an owner/admin of the device's network.
func (s *DeviceService) authorizeDeviceAccess(ctx context.Context, deviceID, userID uuid.UUID) (*domain.Device, error) {
	d, err := s.devices.GetByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if d.UserID == userID {
		return d, nil
	}
	member, err := s.members.Get(ctx, d.NetworkID, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, domain.ErrForbidden
		}
		return nil, err
	}
	if member.Role != domain.NetworkRoleOwner && member.Role != domain.NetworkRoleAdmin {
		return nil, domain.ErrForbidden
	}
	return d, nil
}

// Get returns a device if the caller may access it.
func (s *DeviceService) Get(ctx context.Context, deviceID, userID uuid.UUID) (*domain.Device, error) {
	d, err := s.authorizeDeviceAccess(ctx, deviceID, userID)
	if err != nil {
		return nil, err
	}
	s.mergePresence(ctx, []*domain.Device{d})
	return d, nil
}

// Delete removes a device if the caller may access it.
func (s *DeviceService) Delete(ctx context.Context, deviceID, userID uuid.UUID) error {
	d, err := s.authorizeDeviceAccess(ctx, deviceID, userID)
	if err != nil {
		return err
	}
	if err := s.devices.Delete(ctx, deviceID); err != nil {
		return err
	}
	if s.presence != nil {
		_ = s.presence.Remove(ctx, d.NetworkID, d.ID)
	}
	if s.bus != nil {
		_ = s.bus.Publish(ctx, domain.PeerEvent{Type: domain.PeerEventLeft, NetworkID: d.NetworkID, DeviceID: d.ID})
	}
	s.audit.Record(ctx, &userID, &d.NetworkID, domain.AuditDeviceRemoved, "device", d.ID.String(), "", nil)
	return nil
}

// Heartbeat updates a device's liveness/status/counters via the lightweight
// REST path (the gRPC Heartbeat RPC is primary for real clients).
func (s *DeviceService) Heartbeat(ctx context.Context, deviceID, userID uuid.UUID, publicIP, privateIP, natType *string, bytesSent, bytesReceived int64) error {
	d, err := s.devices.GetByID(ctx, deviceID)
	if err != nil {
		return err
	}
	if d.UserID != userID {
		return domain.ErrForbidden
	}
	if err := s.devices.UpdateHeartbeat(ctx, deviceID, domain.DeviceStatusOnline, publicIP, privateIP, natType, bytesSent, bytesReceived); err != nil {
		return err
	}
	if s.presence != nil {
		ep := domain.PresenceEndpoint{UpdatedAt: time.Now()}
		if publicIP != nil {
			ep.PublicIP = *publicIP
		}
		if privateIP != nil {
			ep.PrivateIP = *privateIP
		}
		if natType != nil {
			ep.NATType = *natType
		}
		_ = s.presence.SetOnline(ctx, d.NetworkID, deviceID, ep, 45*time.Second)
	}
	if s.bus != nil {
		_ = s.bus.Publish(ctx, domain.PeerEvent{Type: domain.PeerEventStatusChanged, NetworkID: d.NetworkID, DeviceID: deviceID})
	}
	return nil
}

// ListPeers returns other devices on the same networks as deviceID (all
// networks the device's user is a member of that this device also belongs
// to — in the current data model a device belongs to exactly one network),
// annotated with online status/endpoint from the presence store.
func (s *DeviceService) ListPeers(ctx context.Context, deviceID, userID uuid.UUID) ([]*domain.Device, error) {
	d, err := s.authorizeDeviceAccess(ctx, deviceID, userID)
	if err != nil {
		return nil, err
	}
	peers, err := s.devices.ListPeers(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	s.mergePresence(ctx, peers)
	return peers, nil
}
