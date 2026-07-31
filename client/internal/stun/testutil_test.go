package stun

import (
	"net"
	"net/netip"
	"testing"

	pionstun "github.com/pion/stun/v3"
)

// fakeSTUNServer is a minimal RFC 5389 Binding-request responder used to
// exercise NAT classification deterministically, without depending on
// real public STUN infrastructure or actually sitting behind a NAT. It
// reports a caller-configured "mapped" address (rather than the request's
// real source address) so tests can simulate arbitrary NAT behavior, and
// can optionally ignore CHANGE-REQUEST attributes to simulate a NAT/
// firewall that blocks unsolicited traffic from an unexpected source.
type fakeSTUNServer struct {
	conn                 net.PacketConn
	mapped               netip.AddrPort
	supportChangeRequest bool
	ignoreAllRequests    bool
	closeCh              chan struct{}
}

func newFakeSTUNServer(t *testing.T, mapped netip.AddrPort, supportChangeRequest bool) *fakeSTUNServer {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen fake stun server: %v", err)
	}
	s := &fakeSTUNServer{
		conn:                 conn,
		mapped:               mapped,
		supportChangeRequest: supportChangeRequest,
		closeCh:              make(chan struct{}),
	}
	go s.serve()
	t.Cleanup(func() {
		close(s.closeCh)
		conn.Close()
	})
	return s
}

func (s *fakeSTUNServer) Addr() string {
	return s.conn.LocalAddr().String()
}

func (s *fakeSTUNServer) serve() {
	buf := make([]byte, 1500)
	for {
		select {
		case <-s.closeCh:
			return
		default:
		}
		n, from, err := s.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if s.ignoreAllRequests {
			continue
		}
		if !pionstun.IsMessage(buf[:n]) {
			continue
		}
		req := new(pionstun.Message)
		req.Raw = append([]byte(nil), buf[:n]...)
		if err := req.Decode(); err != nil {
			continue
		}
		if req.Contains(pionstun.AttrChangeRequest) && !s.supportChangeRequest {
			// Simulate a server that can't honor the (legacy, optional)
			// change-IP/change-port test.
			continue
		}

		resp := new(pionstun.Message)
		if err := resp.Build(pionstun.NewTransactionIDSetter(req.TransactionID), pionstun.BindingSuccess); err != nil {
			continue
		}
		xorAddr := pionstun.XORMappedAddress{IP: s.mapped.Addr().AsSlice(), Port: int(s.mapped.Port())}
		if err := xorAddr.AddTo(resp); err != nil {
			continue
		}
		resp.WriteLength()

		_, _ = s.conn.WriteTo(resp.Raw, from)
	}
}

// serveRestrictedCone runs a fake STUN server that answers plain Binding
// requests and CHANGE-REQUEST(change-port-only) requests, but ignores
// CHANGE-REQUEST(change-IP) requests — modeling a restricted-cone NAT
// (filters by peer IP but not port) for TestClassify_RestrictedCone.
func serveRestrictedCone(s *fakeSTUNServer) {
	buf := make([]byte, 1500)
	for {
		select {
		case <-s.closeCh:
			return
		default:
		}
		n, from, err := s.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if !pionstun.IsMessage(buf[:n]) {
			continue
		}
		req := new(pionstun.Message)
		req.Raw = append([]byte(nil), buf[:n]...)
		if err := req.Decode(); err != nil {
			continue
		}
		if raw, ok := req.Attributes.Get(pionstun.AttrChangeRequest); ok && len(raw.Value) == 4 {
			flags := uint32(raw.Value[0])<<24 | uint32(raw.Value[1])<<16 | uint32(raw.Value[2])<<8 | uint32(raw.Value[3])
			changeIP := flags&0x04 != 0
			if changeIP {
				continue // deny change-IP requests
			}
		}

		resp := new(pionstun.Message)
		if err := resp.Build(pionstun.NewTransactionIDSetter(req.TransactionID), pionstun.BindingSuccess); err != nil {
			continue
		}
		xorAddr := pionstun.XORMappedAddress{IP: s.mapped.Addr().AsSlice(), Port: int(s.mapped.Port())}
		if err := xorAddr.AddTo(resp); err != nil {
			continue
		}
		resp.WriteLength()

		_, _ = s.conn.WriteTo(resp.Raw, from)
	}
}
