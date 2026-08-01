// Package coordination wraps the 5 CoordinationService gRPC RPCs
// (proto/coordination/v1/coordination.proto) that the client agent uses
// to register itself, discover peers, exchange ICE candidates for hole
// punching, and request relay allocation. The generated stubs live in
// internal/coordination/gen (generated from the shared proto with
// protoc-gen-go / protoc-gen-go-grpc; see client/README.md for the exact
// command).
package coordination

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/protogen/coordination/v1"
)

// TokenSource supplies the bearer token to attach to outgoing RPCs (the
// same JWT access token used against the REST API). Implementations
// should handle their own refresh logic; internal/apiclient's token
// manager satisfies this.
type TokenSource interface {
	AccessToken(ctx context.Context) (string, error)
}

// StaticToken is a trivial TokenSource for tests/CLI use.
type StaticToken string

func (s StaticToken) AccessToken(context.Context) (string, error) { return string(s), nil }

// Client wraps a gRPC connection to the control plane's
// CoordinationService.
type Client struct {
	conn   *grpc.ClientConn
	stub   coordinationv1.CoordinationServiceClient
	tokens TokenSource
}

// DialOptions configures Dial.
type DialOptions struct {
	// Insecure disables TLS (only for local development against a plain
	// gRPC server, e.g. `localhost:9090`).
	Insecure bool
	// InsecureSkipVerify skips server certificate verification (TLS
	// stays on, but no chain/hostname validation — for self-signed
	// dev/test deployments only).
	InsecureSkipVerify bool
}

// Dial connects to the coordination gRPC endpoint (host:port).
func Dial(ctx context.Context, target string, tokens TokenSource, opts DialOptions) (*Client, error) {
	var creds credentials.TransportCredentials
	if opts.Insecure {
		creds = insecure.NewCredentials()
	} else {
		creds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: opts.InsecureSkipVerify}) //nolint:gosec
	}

	conn, err := grpc.NewClient(target,
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		return nil, fmt.Errorf("coordination: dial %s: %w", target, err)
	}
	return &Client{
		conn:   conn,
		stub:   coordinationv1.NewCoordinationServiceClient(conn),
		tokens: tokens,
	}, nil
}

// NewFromConn wraps an already-established *grpc.ClientConn (used by
// tests against an in-process bufconn server, and by callers who want
// custom dial options).
func NewFromConn(conn *grpc.ClientConn, tokens TokenSource) *Client {
	return &Client{conn: conn, stub: coordinationv1.NewCoordinationServiceClient(conn), tokens: tokens}
}

// Close closes the underlying gRPC connection.
func (c *Client) Close() error {
	return c.conn.Close()
}

func (c *Client) authContext(ctx context.Context) (context.Context, error) {
	if c.tokens == nil {
		return ctx, nil
	}
	tok, err := c.tokens.AccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("coordination: get access token: %w", err)
	}
	if tok == "" {
		return ctx, nil
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+tok), nil
}

// RegisterDevice registers this device's public key against a network and
// returns its assigned virtual IP and current peer topology.
func (c *Client) RegisterDevice(ctx context.Context, req *coordinationv1.RegisterDeviceRequest) (*coordinationv1.RegisterDeviceResponse, error) {
	ctx, err := c.authContext(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.stub.RegisterDevice(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("coordination: RegisterDevice: %w", err)
	}
	return resp, nil
}

// Heartbeat reports liveness, NAT/endpoint info and traffic counters.
func (c *Client) Heartbeat(ctx context.Context, req *coordinationv1.HeartbeatRequest) (*coordinationv1.HeartbeatResponse, error) {
	ctx, err := c.authContext(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.stub.Heartbeat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("coordination: Heartbeat: %w", err)
	}
	return resp, nil
}

// PeerUpdateStream is the receive side of StreamPeerUpdates.
type PeerUpdateStream interface {
	Recv() (*coordinationv1.PeerUpdate, error)
}

// StreamPeerUpdates opens the server-streaming RPC that pushes peer
// topology changes for all networks this device belongs to. The returned
// stream should be read in a loop until it errors (including on context
// cancellation), at which point callers should back off and reconnect.
func (c *Client) StreamPeerUpdates(ctx context.Context, deviceID string) (PeerUpdateStream, error) {
	ctx, err := c.authContext(ctx)
	if err != nil {
		return nil, err
	}
	stream, err := c.stub.StreamPeerUpdates(ctx, &coordinationv1.StreamPeerUpdatesRequest{DeviceId: deviceID})
	if err != nil {
		return nil, fmt.Errorf("coordination: StreamPeerUpdates: %w", err)
	}
	return stream, nil
}

// ExchangeICECandidates relays this device's STUN-discovered candidates to
// a peer via the control plane and returns the peer's candidates.
func (c *Client) ExchangeICECandidates(ctx context.Context, req *coordinationv1.ICEExchangeRequest) (*coordinationv1.ICEExchangeResponse, error) {
	ctx, err := c.authContext(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.stub.ExchangeICECandidates(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("coordination: ExchangeICECandidates: %w", err)
	}
	return resp, nil
}

// RequestRelay asks the control plane for a relay allocation to fall back
// to when direct connectivity can't be established.
func (c *Client) RequestRelay(ctx context.Context, req *coordinationv1.RequestRelayRequest) (*coordinationv1.RequestRelayResponse, error) {
	ctx, err := c.authContext(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.stub.RequestRelay(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("coordination: RequestRelay: %w", err)
	}
	return resp, nil
}

// DefaultDialTimeout bounds how long Dial-then-first-RPC style startup
// waits before giving up.
const DefaultDialTimeout = 10 * time.Second
