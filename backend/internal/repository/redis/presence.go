// Package redis implements the ephemeral, fast-path pieces of the
// coordination plane on top of Redis: device presence (with TTL-based
// auto-expiry), peer topology pubsub fanout (so it works across multiple
// backend replicas), ICE candidate rendezvous, and a simple rate limiter.
package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

const (
	presenceKeyPrefix    = "nexusvpn:presence:device:"
	presenceNetSetPrefix = "nexusvpn:presence:network:"
	presenceAllSet       = "nexusvpn:presence:online"
)

func presenceKey(deviceID uuid.UUID) string {
	return presenceKeyPrefix + deviceID.String()
}

func networkSetKey(networkID uuid.UUID) string {
	return presenceNetSetPrefix + networkID.String()
}

// PresenceStore is a Redis-backed implementation of domain.PresenceStore.
type PresenceStore struct {
	rdb *redis.Client
}

// NewPresenceStore builds a PresenceStore.
func NewPresenceStore(rdb *redis.Client) *PresenceStore {
	return &PresenceStore{rdb: rdb}
}

type presencePayload struct {
	NetworkID   string    `json:"networkId"`
	PublicIP    string    `json:"publicIp"`
	PublicPort  uint32    `json:"publicPort"`
	PrivateIP   string    `json:"privateIp"`
	PrivatePort uint32    `json:"privatePort"`
	NATType     string    `json:"natType"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// SetOnline marks a device online with a TTL'd key, and adds it to its
// network's online-device set (also TTL'd, refreshed on every call, so a
// crashed backend replica doesn't leave stale members behind forever).
func (s *PresenceStore) SetOnline(ctx context.Context, networkID, deviceID uuid.UUID, ep domain.PresenceEndpoint, ttl time.Duration) error {
	payload := presencePayload{
		NetworkID: networkID.String(), PublicIP: ep.PublicIP, PublicPort: ep.PublicPort,
		PrivateIP: ep.PrivateIP, PrivatePort: ep.PrivatePort, NATType: ep.NATType, UpdatedAt: ep.UpdatedAt,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	pipe := s.rdb.TxPipeline()
	pipe.Set(ctx, presenceKey(deviceID), b, ttl)
	pipe.SAdd(ctx, networkSetKey(networkID), deviceID.String())
	pipe.Expire(ctx, networkSetKey(networkID), ttl+time.Minute)
	pipe.SAdd(ctx, presenceAllSet, deviceID.String())
	_, err = pipe.Exec(ctx)
	return err
}

// Remove marks a device offline immediately.
func (s *PresenceStore) Remove(ctx context.Context, networkID, deviceID uuid.UUID) error {
	pipe := s.rdb.TxPipeline()
	pipe.Del(ctx, presenceKey(deviceID))
	pipe.SRem(ctx, networkSetKey(networkID), deviceID.String())
	pipe.SRem(ctx, presenceAllSet, deviceID.String())
	_, err := pipe.Exec(ctx)
	return err
}

// Get returns the last known presence endpoint for a device, or nil if
// it isn't currently marked online (expired or never registered).
func (s *PresenceStore) Get(ctx context.Context, deviceID uuid.UUID) (*domain.PresenceEndpoint, error) {
	b, err := s.rdb.Get(ctx, presenceKey(deviceID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var payload presencePayload
	if err := json.Unmarshal(b, &payload); err != nil {
		return nil, err
	}
	return &domain.PresenceEndpoint{
		PublicIP: payload.PublicIP, PublicPort: payload.PublicPort,
		PrivateIP: payload.PrivateIP, PrivatePort: payload.PrivatePort,
		NATType: payload.NATType, UpdatedAt: payload.UpdatedAt,
	}, nil
}

// OnlineInNetwork returns the set of online device IDs for a network,
// filtering out any set members whose individual presence key has since
// expired (the set TTL is intentionally looser than each member's TTL).
func (s *PresenceStore) OnlineInNetwork(ctx context.Context, networkID uuid.UUID) ([]uuid.UUID, error) {
	members, err := s.rdb.SMembers(ctx, networkSetKey(networkID)).Result()
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, m := range members {
		id, err := uuid.Parse(m)
		if err != nil {
			continue
		}
		exists, err := s.rdb.Exists(ctx, presenceKey(id)).Result()
		if err != nil {
			return nil, err
		}
		if exists == 1 {
			out = append(out, id)
		} else {
			s.rdb.SRem(ctx, networkSetKey(networkID), m)
			s.rdb.SRem(ctx, presenceAllSet, m)
		}
	}
	return out, nil
}

// CountOnline returns the total number of online devices across all networks.
func (s *PresenceStore) CountOnline(ctx context.Context) (int, error) {
	members, err := s.rdb.SMembers(ctx, presenceAllSet).Result()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, m := range members {
		exists, err := s.rdb.Exists(ctx, presenceKeyPrefix+m).Result()
		if err != nil {
			return 0, err
		}
		if exists == 1 {
			count++
		} else {
			s.rdb.SRem(ctx, presenceAllSet, m)
		}
	}
	return count, nil
}
