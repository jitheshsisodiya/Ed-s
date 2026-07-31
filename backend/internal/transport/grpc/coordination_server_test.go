package grpc

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/backend/gen/coordination/v1"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// --- Minimal in-memory repositories, scoped to the gRPC transport tests ---

type memberRepo struct {
	mu      sync.Mutex
	members map[string]*domain.NetworkMember
}

func (r *memberRepo) key(n, u uuid.UUID) string { return n.String() + "/" + u.String() }

func (r *memberRepo) Create(_ context.Context, m *domain.NetworkMember) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.members[r.key(m.NetworkID, m.UserID)] = m
	return nil
}

func (r *memberRepo) Get(_ context.Context, networkID, userID uuid.UUID) (*domain.NetworkMember, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[r.key(networkID, userID)]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return m, nil
}
func (r *memberRepo) List(context.Context, uuid.UUID) ([]*domain.NetworkMember, error) {
	return nil, nil
}
func (r *memberRepo) ListNetworkIDsForUser(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (r *memberRepo) UpdateRole(context.Context, uuid.UUID, uuid.UUID, domain.NetworkRole) error {
	return nil
}
func (r *memberRepo) Delete(context.Context, uuid.UUID, uuid.UUID) error { return nil }
func (r *memberRepo) CountByRole(context.Context, uuid.UUID, domain.NetworkRole) (int, error) {
	return 0, nil
}

type networkRepo struct{ networks map[uuid.UUID]*domain.Network }

func (r *networkRepo) Create(context.Context, *domain.Network) error { return nil }
func (r *networkRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Network, error) {
	n, ok := r.networks[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return n, nil
}
func (r *networkRepo) GetByInviteCode(context.Context, string) (*domain.Network, error) {
	return nil, domain.ErrNotFound
}
func (r *networkRepo) ListForUser(context.Context, uuid.UUID) ([]*domain.Network, error) {
	return nil, nil
}
func (r *networkRepo) Update(context.Context, *domain.Network) error { return nil }
func (r *networkRepo) Delete(context.Context, uuid.UUID) error       { return nil }
func (r *networkRepo) RotateInviteCode(context.Context, uuid.UUID, string, *time.Time) error {
	return nil
}
func (r *networkRepo) CountMembers(context.Context, uuid.UUID) (int, error) { return 0, nil }
func (r *networkRepo) CountDevices(context.Context, uuid.UUID) (int, error) { return 0, nil }

// deviceRepo is shared between the test goroutine and the gRPC server's
// handler goroutines, so every method locks.
type deviceRepo struct {
	mu      sync.Mutex
	devices map[uuid.UUID]*domain.Device
	nextIP  int
}

func (r *deviceRepo) Create(_ context.Context, d *domain.Device) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	r.devices[d.ID] = d
	return nil
}
func (r *deviceRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[id]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return d, nil
}
func (r *deviceRepo) GetByPublicKey(_ context.Context, pk string) (*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.devices {
		if d.PublicKey == pk {
			return d, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (r *deviceRepo) GetByNetworkAndUser(_ context.Context, n, u uuid.UUID) (*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.devices {
		if d.NetworkID == n && d.UserID == u {
			return d, nil
		}
	}
	return nil, domain.ErrNotFound
}
func (r *deviceRepo) ListByNetwork(_ context.Context, n uuid.UUID) ([]*domain.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*domain.Device
	for _, d := range r.devices {
		if d.NetworkID == n {
			out = append(out, d)
		}
	}
	return out, nil
}
func (r *deviceRepo) ListByUser(context.Context, uuid.UUID) ([]*domain.Device, error) {
	return nil, nil
}
func (r *deviceRepo) ListPeers(ctx context.Context, deviceID uuid.UUID) ([]*domain.Device, error) {
	self, err := r.GetByID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	all, _ := r.ListByNetwork(ctx, self.NetworkID)
	var out []*domain.Device
	for _, d := range all {
		if d.ID != deviceID {
			out = append(out, d)
		}
	}
	return out, nil
}
func (r *deviceRepo) Update(context.Context, *domain.Device) error { return nil }
func (r *deviceRepo) UpdateHeartbeat(_ context.Context, id uuid.UUID, status domain.DeviceStatus, pub, priv, nat *string, sent, recv int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.devices[id]
	if !ok {
		return domain.ErrNotFound
	}
	now := time.Now()
	d.Status = status
	d.LastSeenAt = &now
	d.LastPublicIP = pub
	d.BytesSent = sent
	d.BytesReceived = recv
	return nil
}
func (r *deviceRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (r *deviceRepo) NextFreeVirtualIP(context.Context, uuid.UUID, string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextIP++
	return "10.77.0." + string(rune('0'+r.nextIP)), nil
}
func (r *deviceRepo) CountDistinctActiveUsers(context.Context, time.Time) (int, error) {
	return 0, nil
}
func (r *deviceRepo) CountDistinctActiveNetworks(context.Context, time.Time) (int, error) {
	return 0, nil
}
func (r *deviceRepo) CountTotal(context.Context) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.devices), nil
}

type relayRepo struct {
	mu    sync.Mutex
	relay *domain.RelayServer
}

func (r *relayRepo) Create(context.Context, *domain.RelayServer) error { return nil }
func (r *relayRepo) GetByID(context.Context, uuid.UUID) (*domain.RelayServer, error) {
	return r.relay, nil
}
func (r *relayRepo) ListActive(context.Context) ([]*domain.RelayServer, error) {
	return []*domain.RelayServer{r.relay}, nil
}
func (r *relayRepo) PickLeastLoaded(context.Context, string) (*domain.RelayServer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.relay, nil
}
func (r *relayRepo) IncrementLoad(_ context.Context, _ uuid.UUID, delta int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.relay != nil {
		r.relay.CurrentLoad += delta
	}
	return nil
}
func (r *relayRepo) Heartbeat(context.Context, uuid.UUID, int) error { return nil }

type auditRepo struct{}

func (auditRepo) Create(context.Context, *domain.AuditLog) error { return nil }
func (auditRepo) ListByNetwork(context.Context, uuid.UUID, int, int) ([]*domain.AuditLog, error) {
	return nil, nil
}
func (auditRepo) List(context.Context, int, int) ([]*domain.AuditLog, error) { return nil, nil }

type presenceStore struct {
	mu  sync.Mutex
	eps map[uuid.UUID]domain.PresenceEndpoint
}

func (p *presenceStore) SetOnline(_ context.Context, _, deviceID uuid.UUID, ep domain.PresenceEndpoint, _ time.Duration) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.eps[deviceID] = ep
	return nil
}
func (p *presenceStore) Remove(_ context.Context, _, deviceID uuid.UUID) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.eps, deviceID)
	return nil
}
func (p *presenceStore) Get(_ context.Context, deviceID uuid.UUID) (*domain.PresenceEndpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	ep, ok := p.eps[deviceID]
	if !ok {
		return nil, nil
	}
	return &ep, nil
}
func (p *presenceStore) OnlineInNetwork(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	return nil, nil
}
func (p *presenceStore) CountOnline(context.Context) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.eps), nil
}

