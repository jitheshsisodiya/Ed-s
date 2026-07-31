package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// RelaySessionIssuer issues signed short-lived relay session tokens.
type RelaySessionIssuer interface {
	Issue(deviceID, relayID, networkID uuid.UUID) (string, error)
}

// RegisteredDevice is the result of RegisterDevice: the device record plus
// its current network's topology.
type RegisteredDevice struct {
	Device        *domain.Device
	NetworkCIDR   string
	DNSServers    []string
	ExistingPeers []*domain.Device
}

// RelayAllocation is the result of RequestRelay.
type RelayAllocation struct {
	Relay        *domain.RelayServer
	SessionToken string
}

// CoordinationService implements the business logic behind the gRPC
// CoordinationService: device registration, heartbeats, peer discovery,
// ICE candidate rendezvous and relay allocation.
type CoordinationService struct {
	devices     domain.DeviceRepository
	networks    domain.NetworkRepository
	members     domain.NetworkMemberRepository
	relays      domain.RelayServerRepository
	presence    domain.PresenceStore
	bus         domain.PeerEventBus
	ice         domain.ICEStore
	relayTok    RelaySessionIssuer
	audit       *AuditRecorder
	presenceTTL time.Duration
}

// NewCoordinationService builds a CoordinationService.
func NewCoordinationService(
	devices domain.DeviceRepository,
	networks domain.NetworkRepository,
	members domain.NetworkMemberRepository,
	relays domain.RelayServerRepository,
	presence domain.PresenceStore,
	bus domain.PeerEventBus,
	ice domain.ICEStore,
	relayTok RelaySessionIssuer,
	audit *AuditRecorder,
	presenceTTL time.Duration,
) *CoordinationService {
	if presenceTTL <= 0 {
		presenceTTL = 45 * time.Second
	}
	return &CoordinationService{
		devices: devices, networks: networks, members: members, relays: relays,
		presence: presence, bus: bus, ice: ice, relayTok: relayTok, audit: audit,
		presenceTTL: presenceTTL,
	}
}

