// Package relayproxy bridges the local WireGuard engine to a NexusVPN relay
// node.
//
// WireGuard sends plain UDP datagrams to whatever endpoint a peer is
// configured with, but a relay expects those datagrams wrapped in its own
// framing (see relay/README.md). Rather than replacing wireguard-go's
// conn.Bind, this package runs a small loopback UDP proxy per relayed peer:
//
//	WireGuard  --raw-->  127.0.0.1:P  --DATA frame-->  relay  -->  peer
//	WireGuard  <--raw--  127.0.0.1:P  <--DATA frame--  relay  <--  peer
//
// The peer's WireGuard endpoint is set to 127.0.0.1:P, so the engine needs
// no knowledge of relaying at all. Payloads pass through untouched — the
// proxy never inspects or modifies the encrypted WireGuard datagram.
package relayproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// KeepaliveInterval is how often a KEEPALIVE frame is sent to the relay to
// hold the NAT mapping open. It is comfortably below the relay's default
// five-minute idle eviction.
const KeepaliveInterval = 25 * time.Second

// Options configures a Proxy.
type Options struct {
	// RelayAddr is the relay node's UDP address.
	RelayAddr *net.UDPAddr
	// SessionToken authorizes this device on the relay, as issued by the
	// control plane's RequestRelay RPC.
	SessionToken string
	// SelfDeviceID is this device's ID (must match the token's subject).
	SelfDeviceID uuid.UUID
	// PeerDeviceID is the device this proxy carries traffic to.
	PeerDeviceID uuid.UUID
	// Logf, if set, receives diagnostic messages.
	Logf func(format string, args ...any)
}

// Proxy relays one peer's traffic through a relay node.
type Proxy struct {
	opts Options

	// local is the loopback socket WireGuard sends to.
	local *net.UDPConn
	// remote is the socket used to talk to the relay.
	remote *net.UDPConn

	// wgAddr is the WireGuard engine's source address, learned from the
	// first packet it sends, so replies can be delivered back to it.
	wgAddr atomic.Pointer[net.UDPAddr]

	bytesSent atomic.Uint64
	bytesRecv atomic.Uint64
	bound     atomic.Bool

	closeOnce sync.Once
	closed    chan struct{}
	wg        sync.WaitGroup
}

// New creates a Proxy, binding both sockets but not yet pumping traffic.
// Call Start to begin.
func New(opts Options) (*Proxy, error) {
	if opts.RelayAddr == nil {
		return nil, errors.New("relayproxy: relay address is required")
	}
	if opts.SessionToken == "" {
		return nil, errors.New("relayproxy: session token is required")
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}

	// Loopback-only: nothing outside this host should be able to inject
	// traffic into the tunnel through the proxy.
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		return nil, fmt.Errorf("relayproxy: bind loopback socket: %w", err)
	}

	remote, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	if err != nil {
		_ = local.Close()
		return nil, fmt.Errorf("relayproxy: bind relay socket: %w", err)
	}

	return &Proxy{
		opts:   opts,
		local:  local,
		remote: remote,
		closed: make(chan struct{}),
	}, nil
}

// LocalEndpoint is the loopback address to configure as the peer's
// WireGuard endpoint.
func (p *Proxy) LocalEndpoint() *net.UDPAddr {
	return p.local.LocalAddr().(*net.UDPAddr)
}

// Bound reports whether the relay has acknowledged this device's BIND.
func (p *Proxy) Bound() bool { return p.bound.Load() }

// Stats returns bytes sent to and received from the relay.
func (p *Proxy) Stats() (sent, received uint64) {
	return p.bytesSent.Load(), p.bytesRecv.Load()
}

// Start binds to the relay and begins pumping traffic in both directions
// until ctx is cancelled or Close is called.
func (p *Proxy) Start(ctx context.Context) error {
	if err := p.sendBind(); err != nil {
		return err
	}

	p.wg.Add(3)
	go func() { defer p.wg.Done(); p.pumpLocalToRelay(ctx) }()
	go func() { defer p.wg.Done(); p.pumpRelayToLocal(ctx) }()
	go func() { defer p.wg.Done(); p.keepalive(ctx) }()

	// Unblock the socket reads on shutdown.
	go func() {
		select {
		case <-ctx.Done():
		case <-p.closed:
		}
		_ = p.local.SetReadDeadline(time.Now())
		_ = p.remote.SetReadDeadline(time.Now())
	}()

	return nil
}

