// Package stun implements a RFC 5389 STUN client used to discover this
// device's public (server-reflexive) UDP endpoint and classify the NAT it
// sits behind, using the classic multi-STUN-server comparison technique
// (RFC 3489 §10.1-style Test I/II/III). Message encoding/decoding is
// provided by github.com/pion/stun/v3; this package owns the UDP
// transport (so it can share a socket with the hole-punch/WireGuard
// logic) and the NAT classification algorithm.
package stun

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"time"

	pionstun "github.com/pion/stun/v3"
)

// DefaultTimeout is the per-attempt response wait before retrying.
const DefaultTimeout = 1500 * time.Millisecond

// DefaultRetries is how many additional attempts are made after the first.
const DefaultRetries = 2

// DefaultServers is a small set of public STUN servers used when the
// caller/config doesn't override them. At least two independent hosts are
// required for NAT-type classification.
var DefaultServers = []string{
	"stun.l.google.com:19302",
	"stun1.l.google.com:19302",
	"stun.cloudflare.com:3478",
}

// ErrTimeout is returned when no STUN response is received after all
// retries are exhausted.
var ErrTimeout = errors.New("stun: request timed out")

// Client performs STUN Binding requests over a caller-supplied PacketConn.
// Using an injected connection (rather than dialing internally) lets the
// tunnel orchestrator reuse the exact same local UDP socket for STUN
// discovery and later WireGuard hole punching, which matters for NAT
// mapping consistency.
type Client struct {
	conn    net.PacketConn
	timeout time.Duration
	retries int
}

// Option configures a Client.
type Option func(*Client)

// WithTimeout overrides the per-attempt response timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithRetries overrides the retry count (attempts beyond the first).
func WithRetries(n int) Option {
	return func(c *Client) { c.retries = n }
}

// NewClient constructs a Client bound to conn.
func NewClient(conn net.PacketConn, opts ...Option) *Client {
	c := &Client{conn: conn, timeout: DefaultTimeout, retries: DefaultRetries}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BindingResult is the outcome of a successful Binding request.
type BindingResult struct {
	// Mapped is the server-reflexive address the STUN server observed
	// this request as coming from.
	Mapped netip.AddrPort
	// From is the address the response was received from (i.e. which
	// STUN server / interface answered).
	From netip.AddrPort
}

// Binding performs a plain STUN Binding request/response exchange against
// server (host:port), retrying up to c.retries additional times on
// timeout.
func (c *Client) Binding(ctx context.Context, server string) (BindingResult, error) {
	return c.bindingRequest(ctx, server, nil)
}

// bindingWithChangeRequest performs a Binding request carrying the classic
// (RFC 3489) CHANGE-REQUEST attribute, asking the STUN server to reply
// from a different IP and/or port than it received the request on. This
// is how full-cone vs. restricted-cone vs. port-restricted-cone NAT types
// are distinguished. Many modern public STUN servers ignore or don't
// support this attribute, in which case the request simply times out,
// which callers interpret as "server declined/doesn't support the test".
func (c *Client) bindingWithChangeRequest(ctx context.Context, server string, changeIP, changePort bool) (BindingResult, error) {
	var flags uint32
	if changeIP {
		flags |= 0x04
	}
	if changePort {
		flags |= 0x02
	}
	val := make([]byte, 4)
	binary.BigEndian.PutUint32(val, flags)
	return c.bindingRequest(ctx, server, val)
}

func (c *Client) bindingRequest(ctx context.Context, server string, changeRequestAttr []byte) (BindingResult, error) {
	raddr, err := net.ResolveUDPAddr("udp", server)
	if err != nil {
		return BindingResult{}, fmt.Errorf("stun: resolve %q: %w", server, err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return BindingResult{}, err
		}

		msg := new(pionstun.Message)
		if err := msg.Build(pionstun.TransactionID, pionstun.BindingRequest); err != nil {
			return BindingResult{}, fmt.Errorf("stun: build request: %w", err)
		}
		if changeRequestAttr != nil {
			msg.Add(pionstun.AttrChangeRequest, changeRequestAttr)
			msg.WriteLength()
		}

		if dl, ok := ctx.Deadline(); ok {
			_ = c.conn.SetWriteDeadline(dl)
		}
		if _, err := c.conn.WriteTo(msg.Raw, raddr); err != nil {
			return BindingResult{}, fmt.Errorf("stun: send to %s: %w", server, err)
		}

		deadline := time.Now().Add(c.timeout)
		if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
			deadline = dl
		}
		_ = c.conn.SetReadDeadline(deadline)

		result, err := c.awaitResponse(msg.TransactionID)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = ErrTimeout
	}
	return BindingResult{}, lastErr
}

// awaitResponse reads packets until it finds a STUN success response
// matching wantTxID, the read deadline is hit, or a hard read error
// occurs. Non-matching / non-STUN packets are ignored so an in-flight
// exchange on a shared socket doesn't get derailed by unrelated traffic.
func (c *Client) awaitResponse(wantTxID [pionstun.TransactionIDSize]byte) (BindingResult, error) {
	buf := make([]byte, 1500)
	for {
		n, from, err := c.conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return BindingResult{}, ErrTimeout
			}
			return BindingResult{}, err
		}
		if !pionstun.IsMessage(buf[:n]) {
			continue
		}
		resp := new(pionstun.Message)
		resp.Raw = append([]byte(nil), buf[:n]...)
		if err := resp.Decode(); err != nil {
			continue
		}
		if resp.TransactionID != wantTxID {
			continue // stray/late response to a previous attempt
		}
		if resp.Type.Class != pionstun.ClassSuccessResponse {
			return BindingResult{}, fmt.Errorf("stun: error response from server (class=%v)", resp.Type.Class)
		}

		var xorAddr pionstun.XORMappedAddress
		var mapped netip.AddrPort
		if err := xorAddr.GetFrom(resp); err == nil {
			ip, ok := netip.AddrFromSlice(xorAddr.IP)
			if !ok {
				return BindingResult{}, fmt.Errorf("stun: invalid XOR-MAPPED-ADDRESS IP")
			}
			mapped = netip.AddrPortFrom(ip.Unmap(), uint16(xorAddr.Port))
		} else {
			var addr pionstun.MappedAddress
			if err := addr.GetFrom(resp); err != nil {
				return BindingResult{}, fmt.Errorf("stun: response has no mapped address: %w", err)
			}
			ip, ok := netip.AddrFromSlice(addr.IP)
			if !ok {
				return BindingResult{}, fmt.Errorf("stun: invalid MAPPED-ADDRESS IP")
			}
			mapped = netip.AddrPortFrom(ip.Unmap(), uint16(addr.Port))
		}

		fromAddrPort, err := udpAddrToAddrPort(from)
		if err != nil {
			return BindingResult{}, err
		}
		return BindingResult{Mapped: mapped, From: fromAddrPort}, nil
	}
}

func udpAddrToAddrPort(addr net.Addr) (netip.AddrPort, error) {
	udpAddr, ok := addr.(*net.UDPAddr)
	if !ok {
		a, err := netip.ParseAddrPort(addr.String())
		if err != nil {
			return netip.AddrPort{}, fmt.Errorf("stun: parse peer addr %q: %w", addr.String(), err)
		}
		return a, nil
	}
	ip, ok := netip.AddrFromSlice(udpAddr.IP)
	if !ok {
		return netip.AddrPort{}, fmt.Errorf("stun: invalid source IP %v", udpAddr.IP)
	}
	return netip.AddrPortFrom(ip.Unmap(), uint16(udpAddr.Port)), nil
}
