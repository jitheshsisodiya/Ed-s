package usecase

import (
	"context"
	"time"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

// DashboardStats is the aggregate analytics payload for the admin dashboard.
type DashboardStats struct {
	ActiveUsers         int
	ActiveNetworks      int
	OnlineDevices       int
	TotalDevices        int
	RelayBandwidthBytes int64
	P2PConnectionRatio  float64
}

// DashboardService computes aggregate stats across Postgres and the Redis
// presence store.
type DashboardService struct {
	devices      domain.DeviceRepository
	connLogs     domain.ConnectionLogRepository
	presence     domain.PresenceStore
	activeWindow time.Duration
}

// NewDashboardService builds a DashboardService. activeWindow controls how
// recently a device must have been seen to count as "active" (default 5m
// is applied by the caller if zero).
func NewDashboardService(devices domain.DeviceRepository, connLogs domain.ConnectionLogRepository, presence domain.PresenceStore, activeWindow time.Duration) *DashboardService {
	if activeWindow <= 0 {
		activeWindow = 5 * time.Minute
	}
	return &DashboardService{devices: devices, connLogs: connLogs, presence: presence, activeWindow: activeWindow}
}

// Stats computes the current DashboardStats snapshot.
func (s *DashboardService) Stats(ctx context.Context) (*DashboardStats, error) {
	since := time.Now().Add(-s.activeWindow)

	activeUsers, err := s.devices.CountDistinctActiveUsers(ctx, since)
	if err != nil {
		return nil, err
	}
	activeNetworks, err := s.devices.CountDistinctActiveNetworks(ctx, since)
	if err != nil {
		return nil, err
	}
	totalDevices, err := s.devices.CountTotal(ctx)
	if err != nil {
		return nil, err
	}

	onlineDevices := 0
	if s.presence != nil {
		onlineDevices, err = s.presence.CountOnline(ctx)
		if err != nil {
			return nil, err
		}
	}

	relayBytes, err := s.connLogs.SumBytesByEventType(ctx, domain.ConnEventRelayFallback, time.Time{})
	if err != nil {
		return nil, err
	}

	counts, err := s.connLogs.CountByEventType(ctx, time.Time{})
	if err != nil {
		return nil, err
	}
	p2p := counts[domain.ConnEventP2PEstablished]
	relay := counts[domain.ConnEventRelayFallback]
	ratio := 0.0
	if total := p2p + relay; total > 0 {
		ratio = float64(p2p) / float64(total)
	}

	return &DashboardStats{
		ActiveUsers:         activeUsers,
		ActiveNetworks:      activeNetworks,
		OnlineDevices:       onlineDevices,
		TotalDevices:        totalDevices,
		RelayBandwidthBytes: relayBytes,
		P2PConnectionRatio:  ratio,
	}, nil
}