// RegisterDevice authenticates userID as a member of networkID, then
// looks up (by public key) or creates the device, assigning the next free
// virtual IP. Returns the device plus the network's other known peers.
func (s *CoordinationService) RegisterDevice(ctx context.Context, userID, networkID uuid.UUID, publicKey, deviceName, os, osVersion, clientVersion string) (*RegisteredDevice, error) {
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

	d, err := s.devices.GetByPublicKey(ctx, publicKey)
	if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	if d != nil {
		if d.NetworkID != networkID || d.UserID != userID {
			return nil, domain.ErrForbidden
		}
		d.Name = deviceName
		d.OS = domain.NormalizeDeviceOS(os)
		d.OSVersion = osVersion
		if err := s.devices.Update(ctx, d); err != nil {
			return nil, err
		}
	} else {
		ip, err := s.devices.NextFreeVirtualIP(ctx, networkID, n.CIDR)
		if err != nil {
			return nil, err
		}
		d = &domain.Device{
			ID:        uuid.New(),
			UserID:    userID,
			NetworkID: networkID,
			Name:      deviceName,
			OS:        domain.NormalizeDeviceOS(os),
			OSVersion: osVersion,
			PublicKey: publicKey,
			VirtualIP: ip,
			Status:    domain.DeviceStatusUnknown,
		}
		if err := s.devices.Create(ctx, d); err != nil {
			return nil, err
		}
		s.audit.Record(ctx, &userID, &networkID, domain.AuditDeviceRegistered, "device", d.ID.String(), "", map[string]any{"virtual_ip": ip, "via": "grpc"})
	}

	peers, err := s.devices.ListPeers(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	for _, p := range peers {
		if ep, err := s.presence.Get(ctx, p.ID); err == nil && ep != nil {
			p.Status = domain.DeviceStatusOnline
			if ep.PublicIP != "" {
				ip := ep.PublicIP
				p.LastPublicIP = &ip
			}
			if ep.NATType != "" {
				nt := ep.NATType
				p.NATType = &nt
			}
		}
	}

	if s.bus != nil {
		_ = s.bus.Publish(ctx, domain.PeerEvent{Type: domain.PeerEventJoined, NetworkID: networkID, DeviceID: d.ID})
	}

	return &RegisteredDevice{
		Device:        d,
		NetworkCIDR:   n.CIDR,
		DNSServers:    n.DNSServers,
		ExistingPeers: peers,
	}, nil
}

// Heartbeat records a device's liveness, endpoint and traffic counters. It
// refreshes the Redis presence TTL immediately and persists counters/status
// to Postgres. Returns the number of seconds the client should wait before
// its next heartbeat.
func (s *CoordinationService) Heartbeat(ctx context.Context, deviceID uuid.UUID, publicEP, privateEP domain.PresenceEndpoint, natType string, bytesSent, bytesReceived uint64) (int32, error) {
	d, err := s.devices.GetByID(ctx, deviceID)
	if err != nil {
		return 0, err
	}

	if s.presence != nil {
		ep := publicEP
		ep.PrivateIP = privateEP.PrivateIP
		ep.PrivatePort = privateEP.PrivatePort
		ep.NATType = natType
		ep.UpdatedAt = time.Now()
		if err := s.presence.SetOnline(ctx, d.NetworkID, deviceID, ep, s.presenceTTL); err != nil {
			return 0, err
		}
	}

	var pubIP, privIP, nt *string
	if publicEP.PublicIP != "" {
		pubIP = &publicEP.PublicIP
	}
	if privateEP.PrivateIP != "" {
		privIP = &privateEP.PrivateIP
	}
	if natType != "" {
		nt = &natType
	}
	if err := s.devices.UpdateHeartbeat(ctx, deviceID, domain.DeviceStatusOnline, pubIP, privIP, nt, int64(bytesSent), int64(bytesReceived)); err != nil {
		return 0, err
	}

	if s.bus != nil {
		_ = s.bus.Publish(ctx, domain.PeerEvent{Type: domain.PeerEventEndpointChanged, NetworkID: d.NetworkID, DeviceID: deviceID})
	}

	return int32(s.presenceTTL.Seconds() / 1.5), nil
}

// NetworksForDevice returns the network the device belongs to, used by the
// gRPC transport to authorize/scope a StreamPeerUpdates subscription.
func (s *CoordinationService) NetworksForDevice(ctx context.Context, deviceID uuid.UUID) (*domain.Device, error) {
	return s.devices.GetByID(ctx, deviceID)
}

// BuildPeer converts a device + its presence info into a gRPC-ready Peer
// snapshot; kept in the usecase layer so the transport layer doesn't touch
// the presence store directly.
func (s *CoordinationService) DevicePresence(ctx context.Context, deviceID uuid.UUID) (*domain.PresenceEndpoint, error) {
	if s.presence == nil {
		return nil, nil
	}
	return s.presence.Get(ctx, deviceID)
}

// Subscribe opens a PeerEventBus subscription for the given networks.
func (s *CoordinationService) Subscribe(ctx context.Context, networkIDs []uuid.UUID) (domain.PeerEventSubscription, error) {
	return s.bus.Subscribe(ctx, networkIDs)
}

// GetDevice loads a device by ID (used by the gRPC transport to hydrate PeerUpdate payloads).
func (s *CoordinationService) GetDevice(ctx context.Context, deviceID uuid.UUID) (*domain.Device, error) {
	return s.devices.GetByID(ctx, deviceID)
}

// ExchangeICE stores the caller's candidates addressed to targetDeviceID
// and returns whatever candidates targetDeviceID has already addressed back
// to the caller (empty if the peer hasn't called yet; clients retry).
func (s *CoordinationService) ExchangeICE(ctx context.Context, deviceID, targetDeviceID uuid.UUID, candidates []domain.ICECandidate) ([]domain.ICECandidate, error) {
	if _, err := s.devices.GetByID(ctx, deviceID); err != nil {
		return nil, err
	}
	if _, err := s.devices.GetByID(ctx, targetDeviceID); err != nil {
		return nil, err
	}
	if err := s.ice.Put(ctx, deviceID, targetDeviceID, candidates, 30*time.Second); err != nil {
		return nil, err
	}
	return s.ice.Get(ctx, deviceID, targetDeviceID)
}

// RequestRelay selects the least-loaded active relay (optionally matching
// preferredRegion), increments its load counter, and issues a short-lived
// session token scoped to this device+relay+network.
func (s *CoordinationService) RequestRelay(ctx context.Context, deviceID, networkID uuid.UUID, preferredRegion string) (*RelayAllocation, error) {
	relay, err := s.relays.PickLeastLoaded(ctx, preferredRegion)
	if err != nil {
		return nil, err
	}
	if relay == nil {
		return nil, domain.ErrNoRelayAvailable
	}
	if err := s.relays.IncrementLoad(ctx, relay.ID, 1); err != nil {
		return nil, err
	}
	token, err := s.relayTok.Issue(deviceID, relay.ID, networkID)
	if err != nil {
		return nil, err
	}
	return &RelayAllocation{Relay: relay, SessionToken: token}, nil
}
