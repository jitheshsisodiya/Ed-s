// Package config manages the local NexusVPN client configuration and
// keystore: the control-plane server URL, auth tokens, this device's
// Curve25519 WireGuard keypair, and per-network connection state. It is
// persisted as JSON under the OS-appropriate user config directory
// (os.UserConfigDir()) so no elevated privileges are required just to
// store client state (only bringing up the TUN device needs that).
package config

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
)

// DirName is the subdirectory created under the OS user config directory.
const DirName = "nexusvpn"

// FileName is the config file name within DirName.
const FileName = "config.json"

// Keypair is a WireGuard-compatible Curve25519 keypair, stored as the
// standard WireGuard base64 encoding of the raw 32-byte keys.
type Keypair struct {
	PrivateKey string `json:"privateKey"`
	PublicKey  string `json:"publicKey"`
}

// IsZero reports whether the keypair has not been generated yet.
func (k Keypair) IsZero() bool {
	return k.PrivateKey == "" || k.PublicKey == ""
}

// NetworkState holds this device's local knowledge of a joined network.
type NetworkState struct {
	NetworkID  string    `json:"networkId"`
	Name       string    `json:"name"`
	DeviceID   string    `json:"deviceId"`
	VirtualIP  string    `json:"virtualIp"`
	CIDR       string    `json:"cidr"`
	DNSServers []string  `json:"dnsServers,omitempty"`
	JoinedAt   time.Time `json:"joinedAt"`
	// AutoConnect indicates the tunnel should be brought up for this
	// network automatically (e.g. on `up` with no explicit target, or on
	// daemon/desktop-app startup).
	AutoConnect bool `json:"autoConnect"`
}

// Config is the full persisted client configuration.
type Config struct {
	ServerURL string `json:"serverUrl"`

	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	TokenExpiry  time.Time `json:"tokenExpiry,omitempty"`
	UserEmail    string    `json:"userEmail,omitempty"`

	DeviceName string  `json:"deviceName"`
	Keypair    Keypair `json:"keypair"`

	// Networks is keyed by network ID.
	Networks map[string]*NetworkState `json:"networks"`

	// STUNServers overrides the default STUN server list, if set.
	STUNServers []string `json:"stunServers,omitempty"`
}

// NewEmpty returns a freshly initialized Config with a generated keypair
// and a sane default device name.
func NewEmpty() (*Config, error) {
	kp, err := GenerateKeypair()
	if err != nil {
		return nil, fmt.Errorf("generate keypair: %w", err)
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "nexusvpn-device"
	}
	return &Config{
		DeviceName: hostname,
		Keypair:    kp,
		Networks:   map[string]*NetworkState{},
	}, nil
}

// GenerateKeypair creates a new Curve25519 keypair in WireGuard's standard
// base64 wire format.
func GenerateKeypair() (Keypair, error) {
	var priv [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		return Keypair{}, err
	}
	// Clamp per RFC 7748 / WireGuard convention.
	priv[0] &= 248
	priv[31] &= 127
	priv[31] |= 64

	pub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		return Keypair{}, err
	}
	return Keypair{
		PrivateKey: base64.StdEncoding.EncodeToString(priv[:]),
		PublicKey:  base64.StdEncoding.EncodeToString(pub),
	}, nil
}

// Dir returns the directory NexusVPN config is stored in, creating it if
// it does not already exist.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	dir := filepath.Join(base, DirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// Path returns the full path to the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FileName), nil
}

// Store provides mutex-guarded, atomic load/save of the Config on disk.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore constructs a Store bound to the default per-OS config path.
func NewStore() (*Store, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	return &Store{path: p}, nil
}

// NewStoreAt constructs a Store bound to an explicit path (mainly for
// tests).
func NewStoreAt(path string) *Store {
	return &Store{path: path}
}

// Load reads the config file, returning a freshly-initialized Config (with
// a newly generated keypair) if none exists yet.
func (s *Store) Load() (*Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

func (s *Store) loadLocked() (*Config, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		cfg, ferr := NewEmpty()
		if ferr != nil {
			return nil, ferr
		}
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Networks == nil {
		cfg.Networks = map[string]*NetworkState{}
	}
	if cfg.Keypair.IsZero() {
		kp, err := GenerateKeypair()
		if err != nil {
			return nil, err
		}
		cfg.Keypair = kp
	}
	return &cfg, nil
}

// Save persists cfg to disk atomically (write-temp-then-rename) with
// owner-only permissions, since it contains auth tokens and a private key.
func (s *Store) Save(cfg *Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked(cfg)
}

func (s *Store) saveLocked(cfg *Config) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename temp config into place: %w", err)
	}
	return nil
}

// Update loads the config, applies fn, and saves it back, all under the
// store's lock, so callers get read-modify-write atomicity.
func (s *Store) Update(fn func(cfg *Config) error) (*Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	if err := fn(cfg); err != nil {
		return nil, err
	}
	if err := s.saveLocked(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// IsLoggedIn reports whether the config has a usable access token.
func (c *Config) IsLoggedIn() bool {
	return c.AccessToken != "" && c.ServerURL != ""
}

// Network returns the stored state for a network ID, if present.
func (c *Config) Network(id string) (*NetworkState, bool) {
	ns, ok := c.Networks[id]
	return ns, ok
}
