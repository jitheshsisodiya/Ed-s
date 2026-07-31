// Command relayd is a NexusVPN relay node: a UDP forwarder that carries
// WireGuard traffic between peers whose NATs defeat direct hole punching.
//
// It authenticates peers with session tokens signed by the control plane,
// forwards their datagrams without decrypting them, and reports load back
// to the control plane so allocations spread across the fleet.
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/jitheshsisodiya/Ed-s/relay/internal/config"
	"github.com/jitheshsisodiya/Ed-s/relay/internal/controlplane"
	"github.com/jitheshsisodiya/Ed-s/relay/internal/metrics"
	"github.com/jitheshsisodiya/Ed-s/relay/internal/relay"
	"github.com/jitheshsisodiya/Ed-s/relay/internal/token"
)

func main() {
	if err := run(); err != nil {
		os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := newLogger(cfg.LogLevel, cfg.Environment)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- UDP data plane ---
	udpAddr, err := net.ResolveUDPAddr("udp", cfg.DataAddr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	logger.Info("relay_listening",
		zap.String("addr", conn.LocalAddr().String()),
		zap.String("region", cfg.Region),
		zap.String("hostname", cfg.PublicHostname))

	sessions := relay.NewSessionTable(cfg.SessionIdleTimeout, cfg.Capacity)
	verifier := token.NewVerifier(cfg.RelaySecret, "")

	// --- Control-plane registration ---
	cp := controlplane.New(cfg.BackendURL, cfg.RelaySecret)
	relayID := registerWithRetry(ctx, cp, cfg, logger)
	if relayID != "" {
		// Once we know our own ID, require session tokens to be scoped to
		// this node so a token minted for another relay can't be replayed.
		verifier.SetRelayID(relayID)
		logger.Info("relay_registered", zap.String("relay_id", relayID))
	} else {
		logger.Warn("relay_registration_incomplete",
			zap.String("reason", "control plane unreachable; serving without per-relay token scoping"))
	}

	// --- Metrics ---
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	metricsServer := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		logger.Info("metrics_listening", zap.String("addr", cfg.MetricsAddr))
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("metrics_server_failed", zap.Error(err))
		}
	}()

	// --- Periodic stats publishing + control-plane heartbeat ---
	go reportLoop(ctx, cfg, cp, sessions, relayID, logger)

	// --- Serve ---
	server := relay.NewServer(relay.Config{
		Conn:     conn,
		Verifier: verifier,
		Sessions: sessions,
		Logger:   logger,
	})

	serveErr := server.Serve(ctx)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("metrics_shutdown_error", zap.Error(err))
	}

	// Best-effort: tell the control plane we're going away so allocations
	// stop landing here before the heartbeat would have timed out.
	if relayID != "" {
		if err := cp.Heartbeat(shutdownCtx, relayID, 0); err != nil {
			logger.Debug("final_heartbeat_failed", zap.Error(err))
		}
	}

	logger.Info("shutdown_complete")
	return serveErr
}

// registerWithRetry advertises this node to the control plane, retrying with
// backoff. A relay is useful even before registration succeeds (clients that
// already hold a token can bind), so this gives up rather than blocking
// startup forever.
func registerWithRetry(ctx context.Context, cp *controlplane.Client, cfg *config.Config, logger *zap.Logger) string {
	req := controlplane.RegisterRequest{
		Region:      cfg.Region,
		Hostname:    cfg.PublicHostname,
		ControlPort: cfg.ControlPort,
		RelayPort:   cfg.RelayPort,
		Capacity:    cfg.Capacity,
	}

	backoff := time.Second
	for attempt := 1; attempt <= 5; attempt++ {
		registerCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		id, err := cp.Register(registerCtx, req)
		cancel()
		if err == nil {
			return id
		}
		logger.Warn("relay_registration_failed",
			zap.Int("attempt", attempt), zap.Error(err), zap.Duration("retry_in", backoff))

		select {
		case <-ctx.Done():
			return ""
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return ""
}

// reportLoop publishes Prometheus stats and heartbeats load upstream.
func reportLoop(
	ctx context.Context,
	cfg *config.Config,
	cp *controlplane.Client,
	sessions *relay.SessionTable,
	relayID string,
	logger *zap.Logger,
) {
	publisher := &metrics.Publisher{}
	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			stats := sessions.Stats()
			publisher.Publish(metrics.Snapshot{
				Sessions:       stats.Sessions,
				Bindings:       stats.Bindings,
				BytesRelayed:   stats.BytesRelayed,
				PacketsRelayed: stats.PacketsRelayed,
				PacketsDropped: stats.PacketsDropped,
			})

			if relayID == "" {
				continue
			}
			hbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := cp.Heartbeat(hbCtx, relayID, stats.Sessions)
			cancel()
			if err != nil {
				logger.Warn("relay_heartbeat_failed", zap.Error(err))
			}
		}
	}
}

// newLogger builds a structured logger matching the backend's conventions.
func newLogger(level, env string) (*zap.Logger, error) {
	var cfg zap.Config
	if env == "development" {
		cfg = zap.NewDevelopmentConfig()
	} else {
		cfg = zap.NewProductionConfig()
	}
	var lvl zap.AtomicLevel
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = zap.NewAtomicLevelAt(zap.InfoLevel)
	}
	cfg.Level = lvl
	return cfg.Build(zap.Fields(zap.String("service", "relay")))
}
