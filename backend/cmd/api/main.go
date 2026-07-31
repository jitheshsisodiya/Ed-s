// Command api is the NexusVPN control-plane server. It runs the REST API,
// the gRPC CoordinationService, the WebSocket signaling hub and the
// Prometheus metrics endpoint, all backed by Postgres and Redis.
package main

import (
	"context"
	"errors"
	"net"
	stdhttp "net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	coordgrpc "github.com/jitheshsisodiya/Ed-s/backend/internal/transport/grpc"
	httptransport "github.com/jitheshsisodiya/Ed-s/backend/internal/transport/http"
	wstransport "github.com/jitheshsisodiya/Ed-s/backend/internal/transport/ws"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/auth"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/config"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/logging"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/metrics"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/repository/postgres"
	redisrepo "github.com/jitheshsisodiya/Ed-s/backend/internal/repository/redis"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet if config/logging setup failed.
		os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, err := logging.New(cfg.LogLevel, "backend-api", cfg.Environment)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	// Root context cancelled on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- Infrastructure ---
	startupCtx, cancelStartup := context.WithTimeout(ctx, 60*time.Second)
	defer cancelStartup()

	pool, err := postgres.NewPool(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("postgres_connected")

	if err := postgres.RunMigrations(startupCtx, pool); err != nil {
		return err
	}
	logger.Info("migrations_applied")

	rdb, err := redisrepo.NewClient(startupCtx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		return err
	}
	defer func() { _ = rdb.Close() }()
	logger.Info("redis_connected")

	// --- Repositories ---
	users := postgres.NewUserRepo(pool)
	refreshTokens := postgres.NewRefreshTokenRepo(pool)
	passwordResets := postgres.NewPasswordResetRepo(pool)
	networks := postgres.NewNetworkRepo(pool)
	members := postgres.NewNetworkMemberRepo(pool)
	devices := postgres.NewDeviceRepo(pool)
	relays := postgres.NewRelayServerRepo(pool)
	auditLogs := postgres.NewAuditLogRepo(pool)
	connLogs := postgres.NewConnectionLogRepo(pool)
	errorLogs := postgres.NewErrorLogRepo(pool)

	// Persist error-level logs to Postgres from here on.
	logging.SetErrorSink(postgres.ErrorSinkAdapter{Repo: errorLogs})

	presence := redisrepo.NewPresenceStore(rdb)
	peerBus := redisrepo.NewPeerEventBus(rdb)
	iceStore := redisrepo.NewICEStore(rdb)
	rateLimiter := redisrepo.NewRateLimiter(rdb)

	// --- Auth primitives ---
	tokenManager := auth.NewTokenManager(
		cfg.JWTAccessSecret, cfg.JWTRefreshSecret, cfg.JWTIssuer,
		cfg.AccessTokenTTL, cfg.RefreshTokenTTL,
	)
	passwordHasher := auth.NewPasswordHasher(0) // 0 -> bcrypt default cost
	mfaManager := auth.NewMFAManager("NexusVPN")
	relaySessions := auth.NewRelaySessionManager(cfg.RelaySessionSecret, cfg.RelaySessionTTL)

	var googleOAuth usecase.GoogleOAuthProvider
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" {
		googleOAuth = auth.NewGoogleOAuthConfig(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL)
		logger.Info("google_oauth_enabled")
	} else {
		logger.Info("google_oauth_disabled", zap.String("reason", "GOOGLE_CLIENT_ID/SECRET not set"))
	}

	// --- Use cases ---
	auditRecorder := usecase.NewAuditRecorder(auditLogs, logger)
	authService := usecase.NewAuthService(
		users, refreshTokens, passwordResets, tokenManager, passwordHasher,
		mfaManager, googleOAuth, rateLimiter, auditRecorder, cfg.PasswordResetTTL,
	)
	networkService := usecase.NewNetworkService(networks, members, users, auditRecorder)
	deviceService := usecase.NewDeviceService(devices, networks, members, presence, peerBus, auditRecorder)
	logsService := usecase.NewLogsService(auditLogs, connLogs, members)
	dashboardService := usecase.NewDashboardService(devices, connLogs, presence, 5*time.Minute)
	coordinationService := usecase.NewCoordinationService(
		devices, networks, members, relays, presence, peerBus, iceStore,
		relaySessions, auditRecorder, cfg.PresenceTTL,
	)

	// --- Transports ---
	wsHub := wstransport.NewHub(wstransport.Config{
		Bus:            peerBus,
		Presence:       presence,
		Members:        members,
		Devices:        devices,
		Auth:           wstransport.TokenAuthenticator{Tokens: tokenManager},
		Logger:         logger,
		AllowedOrigins: cfg.CORSOrigins,
	})

	router := httptransport.NewRouter(httptransport.RouterConfig{
		Auth:        httptransport.NewAuthHandler(authService, logger, cfg.Environment != "production"),
		Networks:    httptransport.NewNetworkHandler(networkService, deviceService),
		Devices:     httptransport.NewDeviceHandler(deviceService),
		Logs:        httptransport.NewLogsHandler(logsService, dashboardService),
		Tokens:      tokenManager,
		Logger:      logger,
		CORSOrigins: cfg.CORSOrigins,
		WSHandler:   wsHub,
	})
	router = logging.HTTPMiddleware(logger)(router)

	httpServer := &stdhttp.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: the WebSocket endpoint holds connections open for
		// the lifetime of a client session.
		IdleTimeout: 120 * time.Second,
	}

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			coordgrpc.RecoveryUnaryInterceptor(logger),
			coordgrpc.MetricsUnaryInterceptor(),
			coordgrpc.LoggingUnaryInterceptor(logger),
			coordgrpc.AuthUnaryInterceptor(tokenManager),
		),
		grpc.ChainStreamInterceptor(
			coordgrpc.AuthStreamInterceptor(tokenManager),
		),
	)
	coordgrpc.NewCoordinationServer(coordinationService, logger).Register(grpcServer)
	reflection.Register(grpcServer)

	metricsMux := stdhttp.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsServer := &stdhttp.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           metricsMux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- Start servers ---
	errCh := make(chan error, 3)

	go func() {
		logger.Info("http_listening", zap.String("addr", cfg.HTTPAddr))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			errCh <- err
		}
	}()

	go func() {
		lis, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			errCh <- err
			return
		}
		logger.Info("grpc_listening", zap.String("addr", cfg.GRPCAddr))
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			errCh <- err
		}
	}()

	go func() {
		logger.Info("metrics_listening", zap.String("addr", cfg.MetricsAddr))
		if err := metricsServer.ListenAndServe(); err != nil && !errors.Is(err, stdhttp.ErrServerClosed) {
			errCh <- err
		}
	}()

	// Keep the devices_online gauge fresh for Prometheus.
	go pollPresenceGauge(ctx, presence, logger)

	select {
	case err := <-errCh:
		logger.Error("server_failed", zap.Error(err))
		return err
	case <-ctx.Done():
		logger.Info("shutdown_signal_received")
	}

	// --- Graceful shutdown ---
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	grpcStopped := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcStopped)
	}()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("http_shutdown_error", zap.Error(err))
	}
	if err := metricsServer.Shutdown(shutdownCtx); err != nil {
		logger.Warn("metrics_shutdown_error", zap.Error(err))
	}

	select {
	case <-grpcStopped:
	case <-shutdownCtx.Done():
		logger.Warn("grpc_graceful_stop_timed_out")
		grpcServer.Stop()
	}

	logger.Info("shutdown_complete")
	return nil
}

// pollPresenceGauge periodically refreshes the devices_online Prometheus
// gauge from the Redis presence set.
func pollPresenceGauge(ctx context.Context, presence *redisrepo.PresenceStore, logger *zap.Logger) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := presence.CountOnline(ctx)
			if err != nil {
				logger.Debug("presence_gauge_refresh_failed", zap.Error(err))
				continue
			}
			metrics.DevicesOnline.Set(float64(count))
		}
	}
}
