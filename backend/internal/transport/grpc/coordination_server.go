package grpc

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/backend/gen/coordination/v1"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/metrics"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// CoordinationServer adapts usecase.CoordinationService to the gRPC contract.
type CoordinationServer struct {
	coordinationv1.UnimplementedCoordinationServiceServer

	svc    *usecase.CoordinationService
	logger *zap.Logger
}

// NewCoordinationServer builds a CoordinationServer.
func NewCoordinationServer(svc *usecase.CoordinationService, logger *zap.Logger) *CoordinationServer {
	return &CoordinationServer{svc: svc, logger: logger}
}

// Register attaches the server to a grpc.Server.
func (s *CoordinationServer) Register(gs *grpc.Server) {
	coordinationv1.RegisterCoordinationServiceServer(gs, s)
}

// --- enum/domain conversion helpers ---

func natTypeToProto(s string) coordinationv1.NATType {
	switch strings.ToLower(s) {
	case "open":
		return coordinationv1.NATType_NAT_TYPE_OPEN
	case "full_cone", "fullcone":
		return coordinationv1.NATType_NAT_TYPE_FULL_CONE
	case "restricted_cone", "restricted":
		return coordinationv1.NATType_NAT_TYPE_RESTRICTED_CONE
	case "port_restricted_cone", "port_restricted":
		return coordinationv1.NATType_NAT_TYPE_PORT_RESTRICTED_CONE
	case "symmetric":
		return coordinationv1.NATType_NAT_TYPE_SYMMETRIC
	default:
		return coordinationv1.NATType_NAT_TYPE_UNSPECIFIED
	}
}

func natTypeFromProto(t coordinationv1.NATType) string {
	switch t {
	case coordinationv1.NATType_NAT_TYPE_OPEN:
		return "open"
	case coordinationv1.NATType_NAT_TYPE_FULL_CONE:
		return "full_cone"
	case coordinationv1.NATType_NAT_TYPE_RESTRICTED_CONE:
		return "restricted_cone"
	case coordinationv1.NATType_NAT_TYPE_PORT_RESTRICTED_CONE:
		return "port_restricted_cone"
	case coordinationv1.NATType_NAT_TYPE_SYMMETRIC:
		return "symmetric"
	default:
		return ""
	}
}

func peerUpdateTypeToProto(t domain.PeerEventType) coordinationv1.PeerUpdateType {
	switch t {
	case domain.PeerEventJoined:
		return coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_JOINED
	case domain.PeerEventLeft:
		return coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_LEFT
	case domain.PeerEventEndpointChanged:
		return coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_ENDPOINT_CHANGED
	case domain.PeerEventStatusChanged:
		return coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_STATUS_CHANGED
	default:
		return coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_UNSPECIFIED
	}
}

// toProtoPeer converts a device (plus its live presence, if any) to a Peer.
func (s *CoordinationServer) toProtoPeer(ctx context.Context, d *domain.Device) *coordinationv1.Peer {
	p := &coordinationv1.Peer{
		DeviceId:   d.ID.String(),
		DeviceName: d.Name,
		PublicKey:  d.PublicKey,
		VirtualIp:  d.VirtualIP,
		Os:         string(d.OS),
		ExitNode:   d.AdvertisesExitNode,
		Online:     d.Status == domain.DeviceStatusOnline,
	}
	if d.LastSeenAt != nil {
		p.LastSeen = timestamppb.New(*d.LastSeenAt)
	}
	if d.NATType != nil {
		p.NatType = natTypeToProto(*d.NATType)
	}

	// Prefer the live presence endpoint (fresher than the Postgres columns).
	if ep, err := s.svc.DevicePresence(ctx, d.ID); err == nil && ep != nil {
		p.LastKnownEndpoint = &coordinationv1.Endpoint{
			Ip:       ep.PublicIP,
			Port:     ep.PublicPort,
			Protocol: "udp",
		}
		p.Online = true
		if ep.NATType != "" {
			p.NatType = natTypeToProto(ep.NATType)
		}
	} else if d.LastPublicIP != nil {
		p.LastKnownEndpoint = &coordinationv1.Endpoint{Ip: *d.LastPublicIP, Protocol: "udp"}
	}
	return p
}

