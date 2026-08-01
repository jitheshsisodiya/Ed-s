package agent

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jitheshsisodiya/Ed-s/client/internal/config"
)

func newTestAgent(t *testing.T) *Agent {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a, err := New()
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestDeviceKeyIsPerNetwork is the whole point. The control plane looks a
// device up by public key alone and records it against one network, so a key
// reused on a second network is refused as belonging to somebody else — which
// made "join two networks" impossible.
func TestDeviceKeyIsPerNetwork(t *testing.T) {
	a := newTestAgent(t)

	first, err := a.deviceKey("network-a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.deviceKey("network-b")
	if err != nil {
		t.Fatal(err)
	}

	if first.PublicKey == second.PublicKey {
		t.Fatal("two networks were given the same identity; the server will refuse the second")
	}
	if first.IsZero() || second.IsZero() {
		t.Fatal("a network was left without an identity")
	}
}

// TestDeviceKeyIsStable: the key is this machine's address on the network.
// Minting a new one per connection would make it a different machine every
// time, with a different address, leaving a trail of dead peers behind it.
func TestDeviceKeyIsStable(t *testing.T) {
	a := newTestAgent(t)

	first, err := a.deviceKey("network-a")
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.deviceKey("network-a")
	if err != nil {
		t.Fatal(err)
	}
	if first.PublicKey != again.PublicKey {
		t.Fatal("the same network was given a new identity on the second connection")
	}
}

// TestFirstNetworkAdoptsTheExistingKey: an installation that already
// registered under the old installation-wide key keeps its identity, and with
// it its address, instead of coming back as a new machine.
func TestFirstNetworkAdoptsTheExistingKey(t *testing.T) {
	a := newTestAgent(t)

	// An installation that has already been used: Load on a missing file
	// hands back a throwaway key it never writes, so the config has to be
	// persisted for there to be an installation key at all.
	cfg, err := a.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.Save(cfg); err != nil {
		t.Fatal(err)
	}
	existing := cfg.Keypair
	if existing.IsZero() {
		t.Fatal("expected a generated installation key to adopt")
	}

	adopted, err := a.deviceKey("network-a")
	if err != nil {
		t.Fatal(err)
	}
	if adopted.PublicKey != existing.PublicKey {
		t.Fatalf("first network minted a new key instead of adopting the existing one")
	}

	// Only one network can hold it — that is the constraint the server
	// enforces, and the reason the second must not take it too.
	other, err := a.deviceKey("network-b")
	if err != nil {
		t.Fatal(err)
	}
	if other.PublicKey == existing.PublicKey {
		t.Fatal("a second network adopted the same key the first is using")
	}
}

// TestResetDeviceKeyGivesUpTheAddressToo: the address was assigned to the old
// identity. Keeping it after the key is replaced would mean claiming an
// address the server has given to a device this installation no longer is.
func TestResetDeviceKeyGivesUpTheAddressToo(t *testing.T) {
	a := newTestAgent(t)

	before, err := a.deviceKey("network-a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.Update(func(c *config.Config) error {
		c.Networks["network-a"].VirtualIP = "10.77.0.2"
		c.Networks["network-a"].DeviceID = "old-device"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.resetDeviceKey("network-a"); err != nil {
		t.Fatal(err)
	}

	after, err := a.deviceKey("network-a")
	if err != nil {
		t.Fatal(err)
	}
	if after.PublicKey == before.PublicKey {
		t.Fatal("reset kept the key the server had already refused")
	}

	cfg, err := a.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Networks["network-a"].VirtualIP; got != "" {
		t.Fatalf("kept address %q from the old identity", got)
	}
	if got := cfg.Networks["network-a"].DeviceID; got != "" {
		t.Fatalf("kept device id %q from the old identity", got)
	}
}

// TestKeysSurviveReopening: the config is the only place this identity lives.
func TestKeysSurviveReopening(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)

	first, err := New()
	if err != nil {
		t.Fatal(err)
	}
	want, err := first.deviceKey("network-a")
	if err != nil {
		t.Fatal(err)
	}

	second, err := New()
	if err != nil {
		t.Fatal(err)
	}
	got, err := second.deviceKey("network-a")
	if err != nil {
		t.Fatal(err)
	}
	if got.PublicKey != want.PublicKey {
		t.Fatal("the identity did not survive reopening the config")
	}
}

func TestIsStaleKeyRejection(t *testing.T) {
	denied := status.Error(codes.PermissionDenied, "forbidden")

	if !isStaleKeyRejection(denied) {
		t.Fatal("a bare PermissionDenied was not recognised")
	}
	// This is the shape the error actually arrives in: the coordination
	// client wraps it, then Tunnel.Register wraps that. A check that only
	// worked on the bare status would pass a test written against the bare
	// status and never once fire in the running app.
	wrapped := fmt.Errorf("tunnel: register device: %w",
		fmt.Errorf("coordination: RegisterDevice: %w", denied))
	if !isStaleKeyRejection(wrapped) {
		t.Fatal("the rejection was not recognised through the wrapping it " +
			"actually arrives in — the retry would never happen")
	}
	if isStaleKeyRejection(status.Error(codes.Unavailable, "server down")) {
		t.Fatal("an unreachable server was mistaken for a rejected key")
	}
	if isStaleKeyRejection(errors.New("something else")) {
		t.Fatal("an ordinary error was mistaken for a rejected key")
	}
}
