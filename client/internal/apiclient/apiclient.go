// Package apiclient is a REST client for the NexusVPN control plane's
// OpenAPI surface (api/openapi.yaml): auth (login/refresh/logout) and
// network/device management. Real-time peer signaling is handled
// separately by internal/coordination (gRPC).
package apiclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// APIError represents a non-2xx JSON error response from the control
// plane, matching the `Error` schema in api/openapi.yaml.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("apiclient: %d %s: %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("apiclient: unexpected status %d", e.StatusCode)
}

// TokenPair mirrors the `TokenPair` schema.
type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
	MFARequired  bool   `json:"mfaRequired"`
}

// User mirrors the `User` schema.
type User struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	MFAEnabled  bool   `json:"mfaEnabled"`
	Status      string `json:"status"`
	CreatedAt   string `json:"createdAt"`
}

// Network mirrors the `Network` schema.
type Network struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	CIDR        string   `json:"cidr"`
	DNSServers  []string `json:"dnsServers"`
	Role        string   `json:"role"`
	MemberCount int      `json:"memberCount"`
	DeviceCount int      `json:"deviceCount"`
	InviteCode  string   `json:"inviteCode"`
	CreatedAt   string   `json:"createdAt"`
}

// Member mirrors the `Member` schema.
type Member struct {
	UserID      string `json:"userId"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
	JoinedAt    string `json:"joinedAt"`
}

// Device mirrors the `Device` schema.
type Device struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	OS             string `json:"os"`
	OSVersion      string `json:"osVersion"`
	PublicKey      string `json:"publicKey"`
	VirtualIP      string `json:"virtualIp"`
	LastPublicIP   string `json:"lastPublicIp"`
	Status         string `json:"status"`
	NATType        string `json:"natType"`
	LatencyMs      int    `json:"latencyMs"`
	BytesSent      int64  `json:"bytesSent"`
	BytesReceived  int64  `json:"bytesReceived"`
	LastSeenAt     string `json:"lastSeenAt"`
}

// Client is a REST client for the control plane API. It is safe for
// concurrent use; AccessToken()/SetTokens() are protected by a mutex so
// it can double as the coordination package's TokenSource.
type Client struct {
	httpClient *http.Client
	baseURL    string

	mu           sync.RWMutex
	accessToken  string
	refreshToken string
	onRefresh    func(TokenPair)
}

// Options configures a new Client.
type Options struct {
	// HTTPClient overrides the default http.Client (e.g. for injecting a
	// custom transport in tests, or a longer timeout).
	HTTPClient *http.Client
	// OnTokenRefresh, if set, is called whenever RefreshToken() succeeds,
	// so callers (e.g. the config store) can persist the new tokens.
	OnTokenRefresh func(TokenPair)
}

// New constructs a Client against baseURL (e.g.
// "https://api.nexusvpn.example.com/api/v1" or
// "http://localhost:8080/api/v1", matching the `servers` list in
// api/openapi.yaml).
func New(baseURL string, opts Options) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		httpClient: hc,
		baseURL:    strings.TrimRight(baseURL, "/"),
		onRefresh:  opts.OnTokenRefresh,
	}
}

// SetTokens installs an access/refresh token pair to authenticate
// subsequent requests (used after Login/RefreshToken, or when restoring
// from local config).
func (c *Client) SetTokens(access, refresh string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = access
	c.refreshToken = refresh
}

// AccessToken implements coordination.TokenSource so the same Client can
// authenticate both REST calls and gRPC coordination calls.
func (c *Client) AccessToken(_ context.Context) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accessToken, nil
}

func (c *Client) refreshTokenValue() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.refreshToken
}

// do performs an HTTP request against path (relative to baseURL),
// marshaling body as JSON if non-nil and unmarshaling a JSON response
// into out if non-nil. If authenticated is true, the bearer token is
// attached (and, on a single 401, a refresh is attempted and the request
// retried once).
func (c *Client) do(ctx context.Context, method, path string, body, out any, authenticated bool) (*http.Response, error) {
	return c.doAttempt(ctx, method, path, body, out, authenticated, true)
}

func (c *Client) doAttempt(ctx context.Context, method, path string, body, out any, authenticated, allowRetry bool) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("apiclient: marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	fullURL := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("apiclient: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if authenticated {
		if tok, _ := c.AccessToken(ctx); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("apiclient: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && authenticated && allowRetry && c.refreshTokenValue() != "" {
		if _, err := c.RefreshToken(ctx); err == nil {
			return c.doAttempt(ctx, method, path, body, out, authenticated, false)
		}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("apiclient: read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		var wrapped struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if len(data) > 0 && json.Unmarshal(data, &wrapped) == nil {
			apiErr.Code = wrapped.Error.Code
			apiErr.Message = wrapped.Error.Message
		}
		return resp, apiErr
	}

	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp, fmt.Errorf("apiclient: decode response: %w", err)
		}
	}
	return resp, nil
}

// --- auth ---

// Register creates a new user account. POST /auth/register
func (c *Client) Register(ctx context.Context, email, password, displayName string) (*User, error) {
	var user User
	_, err := c.do(ctx, http.MethodPost, "/auth/register", map[string]string{
		"email":       email,
		"password":    password,
		"displayName": displayName,
	}, &user, false)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// Login authenticates and stores the resulting tokens on success.
// POST /auth/login
func (c *Client) Login(ctx context.Context, email, password, mfaCode string) (*TokenPair, error) {
	body := map[string]string{"email": email, "password": password}
	if mfaCode != "" {
		body["mfaCode"] = mfaCode
	}
	var tokens TokenPair
	_, err := c.do(ctx, http.MethodPost, "/auth/login", body, &tokens, false)
	if err != nil {
		return nil, err
	}
	if !tokens.MFARequired {
		c.SetTokens(tokens.AccessToken, tokens.RefreshToken)
	}
	return &tokens, nil
}

// RefreshToken exchanges the stored refresh token for a new token pair.
// POST /auth/refresh
func (c *Client) RefreshToken(ctx context.Context) (*TokenPair, error) {
	refresh := c.refreshTokenValue()
	if refresh == "" {
		return nil, fmt.Errorf("apiclient: no refresh token available")
	}
	var tokens TokenPair
	_, err := c.do(ctx, http.MethodPost, "/auth/refresh", map[string]string{"refreshToken": refresh}, &tokens, false)
	if err != nil {
		return nil, err
	}
	c.SetTokens(tokens.AccessToken, tokens.RefreshToken)
	if c.onRefresh != nil {
		c.onRefresh(tokens)
	}
	return &tokens, nil
}

// Logout invalidates the current session server-side. POST /auth/logout
func (c *Client) Logout(ctx context.Context) error {
	_, err := c.do(ctx, http.MethodPost, "/auth/logout", nil, nil, true)
	c.SetTokens("", "")
	return err
}

// --- networks ---

// ListNetworks returns the networks the current user belongs to.
// GET /networks
func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	var networks []Network
	_, err := c.do(ctx, http.MethodGet, "/networks", nil, &networks, true)
	return networks, err
}

// CreateNetwork creates a new network. POST /networks
func (c *Client) CreateNetwork(ctx context.Context, name, description, cidr string, dnsServers []string) (*Network, error) {
	var network Network
	_, err := c.do(ctx, http.MethodPost, "/networks", map[string]any{
		"name":        name,
		"description": description,
		"cidr":        cidr,
		"dnsServers":  dnsServers,
	}, &network, true)
	if err != nil {
		return nil, err
	}
	return &network, nil
}

// JoinNetwork joins a network by invite code. POST /networks/join
func (c *Client) JoinNetwork(ctx context.Context, inviteCode string) (*Network, error) {
	var network Network
	_, err := c.do(ctx, http.MethodPost, "/networks/join", map[string]string{"inviteCode": inviteCode}, &network, true)
	if err != nil {
		return nil, err
	}
	return &network, nil
}

// GetNetwork fetches network details. GET /networks/{networkId}
func (c *Client) GetNetwork(ctx context.Context, networkID string) (*Network, error) {
	var network Network
	_, err := c.do(ctx, http.MethodGet, "/networks/"+url.PathEscape(networkID), nil, &network, true)
	if err != nil {
		return nil, err
	}
	return &network, nil
}

// DeleteNetwork deletes a network. DELETE /networks/{networkId}
func (c *Client) DeleteNetwork(ctx context.Context, networkID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/networks/"+url.PathEscape(networkID), nil, nil, true)
	return err
}

// RotateInvite rotates a network's invite code.
// POST /networks/{networkId}/invite
func (c *Client) RotateInvite(ctx context.Context, networkID string) (string, string, error) {
	var out struct {
		InviteCode string `json:"inviteCode"`
		ExpiresAt  string `json:"expiresAt"`
	}
	_, err := c.do(ctx, http.MethodPost, "/networks/"+url.PathEscape(networkID)+"/invite", nil, &out, true)
	return out.InviteCode, out.ExpiresAt, err
}

// ListMembers lists a network's members.
// GET /networks/{networkId}/members
func (c *Client) ListMembers(ctx context.Context, networkID string) ([]Member, error) {
	var members []Member
	_, err := c.do(ctx, http.MethodGet, "/networks/"+url.PathEscape(networkID)+"/members", nil, &members, true)
	return members, err
}

// --- devices ---

// ListDevices lists devices registered on a network.
// GET /networks/{networkId}/devices
func (c *Client) ListDevices(ctx context.Context, networkID string) ([]Device, error) {
	var devices []Device
	_, err := c.do(ctx, http.MethodGet, "/networks/"+url.PathEscape(networkID)+"/devices", nil, &devices, true)
	return devices, err
}

// RegisterDevice registers this device on a network via the REST API
// (used for bootstrap/inventory; live topology/endpoint exchange happens
// over the gRPC CoordinationService — see internal/coordination).
// POST /networks/{networkId}/devices
func (c *Client) RegisterDevice(ctx context.Context, networkID, name, os, osVersion, publicKey string) (*Device, error) {
	var device Device
	_, err := c.do(ctx, http.MethodPost, "/networks/"+url.PathEscape(networkID)+"/devices", map[string]string{
		"name":      name,
		"os":        os,
		"osVersion": osVersion,
		"publicKey": publicKey,
	}, &device, true)
	if err != nil {
		return nil, err
	}
	return &device, nil
}

// GetDevice fetches device details. GET /devices/{deviceId}
func (c *Client) GetDevice(ctx context.Context, deviceID string) (*Device, error) {
	var device Device
	_, err := c.do(ctx, http.MethodGet, "/devices/"+url.PathEscape(deviceID), nil, &device, true)
	if err != nil {
		return nil, err
	}
	return &device, nil
}

// DeleteDevice removes a device. DELETE /devices/{deviceId}
func (c *Client) DeleteDevice(ctx context.Context, deviceID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/devices/"+url.PathEscape(deviceID), nil, nil, true)
	return err
}

// SendHeartbeat pings the REST heartbeat endpoint (a lighter-weight
// liveness signal than the gRPC Heartbeat RPC; both may be used).
// POST /devices/{deviceId}/heartbeat
func (c *Client) SendHeartbeat(ctx context.Context, deviceID string) error {
	_, err := c.do(ctx, http.MethodPost, "/devices/"+url.PathEscape(deviceID)+"/heartbeat", nil, nil, true)
	return err
}

// ListPeers lists peer devices (with endpoints) for this device's
// networks. GET /devices/{deviceId}/peers
func (c *Client) ListPeers(ctx context.Context, deviceID string) ([]Device, error) {
	var peers []Device
	_, err := c.do(ctx, http.MethodGet, "/devices/"+url.PathEscape(deviceID)+"/peers", nil, &peers, true)
	return peers, err
}