// toGRPCError maps domain sentinel errors onto gRPC status codes.
func toGRPCError(err error) error {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, domain.ErrForbidden):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, domain.ErrUnauthorized):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, domain.ErrInvalidInput):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, domain.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, err.Error())
	case errors.Is(err, domain.ErrNoRelayAvailable):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, domain.ErrNetworkFull):
		return status.Error(codes.ResourceExhausted, err.Error())
	default:
		return status.Error(codes.Internal, "internal error")
	}
}

// RegisterDevice implements CoordinationServiceServer.
func (s *CoordinationServer) RegisterDevice(ctx context.Context, req *coordinationv1.RegisterDeviceRequest) (*coordinationv1.RegisterDeviceResponse, error) {
	userID, ok := userIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	networkID, err := uuid.Parse(req.GetNetworkId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "malformed network_id")
	}
	if req.GetDevicePublicKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "device_public_key is required")
	}

	result, err := s.svc.RegisterDevice(ctx, userID, networkID, usecase.RegisterDeviceInput{
		PublicKey:         req.GetDevicePublicKey(),
		DeviceName:        req.GetDeviceName(),
		OS:                req.GetOs(),
		OSVersion:         req.GetOsVersion(),
		ClientVersion:     req.GetClientVersion(),
		AdvertiseExitNode: req.GetAdvertiseExitNode(),
	})
	if err != nil {
		return nil, toGRPCError(err)
	}

	peers := make([]*coordinationv1.Peer, 0, len(result.ExistingPeers))
	for _, p := range result.ExistingPeers {
		peers = append(peers, s.toProtoPeer(ctx, p))
	}

	return &coordinationv1.RegisterDeviceResponse{
		DeviceId:          result.Device.ID.String(),
		AssignedVirtualIp: result.Device.VirtualIP,
		NetworkCidr:       result.NetworkCIDR,
		DnsServers:        result.DNSServers,
		ExistingPeers:     peers,
	}, nil
}

// Heartbeat implements CoordinationServiceServer.
func (s *CoordinationServer) Heartbeat(ctx context.Context, req *coordinationv1.HeartbeatRequest) (*coordinationv1.HeartbeatResponse, error) {
	if _, ok := userIDFromContext(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	deviceID, err := uuid.Parse(req.GetDeviceId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "malformed device_id")
	}

	var publicEP, privateEP domain.PresenceEndpoint
	if ep := req.GetPublicEndpoint(); ep != nil {
		publicEP.PublicIP = ep.GetIp()
		publicEP.PublicPort = ep.GetPort()
	}
	if ep := req.GetPrivateEndpoint(); ep != nil {
		privateEP.PrivateIP = ep.GetIp()
		privateEP.PrivatePort = ep.GetPort()
	}

	next, err := s.svc.Heartbeat(ctx, deviceID, publicEP, privateEP,
		natTypeFromProto(req.GetNatType()), req.GetBytesSent(), req.GetBytesReceived())
	if err != nil {
		return nil, toGRPCError(err)
	}
	return &coordinationv1.HeartbeatResponse{Ok: true, NextHeartbeatSeconds: next}, nil
}