type eventBus struct{ ch chan domain.PeerEvent }

func (b *eventBus) Publish(_ context.Context, ev domain.PeerEvent) error {
	select {
	case b.ch <- ev:
	default: // drop when nobody is listening, as a real broker would
	}
	return nil
}
func (b *eventBus) Subscribe(context.Context, []uuid.UUID) (domain.PeerEventSubscription, error) {
	return &subscription{ch: b.ch}, nil
}

type subscription struct{ ch chan domain.PeerEvent }

func (s *subscription) Events() <-chan domain.PeerEvent { return s.ch }
func (s *subscription) Close() error                    { return nil }

type iceStore struct {
	mu         sync.Mutex
	candidates map[string][]domain.ICECandidate
}

func (s *iceStore) key(from, to uuid.UUID) string { return from.String() + "->" + to.String() }
func (s *iceStore) Put(_ context.Context, from, to uuid.UUID, c []domain.ICECandidate, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates[s.key(from, to)] = c
	return nil
}
func (s *iceStore) Get(_ context.Context, forDevice, fromDevice uuid.UUID) ([]domain.ICECandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.candidates[s.key(fromDevice, forDevice)], nil
}

// --- Test harness ---

type harness struct {
	client   coordinationv1.CoordinationServiceClient
	tokens   *auth.TokenManager
	devices  *deviceRepo
	relays   *relayRepo
	presence *presenceStore
	userID   uuid.UUID
	network  *domain.Network
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	userID := uuid.New()
	network := &domain.Network{
		ID: uuid.New(), Name: "Test Net", CIDR: "10.77.0.0/24",
		DNSServers: []string{"1.1.1.1"}, OwnerID: userID,
	}

	members := &memberRepo{members: map[string]*domain.NetworkMember{}}
	_ = members.Create(context.Background(), &domain.NetworkMember{
		ID: uuid.New(), NetworkID: network.ID, UserID: userID, Role: domain.NetworkRoleOwner,
	})

	devices := &deviceRepo{devices: map[uuid.UUID]*domain.Device{}}
	relays := &relayRepo{relay: &domain.RelayServer{
		ID: uuid.New(), Region: "local", Hostname: "relay.test",
		RelayPort: 3479, PublicKey: "relay-pubkey", Capacity: 100, Status: "active",
	}}
	presence := &presenceStore{eps: map[uuid.UUID]domain.PresenceEndpoint{}}

	svc := usecase.NewCoordinationService(
		devices,
		&networkRepo{networks: map[uuid.UUID]*domain.Network{network.ID: network}},
		members, relays, presence,
		&eventBus{ch: make(chan domain.PeerEvent, 16)},
		&iceStore{candidates: map[string][]domain.ICECandidate{}},
		auth.NewRelaySessionManager("relay-secret-test", time.Minute),
		usecase.NewAuditRecorder(auditRepo{}, zap.NewNop()),
		45*time.Second,
	)

	tokens := auth.NewTokenManager("access-secret-test", "refresh-secret-test", "nexusvpn-test", time.Hour, time.Hour)

	lis := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(AuthUnaryInterceptor(tokens)),
		grpc.ChainStreamInterceptor(AuthStreamInterceptor(tokens)),
	)
	NewCoordinationServer(svc, zap.NewNop()).Register(server)

	go func() { _ = server.Serve(lis) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &harness{
		client:   coordinationv1.NewCoordinationServiceClient(conn),
		tokens:   tokens,
		devices:  devices,
		relays:   relays,
		presence: presence,
		userID:   userID,
		network:  network,
	}
}

