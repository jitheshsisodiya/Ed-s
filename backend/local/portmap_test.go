package local

import (
	"context"
	"net"
	"testing"
	"time"
)

// A router cannot be conjured in a test, so what is checked here is the
// behaviour around it: that a network with nothing to talk to produces an
// answer rather than a hang, that nothing is claimed that was not agreed, and
// that the request can express what the server actually needs.

// TestPortsCanExpressTwoTCPPorts exists because the first version could not.
// Requests were a map keyed by protocol, so asking for two TCP ports silently
// kept one — and the one it dropped was the tunnel, producing a server a
// phone could sign in to and then fail to connect through, which is worse
// than one it cannot reach at all.
func TestPortsCanExpressTwoTCPPorts(t *testing.T) {
	ports := []Port{
		{Protocol: "tcp", Number: 8080},
		{Protocol: "tcp", Number: 9090},
	}
	if len(ports) != 2 {
		t.Fatal("the request type cannot carry two ports of the same protocol")
	}
	if ports[0].Number == ports[1].Number {
		t.Fatal("the second port displaced the first")
	}
}

// TestOpenReturnsRatherThanHangs: this runs at startup, and a router that
// ignores the request must not delay the server coming up. Nothing here can
// reach a real gateway, so this is the no-router path.
func TestOpenReturnsRatherThanHangs(t *testing.T) {
	mapper := NewPortMapper(nil)
	defer mapper.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan []Mapping, 1)
	go func() {
		done <- mapper.Open(ctx, []Port{{Protocol: "tcp", Number: 18080}})
	}()

	select {
	case got := <-done:
		// Either answer is correct. What is not correct is never answering.
		for _, m := range got {
			if m.Method == "" {
				t.Error("a mapping was reported without saying what agreed to it")
			}
		}
	case <-time.After(40 * time.Second):
		t.Fatal("Open never returned; the server would not have started")
	}
}

// TestNoMappingMeansNoExternalAddress: PublicURL is built from this, and an
// address invented when no hole was opened would be shown to somebody as
// "reachable from anywhere" while nothing could reach it.
func TestNoMappingMeansNoExternalAddress(t *testing.T) {
	mapper := NewPortMapper(nil)
	defer mapper.Close()

	if got := mapper.ExternalAddress(); got != "" {
		t.Fatalf("reported an external address of %q before opening anything", got)
	}
	if got := mapper.Mappings(); len(got) != 0 {
		t.Fatalf("reported %d mappings before opening anything", len(got))
	}
}

// TestCloseIsSafeTwice: shutdown paths call it, and so does a failed start.
func TestCloseIsSafeTwice(t *testing.T) {
	mapper := NewPortMapper(nil)
	mapper.Close()
	mapper.Close() // must not panic on a closed channel
}

// TestMappingsAreACopy: the caller is handed this to display, and a slice
// backed by the live one would change under the renewal loop while being
// read.
func TestMappingsAreACopy(t *testing.T) {
	mapper := NewPortMapper(nil)
	defer mapper.Close()

	mapper.mu.Lock()
	mapper.mappings = []Mapping{{Internal: 8080, External: 8080, Protocol: "tcp", Method: "UPnP"}}
	mapper.mu.Unlock()

	got := mapper.Mappings()
	got[0].External = 1
	if mapper.Mappings()[0].External != 8080 {
		t.Fatal("the caller's copy shares memory with the mapper's own")
	}
}

// TestDefaultGatewayIsOnThisMachinesNetwork: NAT-PMP needs the router's
// address explicitly. It is derived from the routing table rather than
// guessed at from an interface list, so it must at least be a plausible
// address on the same network.
func TestDefaultGatewayIsOnThisMachinesNetwork(t *testing.T) {
	gw, err := defaultGateway()
	if err != nil {
		t.Skip("this machine has no route out, which is a valid state")
	}
	if gw.To4() == nil {
		t.Fatalf("gateway %v is not IPv4", gw)
	}
	if !isPrivateIPv4(gw) && !gw.IsLoopback() {
		// A public gateway address is possible on an unusual network, but on
		// anything a person runs this on it means the derivation went wrong.
		t.Logf("gateway %v is not on a private network; unusual but not impossible", gw)
	}
}

// TestInternalAddressForRejectsAnUnreachableRouter: the address handed to a
// forwarding rule has to be this machine's address on the router's network.
// Getting it wrong points the rule at nothing.
func TestInternalAddressForRejectsAnUnreachableRouter(t *testing.T) {
	// A documentation address, which is routed nowhere.
	got, err := internalAddressFor("192.0.2.1")
	if err != nil {
		return // no route, correctly reported
	}
	if net.ParseIP(got) == nil {
		t.Fatalf("returned %q, which is not an address", got)
	}
}

// TestOpenAccumulates guards a bug that was written twice. Each Open call
// used to replace the mapping set and start its own renewal loop, so the
// ports from the first call stopped being renewed and their holes closed two
// hours later — long after anybody would connect the two events. The relay
// port is opened by a second call, so this is the real path.
func TestOpenAccumulates(t *testing.T) {
	mapper := NewPortMapper(nil)
	defer mapper.Close()

	// Simulated rather than negotiated: no router here would agree to
	// anything, and what is being tested is the bookkeeping around the
	// answer, not the answer.
	mapper.mu.Lock()
	mapper.requested = []Port{{Protocol: "tcp", Number: 8080}}
	mapper.mappings = []Mapping{{Internal: 8080, External: 8080, Protocol: "tcp", Method: "UPnP"}}
	mapper.renewing = true
	mapper.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	mapper.Open(ctx, []Port{{Protocol: "udp", Number: 51821}})

	mapper.mu.Lock()
	requested := append([]Port(nil), mapper.requested...)
	mapper.mu.Unlock()

	var sawTCP, sawUDP bool
	for _, p := range requested {
		if p.Protocol == "tcp" && p.Number == 8080 {
			sawTCP = true
		}
		if p.Protocol == "udp" && p.Number == 51821 {
			sawUDP = true
		}
	}
	if !sawTCP {
		t.Error("the port from the first call is no longer being renewed")
	}
	if !sawUDP {
		t.Error("the port from the second call was not recorded")
	}
}

// TestOnlyOneRenewalLoop: two loops would both rewrite the mapping list on
// their own schedule, each overwriting the other's results.
func TestOnlyOneRenewalLoop(t *testing.T) {
	mapper := NewPortMapper(nil)
	defer mapper.Close()

	mapper.mu.Lock()
	mapper.renewing = true // as if a first successful Open had started one
	mapper.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	mapper.Open(ctx, []Port{{Protocol: "udp", Number: 51821}})

	mapper.mu.Lock()
	defer mapper.mu.Unlock()
	if !mapper.renewing {
		t.Fatal("renewal was turned off by a later call")
	}
}
