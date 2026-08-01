// Package local runs the whole NexusVPN control plane inside another
// process, backed by the embedded store.
//
// It exists so the desktop app can be the server. Someone who wants their
// own machines on one network should not have to install Postgres, Redis and
// Docker to get there — the setup would be harder than the problem. The
// hosted deployment in cmd/api remains the right answer for an organisation;
// this is the right answer for a person.
//
// It is deliberately outside internal/ so the desktop module can import it,
// the same arrangement client/agent uses.
package local

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	stdhttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/repository/embedded"
	coordgrpc "github.com/jitheshsisodiya/Ed-s/backend/internal/transport/grpc"
	httptransport "github.com/jitheshsisodiya/Ed-s/backend/internal/transport/http"
	wstransport "github.com/jitheshsisodiya/Ed-s/backend/internal/transport/ws"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// Options configures a local control plane.
type Options struct {
	// DataDir is where the store and its secrets live. Created 0700.
	DataDir string

	// Host is the interface to listen on. The default is every interface,
	// because the entire point is that the other machines in the house or
	// the office can reach this one. Set it to "127.0.0.1" to keep the
	// server to this machine alone.
	Host string

	// HTTPPort and GRPCPort default to 8080 and 9090, matching what the
	// hosted deployment uses so a client configured for one works with the
	// other.
	HTTPPort int
	GRPCPort int

	// Logf receives progress messages. Optional.
	Logf func(format string, args ...any)

	// OpenRouterPort asks the router to forward the control-plane port, so
	// devices away from this network can still reach it. Off by default: it
	// makes a machine reachable from the internet, which is a decision for
	// the person who owns it rather than something to do on their behalf.
	OpenRouterPort bool
}

// Server is a running local control plane.
type Server struct {
	// BaseURL is what a client should be pointed at.
	BaseURL string
	// Fingerprint identifies this server's TLS certificate. A device pairing
	// with it is told this value over the QR code and pins it, which is what
	// makes a self-signed certificate safe here.
	Fingerprint string
	// GRPCAddr is the coordination endpoint.
	GRPCAddr string
	// LANURL is the address other machines on this network should use, or
	// empty if this machine has no routable address.
	LANURL string

	// PublicURL is the address that reaches this machine from outside the
	// house, or empty if the router would not open a hole. Empty is a
	// normal outcome, not a failure: plenty of networks have no router that
	// can be asked, and a connection behind carrier-grade NAT cannot be
	// opened at all.
	PublicURL string

	// Recorded so tests and callers can find the data on disk and report
	// which ports were actually taken.
	dataDir  string
	httpPort int
	grpcPort int

	store    *embedded.Store
	ports    *PortMapper
	http     *stdhttp.Server
	grpc     *grpc.Server
	grpcLis  net.Listener
	stopped  chan struct{}
	shutdown func()
}