// authCtx returns a context carrying a valid bearer token for h.userID.
func (h *harness) authCtx(t *testing.T) context.Context {
	t.Helper()
	token, _, err := h.tokens.IssueAccessToken(h.userID, "grpc@example.com")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+token)
}

// --- Tests ---

func TestUnauthenticatedRPCRejected(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.RegisterDevice(t.Context(), &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "pk", NetworkId: h.network.ID.String(),
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %s, want Unauthenticated", status.Code(err))
	}
}

func TestInvalidTokenRejected(t *testing.T) {
	h := newHarness(t)
	ctx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer garbage")

	_, err := h.client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "pk", NetworkId: h.network.ID.String(),
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("code = %s, want Unauthenticated", status.Code(err))
	}
}

func TestRegisterDeviceReturnsVirtualIPAndTopology(t *testing.T) {
	h := newHarness(t)
	ctx := h.authCtx(t)

	resp, err := h.client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "device-pubkey-1",
		NetworkId:       h.network.ID.String(),
		DeviceName:      "laptop",
		Os:              "linux",
		OsVersion:       "6.1",
		ClientVersion:   "1.0.0",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if resp.GetAssignedVirtualIp() == "" {
		t.Fatal("expected an assigned virtual IP")
	}
	if resp.GetNetworkCidr() != "10.77.0.0/24" {
		t.Fatalf("cidr = %q", resp.GetNetworkCidr())
	}
	if len(resp.GetDnsServers()) != 1 || resp.GetDnsServers()[0] != "1.1.1.1" {
		t.Fatalf("dns servers = %v", resp.GetDnsServers())
	}
	if _, err := uuid.Parse(resp.GetDeviceId()); err != nil {
		t.Fatalf("device_id is not a UUID: %v", err)
	}
}

func TestRegisterDeviceRejectsMalformedNetworkID(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.RegisterDevice(h.authCtx(t), &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "pk", NetworkId: "not-a-uuid",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %s, want InvalidArgument", status.Code(err))
	}
}

func TestRegisterDeviceRequiresMembership(t *testing.T) {
	h := newHarness(t)
	// A token for a user who is not a member of the network.
	stranger, _, err := h.tokens.IssueAccessToken(uuid.New(), "stranger@example.com")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	ctx := metadata.AppendToOutgoingContext(t.Context(), "authorization", "Bearer "+stranger)

	_, err = h.client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "pk-stranger", NetworkId: h.network.ID.String(),
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("code = %s, want PermissionDenied", status.Code(err))
	}
}

