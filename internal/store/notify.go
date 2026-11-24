package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const pgChannel = "config_changed"

// ChangeEvent is received when another pod mutates shared state.
type ChangeEvent struct {
	Type string `json:"type"` // "rules", "agents", "reviews", "revocations", "jti"
}

// Notifier broadcasts and subscribes to config changes via PostgreSQL LISTEN/NOTIFY.
// Used in PostgreSQL mode to replace Redis pub/sub for config sync.
type Notifier struct {
	pool *pgxpool.Pool
}

// NewNotifier creates a LISTEN/NOTIFY notifier using a raw pgx pool.
// connStr is the same PostgreSQL URL used for the main database.
func NewNotifier(connStr string) (*Notifier, error) {
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		return nil, fmt.Errorf("notifier pool: %w", err)
	}
	return &Notifier{pool: pool}, nil
}

// Publish sends a NOTIFY with the change type so other pods can reload.
func (n *Notifier) Publish(ctx context.Context, changeType string) error {
	payload, _ := json.Marshal(ChangeEvent{Type: changeType})
	// Use pg_notify() function with parameterized args to avoid SQL injection
	_, err := n.pool.Exec(ctx, "SELECT pg_notify($1, $2)", pgChannel, string(payload))
	if err != nil {
		slog.Warn("pg notify failed", "component", "notifier", "type", changeType, "error", err)
	}
	return err
}

// Subscribe returns a channel that delivers ChangeEvent from other pods.
// Reconnects automatically on error with exponential backoff.
// The channel is closed when ctx is cancelled.
func (n *Notifier) Subscribe(ctx context.Context) <-chan ChangeEvent {
	ch := make(chan ChangeEvent, 32)
	go func() {
		defer close(ch)
		backoff := time.Second
		for {
			if err := n.listen(ctx, ch); err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("pg listen error, reconnecting", "component", "notifier", "error", err, "backoff", backoff)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, 30*time.Second)
			}
		}
	}()
	return ch
}

func (n *Notifier) listen(ctx context.Context, ch chan<- ChangeEvent) error {
	conn, err := n.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN "+pgChannel); err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	for {
		notification, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return fmt.Errorf("wait notification: %w", err)
		}
		var event ChangeEvent
		if json.Unmarshal([]byte(notification.Payload), &event) != nil {
			continue
		}
		select {
		case ch <- event:
		default:
			slog.Warn("pg notify channel full, dropping", "component", "notifier", "type", event.Type)
		}
	}
}

// For returns a ConfigNotifier scoped to a specific change type.
// The returned notifier implements the ConfigNotifier interface used by
// rules.Engine, admin.AgentHandlers, and review.Store.
func (n *Notifier) For(changeType string) ConfigNotifier {
	return &pgConfigNotifier{notifier: n, changeType: changeType}
}

// Close shuts down the notification pool.
func (n *Notifier) Close() {
	n.pool.Close()
}
