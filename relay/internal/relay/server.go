package relay

import (
	"context"
	"errors"
	"net"
	"time"

	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/relay/internal/token"
)

// Verifier validates relay session tokens presented in BIND frames.
type Verifier interface {
	Verify(raw string) (*token.Claims, error)
}

// Config configures a relay Server.
type Config struct {
	// Conn is the UDP socket to serve on. The caller owns its lifetime.
	Conn *net.UDPConn
	// Verifier authenticates BIND frames.
	Verifier Verifier
	// Sessions holds the live session state.
	Sessions *SessionTable
	// Logger receives structured operational logs.
	Logger *zap.Logger
	// EvictInterval controls how often idle sessions are reaped.
	EvictInterval time.Duration
}

// Server pumps UDP datagrams between relayed peers.
//
// It is deliberately dumb about payloads: after a peer authenticates with a
// control-plane-signed token, its DATA frames are forwarded verbatim to the
// addressed peer. The relay cannot read the traffic — WireGuard's Noise
// handshake terminates on the two clients, not here.
type Server struct {
	cfg Config
}

// NewServer builds a Server.
func NewServer(cfg Config) *Server {
	if cfg.EvictInterval <= 0 {
		cfg.EvictInterval = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = zap.NewNop()
	}
	return &Server{cfg: cfg}
}

// Serve reads datagrams until ctx is cancelled or the socket fails. It runs
// the idle-session janitor alongside the read loop and returns once both
// have stopped.
func (s *Server) Serve(ctx context.Context) error {
	janitorDone := make(chan struct{})
	go func() {
		defer close(janitorDone)
		s.runJanitor(ctx)
	}()

	// Unblock the blocking ReadFromUDP when the context is cancelled.
	go func() {
		<-ctx.Done()
		_ = s.cfg.Conn.SetReadDeadline(time.Now())
	}()

	buf := make([]byte, MaxPacketSize)
	var err error
	for {
		if ctx.Err() != nil {
			break
		}

		n, addr, readErr := s.cfg.Conn.ReadFromUDP(buf)
		if readErr != nil {
			if ctx.Err() != nil {
				break // shutting down
			}
			var netErr net.Error
			if errors.As(readErr, &netErr) && netErr.Timeout() {
				continue
			}
			err = readErr
			break
		}
		if n == 0 {
			continue
		}
		s.handlePacket(buf[:n], addr)
	}

	<-janitorDone
	return err
}

// handlePacket dispatches one datagram by frame type.
func (s *Server) handlePacket(frame []byte, addr *net.UDPAddr) {
	switch frame[0] {
	case FrameBind:
		s.handleBind(frame, addr)
	case FrameData:
		s.handleData(frame, addr)
	case FrameKeepalive:
		s.handleKeepalive(addr)
	default:
		// Unknown or unsolicited traffic (including internet background
		// noise hitting an open UDP port) is dropped without a reply, so the
		// node can't be used as a reflection amplifier.
		s.cfg.Sessions.RecordDropped()
	}
}

func (s *Server) handleBind(frame []byte, addr *net.UDPAddr) {
	deviceID, rawToken, err := DecodeBind(frame)
	if err != nil {
		s.cfg.Sessions.RecordDropped()
		return
	}

	claims, err := s.cfg.Verifier.Verify(rawToken)
	if err != nil {
		s.cfg.Sessions.RecordDropped()
		s.cfg.Logger.Debug("relay_bind_rejected",
			zap.String("addr", addr.String()),
			zap.String("device_id", deviceID.String()),
			zap.Error(err))
		s.reply(addr, EncodeError("unauthorized"))
		return
	}

	// The token names the device it was minted for; a peer cannot bind as
	// somebody else by putting a different ID in the frame header.
	if claims.DeviceID != deviceID.String() {
		s.cfg.Sessions.RecordDropped()
		s.cfg.Logger.Warn("relay_bind_device_mismatch",
			zap.String("addr", addr.String()),
			zap.String("frame_device_id", deviceID.String()),
			zap.String("token_device_id", claims.DeviceID))
		s.reply(addr, EncodeError("device mismatch"))
		return
	}

	if err := s.cfg.Sessions.Bind(claims.SessionKey(), deviceID, addr, claims.ExpiresAt()); err != nil {
		s.cfg.Sessions.RecordDropped()
		s.cfg.Logger.Warn("relay_bind_failed", zap.Error(err))
		s.reply(addr, EncodeError(err.Error()))
		return
	}

	s.cfg.Logger.Debug("relay_bound",
		zap.String("addr", addr.String()),
		zap.String("device_id", deviceID.String()),
		zap.String("network_id", claims.NetworkID))
	s.reply(addr, EncodeBindAck())
}

func (s *Server) handleData(frame []byte, addr *net.UDPAddr) {
	networkID, senderID, ok := s.cfg.Sessions.Lookup(addr)
	if !ok {
		// Never forward for an address that hasn't authenticated.
		s.cfg.Sessions.RecordDropped()
		s.reply(addr, EncodeError("not bound"))
		return
	}

	peerID, _, err := DecodeData(frame)
	if err != nil {
		s.cfg.Sessions.RecordDropped()
		return
	}

	dest, ok := s.cfg.Sessions.Route(networkID, senderID, peerID)
	if !ok {
		// The peer isn't (or is no longer) attached to this relay.
		s.cfg.Sessions.RecordDropped()
		return
	}

	// Stamp the frame with the sender's ID so the receiver knows the origin,
	// then forward the payload untouched.
	rewriteDataPeer(frame, senderID)

	if _, err := s.cfg.Conn.WriteToUDP(frame, dest); err != nil {
		s.cfg.Sessions.RecordDropped()
		s.cfg.Logger.Debug("relay_forward_failed", zap.String("dest", dest.String()), zap.Error(err))
		return
	}
	s.cfg.Sessions.RecordRelayed(len(frame))
}

func (s *Server) handleKeepalive(addr *net.UDPAddr) {
	networkID, deviceID, ok := s.cfg.Sessions.Lookup(addr)
	if !ok {
		s.cfg.Sessions.RecordDropped()
		return
	}
	s.cfg.Sessions.Touch(networkID, deviceID)
}

// reply best-effort sends a control frame back to a peer.
func (s *Server) reply(addr *net.UDPAddr, frame []byte) {
	if _, err := s.cfg.Conn.WriteToUDP(frame, addr); err != nil {
		s.cfg.Logger.Debug("relay_reply_failed", zap.String("addr", addr.String()), zap.Error(err))
	}
}

// runJanitor periodically evicts idle and expired sessions.
func (s *Server) runJanitor(ctx context.Context) {
	ticker := time.NewTicker(s.cfg.EvictInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if removed := s.cfg.Sessions.EvictIdle(now); removed > 0 {
				s.cfg.Logger.Debug("relay_sessions_evicted", zap.Int("count", removed))
			}
		}
	}
}