func TestHeartbeatUpdatesPresence(t *testing.T) {
	h := newHarness(t)
	ctx := h.authCtx(t)

	reg, err := h.client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "device-pubkey-hb", NetworkId: h.network.ID.String(), DeviceName: "hb",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	resp, err := h.client.Heartbeat(ctx, &coordinationv1.HeartbeatRequest{
		DeviceId:        reg.GetDeviceId(),
		PublicEndpoint:  &coordinationv1.Endpoint{Ip: "203.0.113.5", Port: 51820, Protocol: "udp"},
		PrivateEndpoint: &coordinationv1.Endpoint{Ip: "192.168.1.10", Port: 51820, Protocol: "udp"},
		NatType:         coordinationv1.NATType_NAT_TYPE_PORT_RESTRICTED_CONE,
		BytesSent:       4096,
		BytesReceived:   8192,
	})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if !resp.GetOk() {
		t.Fatal("expected ok=true")
	}
	if resp.GetNextHeartbeatSeconds() <= 0 {
		t.Fatalf("next_heartbeat_seconds = %d, want > 0", resp.GetNextHeartbeatSeconds())
	}

	deviceID := uuid.MustParse(reg.GetDeviceId())
	ep, err := h.presence.Get(t.Context(), deviceID)
	if err != nil || ep == nil {
		t.Fatalf("expected presence to be recorded, got %v (err %v)", ep, err)
	}
	if ep.PublicIP != "203.0.113.5" || ep.PublicPort != 51820 {
		t.Fatalf("presence endpoint = %s:%d", ep.PublicIP, ep.PublicPort)
	}
	if ep.NATType != "port_restricted_cone" {
		t.Fatalf("nat type = %q", ep.NATType)
	}
}

func TestRequestRelayReturnsAllocationAndToken(t *testing.T) {
	h := newHarness(t)
	ctx := h.authCtx(t)

	reg, err := h.client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "device-pubkey-relay", NetworkId: h.network.ID.String(), DeviceName: "r",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	resp, err := h.client.RequestRelay(ctx, &coordinationv1.RequestRelayRequest{
		DeviceId: reg.GetDeviceId(), NetworkId: h.network.ID.String(), PreferredRegion: "local",
	})
	if err != nil {
		t.Fatalf("RequestRelay: %v", err)
	}
	if resp.GetHostname() != "relay.test" || resp.GetRelayPort() != 3479 {
		t.Fatalf("relay = %s:%d", resp.GetHostname(), resp.GetRelayPort())
	}
	if resp.GetSessionToken() == "" {
		t.Fatal("expected a relay session token")
	}

	// The token must verify against the same shared secret the relay uses.
	claims, err := auth.NewRelaySessionManager("relay-secret-test", time.Minute).Verify(resp.GetSessionToken())
	if err != nil {
		t.Fatalf("relay session token does not verify: %v", err)
	}
	if claims.DeviceID != reg.GetDeviceId() {
		t.Fatalf("token device = %s, want %s", claims.DeviceID, reg.GetDeviceId())
	}

	// Allocating a relay increments its load, which is how the control plane
	// spreads sessions across the relay fleet.
	h.relays.mu.Lock()
	load := h.relays.relay.CurrentLoad
	h.relays.mu.Unlock()
	if load != 1 {
		t.Fatalf("relay load = %d, want 1", load)
	}
}

func TestExchangeICECandidatesRendezvous(t *testing.T) {
	h := newHarness(t)
	ctx := h.authCtx(t)

	a, err := h.client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "pk-a", NetworkId: h.network.ID.String(), DeviceName: "a",
	})
	if err != nil {
		t.Fatalf("register a: %v", err)
	}
	// A second device owned by the same user in this harness; enough to
	// exercise the two-sided candidate rendezvous.
	bDevice := &domain.Device{
		ID: uuid.New(), UserID: h.userID, NetworkID: h.network.ID,
		Name: "b", PublicKey: "pk-b", VirtualIP: "10.77.0.9",
	}
	if err := h.devices.Create(t.Context(), bDevice); err != nil {
		t.Fatalf("seed device b: %v", err)
	}

	// A publishes its candidates first; B hasn't spoken yet, so A gets none.
	respA, err := h.client.ExchangeICECandidates(ctx, &coordinationv1.ICEExchangeRequest{
		DeviceId:       a.GetDeviceId(),
		TargetDeviceId: bDevice.ID.String(),
		Candidates: []*coordinationv1.ICECandidate{{
			Endpoint: &coordinationv1.Endpoint{Ip: "203.0.113.1", Port: 51820, Protocol: "udp"},
			Type:     "srflx", Priority: 100,
		}},
	})
	if err != nil {
		t.Fatalf("exchange from a: %v", err)
	}
	if len(respA.GetPeerCandidates()) != 0 {
		t.Fatalf("expected no peer candidates yet, got %d", len(respA.GetPeerCandidates()))
	}

	// B now publishes and immediately receives A's candidates.
	respB, err := h.client.ExchangeICECandidates(ctx, &coordinationv1.ICEExchangeRequest{
		DeviceId:       bDevice.ID.String(),
		TargetDeviceId: a.GetDeviceId(),
		Candidates: []*coordinationv1.ICECandidate{{
			Endpoint: &coordinationv1.Endpoint{Ip: "198.51.100.2", Port: 51820, Protocol: "udp"},
			Type:     "srflx", Priority: 100,
		}},
	})
	if err != nil {
		t.Fatalf("exchange from b: %v", err)
	}
	if len(respB.GetPeerCandidates()) != 1 {
		t.Fatalf("expected 1 peer candidate, got %d", len(respB.GetPeerCandidates()))
	}
	if got := respB.GetPeerCandidates()[0].GetEndpoint().GetIp(); got != "203.0.113.1" {
		t.Fatalf("peer candidate ip = %q, want 203.0.113.1", got)
	}
}

