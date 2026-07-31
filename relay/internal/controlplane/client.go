// Package controlplane talks to the NexusVPN backend's internal relay
// endpoints, so a relay node can advertise itself and report load without
// holding any database credentials of its own.
package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client calls the backend's /internal/relay/* endpoints, authenticated
// with the shared relay secret.
type Client struct {
	baseURL string
	secret  string
	http    *http.Client
}

// New builds a Client. baseURL is the backend's root (e.g.
// "http://backend:8080"), without the /internal suffix.
func New(baseURL, secret string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		secret:  secret,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// RegisterRequest describes this relay node to the control plane.
type RegisterRequest struct {
	Region      string `json:"region"`
	Hostname    string `json:"hostname"`
	PublicKey   string `json:"publicKey"`
	ControlPort int    `json:"controlPort"`
	RelayPort   int    `json:"relayPort"`
	Capacity    int    `json:"capacity"`
}

// Register advertises this node and returns the relay ID the control plane
// assigned it. Registration is idempotent on (hostname, relayPort), so a
// restarting node reclaims its existing row.
func (c *Client) Register(ctx context.Context, req RegisterRequest) (string, error) {
	var resp struct {
		RelayID string `json:"relayId"`
	}
	if err := c.post(ctx, "/internal/relay/register", req, &resp); err != nil {
		return "", err
	}
	if resp.RelayID == "" {
		return "", fmt.Errorf("controlplane: register returned no relay ID")
	}
	return resp.RelayID, nil
}

// Heartbeat reports liveness and the node's current session count.
func (c *Client) Heartbeat(ctx context.Context, relayID string, currentLoad int) error {
	body := map[string]any{"relayId": relayID, "currentLoad": currentLoad}
	return c.post(ctx, "/internal/relay/heartbeat", body, nil)
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.secret)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var envelope struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&envelope)
		if envelope.Error.Code != "" {
			return fmt.Errorf("controlplane %s: %s: %s", path, envelope.Error.Code, envelope.Error.Message)
		}
		return fmt.Errorf("controlplane %s: unexpected status %d", path, resp.StatusCode)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
