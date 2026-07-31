package redis

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

const iceKeyPrefix = "nexusvpn:ice:"

func iceKey(fromDevice, toDevice uuid.UUID) string {
	return iceKeyPrefix + fromDevice.String() + ":" + toDevice.String()
}

// ICEStore is a Redis-backed implementation of domain.ICEStore: a short-lived
// rendezvous point where each side of a candidate exchange writes its
// candidates addressed to the other, and reads back whatever the other side
// has already written.
type ICEStore struct {
	rdb *redis.Client
}

// NewICEStore builds an ICEStore.
func NewICEStore(rdb *redis.Client) *ICEStore {
	return &ICEStore{rdb: rdb}
}

// Put stores fromDevice's candidates addressed to toDevice, expiring after ttl.
func (s *ICEStore) Put(ctx context.Context, fromDevice, toDevice uuid.UUID, candidates []domain.ICECandidate, ttl time.Duration) error {
	b, err := json.Marshal(candidates)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, iceKey(fromDevice, toDevice), b, ttl).Err()
}

// Get retrieves candidates forDevice previously received from fromDevice.
func (s *ICEStore) Get(ctx context.Context, forDevice, fromDevice uuid.UUID) ([]domain.ICECandidate, error) {
	b, err := s.rdb.Get(ctx, iceKey(fromDevice, forDevice)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}
	var candidates []domain.ICECandidate
	if err := json.Unmarshal(b, &candidates); err != nil {
		return nil, err
	}
	return candidates, nil
}
