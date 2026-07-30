// Package config loads NexusVPN backend configuration from environment
// variables, with sane local-development defaults.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all runtime configuration for the backend service.
type Config struct {
	// HTTP / gRPC / WS / metrics listen addresses.
	HTTPAddr    string
	GRPCAddr    string
	MetricsAddr string

	// Postgres.
	DatabaseURL string

	// Redis.
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// JWT.
	JWTAccessSecret  string
	JWTRefreshSecret string
	JWTIssuer        string
	AccessTokenTTL   time.Duration
	RefreshTokenTTL  time.Duration

	// Relay session tokens (signed JWTs handed to relay nodes).
	RelaySessionSecret string
	RelaySessionTTL    time.Duration

	// OAuth (Google).
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string

	// Password reset.
	PasswordResetTTL time.Duration

	// Presence.
	PresenceTTL time.Duration

	// Misc.
	Environment string
	LogLevel    string
	CORSOrigins []string

	// DNS servers handed to newly created networks when none supplied.
	DefaultDNSServers []string
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func getenvList(key string, def []string) []string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return def
}

// Load reads configuration from the environment. It returns an error if a
// required secret is missing outside of local/dev environments.
func Load() (*Config, error) {
	env := getenv("ENVIRONMENT", "development")

	cfg := &Config{
		HTTPAddr:    getenv("HTTP_ADDR", ":8080"),
		GRPCAddr:    getenv("GRPC_ADDR", ":9090"),
		MetricsAddr: getenv("METRICS_ADDR", ":9091"),

		DatabaseURL: getenv("DATABASE_URL", "postgres://nexusvpn:nexusvpn@localhost:5432/nexusvpn?sslmode=disable"),

		RedisAddr:     getenv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getenv("REDIS_PASSWORD", ""),
		RedisDB:       getenvInt("REDIS_DB", 0),

		JWTAccessSecret:  getenv("JWT_ACCESS_SECRET", "dev-access-secret-change-me-please-32b"),
		JWTRefreshSecret: getenv("JWT_REFRESH_SECRET", "dev-refresh-secret-change-me-please-32"),
		JWTIssuer:        getenv("JWT_ISSUER", "nexusvpn-control-plane"),
		AccessTokenTTL:   getenvDuration("ACCESS_TOKEN_TTL", 15*time.Minute),
		RefreshTokenTTL:  getenvDuration("REFRESH_TOKEN_TTL", 30*24*time.Hour),

		RelaySessionSecret: getenv("RELAY_SESSION_SECRET", "dev-relay-secret-change-me-please-32b"),
		RelaySessionTTL:    getenvDuration("RELAY_SESSION_TTL", 10*time.Minute),

		GoogleClientID:     getenv("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret: getenv("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURL:  getenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/api/v1/auth/oauth/google/callback"),

		PasswordResetTTL: getenvDuration("PASSWORD_RESET_TTL", 1*time.Hour),
		PresenceTTL:      getenvDuration("PRESENCE_TTL", 45*time.Second),

		Environment: env,
		LogLevel:    getenv("LOG_LEVEL", "info"),
		CORSOrigins: getenvList("CORS_ORIGINS", []string{"*"}),

		DefaultDNSServers: getenvList("DEFAULT_DNS_SERVERS", []string{"1.1.1.1", "8.8.8.8"}),
	}

	if env == "production" {
		if cfg.JWTAccessSecret == "dev-access-secret-change-me-please-32b" ||
			cfg.JWTRefreshSecret == "dev-refresh-secret-change-me-please-32" ||
			cfg.RelaySessionSecret == "dev-relay-secret-change-me-please-32b" {
			return nil, fmt.Errorf("config: refusing to start in production with default secrets; set JWT_ACCESS_SECRET, JWT_REFRESH_SECRET, RELAY_SESSION_SECRET")
		}
	}

	return cfg, nil
}