// Start brings the control plane up and returns once it is accepting
// connections, so a caller can point a client at it immediately rather than
// racing the listener.
func Start(ctx context.Context, opts Options) (*Server, error) {
	logf := opts.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if opts.DataDir == "" {
		return nil, errors.New("local: a data directory is required")
	}
	if opts.HTTPPort == 0 {
		opts.HTTPPort = 8080
	}
	if opts.GRPCPort == 0 {
		opts.GRPCPort = 9090
	}

	secrets, err := loadOrCreateSecrets(filepath.Join(opts.DataDir, "secrets.json"))
	if err != nil {
		return nil, err
	}

	store, err := embedded.Open(filepath.Join(opts.DataDir, "server.json"))
	if err != nil {
		return nil, err
	}

	// TLS from the start, not only when exposed. A control plane that speaks
	// plain HTTP on the LAN and TLS off it would mean two behaviours to get
	// right and one of them only exercised by the people most at risk.
	id, err := loadOrCreateIdentity(opts.DataDir)
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	// Errors and above only: this runs behind a desktop app, where a log
	// line per request is noise nobody reads and a file nobody prunes.
	logger := zap.NewNop()

	tokens := auth.NewTokenManager(
		secrets.AccessSecret, secrets.RefreshSecret, "nexusvpn-local",
		15*time.Minute, 720*time.Hour,
	)
	relaySessions := auth.NewRelaySessionManager(secrets.RelaySecret, 5*time.Minute)
	// Five minutes is long enough to carry a phone across a room and short
	// enough that a code left on screen stops being one.
	pairings := auth.NewPairingManager(secrets.PairingSecret, 5*time.Minute)

	audit := usecase.NewAuditRecorder(store.AuditLogs(), logger)
	authService := usecase.NewAuthService(
		store.Users(), store.RefreshTokens(), store.PasswordResets(),
		tokens, auth.NewPasswordHasher(0), auth.NewMFAManager("NexusVPN"),
		nil, store.Limiter(), audit, time.Hour,
	)
	pairingService := usecase.NewPairingService(
		store.Users(), store.Members(), pairings, authService, audit,
	)
	networkService := usecase.NewNetworkService(
		store.Networks(), store.Members(), store.Users(), audit,
	)
	deviceService := usecase.NewDeviceService(
		store.Devices(), store.Networks(), store.Members(),
		store.Presence(), store.Bus(), audit,
	)
	logsService := usecase.NewLogsService(store.AuditLogs(), store.ConnectionLogs(), store.Members())
	dashboardService := usecase.NewDashboardService(
		store.Devices(), store.ConnectionLogs(), store.Presence(), 5*time.Minute,
	)
	coordinationService := usecase.NewCoordinationService(
		store.Devices(), store.Networks(), store.Members(), store.Relays(),
		store.Presence(), store.Bus(), store.ICE(),
		relaySessions, audit, 60*time.Second,
	)

	wsHub := wstransport.NewHub(wstransport.Config{
		Bus:      store.Bus(),
		Presence: store.Presence(),
		Members:  store.Members(),
		Devices:  store.Devices(),
		Auth:     wstransport.TokenAuthenticator{Tokens: tokens},
		Logger:   logger,
		// Only this machine's own apps talk to this server over a browser
		// origin, and none of them do today. An empty list is the closed
		// default rather than the wildcard the hosted deployment allows.
		AllowedOrigins: nil,
	})

	router := httptransport.NewRouter(httptransport.RouterConfig{
		Auth:      httptransport.NewAuthHandler(authService, logger, false),
		Networks:  httptransport.NewNetworkHandler(networkService, deviceService),
		Devices:   httptransport.NewDeviceHandler(deviceService),
		Logs:      httptransport.NewLogsHandler(logsService, dashboardService),
		Tokens:    tokens,
		Logger:    logger,
		WSHandler: wsHub,
		Relay:     httptransport.NewRelayHandler(store.Relays(), secrets.RelaySecret),
		Pairing:   httptransport.NewPairingHandler(pairingService),
	})

	host := opts.Host
	if host == "" {
		host = "0.0.0.0"
	}

	httpAddr := net.JoinHostPort(host, fmt.Sprint(opts.HTTPPort))
	grpcAddr := net.JoinHostPort(host, fmt.Sprint(opts.GRPCPort))

	// Both listeners are opened before either server is started, so a port
	// already in use is reported as an error the caller can show rather
	// than as a background goroutine failing after Start has returned
	// success.
	httpLis, err := net.Listen("tcp", httpAddr)
	if err != nil {
		_ = store.Close()
		return nil, portError(opts.HTTPPort, err)
	}
	grpcLis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		_ = httpLis.Close()
		_ = store.Close()
		return nil, portError(opts.GRPCPort, err)
	}

	httpServer := &stdhttp.Server{
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{id.cert},
			// 1.2 is the floor because some Android versions in the field
			// negotiate it and there is no reason to lock those out; nothing
			// below it is acceptable.
			MinVersion: tls.VersionTLS12,
		},
		// No WriteTimeout: the WebSocket endpoint holds connections open
		// for the lifetime of a session.
		IdleTimeout: 120 * time.Second,
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			coordgrpc.RecoveryUnaryInterceptor(logger),
			coordgrpc.AuthUnaryInterceptor(tokens),
		),
		grpc.ChainStreamInterceptor(coordgrpc.AuthStreamInterceptor(tokens)),
	)
	coordgrpc.NewCoordinationServer(coordinationService, logger).Register(grpcServer)

	srv := &Server{
		BaseURL:     fmt.Sprintf("https://127.0.0.1:%d", opts.HTTPPort),
		Fingerprint: id.fingerprint,
		GRPCAddr:    fmt.Sprintf("127.0.0.1:%d", opts.GRPCPort),
		LANURL:      lanURL(opts.HTTPPort),
		dataDir:     opts.DataDir,
		httpPort:    opts.HTTPPort,
		grpcPort:    opts.GRPCPort,
		store:       store,
		http:        httpServer,
		grpc:        grpcServer,
		grpcLis:     grpcLis,
		stopped:     make(chan struct{}),
	}

	go func() {
		// Certificate and key are already in TLSConfig, so the empty
		// arguments here are correct rather than an omission.
		if err := httpServer.ServeTLS(httpLis, "", ""); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			logf("local: http server stopped: %v", err)
		}
	}()
	go func() {
		if err := grpcServer.Serve(grpcLis); err != nil {
			logf("local: grpc server stopped: %v", err)
		}
	}()

	srv.shutdown = func() {
		if srv.ports != nil {
			srv.ports.Close()
		}
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		grpcServer.GracefulStop()
		_ = store.Close()
	}

	if opts.OpenRouterPort {
		srv.openRouterPort(ctx, opts.HTTPPort, opts.GRPCPort, logf)
	}

	logf("local: control plane listening on %s (grpc %s)", httpAddr, grpcAddr)
	if srv.LANURL != "" {
		logf("local: other machines on this network can use %s", srv.LANURL)
	}
	return srv, nil
}