func TestStreamPeerUpdatesDeliversEvents(t *testing.T) {
	h := newHarness(t)
	ctx := h.authCtx(t)

	reg, err := h.client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "pk-stream", NetworkId: h.network.ID.String(), DeviceName: "streamer",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	stream, err := h.client.StreamPeerUpdates(streamCtx, &coordinationv1.StreamPeerUpdatesRequest{
		DeviceId: reg.GetDeviceId(),
	})
	if err != nil {
		t.Fatalf("StreamPeerUpdates: %v", err)
	}

	// A heartbeat from a *different* device publishes an endpoint-changed
	// event, which the stream should deliver (self-events are filtered out).
	peer := &domain.Device{
		ID: uuid.New(), UserID: h.userID, NetworkID: h.network.ID,
		Name: "peer", PublicKey: "pk-peer-stream", VirtualIP: "10.77.0.20",
	}
	if err := h.devices.Create(t.Context(), peer); err != nil {
		t.Fatalf("seed peer: %v", err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = h.client.Heartbeat(ctx, &coordinationv1.HeartbeatRequest{
			DeviceId:       peer.ID.String(),
			PublicEndpoint: &coordinationv1.Endpoint{Ip: "203.0.113.77", Port: 51820, Protocol: "udp"},
		})
	}()

	update, err := stream.Recv()
	if err != nil {
		t.Fatalf("stream recv: %v", err)
	}
	if update.GetNetworkId() != h.network.ID.String() {
		t.Fatalf("network = %s, want %s", update.GetNetworkId(), h.network.ID)
	}
	if update.GetPeer().GetDeviceId() != peer.ID.String() {
		t.Fatalf("peer = %s, want %s", update.GetPeer().GetDeviceId(), peer.ID)
	}
	if update.GetType() != coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_ENDPOINT_CHANGED {
		t.Fatalf("type = %s", update.GetType())
	}
}

func TestNATTypeConversionRoundTrips(t *testing.T) {
	cases := []string{"open", "full_cone", "restricted_cone", "port_restricted_cone", "symmetric"}
	for _, want := range cases {
		if got := natTypeFromProto(natTypeToProto(want)); got != want {
			t.Errorf("round trip %q -> %q", want, got)
		}
	}
	if natTypeToProto("nonsense") != coordinationv1.NATType_NAT_TYPE_UNSPECIFIED {
		t.Error("unknown NAT type should map to UNSPECIFIED")
	}
	if natTypeFromProto(coordinationv1.NATType_NAT_TYPE_UNSPECIFIED) != "" {
		t.Error("UNSPECIFIED should map back to the empty string")
	}
}

func TestPeerUpdateTypeConversion(t *testing.T) {
	cases := map[domain.PeerEventType]coordinationv1.PeerUpdateType{
		domain.PeerEventJoined:          coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_JOINED,
		domain.PeerEventLeft:            coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_LEFT,
		domain.PeerEventEndpointChanged: coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_ENDPOINT_CHANGED,
		domain.PeerEventStatusChanged:   coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_STATUS_CHANGED,
	}
	for in, want := range cases {
		if got := peerUpdateTypeToProto(in); got != want {
			t.Errorf("peerUpdateTypeToProto(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestToGRPCErrorMapping(t *testing.T) {
	cases := map[error]codes.Code{
		domain.ErrNotFound:         codes.NotFound,
		domain.ErrForbidden:        codes.PermissionDenied,
		domain.ErrUnauthorized:     codes.Unauthenticated,
		domain.ErrInvalidInput:     codes.InvalidArgument,
		domain.ErrAlreadyExists:    codes.AlreadyExists,
		domain.ErrNoRelayAvailable: codes.ResourceExhausted,
	}
	for in, want := range cases {
		if got := status.Code(toGRPCError(in)); got != want {
			t.Errorf("toGRPCError(%v) = %s, want %s", in, got, want)
		}
	}
}
