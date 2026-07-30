package nexusvpn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Client is a typed HTTP client for the NexusVPN control-plane REST API.
// It automatically refreshes the access token on a 401 response using the
// stored refresh token, then retries the request once.
type Client struct {
	baseURL    string
	httpClient *http.Client

	mu           sync.RWMutex
	accessToken  string
	refreshToken string
}

// New creates a Client pointed at baseURL, e.g. "https://api.nexusvpn.example.com/api/v1".
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

type Option func(*Client)

func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

func WithTokens(access, refresh string) Option {
	return func(c *Client) {
		c.accessToken = access
		c.refreshToken = refresh
	}
}

func (c *Client) Tokens() (access, refresh string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.accessToken, c.refreshToken
}

func (c *Client) setTokens(t TokenPair) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = t.AccessToken
	if t.RefreshToken != "" {
		c.refreshToken = t.RefreshToken
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	return c.doWithRetry(ctx, method, path, body, out, true)
}

func (c *Client) doWithRetry(ctx context.Context, method, path string, body, out any, allowRefresh bool) error {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reqBody = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	c.mu.RLock()
	token := c.accessToken
	c.mu.RUnlock()
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized && allowRefresh {
		c.mu.RLock()
		rt := c.refreshToken
		c.mu.RUnlock()
		if rt != "" {
			var tp TokenPair
			if refreshErr := c.doWithRetry(ctx, http.MethodPost, "/auth/refresh", map[string]string{"refreshToken": rt}, &tp, false); refreshErr == nil {
				c.setTokens(tp)
				return c.doWithRetry(ctx, method, path, body, out, false)
			}
		}
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 400 {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(respBody, &envelope)
		return &APIError{Code: envelope.Error.Code, Message: envelope.Error.Message, Status: resp.StatusCode}
	}

	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decoding response: %w", err)
		}
	}
	return nil
}

// --- Auth ---

func (c *Client) Register(ctx context.Context, email, password, displayName string) (*User, error) {
	var u User
	err := c.do(ctx, http.MethodPost, "/auth/register", map[string]string{
		"email": email, "password": password, "displayName": displayName,
	}, &u)
	return &u, err
}

func (c *Client) Login(ctx context.Context, email, password, mfaCode string) (*TokenPair, error) {
	var tp TokenPair
	err := c.do(ctx, http.MethodPost, "/auth/login", map[string]string{
		"email": email, "password": password, "mfaCode": mfaCode,
	}, &tp)
	if err == nil {
		c.setTokens(tp)
	}
	return &tp, err
}

func (c *Client) Logout(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/auth/logout", nil, nil)
}

func (c *Client) ForgotPassword(ctx context.Context, email string) error {
	return c.do(ctx, http.MethodPost, "/auth/password/forgot", map[string]string{"email": email}, nil)
}

func (c *Client) ResetPassword(ctx context.Context, token, newPassword string) error {
	return c.do(ctx, http.MethodPost, "/auth/password/reset", map[string]string{
		"token": token, "newPassword": newPassword,
	}, nil)
}

// --- Networks ---

func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	var networks []Network
	err := c.do(ctx, http.MethodGet, "/networks", nil, &networks)
	return networks, err
}

func (c *Client) CreateNetwork(ctx context.Context, name, description, cidr string, dnsServers []string) (*Network, error) {
	var n Network
	err := c.do(ctx, http.MethodPost, "/networks", map[string]any{
		"name": name, "description": description, "cidr": cidr, "dnsServers": dnsServers,
	}, &n)
	return &n, err
}

func (c *Client) JoinNetwork(ctx context.Context, inviteCode string) (*Network, error) {
	var n Network
	err := c.do(ctx, http.MethodPost, "/networks/join", map[string]string{"inviteCode": inviteCode}, &n)
	return &n, err
}

func (c *Client) GetNetwork(ctx context.Context, networkID string) (*Network, error) {
	var n Network
	err := c.do(ctx, http.MethodGet, "/networks/"+networkID, nil, &n)
	return &n, err
}

func (c *Client) DeleteNetwork(ctx context.Context, networkID string) error {
	return c.do(ctx, http.MethodDelete, "/networks/"+networkID, nil, nil)
}

func (c *Client) ListMembers(ctx context.Context, networkID string) ([]Member, error) {
	var members []Member
	err := c.do(ctx, http.MethodGet, "/networks/"+networkID+"/members", nil, &members)
	return members, err
}

func (c *Client) RemoveMember(ctx context.Context, networkID, userID string) error {
	return c.do(ctx, http.MethodDelete, "/networks/"+networkID+"/members/"+userID, nil, nil)
}

// --- Devices ---

func (c *Client) ListDevices(ctx context.Context, networkID string) ([]Device, error) {
	var devices []Device
	err := c.do(ctx, http.MethodGet, "/networks/"+networkID+"/devices", nil, &devices)
	return devices, err
}

func (c *Client) RegisterDevice(ctx context.Context, networkID, name, os, osVersion, publicKey string) (*Device, error) {
	var d Device
	err := c.do(ctx, http.MethodPost, "/networks/"+networkID+"/devices", map[string]string{
		"name": name, "os": os, "osVersion": osVersion, "publicKey": publicKey,
	}, &d)
	return &d, err
}

func (c *Client) DeviceHeartbeat(ctx context.Context, deviceID string) error {
	return c.do(ctx, http.MethodPost, "/devices/"+deviceID+"/heartbeat", nil, nil)
}

func (c *Client) ListPeers(ctx context.Context, deviceID string) ([]Device, error) {
	var peers []Device
	err := c.do(ctx, http.MethodGet, "/devices/"+deviceID+"/peers", nil, &peers)
	return peers, err
}

// --- Dashboard / logs ---

func (c *Client) DashboardStats(ctx context.Context) (*DashboardStats, error) {
	var s DashboardStats
	err := c.do(ctx, http.MethodGet, "/dashboard/stats", nil, &s)
	return &s, err
}

func (c *Client) AuditLogs(ctx context.Context, networkID string) ([]AuditLog, error) {
	var logs []AuditLog
	err := c.do(ctx, http.MethodGet, "/logs/audit?networkId="+networkID, nil, &logs)
	return logs, err
}

func (c *Client) ConnectionLogs(ctx context.Context, networkID string) ([]ConnectionLog, error) {
	var logs []ConnectionLog
	err := c.do(ctx, http.MethodGet, "/logs/connections?networkId="+networkID, nil, &logs)
	return logs, err
}
