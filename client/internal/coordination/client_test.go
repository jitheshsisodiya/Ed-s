package coordination

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/client/internal/coordination/gen"
)

// fakeCoordinationServer is an in-process implementation of
// CoordinationServiceServer used to test the client wrapper's request
// construction, auth-metadata attachment, and streaming behavior without
// a real backend.
type fakeCoordinationServer struct {
	coordinationv1.UnimplementedCoordinationServiceServer

	lastAuthHeader string

	registerResp *coordinationv1.RegisterDeviceResponse
	heartbeatErr error

	peerUpdates []*coordinationv1.PeerUpdate

	relayResp *coordinationv1.RequestRelayResponse
}

func (f *fakeCoordinationServer) RegisterDevice(ctx context.Context, req *coordinationv1.RegisterDeviceRequest) (*coordinationv1.RegisterDeviceResponse, error) {
	f.captureAuth(ctx)
	if req.GetDevicePublicKey() == "" {
		return nil, errors.New("missing public key")
	}
	if f.registerResp != nil {
		return f.registerResp, nil
	}
	return &coordinationv1.RegisterDeviceResponse{
		DeviceId:          "device-1",
		AssignedVirtualIp: "10.77.0.5",
		NetworkCidr:       "10.77.0.0/24",
	}, nil
}

func (f *fakeCoordinationServer) Heartbeat(ctx context.Context, req *coordinationv1.HeartbeatRequest) (*coordinationv1.HeartbeatResponse, error) {
	f.captureAuth(ctx)
	if f.heartbeatErr != nil {
		return nil, f.heartbeatErr
	}
	return &coordinationv1.HeartbeatResponse{Ok: true, NextHeartbeatSeconds: 15}, nil
}

func (f *fakeCoordinationServer) StreamPeerUpdates(req *coordinationv1.StreamPeerUpdatesRequest, stream grpc.ServerStreamingServer[coordinationv1.PeerUpdate]) error {
	for _, u := range f.peerUpdates {
		if err := stream.Send(u); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeCoordinationServer) ExchangeICECandidates(ctx context.Context, req *coordinationv1.ICEExchangeRequest) (*coordinationv1.ICEExchangeResponse, error) {
	f.captureAuth(ctx)
	return &coordinationv1.ICEExchangeResponse{
		PeerCandidates: []*coordinationv1.ICECandidate{
			{Endpoint: &coordinationv1.Endpoint{Ip: "198.51.100.1", Port: 51820, Protocol: "udp"}, Type: "srflx", Priority: 100},
		},
	}, nil
}

func (f *fakeCoordinationServer) RequestRelay(ctx context.Context, req *coordinationv1.RequestRelayRequest) (*coordinationv1.RequestRelayResponse, error) {
	f.captureAuth(ctx)
	if f.relayResp != nil {
		return f.relayResp, nil
	}
	return &coordinationv1.RequestRelayResponse{
		RelayId:  "relay-1",
		Hostname: "relay1.nexusvpn.example.com",
		RelayPort: 51821,
	}, nil
}

func (f *fakeCoordinationServer) captureAuth(ctx context.Context) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("authorization"); len(vals) > 0 {
			f.lastAuthHeader = vals[0]
		}
	}
}

// newTestClient spins up an in-process gRPC server over bufconn and
// returns a Client dialed against it.
func newTestClient(t *testing.T, fake *fakeCoordinationServer, tokens TokenSource) *Client {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	srv := grpc.NewServer()
	coordinationv1.RegisterCoordinationServiceServer(srv, fake)
	go func() {
		_ = srv.Serve(lis)
	}()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	return NewFromConn(conn, tokens)
}

func TestRegisterDevice(t *testing.T) {
	fake := &fakeCoordinationServer{}
	client := newTestClient(t, fake, StaticToken("test-jwt"))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{
		DevicePublicKey: "abc123=",
		NetworkId:       "net-1",
		DeviceName:      "laptop",
		Os:              "linux",
	})
	if err != nil {
		t.Fatalf("RegisterDevice: %v", err)
	}
	if resp.GetAssignedVirtualIp() != "10.77.0.5" {
		t.Fatalf("AssignedVirtualIp = %q, want 10.77.0.5", resp.GetAssignedVirtualIp())
	}
	if fake.lastAuthHeader != "Bearer test-jwt" {
		t.Fatalf("authorization header = %q, want %q", fake.lastAuthHeader, "Bearer test-jwt")
	}
}

func TestRegisterDevice_ServerError(t *testing.T) {
	fake := &fakeCoordinationServer{}
	client := newTestClient(t, fake, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.RegisterDevice(ctx, &coordinationv1.RegisterDeviceRequest{})
	if err == nil {
		t.Fatalf("expected error for missing public key")
	}
}

func TestHeartbeat(t *testing.T) {
	fake := &fakeCoordinationServer{}
	client := newTestClient(t, fake, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.Heartbeat(ctx, &coordinationv1.HeartbeatRequest{DeviceId: "device-1"})
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if !resp.GetOk() || resp.GetNextHeartbeatSeconds() != 15 {
		t.Fatalf("unexpected heartbeat response: %+v", resp)
	}
}

func TestStreamPeerUpdates(t *testing.T) {
	fake := &fakeCoordinationServer{
		peerUpdates: []*coordinationv1.PeerUpdate{
			{Type: coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_JOINED, Peer: &coordinationv1.Peer{DeviceId: "peer-1"}, NetworkId: "net-1"},
			{Type: coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_LEFT, Peer: &coordinationv1.Peer{DeviceId: "peer-1"}, NetworkId: "net-1"},
		},
	}
	client := newTestClient(t, fake, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.StreamPeerUpdates(ctx, "device-1")
	if err != nil {
		t.Fatalf("StreamPeerUpdates: %v", err)
	}

	var received []*coordinationv1.PeerUpdate
	for {
		update, err := stream.Recv()
		if err != nil {
			break
		}
		received = append(received, update)
	}
	if len(received) != 2 {
		t.Fatalf("received %d updates, want 2", len(received))
	}
	if received[0].GetType() != coordinationv1.PeerUpdateType_PEER_UPDATE_TYPE_JOINED {
		t.Fatalf("first update type = %v, want JOINED", received[0].GetType())
	}
}

func TestExchangeICECandidates(t *testing.T) {
	fake := &fakeCoordinationServer{}
	client := newTestClient(t, fake, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.ExchangeICECandidates(ctx, &coordinationv1.ICEExchangeRequest{
		DeviceId:       "device-1",
		TargetDeviceId: "peer-1",
	})
	if err != nil {
		t.Fatalf("ExchangeICECandidates: %v", err)
	}
	if len(resp.GetPeerCandidates()) != 1 {
		t.Fatalf("got %d candidates, want 1", len(resp.GetPeerCandidates()))
	}
}

func TestRequestRelay(t *testing.T) {
	fake := &fakeCoordinationServer{}
	client := newTestClient(t, fake, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := client.RequestRelay(ctx, &coordinationv1.RequestRelayRequest{DeviceId: "device-1", NetworkId: "net-1"})
	if err != nil {
		t.Fatalf("RequestRelay: %v", err)
	}
	if resp.GetRelayId() != "relay-1" {
		t.Fatalf("RelayId = %q, want relay-1", resp.GetRelayId())
	}
}
