package config

import (
	"strings"
	"testing"
)

// setProductionSecrets sets the non-default secrets production requires, so
// each test isolates the one condition it is actually asserting.
func setProductionSecrets(t *testing.T) {
	t.Helper()
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("JWT_ACCESS_SECRET", "a-real-access-secret-value-32-bytes")
	t.Setenv("JWT_REFRESH_SECRET", "a-real-refresh-secret-value-32-byte")
	t.Setenv("RELAY_SESSION_SECRET", "a-real-relay-secret-value-32-bytes!")
}

func TestProductionRejectsDefaultSecrets(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")

	if _, err := Load(); err == nil {
		t.Fatal("expected production startup to fail with default secrets")
	}
}

// The reset-token echo hands out a password-reset token to anyone who can
// submit an email address, so it must never be enabled in production.
func TestProductionRejectsResetTokenExposure(t *testing.T) {
	setProductionSecrets(t)
	t.Setenv("DEV_EXPOSE_RESET_TOKEN", "true")

	_, err := Load()
	if err == nil {
		t.Fatal("expected production startup to fail with DEV_EXPOSE_RESET_TOKEN enabled")
	}
	if !strings.Contains(err.Error(), "DEV_EXPOSE_RESET_TOKEN") {
		t.Fatalf("error should name the offending setting, got: %v", err)
	}
}

// It must also stay off by default in development: only a deliberate opt-in
// enables it, so forgetting to set ENVIRONMENT can't expose reset tokens.
func TestResetTokenExposureIsOffByDefault(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DevExposeResetToken {
		t.Fatal("DEV_EXPOSE_RESET_TOKEN must default to disabled")
	}
	if cfg.Environment != "development" {
		t.Fatalf("environment = %q, want the development default", cfg.Environment)
	}
}

func TestResetTokenExposureRequiresExactOptIn(t *testing.T) {
	for _, value := range []string{"1", "yes", "TRUE", "on", ""} {
		t.Setenv("DEV_EXPOSE_RESET_TOKEN", value)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load with %q: %v", value, err)
		}
		if cfg.DevExposeResetToken {
			t.Fatalf("value %q should not enable reset-token exposure", value)
		}
	}

	t.Setenv("DEV_EXPOSE_RESET_TOKEN", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.DevExposeResetToken {
		t.Fatal(`"true" should enable reset-token exposure`)
	}
}

func TestProductionStartsWithRealSecrets(t *testing.T) {
	setProductionSecrets(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("production startup with real secrets failed: %v", err)
	}
	if cfg.Environment != "production" {
		t.Fatalf("environment = %q", cfg.Environment)
	}
}

func TestDefaultsAreUsable(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.GRPCAddr != ":9090" || cfg.MetricsAddr != ":9091" {
		t.Fatalf("unexpected listen defaults: %s %s %s", cfg.HTTPAddr, cfg.GRPCAddr, cfg.MetricsAddr)
	}
	if cfg.AccessTokenTTL <= 0 || cfg.RefreshTokenTTL <= cfg.AccessTokenTTL {
		t.Fatalf("token TTLs look wrong: access=%s refresh=%s", cfg.AccessTokenTTL, cfg.RefreshTokenTTL)
	}
}
