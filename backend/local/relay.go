package local

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	relaytoken "github.com/jitheshsisodiya/Ed-s/relay/token"
	"github.com/jitheshsisodiya/Ed-s/relay/udprelay"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// startRelay runs a relay on this machine, so traffic that cannot go directly
// between two devices goes through hardware its owner controls.
//
// Two machines behind different NATs usually reach each other directly, by
// punching a hole through both. Usually — symmetric NAT on either side, which
// is what a lot of mobile networks and corporate firewalls do, defeats it.
// Without a relay those two devices simply cannot talk, and the network
// silently has a hole in it.
//
// The usual answer is a rented server somewhere. This is the other answer:
// the machine already hosting the control plane carries it, because it is
// already switched on and already reachable. It costs nothing and belongs to
// the person using it.
//
// The relay cannot read what it carries. WireGuard's handshake terminates on
// the two clients, so what passes through here is ciphertext addressed by
// session, and a relay operator — including one running it on their own
// desktop — sees no more than the internet does.
func (s *Server) startRelay(ctx context.Context, port int, secret, relayID string, logf func(string, ...any)) error {
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("could not open the relay port %d — another program "+
			"is probably using it: %w", port, err)
	}

	s.relayConn = conn
	server := udprelay.NewServer(udprelay.Config{
		Conn: conn,
		// Bound to this relay's own ID, so a token minted for a different
		// relay — one the same control plane also knows about — is refused
		// here rather than honoured by whichever relay it reaches first.
		Verifier: relaytoken.NewVerifier(secret, relayID),
		// Sized for a household or a small office rather than a public
		// relay: this is somebody's desktop, and an unbounded table on a
		// machine somebody is also using is a way to make their computer
		// unusable from the outside.
		Sessions: udprelay.NewSessionTable(5*time.Minute, 256),
		Logger:   zap.NewNop(),
	})

	go func() {
		if err := server.Serve(ctx); err != nil && ctx.Err() == nil {
			logf("local: relay stopped: %v", err)
		}
	}()

	logf("local: relaying for peers that cannot reach each other directly, on port %d", port)
	return nil
}

// registerRelay tells the control plane about the relay running here, so
// clients are offered it when a direct path fails.
//
// The address recorded is the one that works from outside where possible,
// because a relay exists precisely for the case where two devices are not on
// the same network. A relay reachable only on the LAN is still worth
// registering — two machines in the same building behind an access point that
// blocks client-to-client traffic need exactly that — so a private address is
// used rather than nothing.
func (s *Server) registerRelay(ctx context.Context, port int) error {
	host := ""
	if s.ports != nil {
		host = s.ports.ExternalAddress()
	}
	if host == "" {
		host = firstPrivateAddress()
	}
	if host == "" {
		return fmt.Errorf("this machine has no address other devices could send to")
	}

	now := time.Now()
	return s.store.Relays().Upsert(ctx, &domain.RelayServer{
		ID:       relayIDFor(host, port),
		Region:   "this machine",
		Hostname: host,
		// The control port is unused here: registration and heartbeat happen
		// in-process rather than over HTTP, because the relay and the control
		// plane are the same program.
		RelayPort: port,
		// Sized to match the session table, so the control plane does not
		// keep sending devices to a relay that is already full.
		Capacity:        256,
		Status:          "healthy",
		LastHeartbeatAt: &now,
		CreatedAt:       now,
	})
}

// relayIDFor derives a stable identifier from where the relay is, so a
// restart updates the existing record rather than accumulating a new one
// beside every address this machine has ever had.
func relayIDFor(host string, port int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("nexusvpn-relay://%s:%d", host, port)))
}

// startOwnRelay brings the relay up and registers it, reporting rather than
// failing.
//
// A relay that will not start must never stop the control plane: devices that
// can reach each other directly — which is most of them, most of the time —
// carry on working, and the ones that cannot are no worse off than they were
// without a relay at all.
func (s *Server) startOwnRelay(ctx context.Context, port int, secret string, logf func(string, ...any)) {
	host := ""
	if s.ports != nil {
		host = s.ports.ExternalAddress()
	}
	if host == "" {
		host = firstPrivateAddress()
	}
	if host == "" {
		logf("local: not relaying — this machine has no address other devices could send to")
		return
	}
	id := relayIDFor(host, port)

	if err := s.startRelay(ctx, port, secret, id.String(), logf); err != nil {
		logf("local: not relaying — %v", err)
		return
	}
	if err := s.registerRelay(ctx, port); err != nil {
		logf("local: the relay is running but could not be registered, so "+
			"nothing will be sent to it: %v", err)
		return
	}

	// Asked for after the relay is listening, so nothing is ever directed
	// here before there is something to receive it.
	if s.ports != nil {
		if got := s.ports.Open(ctx, []Port{{Protocol: "udp", Number: port}}); len(got) == 0 {
			logf("local: the router would not open UDP %d, so this relay can "+
				"only carry traffic between devices on this network", port)
		}
	}
}
