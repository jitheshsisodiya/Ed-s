// Package nexusvpn is a typed Go client SDK for the NexusVPN control-plane
// REST API described in api/openapi.yaml.
package nexusvpn

import "time"

type User struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	MFAEnabled  bool      `json:"mfaEnabled"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

type TokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
	MFARequired  bool   `json:"mfaRequired"`
}

type Network struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	CIDR         string    `json:"cidr"`
	DNSServers   []string  `json:"dnsServers"`
	Role         string    `json:"role"`
	MemberCount  int       `json:"memberCount"`
	DeviceCount  int       `json:"deviceCount"`
	InviteCode   string    `json:"inviteCode"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Member struct {
	UserID      string    `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
}

type Device struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	OS             string    `json:"os"`
	OSVersion      string    `json:"osVersion"`
	PublicKey      string    `json:"publicKey"`
	VirtualIP      string    `json:"virtualIp"`
	LastPublicIP   string    `json:"lastPublicIp"`
	Status         string    `json:"status"`
	NATType        string    `json:"natType"`
	LatencyMs      int       `json:"latencyMs"`
	BytesSent      int64     `json:"bytesSent"`
	BytesReceived  int64     `json:"bytesReceived"`
	LastSeenAt     time.Time `json:"lastSeenAt"`
}

type ConnectionLog struct {
	ID           string    `json:"id"`
	DeviceID     string    `json:"deviceId"`
	PeerDeviceID string    `json:"peerDeviceId"`
	EventType    string    `json:"eventType"`
	LatencyMs    int       `json:"latencyMs"`
	CreatedAt    time.Time `json:"createdAt"`
}

type AuditLog struct {
	ID          string    `json:"id"`
	ActorUserID string    `json:"actorUserId"`
	Action      string    `json:"action"`
	TargetType  string    `json:"targetType"`
	TargetID    string    `json:"targetId"`
	CreatedAt   time.Time `json:"createdAt"`
}

type DashboardStats struct {
	ActiveUsers          int     `json:"activeUsers"`
	ActiveNetworks       int     `json:"activeNetworks"`
	OnlineDevices        int     `json:"onlineDevices"`
	TotalDevices         int     `json:"totalDevices"`
	RelayBandwidthBytes  int64   `json:"relayBandwidthBytes"`
	P2PConnectionRatio   float32 `json:"p2pConnectionRatio"`
}

type APIError struct {
	Code    string
	Message string
	Status  int
}

func (e *APIError) Error() string {
	return e.Code + ": " + e.Message
}
