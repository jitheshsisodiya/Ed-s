package redis

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/jitheshsisodiya/Ed-s/backend/internal/domain"
)

const peerEventChannelPrefix = "nexusvpn:peerevents:"

func peerEventChannel(networkID uuid.UUID) string {
	return peerEventChannelPrefix + networkID.String()
}

type peerEventPayload struct {
	Type      string `json:"type"`
	NetworkID string `json:"networkId"`
	DeviceID  string `json:"deviceId"`
}

// PeerEventBus is a Redis-pubsub-backed implementation of domain.PeerEventBus.
// Publishing/subscribing via Redis (rather than an in-process channel) is
// what lets StreamPeerUpdates/the WebSocket hub fan out correctly across
// multiple backend replicas.
type PeerEventBus struct {
	rdb *redis.Client
}

// NewPeerEventBus builds a PeerEventBus.
func NewPeerEventBus(rdb *redis.Client) *PeerEventBus {
	return &PeerEventBus{rdb: rdb}
}

// Publish broadcasts a PeerEvent to every subscriber of its network.
func (b *PeerEventBus) Publish(ctx context.Context, event domain.PeerEvent) error {
	payload := peerEventPayload{
		Type:      string(event.Type),
		NetworkID: event.NetworkID.String(),
		DeviceID:  event.DeviceID.String(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, peerEventChannel(event.NetworkID), raw).Err()
}

// subscription implements domain.PeerEventSubscription over a Redis pubsub
// connection subscribed to one channel per network ID.
type subscription struct {
	pubsub *redis.PubSub
	events chan domain.PeerEvent
	cancel context.CancelFunc
}

func (s *subscription) Events() <-chan domain.PeerEvent { return s.events }

func (s *subscription) Close() error {
	s.cancel()
	return s.pubsub.Close()
}

// Subscribe opens a live subscription to peer events for the given networks.
// The returned subscription's Events channel is closed once ctx is done or
// Close is called.
func (b *PeerEventBus) Subscribe(ctx context.Context, networkIDs []uuid.UUID) (domain.PeerEventSubscription, error) {
	channels := make([]string, 0, len(networkIDs))
	for _, id := range networkIDs {
		channels = append(channels, peerEventChannel(id))
	}

	subCtx, cancel := context.WithCancel(ctx)
	ps := b.rdb.Subscribe(subCtx, channels...)
	if _, err := ps.Receive(subCtx); err != nil {
		cancel()
		return nil, err
	}

	sub := &subscription{pubsub: ps, events: make(chan domain.PeerEvent, 64), cancel: cancel}

	go func() {
		defer close(sub.events)
		ch := ps.Channel()
		for {
			select {
			case <-subCtx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var payload peerEventPayload
				if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
					continue
				}
				netID, err := uuid.Parse(payload.NetworkID)
				if err != nil {
					continue
				}
				devID, err := uuid.Parse(payload.DeviceID)
				if err != nil {
					continue
				}
				event := domain.PeerEvent{
					Type:      domain.PeerEventType(payload.Type),
					NetworkID: netID,
					DeviceID:  devID,
				}
				select {
				case sub.events <- event:
				case <-subCtx.Done():
					return
				}
			}
		}
	}()

	return sub, nil
}
