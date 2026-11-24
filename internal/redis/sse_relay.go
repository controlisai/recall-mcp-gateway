package redis

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const sseChannel = "sse:events"

// RedisSSERelay relays SSE audit events across pods via Redis pub/sub.
// Each pod publishes events it generates; each pod subscribes to receive
// events from all other pods and injects them into its local SSE feed.
type RedisSSERelay struct {
	rdb *goredis.Client
}

// NewRedisSSERelay creates a Redis pub/sub relay for SSE events.
func NewRedisSSERelay(rdb *goredis.Client) *RedisSSERelay {
	return &RedisSSERelay{rdb: rdb}
}

// PublishEvent broadcasts a raw audit/event JSON blob to all pods.
func (r *RedisSSERelay) PublishEvent(entry json.RawMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.rdb.Publish(ctx, sseChannel, []byte(entry)).Err(); err != nil {
		slog.Warn("redis sse relay: PublishEvent failed", "error", err)
		return err
	}
	return nil
}

// SubscribeEvents returns a channel that delivers raw event JSON blobs
// published by any pod. The channel is closed when ctx is cancelled.
func (r *RedisSSERelay) SubscribeEvents(ctx context.Context) <-chan json.RawMessage {
	ch := make(chan json.RawMessage, 64)
	go func() {
		defer close(ch)
		sub := r.rdb.Subscribe(ctx, sseChannel)
		defer sub.Close()
		msgCh := sub.Channel()
		for {
			select {
			case msg, ok := <-msgCh:
				if !ok {
					return
				}
				select {
				case ch <- json.RawMessage(msg.Payload):
				default:
					slog.Warn("redis sse relay: SubscribeEvents channel full, dropping event")
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// Close is a no-op — the Redis client lifecycle is managed externally.
func (r *RedisSSERelay) Close() {}
