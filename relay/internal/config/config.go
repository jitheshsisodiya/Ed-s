// Package config loads relay node configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds a relay node's runtime configuration.
type Config struct {
	// DataAddr is the UDP listen address for relayed traffic.
	DataAddr string
	// RelayPort is the port clients are told to send to; it always matches
	// the port in DataAddr.
	RelayPort int
	// ControlPort is advertised to the control plane for diagnostics.
	ControlPort int
	// MetricsAddr serves Prometheus /metrics.
	MetricsAddr string

	// BackendURL is the control plane's HTTP root.
	BackendURL string
	// RelaySecret is shared with the control plane: it authenticates the
	// register/heartbeat calls and verifies relay session tokens.
	RelaySecret string

	// Region and PublicHostname describe this node to clients.
	Region         string
	PublicHostname string

	// Capacity caps concurrent sessions; also advertised for load balancing.
	Capacity int

	// SessionIdleTimeout evicts silent sessions.
	SessionIdleTimeout time.Duration
	// HeartbeatInterval controls how often load is reported upstream.
	HeartbeatInterval time.Duration

	Environment string
	LogLevel    string
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

// Load reads configuration from the environment, applying local-development
// defaults and refusing to start in production with a default secret.
func Load() (*Config, error) {
	dataPort := getenvInt("DATA_PORT", 3479)
	env := getenv("ENVIRONMENT", "development")

	cfg := &Config{
		DataAddr:    fmt.Sprintf(":%d", dataPort),
		ControlPort: getenvInt("CONTROL_PORT", 3478),
		MetricsAddr: fmt.Sprintf(":%d", getenvInt("METRICS_PORT", 9092)),

		BackendURL:  getenv("BACKEND_URL", "http://localhost:8080"),
		RelaySecret: getenv("RELAY_SESSION_SECRET", "dev-relay-secret-change-me-please-32b"),

		Region:         getenv("RELAY_REGION", "local"),
		PublicHostname: getenv("RELAY_PUBLIC_HOSTNAME", "localhost"),

		Capacity: getenvInt("RELAY_CAPACITY", 1000),

		SessionIdleTimeout: getenvDuration("SESSION_IDLE_TIMEOUT", 5*time.Minute),
		HeartbeatInterval:  getenvDuration("HEARTBEAT_INTERVAL", 30*time.Second),

		Environment: env,
		LogLevel:    getenv("LOG_LEVEL", "info"),
	}

	if env == "production" && cfg.RelaySecret == "dev-relay-secret-change-me-please-32b" {
		return nil, fmt.Errorf("config: refusing to start in production with the default RELAY_SESSION_SECRET")
	}

	// The relay port is what clients are told to send to, so it must match
	// the socket we actually bind.
	cfg.RelayPort = dataPort
	return cfg, nil
}
