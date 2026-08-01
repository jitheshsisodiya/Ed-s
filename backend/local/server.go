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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	stdhttp "net/http"
	"os"
	"path/filepath"
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
}

// Server is a running local control plane.
type Server struct {
	// BaseURL is what a client should be pointed at.
	BaseURL string
	// GRPCAddr is the coordination endpoint.
	GRPCAddr string
	// LANURL is the address other machines on this network should use, or
	// empty if this machine has no routable address.
	LANURL string

	// Recorded so tests and callers can find the data on disk and report
	// which ports were actually taken.
	dataDir  string
	httpPort int
	grpcPort int

	store    *embedded.Store
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
		BaseURL:  fmt.Sprintf("http://127.0.0.1:%d", opts.HTTPPort),
		GRPCAddr: fmt.Sprintf("127.0.0.1:%d", opts.GRPCPort),
		LANURL:   lanURL(opts.HTTPPort),
		dataDir:  opts.DataDir,
		httpPort: opts.HTTPPort,
		grpcPort: opts.GRPCPort,
		store:    store,
		http:     httpServer,
		grpc:     grpcServer,
		grpcLis:  grpcLis,
		stopped:  make(chan struct{}),
	}

	go func() {
		if err := httpServer.Serve(httpLis); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			logf("local: http server stopped: %v", err)
		}
	}()
	go func() {
		if err := grpcServer.Serve(grpcLis); err != nil {
			logf("local: grpc server stopped: %v", err)
		}
	}()

	srv.shutdown = func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutCtx)
		grpcServer.GracefulStop()
		_ = store.Close()
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

// lanURL finds this machine's routable address, which is what other machines
// have to be pointed at. Loopback and link-local are skipped because neither
// is reachable from anywhere else.
func lanURL(port int) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			n, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := n.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			return fmt.Sprintf("http://%s:%d", ip, port)
		}
	}
	return ""
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