// StreamPeerUpdates implements CoordinationServiceServer. It streams topology
// changes for the requesting device's network until the client disconnects.
func (s *CoordinationServer) StreamPeerUpdates(
	req *coordinationv1.StreamPeerUpdatesRequest,
	stream grpc.ServerStreamingServer[coordinationv1.PeerUpdate],
) error {
	ctx := stream.Context()
	if _, ok := userIDFromContext(ctx); !ok {
		return status.Error(codes.Unauthenticated, "authentication required")
	}
	deviceID, err := uuid.Parse(req.GetDeviceId())
	if err != nil {
		return status.Error(codes.InvalidArgument, "malformed device_id")
	}

	device, err := s.svc.NetworksForDevice(ctx, deviceID)
	if err != nil {
		return toGRPCError(err)
	}

	sub, err := s.svc.Subscribe(ctx, []uuid.UUID{device.NetworkID})
	if err != nil {
		return toGRPCError(err)
	}
	defer sub.Close()

	metrics.ActiveGRPCStreams.Inc()
	defer metrics.ActiveGRPCStreams.Dec()

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-sub.Events():
			if !ok {
				return nil
			}
			// Don't echo a device's own updates back to itself.
			if ev.DeviceID == deviceID {
				continue
			}

			update := &coordinationv1.PeerUpdate{
				Type:      peerUpdateTypeToProto(ev.Type),
				NetworkId: ev.NetworkID.String(),
			}
			// A "left" event may reference an already-deleted device; send the
			// update with just the ID so the client can drop the peer.
			if peerDevice, err := s.svc.GetDevice(ctx, ev.DeviceID); err == nil && peerDevice != nil {
				update.Peer = s.toProtoPeer(ctx, peerDevice)
			} else {
				update.Peer = &coordinationv1.Peer{DeviceId: ev.DeviceID.String()}
			}

			if err := stream.Send(update); err != nil {
				return err
			}
		}
	}
}

// ExchangeICECandidates implements CoordinationServiceServer.
func (s *CoordinationServer) ExchangeICECandidates(ctx context.Context, req *coordinationv1.ICEExchangeRequest) (*coordinationv1.ICEExchangeResponse, error) {
	if _, ok := userIDFromContext(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	deviceID, err := uuid.Parse(req.GetDeviceId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "malformed device_id")
	}
	targetID, err := uuid.Parse(req.GetTargetDeviceId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "malformed target_device_id")
	}

	candidates := make([]domain.ICECandidate, 0, len(req.GetCandidates()))
	for _, c := range req.GetCandidates() {
		ep := c.GetEndpoint()
		if ep == nil {
			continue
		}
		candidates = append(candidates, domain.ICECandidate{
			IP:       ep.GetIp(),
			Port:     ep.GetPort(),
			Protocol: ep.GetProtocol(),
			Type:     c.GetType(),
			Priority: c.GetPriority(),
		})
	}

	peerCandidates, err := s.svc.ExchangeICE(ctx, deviceID, targetID, candidates)
	if err != nil {
		return nil, toGRPCError(err)
	}

	out := make([]*coordinationv1.ICECandidate, 0, len(peerCandidates))
	for _, c := range peerCandidates {
		protocol := c.Protocol
		if protocol == "" {
			protocol = "udp"
		}
		out = append(out, &coordinationv1.ICECandidate{
			Endpoint: &coordinationv1.Endpoint{Ip: c.IP, Port: c.Port, Protocol: protocol},
			Type:     c.Type,
			Priority: c.Priority,
		})
	}
	return &coordinationv1.ICEExchangeResponse{PeerCandidates: out}, nil
}

// RequestRelay implements CoordinationServiceServer.
func (s *CoordinationServer) RequestRelay(ctx context.Context, req *coordinationv1.RequestRelayRequest) (*coordinationv1.RequestRelayResponse, error) {
	if _, ok := userIDFromContext(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	deviceID, err := uuid.Parse(req.GetDeviceId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "malformed device_id")
	}
	networkID, err := uuid.Parse(req.GetNetworkId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "malformed network_id")
	}

	alloc, err := s.svc.RequestRelay(ctx, deviceID, networkID, req.GetPreferredRegion())
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &coordinationv1.RequestRelayResponse{
		RelayId:        alloc.Relay.ID.String(),
		Hostname:       alloc.Relay.Hostname,
		RelayPort:      uint32(alloc.Relay.RelayPort),
		RelayPublicKey: alloc.Relay.PublicKey,
		SessionToken:   alloc.SessionToken,
	}, nil
}
