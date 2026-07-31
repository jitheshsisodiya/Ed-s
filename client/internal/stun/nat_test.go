package stun

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

func openConn(t *testing.T) net.PacketConn {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestClassify_Open(t *testing.T) {
	conn := openConn(t)
	local := conn.LocalAddr().(*net.UDPAddr)
	localAddrPort := netip.AddrPortFrom(netip.MustParseAddr(local.IP.String()), uint16(local.Port))

	// Mapped address equals local address (no NAT) and the server can
	// answer via CHANGE-REQUEST (change-both) => genuinely open.
	primary := newFakeSTUNServer(t, localAddrPort, true)
	secondary := newFakeSTUNServer(t, localAddrPort, true)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := Classify(ctx, conn, primary.Addr(), secondary.Addr(), WithTimeout(300*time.Millisecond), WithRetries(0))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Type != NATOpen {
		t.Fatalf("Type = %v, want %v", res.Type, NATOpen)
	}
}

func TestClassify_FullCone(t *testing.T) {
	conn := openConn(t)
	mapped := netip.MustParseAddrPort("203.0.113.10:40000")

	primary := newFakeSTUNServer(t, mapped, true) // supports change-request => full cone
	secondary := newFakeSTUNServer(t, mapped, true)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := Classify(ctx, conn, primary.Addr(), secondary.Addr(), WithTimeout(300*time.Millisecond), WithRetries(0))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Type != NATFullCone {
		t.Fatalf("Type = %v, want %v", res.Type, NATFullCone)
	}
	if res.PublicAddr != mapped {
		t.Fatalf("PublicAddr = %v, want %v", res.PublicAddr, mapped)
	}
}

func TestClassify_Symmetric(t *testing.T) {
	conn := openConn(t)
	mappedByPrimary := netip.MustParseAddrPort("203.0.113.10:40001")
	mappedBySecondary := netip.MustParseAddrPort("203.0.113.10:40002") // different port => symmetric

	primary := newFakeSTUNServer(t, mappedByPrimary, false) // no change-request support
	secondary := newFakeSTUNServer(t, mappedBySecondary, false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := Classify(ctx, conn, primary.Addr(), secondary.Addr(), WithTimeout(300*time.Millisecond), WithRetries(0))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Type != NATSymmetric {
		t.Fatalf("Type = %v, want %v", res.Type, NATSymmetric)
	}
}

func TestClassify_RestrictedCone(t *testing.T) {
	conn := openConn(t)
	mapped := netip.MustParseAddrPort("203.0.113.10:40003")

	// Consistent mapping across servers (not symmetric), no full-cone
	// change-both support, but change-port-only succeeds => restricted
	// cone. We model this with a server that only supports the request
	// when change-IP is NOT requested (i.e. same IP, different port).
	primary := &fakeSTUNServer{mapped: mapped}
	var err error
	primary.conn, err = net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	primary.closeCh = make(chan struct{})
	t.Cleanup(func() { close(primary.closeCh); primary.conn.Close() })
	go serveRestrictedCone(primary)

	secondary := newFakeSTUNServer(t, mapped, false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := Classify(ctx, conn, primary.Addr(), secondary.Addr(), WithTimeout(300*time.Millisecond), WithRetries(0))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Type != NATRestrictedCone {
		t.Fatalf("Type = %v, want %v", res.Type, NATRestrictedCone)
	}
}

func TestClassify_PortRestrictedCone(t *testing.T) {
	conn := openConn(t)
	mapped := netip.MustParseAddrPort("203.0.113.10:40004")

	// Consistent mapping, no change-request support at all => most
	// conservative classification.
	primary := newFakeSTUNServer(t, mapped, false)
	secondary := newFakeSTUNServer(t, mapped, false)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	res, err := Classify(ctx, conn, primary.Addr(), secondary.Addr(), WithTimeout(300*time.Millisecond), WithRetries(0))
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Type != NATPortRestrictedCone {
		t.Fatalf("Type = %v, want %v", res.Type, NATPortRestrictedCone)
	}
}

func TestClassify_PrimaryUnreachable(t *testing.T) {
	conn := openConn(t)

	dead, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadAddr := dead.LocalAddr().String()
	dead.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = Classify(ctx, conn, deadAddr, "", WithTimeout(150*time.Millisecond), WithRetries(0))
	if err == nil {
		t.Fatalf("expected error for unreachable primary server")
	}
}
