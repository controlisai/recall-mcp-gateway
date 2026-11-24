package store

import "context"

// ConfigNotifier broadcasts config changes to other pods.
// In PostgreSQL mode: wraps Notifier (PG LISTEN/NOTIFY). Data payload is ignored
// since all pods share the same database — they just reload on notification.
// In Redis mode: RedisConfigStore wrapper — data is the full JSON payload.
// In standalone SQLite mode: nil (caller checks before calling).
type ConfigNotifier interface {
	NotifyChange(configType string, data []byte) error
}

// pgConfigNotifier wraps a scoped PG notifier to implement ConfigNotifier.
// The data parameter is ignored — PG pods read from the shared database.
type pgConfigNotifier struct {
	notifier   *Notifier
	changeType string
}

func (p *pgConfigNotifier) NotifyChange(configType string, data []byte) error {
	return p.notifier.Publish(context.Background(), p.changeType)
}
