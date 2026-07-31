package http

import (
	"time"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
	"github.com/jitheshsisodiya/Ed-s/backend/internal/usecase"
)

// --- Response DTOs (field names match components.schemas in api/openapi.yaml) ---

type userDTO struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	MFAEnabled  bool      `json:"mfaEnabled"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

func toUserDTO(u *domain.User) userDTO {
	return userDTO{
		ID:          u.ID.String(),
		Email:       u.Email,
		DisplayName: u.DisplayName,
		MFAEnabled:  u.MFAEnabled,
		Status:      string(u.Status),
		CreatedAt:   u.CreatedAt,
	}
}

type tokenPairDTO struct {
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresIn    int    `json:"expiresIn,omitempty"`
	MFARequired  bool   `json:"mfaRequired,omitempty"`
}

func toTokenPairDTO(r *usecase.AuthResult) tokenPairDTO {
	return tokenPairDTO{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		ExpiresIn:    r.ExpiresIn,
		MFARequired:  r.MFARequired,
	}
}

type networkDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CIDR        string    `json:"cidr"`
	DNSServers  []string  `json:"dnsServers"`
	Role        string    `json:"role,omitempty"`
	MemberCount int       `json:"memberCount"`
	DeviceCount int       `json:"deviceCount"`
	InviteCode  string    `json:"inviteCode,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func toNetworkDTO(n *domain.Network) networkDTO {
	dns := n.DNSServers
	if dns == nil {
		dns = []string{}
	}
	return networkDTO{
		ID:          n.ID.String(),
		Name:        n.Name,
		Description: n.Description,
		CIDR:        n.CIDR,
		DNSServers:  dns,
		Role:        string(n.CallerRole),
		MemberCount: n.MemberCount,
		DeviceCount: n.DeviceCount,
		InviteCode:  n.InviteCode,
		CreatedAt:   n.CreatedAt,
	}
}

func toNetworkDTOs(ns []*domain.Network) []networkDTO {
	out := make([]networkDTO, 0, len(ns))
	for _, n := range ns {
		out = append(out, toNetworkDTO(n))
	}
	return out
}