// IsFirstRun reports whether no account exists yet, so a caller can offer to
// create one instead of asking for a password nobody has set.
func (s *Server) IsFirstRun() bool { return s.store.IsEmpty() }

// Stop shuts the control plane down and flushes the store.
func (s *Server) Stop() {
	select {
	case <-s.stopped:
		return
	default:
		close(s.stopped)
	}
	if s.shutdown != nil {
		s.shutdown()
	}
}

// portError turns "address already in use" into something worth reading.
func portError(port int, err error) error {
	return fmt.Errorf("could not start the NexusVPN server on port %d — "+
		"another program is probably already using it: %w", port, err)
}

// lanURL is the address other machines have to be pointed at.
//
// Which is not "the first interface that is up". Radmin VPN, Hamachi,
// VirtualBox, WSL and NexusVPN's own tunnel all present interfaces that are
// up and not loopback, carrying addresses nothing on the Wi-Fi can reach, and
// this string is shown to somebody as where to send their other machines. One
// user was handed a Radmin address in 26.0.0.0/8 that way — real, routable,
// publicly allocated space Radmin squats on, and nowhere near their LAN.
//
// So the operating system is asked which address it would send from, and the
// answer is only accepted if it is in a range reserved for private networks.
func lanURL(port int) string {
	if ip := routableSourceAddress(); ip != "" {
		return fmt.Sprintf("https://%s:%d", ip, port)
	}
	if ip := firstPrivateAddress(); ip != "" {
		return fmt.Sprintf("https://%s:%d", ip, port)
	}
	return ""
}

// routableSourceAddress reads the routing table's own answer. The UDP
// "connection" sends nothing — connect on a datagram socket only fixes the
// peer and picks a route — so this reaches no network and needs none.
func routableSourceAddress() string {
	conn, err := net.DialTimeout("udp4", "192.0.2.1:9", 2*time.Second)
	if err != nil {
		return ""
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP == nil || !isPrivateIPv4(addr.IP) {
		return ""
	}
	return addr.IP.String()
}

// firstPrivateAddress is the fallback for a machine with no route out, which
// can still be the one hosting a network in a room with no internet.
func firstPrivateAddress() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// NexusVPN's own tunnel carries a private address too, so it would
		// otherwise be a candidate for "where other machines can reach this
		// one" — which is circular: a machine that is not on the network yet
		// cannot use an address that only exists on it.
		if strings.HasPrefix(strings.ToLower(iface.Name), "nexus") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if n, ok := addr.(*net.IPNet); ok && isPrivateIPv4(n.IP) {
				return n.IP.To4().String()
			}
		}
	}
	return ""
}

