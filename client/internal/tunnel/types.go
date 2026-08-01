// Package tunnel implements the NexusVPN client connect state machine:
// register the device, discover its public endpoint via STUN, keep the
// control plane informed, track peer topology, and for each peer attempt a
// direct hole-punched connection before falling back to a relay.
//
// The flow it implements is the one described in docs/architecture.md
// ("Connectivity strategy" and the sequence diagram).
package tunnel

import (
	"context"
	"net"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/jitheshsisodiya/Ed-s/client/internal/disco"

	"github.com/jitheshsisodiya/Ed-s/client/internal/stun"
	"github.com/jitheshsisodiya/Ed-s/client/internal/wireguard"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/client/internal/coordination/gen"
)

// ConnectionMode describes how traffic currently reaches a peer.
type ConnectionMode string

const (
	// ModeConnecting means no path has been established yet.
	ModeConnecting ConnectionMode = "connecting"
	// ModeDirect means packets flow peer-to-peer after a successful hole punch.
	ModeDirect ConnectionMode = "direct"
	// ModeRelay means packets are forwarded by a relay node.
	ModeRelay ConnectionMode = "relay"
	// ModeOffline means the peer is known but not currently reachable.
	ModeOffline ConnectionMode = "offline"
)

// PeerStatus is a snapshot of one peer's connectivity, as surfaced by
// `nexusvpnctl status` and the desktop UI.
type PeerStatus struct {
	DeviceID   string
	DeviceName string
	// OS is the peer's platform, as reported by the control plane.
	OS string
	// ExitNode reports that this peer has offered to carry this machine's
	// internet traffic. Offering is not being used — SetExitNode is what
	// takes them up on it.
	ExitNode      bool
	PublicKey     string
	VirtualIP     string
	Mode          ConnectionMode
	Endpoint      string
	LastHandshake time.Time
	BytesSent     uint64
	BytesReceived uint64
	// LatencyMs is the most recent round-trip estimate, or -1 if unknown.
	LatencyMs int
}

// Status is a snapshot of the whole tunnel.
type Status struct {
	Connected      bool
	State          State
	NetworkID      string
	NetworkName    string
	DeviceID       string
	InterfaceName  string
	VirtualIP      string
	CIDR           string
	NATType        string
	PublicEndpoint string
	Peers          []PeerStatus

	// ExitNodeID is the peer carrying the default route, empty when
	// traffic is split-tunnelled as usual.
	ExitNodeID string
	// KillSwitchEngaged reports that traffic outside the tunnel is
	// currently blocked.
	KillSwitchEngaged bool
	// ActiveSince is when the tunnel last came up, zero when it is not up.
	ActiveSince time.Time
}

// WireGuardDevice is the subset of *wireguard.Device the orchestrator uses,
// expressed as an interface so the state machine can be tested without
// creating a real TUN device (which requires root).
type WireGuardDevice interface {
	Name() string
	ListenPort() (int, error)
	UpsertPeer(p wireguard.PeerConfig) error
	UpdateEndpoint(publicKeyBase64 string, endpoint *net.UDPAddr) error
	RemovePeer(publicKeyBase64 string) error
	PeerStats(publicKeyBase64 string) (wireguard.PeerStats, bool, error)
	Stats() ([]wireguard.PeerStats, error)
	Close() error
}

// Coordinator is the subset of *coordination.Client the orchestrator uses.
type Coordinator interface {
	RegisterDevice(ctx context.Context, req *coordinationv1.RegisterDeviceRequest) (*coordinationv1.RegisterDeviceResponse, error)
	Heartbeat(ctx context.Context, req *coordinationv1.HeartbeatRequest) (*coordinationv1.HeartbeatResponse, error)
	StreamPeerUpdates(ctx context.Context, deviceID string) (PeerUpdateStream, error)
	ExchangeICECandidates(ctx context.Context, req *coordinationv1.ICEExchangeRequest) (*coordinationv1.ICEExchangeResponse, error)
	RequestRelay(ctx context.Context, req *coordinationv1.RequestRelayRequest) (*coordinationv1.RequestRelayResponse, error)
}

// PeerUpdateStream is the receive side of the StreamPeerUpdates RPC.
type PeerUpdateStream interface {
	Recv() (*coordinationv1.PeerUpdate, error)
}

// PathProber measures round-trip time to a peer over the tunnel's own
// socket. internal/disco.Prober implements it.
type PathProber interface {
	// Ping sends a probe to addr; the reply is recorded asynchronously.
	Ping(addr netip.AddrPort) error
	// Result returns the most recent successful probe for a peer.
	Result(peer uuid.UUID) (disco.Result, bool)
	// Forget drops a peer's recorded result.
	Forget(peer uuid.UUID)
}

// EndpointDiscoverer reports this device's public (server-reflexive)
// endpoint and NAT classification, normally via STUN.
type EndpointDiscoverer interface {
	Discover(ctx context.Context) (DiscoveryResult, error)
}

// DiscoveryResult is what STUN discovery yields.
type DiscoveryResult struct {
	// PublicEndpoint is the address peers on the internet should target.
	PublicEndpoint *net.UDPAddr
	// LocalEndpoint is this device's address on its own LAN, used as a
	// "host" ICE candidate so two peers behind the same NAT connect
	// directly instead of hairpinning through the internet.
	LocalEndpoint *net.UDPAddr
	NATType       stun.NATType
}

// natTypeToProto maps a STUN classification onto the gRPC enum.
func natTypeToProto(t stun.NATType) coordinationv1.NATType {
	switch t {
	case stun.NATOpen:
		return coordinationv1.NATType_NAT_TYPE_OPEN
	case stun.NATFullCone:
		return coordinationv1.NATType_NAT_TYPE_FULL_CONE
	case stun.NATRestrictedCone:
		return coordinationv1.NATType_NAT_TYPE_RESTRICTED_CONE
	case stun.NATPortRestrictedCone:
		return coordinationv1.NATType_NAT_TYPE_PORT_RESTRICTED_CONE
	case stun.NATSymmetric:
		return coordinationv1.NATType_NAT_TYPE_SYMMETRIC
	default:
		return coordinationv1.NATType_NAT_TYPE_UNSPECIFIED
	}
}

// endpointToProto converts a UDP address to the wire type, tolerating nil.
func endpointToProto(addr *net.UDPAddr) *coordinationv1.Endpoint {
	if addr == nil {
		return nil
	}
	return &coordinationv1.Endpoint{
		Ip:       addr.IP.String(),
		Port:     uint32(addr.Port),
		Protocol: "udp",
	}
}

// endpointFromProto converts a wire endpoint to a UDP address, returning nil
// when unset or unparseable.
func endpointFromProto(ep *coordinationv1.Endpoint) *net.UDPAddr {
	if ep == nil || ep.GetIp() == "" || ep.GetPort() == 0 {
		return nil
	}
	ip := net.ParseIP(ep.GetIp())
	if ip == nil {
		return nil
	}
	return &net.UDPAddr{IP: ip, Port: int(ep.GetPort())}
}