type memberDTO struct {
	UserID      string    `json:"userId"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	JoinedAt    time.Time `json:"joinedAt"`
}

func toMemberDTOs(ms []*domain.NetworkMember) []memberDTO {
	out := make([]memberDTO, 0, len(ms))
	for _, m := range ms {
		out = append(out, memberDTO{
			UserID:      m.UserID.String(),
			Email:       m.Email,
			DisplayName: m.DisplayName,
			Role:        string(m.Role),
			JoinedAt:    m.JoinedAt,
		})
	}
	return out
}

type deviceDTO struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	OS            string     `json:"os"`
	OSVersion     string     `json:"osVersion"`
	PublicKey     string     `json:"publicKey"`
	VirtualIP     string     `json:"virtualIp"`
	LastPublicIP  string     `json:"lastPublicIp,omitempty"`
	LastPrivateIP string     `json:"lastPrivateIp,omitempty"`
	Status        string     `json:"status"`
	NATType       string     `json:"natType,omitempty"`
	BytesSent     int64      `json:"bytesSent"`
	BytesReceived int64      `json:"bytesReceived"`
	LastSeenAt    *time.Time `json:"lastSeenAt,omitempty"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func toDeviceDTO(d *domain.Device) deviceDTO {
	return deviceDTO{
		ID:            d.ID.String(),
		Name:          d.Name,
		OS:            string(d.OS),
		OSVersion:     d.OSVersion,
		PublicKey:     d.PublicKey,
		VirtualIP:     d.VirtualIP,
		LastPublicIP:  deref(d.LastPublicIP),
		LastPrivateIP: deref(d.LastPrivateIP),
		Status:        string(d.Status),
		NATType:       deref(d.NATType),
		BytesSent:     d.BytesSent,
		BytesReceived: d.BytesReceived,
		LastSeenAt:    d.LastSeenAt,
	}
}

func toDeviceDTOs(ds []*domain.Device) []deviceDTO {
	out := make([]deviceDTO, 0, len(ds))
	for _, d := range ds {
		out = append(out, toDeviceDTO(d))
	}
	return out
}

type auditLogDTO struct {
	ID          string    `json:"id"`
	ActorUserID string    `json:"actorUserId,omitempty"`
	NetworkID   string    `json:"networkId,omitempty"`
	Action      string    `json:"action"`
	TargetType  string    `json:"targetType,omitempty"`
	TargetID    string    `json:"targetId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func toAuditLogDTOs(ls []*domain.AuditLog) []auditLogDTO {
	out := make([]auditLogDTO, 0, len(ls))
	for _, l := range ls {
		dto := auditLogDTO{
			ID:         l.ID.String(),
			Action:     string(l.Action),
			TargetType: deref(l.TargetType),
			TargetID:   deref(l.TargetID),
			CreatedAt:  l.CreatedAt,
		}
		if l.ActorUserID != nil {
			dto.ActorUserID = l.ActorUserID.String()
		}
		if l.NetworkID != nil {
			dto.NetworkID = l.NetworkID.String()
		}
		out = append(out, dto)
	}
	return out
}

type connectionLogDTO struct {
	ID           string    `json:"id"`
	NetworkID    string    `json:"networkId"`
	DeviceID     string    `json:"deviceId"`
	PeerDeviceID string    `json:"peerDeviceId,omitempty"`
	EventType    string    `json:"eventType"`
	LatencyMs    *int      `json:"latencyMs,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

func toConnectionLogDTOs(ls []*domain.ConnectionLog) []connectionLogDTO {
	out := make([]connectionLogDTO, 0, len(ls))
	for _, l := range ls {
		dto := connectionLogDTO{
			ID:        l.ID.String(),
			NetworkID: l.NetworkID.String(),
			DeviceID:  l.DeviceID.String(),
			EventType: string(l.EventType),
			LatencyMs: l.LatencyMs,
			CreatedAt: l.CreatedAt,
		}
		if l.PeerDeviceID != nil {
			dto.PeerDeviceID = l.PeerDeviceID.String()
		}
		out = append(out, dto)
	}
	return out
}

type dashboardStatsDTO struct {
	ActiveUsers         int     `json:"activeUsers"`
	ActiveNetworks      int     `json:"activeNetworks"`
	OnlineDevices       int     `json:"onlineDevices"`
	TotalDevices        int     `json:"totalDevices"`
	RelayBandwidthBytes int64   `json:"relayBandwidthBytes"`
	P2PConnectionRatio  float64 `json:"p2pConnectionRatio"`
}

func toDashboardStatsDTO(s *usecase.DashboardStats) dashboardStatsDTO {
	return dashboardStatsDTO{
		ActiveUsers:         s.ActiveUsers,
		ActiveNetworks:      s.ActiveNetworks,
		OnlineDevices:       s.OnlineDevices,
		TotalDevices:        s.TotalDevices,
		RelayBandwidthBytes: s.RelayBandwidthBytes,
		P2PConnectionRatio:  s.P2PConnectionRatio,
	}
}

// --- Request DTOs ---

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	MFACode  string `json:"mfaCode"`
}

type refreshRequest struct {
	RefreshToken string `json:"refreshToken"`
}

type mfaVerifyRequest struct {
	Code string `json:"code"`
}

type forgotPasswordRequest struct {
	Email string `json:"email"`
}

type resetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"newPassword"`
}

type networkCreateRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	CIDR        string   `json:"cidr"`
	DNSServers  []string `json:"dnsServers"`
}

type networkUpdateRequest struct {
	Name        *string  `json:"name"`
	Description *string  `json:"description"`
	CIDR        *string  `json:"cidr"`
	DNSServers  []string `json:"dnsServers"`
}

type joinNetworkRequest struct {
	InviteCode string `json:"inviteCode"`
}

type memberRoleRequest struct {
	Role string `json:"role"`
}

type deviceRegisterRequest struct {
	Name      string `json:"name"`
	OS        string `json:"os"`
	OSVersion string `json:"osVersion"`
	PublicKey string `json:"publicKey"`
}

type heartbeatRequest struct {
	PublicIP      *string `json:"publicIp"`
	PrivateIP     *string `json:"privateIp"`
	NATType       *string `json:"natType"`
	BytesSent     int64   `json:"bytesSent"`
	BytesReceived int64   `json:"bytesReceived"`
}