// isPrivateIPv4 reports whether an address is on a network somebody's other
// machines could actually be sitting on. Narrower than "not public" on
// purpose: 26.0.0.0/8 is public space another VPN squats on, and offering one
// of its addresses as this machine's is how this went wrong before.
func isPrivateIPv4(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil || v4.IsLoopback() || v4.IsLinkLocalUnicast() {
		return false
	}
	switch {
	case v4[0] == 10:
		return true // 10.0.0.0/8
	case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
		return true // 172.16.0.0/12
	case v4[0] == 192 && v4[1] == 168:
		return true // 192.168.0.0/16
	}
	return false
}

/* ---------- secrets ---------- */

// secrets are the signing keys for this installation.
//
// Generated once and kept beside the data, never defaulted: a shipped
// default would mean every copy of the app signs tokens the same way, and
// anyone with the binary could mint a session for anyone else's server.
type secrets struct {
	AccessSecret  string `json:"accessSecret"`
	RefreshSecret string `json:"refreshSecret"`
	RelaySecret   string `json:"relaySecret"`
	PairingSecret string `json:"pairingSecret"`
}

func loadOrCreateSecrets(path string) (*secrets, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("local: create data directory: %w", err)
	}

	blob, err := os.ReadFile(path)
	if err == nil {
		var s secrets
		if err := json.Unmarshal(blob, &s); err != nil {
			return nil, fmt.Errorf("local: %s is unreadable; move it aside to "+
				"generate fresh keys, which will sign everyone out: %w", path, err)
		}
		if s.AccessSecret != "" && s.RefreshSecret != "" && s.RelaySecret != "" {
			// Added after the first release, so an existing file has every
			// other key and not this one. Filling the gap in place keeps the
			// sessions those other keys signed.
			if s.PairingSecret == "" {
				s.PairingSecret = randomSecret()
				if err := writeSecrets(path, &s); err != nil {
					return nil, err
				}
			}
			return &s, nil
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("local: read %s: %w", path, err)
	}

	s := &secrets{
		AccessSecret:  randomSecret(),
		RefreshSecret: randomSecret(),
		RelaySecret:   randomSecret(),
		PairingSecret: randomSecret(),
	}
	if err := writeSecrets(path, s); err != nil {
		return nil, err
	}
	return s, nil
}

func writeSecrets(path string, s *secrets) error {
	out, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return fmt.Errorf("local: write %s: %w", path, err)
	}
	return nil
}

// randomSecret returns 32 bytes of cryptographic randomness, hex encoded.
func randomSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is not a condition to paper over with a
		// weaker source: every session token on this installation depends
		// on it.
		panic("local: no source of randomness available: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// openRouterPort asks the router to let the outside world reach this machine.
//
// Best effort, and silent about it beyond a log line, because there is
// nothing a person can do about a router that says no except forward the
// port themselves — which the UI tells them, using PublicURL being empty as
// the signal. Failing to open a port must never stop the server starting: a
// control plane that works on the local network is the common case and by far
// the more important one.
func (s *Server) openRouterPort(ctx context.Context, httpPort, grpcPort int, logf func(string, ...any)) {
	s.ports = NewPortMapper(logf)

	mapCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	// Both ports, because a device signs in over HTTP and then negotiates
	// the tunnel over gRPC. One without the other produces a server that can
	// be reached and not connected through, which is worse than one that
	// cannot be reached at all: it fails later, after the person believes it
	// worked. So a partial result is called out rather than presented as
	// success.
	got := s.ports.Open(mapCtx, []Port{
		{Protocol: "tcp", Number: httpPort},
		{Protocol: "tcp", Number: grpcPort},
	})

	if len(got) == 1 {
		logf("local: the router opened only one of the two ports needed, so "+
			"devices away from this network may sign in and fail to connect; "+
			"forward %d and %d by hand if this matters", httpPort, grpcPort)
	}
	if len(got) == 0 {
		logf("local: the router did not open a port, so this machine is " +
			"reachable on the local network only")
		return
	}
	if addr := s.ports.ExternalAddress(); addr != "" {
		s.PublicURL = fmt.Sprintf("https://%s:%d", addr, got[0].External)
		logf("local: reachable from outside this network at %s (via %s)",
			s.PublicURL, got[0].Method)
	}
}
