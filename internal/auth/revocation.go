package auth

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/store"
	_ "modernc.org/sqlite"
)

// RevocationStore tracks revoked tokens by their SHA-256 hash.
// Optionally backed by SQLite for restart resilience.
type RevocationStore struct {
	mu       sync.RWMutex
	tokens   map[string]time.Time // hash → original expiry
	db       *sql.DB              // optional SQLite backing
	ownsDB   bool                 // true when this store opened the DB itself
	stop     chan struct{}
	onRevoke func(hash string, expiresAt time.Time) // optional hook — called after every local Revoke
}

// NewRevocationStore creates an in-memory revocation store (lost on restart).
func NewRevocationStore() *RevocationStore {
	s := &RevocationStore{tokens: make(map[string]time.Time), stop: make(chan struct{})}
	go s.cleanupLoop()
	return s
}

// NewSQLiteRevocationStore creates a revocation store backed by SQLite.
// Existing revocations are loaded on startup — survives restart.
func NewSQLiteRevocationStore(dbPath string) (*RevocationStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS revocations (
		hash TEXT PRIMARY KEY,
		expires_at TEXT NOT NULL
	)`)
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &RevocationStore{tokens: make(map[string]time.Time), db: db, ownsDB: true, stop: make(chan struct{})}
	// Load existing non-expired revocations
	now := time.Now()
	rows, err := db.Query("SELECT hash, expires_at FROM revocations")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var hash, expStr string
			if rows.Scan(&hash, &expStr) == nil {
				if exp, err := time.Parse(time.RFC3339, expStr); err == nil && exp.After(now) {
					s.tokens[hash] = exp
				}
			}
		}
	}
	slog.Info("revocation store loaded", "count", len(s.tokens), "path", dbPath)
	go s.cleanupLoop()
	return s, nil
}

// NewRevocationStoreWithDB creates a revocation store using an existing *sql.DB connection.
// The caller owns the DB connection — Close() will NOT close the underlying DB.
// The revocations table must already exist (created by store.GatewayDB).
func NewRevocationStoreWithDB(db *sql.DB) *RevocationStore {
	s := &RevocationStore{tokens: make(map[string]time.Time), db: db, ownsDB: false, stop: make(chan struct{})}
	now := time.Now()
	rows, err := db.Query("SELECT hash, expires_at FROM revocations")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var hash, expStr string
			if rows.Scan(&hash, &expStr) == nil {
				if exp, parseErr := time.Parse(time.RFC3339, expStr); parseErr == nil && exp.After(now) {
					s.tokens[hash] = exp
				}
			}
		}
	}
	slog.Info("revocation store loaded", "count", len(s.tokens), "storage", "gateway.db")
	go s.cleanupLoop()
	return s
}

// Close stops the cleanup goroutine and closes the database.
// When created with NewRevocationStoreWithDB, the DB is NOT closed (caller owns it).
func (s *RevocationStore) Close() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	if s.db != nil && s.ownsDB {
		s.db.Close()
	}
}

// ReloadFromDB reloads all revocations from the database into memory.
// Used by PG LISTEN/NOTIFY subscriber to refresh after changes from other pods.
func (s *RevocationStore) ReloadFromDB() {
	if s.db == nil {
		return
	}
	rows, err := s.db.Query("SELECT hash, expires_at FROM revocations")
	if err != nil {
		slog.Warn("failed to reload revocations from DB", "component", "revocation", "error", err)
		return
	}
	defer rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens = make(map[string]time.Time)
	for rows.Next() {
		var hash, expiresAt string
		if rows.Scan(&hash, &expiresAt) != nil {
			continue
		}
		if t, err := time.Parse(time.RFC3339, expiresAt); err == nil {
			s.tokens[hash] = t
		}
	}
}

// SetOnRevoke registers a hook called after every local Revoke().
// Used by main.go to broadcast revocations to other pods via Redis/PG NOTIFY.
// The hook is called without the mutex held — safe to perform I/O.
func (s *RevocationStore) SetOnRevoke(fn func(hash string, expiresAt time.Time)) {
	s.mu.Lock()
	s.onRevoke = fn
	s.mu.Unlock()
}

// Revoke adds a token hash to the revocation list.
func (s *RevocationStore) Revoke(tokenHash string, expiresAt time.Time) {
	s.mu.Lock()
	s.tokens[tokenHash] = expiresAt
	if s.db != nil {
		store.ExecDialect(s.db, //nolint:errcheck
			"INSERT OR REPLACE INTO revocations (hash, expires_at) VALUES (?, ?)",
			"INSERT INTO revocations (hash, expires_at) VALUES ($1, $2) ON CONFLICT (hash) DO UPDATE SET expires_at = $2",
			tokenHash, expiresAt.Format(time.RFC3339))
	}
	hook := s.onRevoke
	s.mu.Unlock()
	if hook != nil {
		hook(tokenHash, expiresAt)
	}
}

// RevokeRaw revokes a raw token string by computing its SHA-256 hash.
func (s *RevocationStore) RevokeRaw(token string, expiresAt time.Time) {
	s.Revoke(hashToken(token), expiresAt)
}

// IsRevoked checks if a token hash is in the revocation list.
func (s *RevocationStore) IsRevoked(tokenHash string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.tokens[tokenHash]
	return ok
}

// IsRevokedRaw checks if a raw token string is revoked.
func (s *RevocationStore) IsRevokedRaw(token string) bool {
	return s.IsRevoked(hashToken(token))
}


// Count returns the number of revoked tokens currently tracked.
func (s *RevocationStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.tokens)
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *RevocationStore) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			now := time.Now()
			for hash, exp := range s.tokens {
				if now.After(exp) {
					delete(s.tokens, hash)
				}
			}
			if s.db != nil {
				store.ExecDialect(s.db, //nolint:errcheck
					"DELETE FROM revocations WHERE expires_at < ?",
					"DELETE FROM revocations WHERE expires_at < $1",
					now.Format(time.RFC3339))
			}
			s.mu.Unlock()
		case <-s.stop:
			return
		}
	}
}
