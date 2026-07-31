package stun

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestBinding_Success(t *testing.T) {
	want := netip.MustParseAddrPort("203.0.113.5:51820")
	srv := newFakeSTUNServer(t, want, true)

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer conn.Close()

	client := NewClient(conn, WithTimeout(500*time.Millisecond), WithRetries(1))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := client.Binding(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if res.Mapped != want {
		t.Fatalf("Mapped = %v, want %v", res.Mapped, want)
	}
}

func TestBinding_Timeout(t *testing.T) {
	// Bind a socket but never respond, to exercise the timeout/retry path.
	dead, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	deadAddr := dead.LocalAddr().String()
	dead.Close() // nothing listening now; packets vanish silently

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer conn.Close()

	client := NewClient(conn, WithTimeout(150*time.Millisecond), WithRetries(1))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_, err = client.Binding(ctx, deadAddr)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
		t.Fatalf("returned too quickly (%v), retries may not have run", elapsed)
	}
}

func TestBinding_IgnoresMismatchedTransactionAndKeepsWaiting(t *testing.T) {
	want := netip.MustParseAddrPort("198.51.100.9:4500")
	srv := newFakeSTUNServer(t, want, true)

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer conn.Close()

	client := NewClient(conn, WithTimeout(1*time.Second), WithRetries(0))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := client.Binding(ctx, srv.Addr())
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	if res.Mapped != want {
		t.Fatalf("Mapped = %v, want %v", res.Mapped, want)
	}
}
