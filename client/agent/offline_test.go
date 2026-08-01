package agent

import (
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	coordinationv1 "github.com/jitheshsisodiya/Ed-s/protogen/coordination/v1"
)

// TestControlPlaneUnreachable is the check the whole safety of this rests on.
//
// Connecting from a remembered configuration is right when nothing answered:
// what was true last time is the best information available. It is wrong when
// the server answered and said no — a device removed from the network, or a
// key no longer accepted, has been told to go away, and honouring a cached
// answer instead would be reconnecting anyway.
func TestControlPlaneUnreachable(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nothing there", status.Error(codes.Unavailable, "connection refused"), true},
		{"took too long", status.Error(codes.DeadlineExceeded, "timeout"), true},
		{"never reached a server", status.Error(codes.Unknown, "transport failure"), true},
		{"a timeout from the network stack", &net.OpError{
			Op: "dial", Err: timeoutError{},
		}, true},

		// Each of these is the server answering. None of them is a reason to
		// carry on with an old configuration.
		{"removed from the network", status.Error(codes.PermissionDenied, "forbidden"), false},
		{"session expired", status.Error(codes.Unauthenticated, "token expired"), false},
		{"network deleted", status.Error(codes.NotFound, "no such network"), false},
		{"malformed request", status.Error(codes.InvalidArgument, "bad id"), false},
		{"server broke", status.Error(codes.Internal, "internal error"), false},

		{"no error at all", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := controlPlaneUnreachable(tc.err); got != tc.want {
				t.Errorf("controlPlaneUnreachable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// The error arrives wrapped by the time it reaches this decision, as it does
// everywhere else in this package.
func TestControlPlaneUnreachableSeesThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("tunnel: register device: %w",
		fmt.Errorf("coordination: RegisterDevice: %w",
			status.Error(codes.Unavailable, "connection refused")))
	if !controlPlaneUnreachable(wrapped) {
		t.Fatal("a wrapped unavailable was not recognised, so the fallback " +
			"would never happen in the running app")
	}
}

func TestRememberAndRecallARegistration(t *testing.T) {
	a := newTestAgent(t)

	want := &coordinationv1.RegisterDeviceResponse{
		DeviceId:          "device-1",
		AssignedVirtualIp: "10.77.0.3",
		NetworkCidr:       "10.77.0.0/24",
		DnsServers:        []string{"10.77.0.1"},
		ExistingPeers: []*coordinationv1.Peer{
			{DeviceId: "peer-1", PublicKey: "key-1", VirtualIp: "10.77.0.2"},
		},
	}
	a.rememberRegistration("network-a", want)

	cfg, err := a.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	got, savedAt, ok := a.cachedRegistration(cfg, "network-a")
	if !ok {
		t.Fatal("nothing was remembered")
	}
	if got.GetAssignedVirtualIp() != want.GetAssignedVirtualIp() {
		t.Fatalf("address = %q", got.GetAssignedVirtualIp())
	}
	if got.GetNetworkCidr() != want.GetNetworkCidr() {
		t.Fatalf("cidr = %q", got.GetNetworkCidr())
	}
	// The peers are the part that matters: without them the tunnel comes up
	// with an address and nothing to talk to, which looks like working.
	if len(got.GetExistingPeers()) != 1 {
		t.Fatalf("remembered %d peers, want 1", len(got.GetExistingPeers()))
	}
	if got.GetExistingPeers()[0].GetPublicKey() != "key-1" {
		t.Fatal("the peer's key did not survive")
	}
	if savedAt.IsZero() {
		t.Fatal("no timestamp, so nothing can say how old this is")
	}
}

// A cache missing the one thing that cannot be worked around must not be
// offered: with no address there is no interface to configure.
func TestAnAddresslessCacheIsNotOffered(t *testing.T) {
	a := newTestAgent(t)
	a.rememberRegistration("network-a", &coordinationv1.RegisterDeviceResponse{
		DeviceId: "device-1",
	})

	cfg, err := a.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := a.cachedRegistration(cfg, "network-a"); ok {
		t.Fatal("offered a cache with no address to configure")
	}
}

func TestNoCacheForAnUnknownNetwork(t *testing.T) {
	a := newTestAgent(t)
	cfg, err := a.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := a.cachedRegistration(cfg, "never-seen"); ok {
		t.Fatal("invented a cache for a network this device has never joined")
	}
}

func TestCacheSurvivesReopening(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	first, err := New()
	if err != nil {
		t.Fatal(err)
	}
	first.rememberRegistration("network-a", &coordinationv1.RegisterDeviceResponse{
		DeviceId: "device-1", AssignedVirtualIp: "10.77.0.3", NetworkCidr: "10.77.0.0/24",
	})

	second, err := New()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := second.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := second.cachedRegistration(cfg, "network-a"); !ok {
		t.Fatal("the remembered configuration did not survive a restart, " +
			"which is the only time it is ever needed")
	}
}

func TestDescribeCacheAge(t *testing.T) {
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{
		{5 * time.Minute, "5 minutes old"},
		{3 * time.Hour, "3 hours old"},
		{72 * time.Hour, "3 days old"},
	} {
		if got := describeCacheAge(time.Now().Add(-tc.age)); got != tc.want {
			t.Errorf("age %v described as %q, want %q", tc.age, got, tc.want)
		}
	}
	if got := describeCacheAge(time.Time{}); got != "unknown age" {
		t.Errorf("a missing timestamp described as %q", got)
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "i/o timeout" }
func (timeoutError) Timeout() bool { return true }

var _ error = timeoutError{}