// sendBind announces this device to the relay.
func (p *Proxy) sendBind() error {
	frame := encodeBind(p.opts.SelfDeviceID, p.opts.SessionToken)
	if _, err := p.remote.WriteToUDP(frame, p.opts.RelayAddr); err != nil {
		return fmt.Errorf("relayproxy: send BIND: %w", err)
	}
	return nil
}

// pumpLocalToRelay forwards WireGuard datagrams out to the relay.
func (p *Proxy) pumpLocalToRelay(ctx context.Context) {
	buf := make([]byte, maxPacketSize)
	for {
		if ctx.Err() != nil {
			return
		}
		n, src, err := p.local.ReadFromUDP(buf)
		if err != nil {
			if p.isShuttingDown(ctx, err) {
				return
			}
			continue
		}
		if n == 0 {
			continue
		}

		// Learn where to send replies. WireGuard uses a single socket per
		// device, so this address is stable for the life of the proxy.
		p.wgAddr.Store(src)

		frame := encodeData(p.opts.PeerDeviceID, buf[:n])
		if _, err := p.remote.WriteToUDP(frame, p.opts.RelayAddr); err != nil {
			p.opts.Logf("relayproxy: forward to relay failed: %v", err)
			continue
		}
		p.bytesSent.Add(uint64(n))
	}
}

// pumpRelayToLocal forwards relayed datagrams back into WireGuard.
func (p *Proxy) pumpRelayToLocal(ctx context.Context) {
	buf := make([]byte, maxPacketSize)
	for {
		if ctx.Err() != nil {
			return
		}
		n, _, err := p.remote.ReadFromUDP(buf)
		if err != nil {
			if p.isShuttingDown(ctx, err) {
				return
			}
			continue
		}
		if n == 0 {
			continue
		}

		switch buf[0] {
		case frameBindAck:
			p.bound.Store(true)
			p.opts.Logf("relayproxy: bound to relay %s", p.opts.RelayAddr)

		case frameData:
			_, payload, err := decodeData(buf[:n])
			if err != nil {
				continue
			}
			dst := p.wgAddr.Load()
			if dst == nil {
				// WireGuard hasn't sent anything yet, so we don't know where
				// to deliver. Dropping is correct: it will retry its handshake.
				continue
			}
			if _, err := p.local.WriteToUDP(payload, dst); err != nil {
				p.opts.Logf("relayproxy: deliver to wireguard failed: %v", err)
				continue
			}
			p.bytesRecv.Add(uint64(len(payload)))

		case frameError:
			reason, _ := decodeError(buf[:n])
			p.bound.Store(false)
			p.opts.Logf("relayproxy: relay error: %s", reason)
			// A bind that lapsed (token expiry, session eviction) is
			// recoverable: re-announce so traffic can resume.
			if err := p.sendBind(); err != nil {
				p.opts.Logf("relayproxy: re-bind failed: %v", err)
			}
		}
	}
}

// keepalive holds the relay session and the NAT mapping open.
func (p *Proxy) keepalive(ctx context.Context) {
	ticker := time.NewTicker(KeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.closed:
			return
		case <-ticker.C:
			frame := encodeKeepalive()
			if !p.bound.Load() {
				// Not (or no longer) bound — re-announce instead.
				frame = encodeBind(p.opts.SelfDeviceID, p.opts.SessionToken)
			}
			if _, err := p.remote.WriteToUDP(frame, p.opts.RelayAddr); err != nil {
				p.opts.Logf("relayproxy: keepalive failed: %v", err)
			}
		}
	}
}

// isShuttingDown distinguishes a shutdown-induced read error from a
// transient one.
func (p *Proxy) isShuttingDown(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return true
	}
	select {
	case <-p.closed:
		return true
	default:
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		// A deadline we set during shutdown, or a genuine timeout; either
		// way, only stop if we're actually closing (checked above).
		return false
	}
	return errors.Is(err, net.ErrClosed)
}

// Close stops the proxy and releases both sockets.
func (p *Proxy) Close() error {
	p.closeOnce.Do(func() {
		close(p.closed)
		_ = p.local.SetReadDeadline(time.Now())
		_ = p.remote.SetReadDeadline(time.Now())
	})
	p.wg.Wait()

	err1 := p.local.Close()
	err2 := p.remote.Close()
	if err1 != nil {
		return err1
	}
	return err2
}
