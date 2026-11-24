package auth

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/store"
	_ "modernc.org/sqlite"
)

// ErrTokenReplay indicates a JWT with this jti has already been used.
var ErrTokenReplay = fmt.Errorf("token replay detected")

// JTICache tracks seen JWT IDs to prevent token replay attacks.
// Optionally backed by SQLite for restart resilience.
type JTICache struct {
	mu       sync.RWMutex
	seen     map[string]time.Time // jti → expires_at
	db       *sql.DB              // optional SQLite backing
	ownsDB   bool                 // true when this cache opened the DB itself
	stop     chan struct{}
	onRecord func(jti string, expiresAt time.Time) // optional hook — called after every new record
}

// NewJTICache creates an in-memory JTI cache (lost on restart).
func NewJTICache() *JTICache {
	c := &JTICache{seen: make(map[string]time.Time), stop: make(chan struct{})}
	go c.cleanupLoop()
	return c
}

// NewSQLiteJTICache creates a JTI cache backed by SQLite.
// Existing entries are loaded on startup — survives restart.
func NewSQLiteJTICache(dbPath string) (*JTICache, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS jti_cache (
		jti TEXT PRIMARY KEY,
		expires_at TEXT NOT NULL
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	c := &JTICache{seen: make(map[string]time.Time), db: db, ownsDB: true, stop: make(chan struct{})}
	now := time.Now()
	rows, err := db.Query("SELECT jti, expires_at FROM jti_cache")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var jti, expStr string
			if rows.Scan(&jti, &expStr) == nil {
				if exp, err := time.Parse(time.RFC3339, expStr); err == nil && exp.After(now) {
					c.seen[jti] = exp
				}
			}
		}
	}
	slog.Info("jti cache loaded", "count", len(c.seen), "path", dbPath)
	go c.cleanupLoop()
	return c, nil
}

// NewJTICacheWithDB creates a JTI cache using an existing *sql.DB connection.
// The caller owns the DB connection — Close() will NOT close the underlying DB.
// The jti_cache table must already exist (created by store.GatewayDB).
func NewJTICacheWithDB(db *sql.DB) *JTICache {
	c := &JTICache{seen: make(map[string]time.Time), db: db, ownsDB: false, stop: make(chan struct{})}
	now := time.Now()
	rows, err := db.Query("SELECT jti, expires_at FROM jti_cache")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var jti, expStr string
			if rows.Scan(&jti, &expStr) == nil {
				if exp, parseErr := time.Parse(time.RFC3339, expStr); parseErr == nil && exp.After(now) {
					c.seen[jti] = exp
				}
			}
		}
	}
	slog.Info("jti cache loaded", "count", len(c.seen), "storage", "gateway.db")
	go c.cleanupLoop()
	return c
}

// Close stops the cleanup goroutine and closes the database.
// When created with NewJTICacheWithDB, the DB is NOT closed (caller owns it).
func (c *JTICache) Close() {
	select {
	case <-c.stop:
	default:
		close(c.stop)
	}
	if c.db != nil && c.ownsDB {
		c.db.Close()
	}
}

// ReloadFromDB reloads all JTI entries from the database into memory.
// Used by PG LISTEN/NOTIFY subscriber to refresh after changes from other pods.
func (c *JTICache) ReloadFromDB() {
	if c.db == nil {
		return
	}
	rows, err := c.db.Query("SELECT jti, expires_at FROM jti_cache")
	if err != nil {
		slog.Warn("failed to reload JTI cache from DB", "component", "jti", "error", err)
		return
	}
	defer rows.Close()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = make(map[string]time.Time)
	for rows.Next() {
		var jti, expiresAt string
		if rows.Scan(&jti, &expiresAt) != nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339, expiresAt); err == nil {
			c.seen[jti] = t
		}
	}
}

// SetOnRecord registers a hook called after every new JTI is recorded.
// Used by main.go to broadcast JTI entries to other pods via Redis/PG NOTIFY.
// The hook is called without the mutex held — safe to perform I/O.
// Not called when a replay is detected or when RecordFromSync is used.
func (c *JTICache) SetOnRecord(fn func(jti string, expiresAt time.Time)) {
	c.mu.Lock()
	c.onRecord = fn
	c.mu.Unlock()
}

// CheckAndRecord returns ErrTokenReplay if the jti was already seen.
// Otherwise records it for future checks.
func (c *JTICache) CheckAndRecord(jti string, expiresAt time.Time) error {
	c.mu.Lock()
	if _, exists := c.seen[jti]; exists {
		c.mu.Unlock()
		return ErrTokenReplay
	}
	c.seen[jti] = expiresAt
	if c.db != nil {
		_, err := store.ExecDialect(c.db,
			"INSERT INTO jti_cache (jti, expires_at) VALUES (?, ?)",
			"INSERT INTO jti_cache (jti, expires_at) VALUES ($1, $2)",
			jti, expiresAt.Format(time.RFC3339))
		if err != nil {
			// UNIQUE constraint = replay (race between memory check and DB)
			delete(c.seen, jti)
			c.mu.Unlock()
			return ErrTokenReplay
		}
	}
	hook := c.onRecord
	c.mu.Unlock()
	if hook != nil {
		hook(jti, expiresAt)
	}
	return nil
}


func (c *JTICache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for jti, exp := range c.seen {
				if now.After(exp) {
					delete(c.seen, jti)
				}
			}
			if c.db != nil {
				store.ExecDialect(c.db, //nolint:errcheck
					"DELETE FROM jti_cache WHERE expires_at < ?",
					"DELETE FROM jti_cache WHERE expires_at < $1",
					now.Format(time.RFC3339))
			}
			c.mu.Unlock()
		case <-c.stop:
			return
		}
	}
}
