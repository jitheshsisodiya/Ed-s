package config

import (
	"encoding/base64"
	"path/filepath"
	"testing"
)

func TestGenerateKeypair(t *testing.T) {
	kp, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair: %v", err)
	}
	if kp.IsZero() {
		t.Fatalf("generated keypair is zero")
	}
	priv, err := base64.StdEncoding.DecodeString(kp.PrivateKey)
	if err != nil || len(priv) != 32 {
		t.Fatalf("private key not a valid 32-byte base64 value: %v (len=%d)", err, len(priv))
	}
	pub, err := base64.StdEncoding.DecodeString(kp.PublicKey)
	if err != nil || len(pub) != 32 {
		t.Fatalf("public key not a valid 32-byte base64 value: %v (len=%d)", err, len(pub))
	}

	kp2, err := GenerateKeypair()
	if err != nil {
		t.Fatalf("GenerateKeypair (2nd): %v", err)
	}
	if kp.PrivateKey == kp2.PrivateKey || kp.PublicKey == kp2.PublicKey {
		t.Fatalf("two generated keypairs were identical, RNG likely broken")
	}
}

func TestStore_LoadCreatesNewConfigWithKeypair(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "config.json"))

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Keypair.IsZero() {
		t.Fatalf("expected freshly loaded config to have a generated keypair")
	}
	if cfg.Networks == nil {
		t.Fatalf("expected Networks map to be initialized")
	}
	if cfg.IsLoggedIn() {
		t.Fatalf("fresh config should not be logged in")
	}
}

func TestStore_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	store := NewStoreAt(path)

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.ServerURL = "https://control.example.com"
	cfg.AccessToken = "access-token-1"
	cfg.RefreshToken = "refresh-token-1"
	cfg.Networks["net-1"] = &NetworkState{
		NetworkID: "net-1",
		Name:      "Home Lab",
		DeviceID:  "dev-1",
		VirtualIP: "10.77.0.5",
		CIDR:      "10.77.0.0/24",
	}
	if err := store.Save(cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load (reload): %v", err)
	}
	if reloaded.ServerURL != cfg.ServerURL {
		t.Fatalf("ServerURL = %q, want %q", reloaded.ServerURL, cfg.ServerURL)
	}
	if reloaded.AccessToken != cfg.AccessToken {
		t.Fatalf("AccessToken mismatch after reload")
	}
	if reloaded.Keypair != cfg.Keypair {
		t.Fatalf("Keypair changed across save/load, should be stable")
	}
	ns, ok := reloaded.Network("net-1")
	if !ok {
		t.Fatalf("expected network net-1 to be present after reload")
	}
	if ns.VirtualIP != "10.77.0.5" {
		t.Fatalf("VirtualIP = %q, want 10.77.0.5", ns.VirtualIP)
	}
	if !reloaded.IsLoggedIn() {
		t.Fatalf("expected IsLoggedIn() true after setting ServerURL+AccessToken")
	}
}

func TestStore_Update(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "config.json"))

	_, err := store.Update(func(cfg *Config) error {
		cfg.DeviceName = "test-device"
		return nil
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DeviceName != "test-device" {
		t.Fatalf("DeviceName = %q, want test-device", cfg.DeviceName)
	}
}

func TestStore_LoadMissingFileDoesNotError(t *testing.T) {
	dir := t.TempDir()
	store := NewStoreAt(filepath.Join(dir, "nested", "config.json"))
	cfg, err := store.Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if cfg == nil {
		t.Fatalf("expected non-nil config")
	}
}
