package approval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/store"
	_ "modernc.org/sqlite"
)

// SQLiteStore is a hybrid approval store: SQLite for persistence,
// in-memory channels for WaitForResolution blocking.
type SQLiteStore struct {
	db      *sql.DB
	path    string
	ownsDB  bool // true when NewSQLiteStore opened the DB
	ttl     time.Duration

	mu      sync.Mutex
	entries map[string]*Entry       // hot cache (pending + recently resolved)
	waiters map[string][]chan *Entry // signaled on resolve (supports multiple waiters)

	stop   chan struct{}
	closed sync.Once
}

// SQLiteStoreConfig configures the SQLite approval store.
type SQLiteStoreConfig struct {
	Path string
	TTL  time.Duration
}

// NewSQLiteStore opens (or creates) a SQLite approval database and loads
// pending entries into memory for fast access and WaitForResolution support.
func NewSQLiteStore(cfg SQLiteStoreConfig) (*SQLiteStore, error) {
	if cfg.Path == "" {
		cfg.Path = filepath.Join(".", "approvals.db")
	}
	if cfg.TTL == 0 {
		cfg.TTL = 5 * time.Minute
	}

	if dir := filepath.Dir(cfg.Path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create approval db dir %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("open approval db %s: %w", cfg.Path, err)
	}

	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA cache_size=-8000",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %s: %w", p, err)
		}
	}

	if err := createApprovalSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	s := &SQLiteStore{
		db:      db,
		path:    cfg.Path,
		ownsDB:  true,
		ttl:     cfg.TTL,
		entries: make(map[string]*Entry),
		waiters: make(map[string][]chan *Entry),
		stop:    make(chan struct{}),
	}

	if err := s.loadPending(); err != nil {
		db.Close()
		return nil, fmt.Errorf("load pending: %w", err)
	}

	go s.cleanupLoop()
	return s, nil
}

// NewSQLiteStoreWithDB creates an approval store using an existing *sql.DB connection.
// The caller owns the DB connection — Close() will NOT close the underlying DB.
// Tables are created via CREATE TABLE IF NOT EXISTS so they are safe to call on a shared DB.
func NewSQLiteStoreWithDB(db *sql.DB, ttl time.Duration) (*SQLiteStore, error) {
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	if err := createApprovalSchema(db); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	s := &SQLiteStore{
		db:      db,
		ownsDB:  false,
		ttl:     ttl,
		entries: make(map[string]*Entry),
		waiters: make(map[string][]chan *Entry),
		stop:    make(chan struct{}),
	}
	if err := s.loadPending(); err != nil {
		return nil, fmt.Errorf("load pending: %w", err)
	}
	go s.cleanupLoop()
	return s, nil
}

func createApprovalSchema(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS approvals (
	id TEXT PRIMARY KEY,
	agent_id TEXT NOT NULL,
	tool TEXT NOT NULL,
	arguments TEXT DEFAULT '',
	args_hash TEXT NOT NULL,
	status TEXT NOT NULL DEFAULT 'pending',
	reason TEXT DEFAULT '',
	created_at TEXT NOT NULL,
	expires_at TEXT NOT NULL,
	resolved_at TEXT DEFAULT '',
	resolved_by TEXT DEFAULT '',
	retry_method TEXT DEFAULT '',
	status_webhook TEXT DEFAULT '',
	mcp_method TEXT DEFAULT '',
	mcp_name TEXT DEFAULT '',
	acting_for TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_approvals_status ON approvals(status);
CREATE INDEX IF NOT EXISTS idx_approvals_args_hash ON approvals(args_hash);
CREATE INDEX IF NOT EXISTS idx_approvals_expires ON approvals(expires_at);
`
	_, err := db.Exec(schema)
	return err
}

func (s *SQLiteStore) loadPending() error {
	q := store.RebindQuery(s.db,
		`SELECT id, agent_id, tool, arguments, args_hash, status, reason,
			created_at, expires_at, resolved_at, resolved_by, retry_method,
			status_webhook, mcp_method, mcp_name, acting_for
		FROM approvals WHERE status = 'pending' AND expires_at > ?`)
	rows, err := s.db.Query(q, time.Now().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		e := &Entry{}
		var argsJSON, createdAt, expiresAt, resolvedAt string
		err := rows.Scan(
			&e.ID, &e.AgentID, &e.Tool, &argsJSON, &e.ArgsHash, &e.Status, &e.Reason,
			&createdAt, &expiresAt, &resolvedAt, &e.ResolvedBy, &e.RetryMethod,
			&e.StatusWebhook, &e.McpMethod, &e.McpName, &e.ActingFor,
		)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(argsJSON), &e.Arguments)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		e.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
		if resolvedAt != "" {
			e.ResolvedAt, _ = time.Parse(time.RFC3339Nano, resolvedAt)
		}
		s.entries[e.ID] = e
	}
	slog.Info("loaded pending approvals from SQLite", "component", "approval", "count", len(s.entries))
	return nil
}

func (s *SQLiteStore) Create(agentID, tool string, args map[string]any) *Entry {
	id := generateApprovalID()
	now := time.Now()
	e := &Entry{
		ID:        id,
		AgentID:   agentID,
		Tool:      tool,
		Arguments: args,
		ArgsHash:  HashArgs(agentID, tool, args),
		Status:    StatusPending,
		CreatedAt: now,
		ExpiresAt: now.Add(s.ttl),
	}

	s.mu.Lock()
	s.entries[id] = e
	s.mu.Unlock()

	s.persistCreate(e)
	return e
}

func (s *SQLiteStore) persistCreate(e *Entry) {
	argsJSON, _ := json.Marshal(e.Arguments)
	store.ExecDialect(s.db, //nolint:errcheck
		`INSERT INTO approvals (id, agent_id, tool, arguments, args_hash, status, created_at, expires_at, mcp_method, mcp_name, acting_for)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		`INSERT INTO approvals (id, agent_id, tool, arguments, args_hash, status, created_at, expires_at, mcp_method, mcp_name, acting_for)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		e.ID, e.AgentID, e.Tool, string(argsJSON), e.ArgsHash, string(e.Status),
		e.CreatedAt.Format(time.RFC3339Nano), e.ExpiresAt.Format(time.RFC3339Nano),
		e.McpMethod, e.McpName, e.ActingFor,
	)
}

// UpdateFields persists mutable entry fields (McpMethod, McpName, RetryMethod, StatusWebhook) to SQLite.
// Called by proxy/server.go after setting fields post-Create.
func (s *SQLiteStore) UpdateFields(id string) {
	s.mu.Lock()
	e, ok := s.entries[id]
	s.mu.Unlock()
	if !ok {
		return
	}
	store.ExecDialect(s.db, //nolint:errcheck
		`UPDATE approvals SET mcp_method = ?, mcp_name = ?, retry_method = ?, status_webhook = ?, acting_for = ? WHERE id = ?`,
		`UPDATE approvals SET mcp_method = $1, mcp_name = $2, retry_method = $3, status_webhook = $4, acting_for = $5 WHERE id = $6`,
		e.McpMethod, e.McpName, e.RetryMethod, e.StatusWebhook, e.ActingFor, id,
	)
}

func (s *SQLiteStore) Approve(id, resolvedBy string) (*Entry, error) {
	return s.resolve(id, StatusApproved, resolvedBy, "")
}

func (s *SQLiteStore) Deny(id, resolvedBy, reason string) (*Entry, error) {
	return s.resolve(id, StatusDenied, resolvedBy, reason)
}

func (s *SQLiteStore) resolve(id string, status Status, resolvedBy, reason string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.entries[id]
	if !ok {
		// Try loading from SQLite (pre-restart entry)
		e = s.loadEntryFromDB(id)
		if e == nil {
			return nil, fmt.Errorf("approval %q not found", id)
		}
		s.entries[id] = e
	}

	if e.Status != StatusPending {
		return nil, fmt.Errorf("approval %q is %s, not pending", id, e.Status)
	}
	if time.Now().After(e.ExpiresAt) {
		e.Status = StatusExpired
		store.ExecDialect(s.db, //nolint:errcheck
			`UPDATE approvals SET status = 'expired' WHERE id = ?`,
			`UPDATE approvals SET status = 'expired' WHERE id = $1`,
			id)
		return nil, fmt.Errorf("approval %q has expired", id)
	}

	now := time.Now()
	e.Status = status
	e.ResolvedBy = resolvedBy
	e.ResolvedAt = now
	e.Reason = reason

	store.ExecDialect(s.db, //nolint:errcheck
		`UPDATE approvals SET status = ?, resolved_by = ?, resolved_at = ?, reason = ? WHERE id = ?`,
		`UPDATE approvals SET status = $1, resolved_by = $2, resolved_at = $3, reason = $4 WHERE id = $5`,
		string(status), resolvedBy, now.Format(time.RFC3339Nano), reason, id,
	)

	s.notifyWaiter(id, e)
	return e, nil
}

func (s *SQLiteStore) loadEntryFromDB(id string) *Entry {
	e := &Entry{}
	var argsJSON, createdAt, expiresAt, resolvedAt string
	q := store.RebindQuery(s.db,
		`SELECT id, agent_id, tool, arguments, args_hash, status, reason,
			created_at, expires_at, resolved_at, resolved_by, retry_method,
			status_webhook, mcp_method, mcp_name, acting_for
		FROM approvals WHERE id = ?`)
	err := s.db.QueryRow(q, id).Scan(
		&e.ID, &e.AgentID, &e.Tool, &argsJSON, &e.ArgsHash, &e.Status, &e.Reason,
		&createdAt, &expiresAt, &resolvedAt, &e.ResolvedBy, &e.RetryMethod,
		&e.StatusWebhook, &e.McpMethod, &e.McpName, &e.ActingFor,
	)
	if err != nil {
		return nil
	}
	json.Unmarshal([]byte(argsJSON), &e.Arguments)
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	e.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
	if resolvedAt != "" {
		e.ResolvedAt, _ = time.Parse(time.RFC3339Nano, resolvedAt)
	}
	return e
}

func (s *SQLiteStore) CheckApproved(agentID, tool string, args map[string]any) bool {
	hash := HashArgs(agentID, tool, args)

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check in-memory first
	for id, e := range s.entries {
		if e.ArgsHash == hash && e.Status == StatusApproved {
			delete(s.entries, id) // consume
			store.ExecDialect(s.db, //nolint:errcheck
				`DELETE FROM approvals WHERE id = ?`,
				`DELETE FROM approvals WHERE id = $1`,
				id)
			return true
		}
	}

	// Fallback: check DB for pre-restart approved entries
	var id string
	q := store.RebindQuery(s.db,
		`SELECT id FROM approvals WHERE args_hash = ? AND status = 'approved' LIMIT 1`)
	err := s.db.QueryRow(q, hash).Scan(&id)
	if err == nil {
		store.ExecDialect(s.db, //nolint:errcheck
			`DELETE FROM approvals WHERE id = ?`,
			`DELETE FROM approvals WHERE id = $1`,
			id)
		return true
	}

	return false
}

func (s *SQLiteStore) CheckPendingOrDenied(agentID, tool string, args map[string]any) *Entry {
	hash := HashArgs(agentID, tool, args)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range s.entries {
		if e.ArgsHash == hash && (e.Status == StatusPending || e.Status == StatusDenied) {
			cp := *e
			return &cp
		}
	}

	// Fallback: check DB
	e := &Entry{}
	var argsJSON, createdAt, expiresAt, resolvedAt string
	q := store.RebindQuery(s.db,
		`SELECT id, agent_id, tool, arguments, args_hash, status, reason,
			created_at, expires_at, resolved_at, resolved_by
		FROM approvals WHERE args_hash = ? AND status IN ('pending','denied') LIMIT 1`)
	err := s.db.QueryRow(q, hash).Scan(
		&e.ID, &e.AgentID, &e.Tool, &argsJSON, &e.ArgsHash, &e.Status, &e.Reason,
		&createdAt, &expiresAt, &resolvedAt, &e.ResolvedBy,
	)
	if err != nil {
		return nil
	}
	json.Unmarshal([]byte(argsJSON), &e.Arguments)
	e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	e.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
	if resolvedAt != "" {
		e.ResolvedAt, _ = time.Parse(time.RFC3339Nano, resolvedAt)
	}
	return e
}

func (s *SQLiteStore) Get(id string) *Entry {
	s.mu.Lock()
	e, ok := s.entries[id]
	s.mu.Unlock()
	if ok {
		cp := *e
		return &cp
	}
	return s.loadEntryFromDB(id)
}

func (s *SQLiteStore) Pending() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []Entry
	now := time.Now()
	for _, e := range s.entries {
		if e.Status == StatusPending && now.Before(e.ExpiresAt) {
			result = append(result, *e)
		}
	}
	return result
}

func (s *SQLiteStore) All() []Entry {
	rows, err := s.db.Query(
		`SELECT id, agent_id, tool, arguments, args_hash, status, reason,
			created_at, expires_at, resolved_at, resolved_by, retry_method,
			status_webhook, mcp_method, mcp_name, acting_for
		FROM approvals ORDER BY created_at DESC LIMIT 200`,
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var result []Entry
	for rows.Next() {
		e := Entry{}
		var argsJSON, createdAt, expiresAt, resolvedAt string
		err := rows.Scan(
			&e.ID, &e.AgentID, &e.Tool, &argsJSON, &e.ArgsHash, &e.Status, &e.Reason,
			&createdAt, &expiresAt, &resolvedAt, &e.ResolvedBy, &e.RetryMethod,
			&e.StatusWebhook, &e.McpMethod, &e.McpName, &e.ActingFor,
		)
		if err != nil {
			continue
		}
		json.Unmarshal([]byte(argsJSON), &e.Arguments)
		e.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		e.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
		if resolvedAt != "" {
			e.ResolvedAt, _ = time.Parse(time.RFC3339Nano, resolvedAt)
		}
		result = append(result, e)
	}
	return result
}

func (s *SQLiteStore) WaitForResolution(id string) *Entry {
	s.mu.Lock()
	e, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if e.Status != StatusPending {
		cp := *e
		s.mu.Unlock()
		return &cp
	}

	ch := make(chan *Entry, 1)
	s.waiters[id] = append(s.waiters[id], ch)
	deadline := time.Until(e.ExpiresAt)
	s.mu.Unlock()

	select {
	case resolved := <-ch:
		return resolved
	case <-time.After(deadline):
		s.mu.Lock()
		delete(s.waiters, id)
		if e.Status == StatusPending {
			e.Status = StatusExpired
			store.ExecDialect(s.db, //nolint:errcheck
				`UPDATE approvals SET status = 'expired' WHERE id = ?`,
				`UPDATE approvals SET status = 'expired' WHERE id = $1`,
				id)
		}
		cp := *e
		s.mu.Unlock()
		return &cp
	}
}

// WaitForResolutionCtx blocks until approved, denied, timed out, or context cancelled.
// In multi-pod (shared PG), also polls the DB every 2s for resolutions from other pods.
func (s *SQLiteStore) WaitForResolutionCtx(ctx context.Context, id string) *Entry {
	s.mu.Lock()
	e, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if e.Status != StatusPending {
		cp := *e
		s.mu.Unlock()
		return &cp
	}
	ch := make(chan *Entry, 1)
	s.waiters[id] = append(s.waiters[id], ch)
	deadline := time.Until(e.ExpiresAt)
	s.mu.Unlock()

	// DB polling ticker for multi-pod: check if another pod resolved this approval.
	dbPoll := time.NewTicker(2 * time.Second)
	defer dbPoll.Stop()

	select {
	case resolved := <-ch:
		return resolved
	case <-time.After(deadline):
		s.mu.Lock()
		delete(s.waiters, id)
		if e.Status == StatusPending {
			e.Status = StatusExpired
			store.ExecDialect(s.db, //nolint:errcheck
				`UPDATE approvals SET status = 'expired' WHERE id = ?`,
				`UPDATE approvals SET status = 'expired' WHERE id = $1`,
				id)
		}
		cp := *e
		s.mu.Unlock()
		return &cp
	case <-dbPoll.C:
		// Check DB for resolution from another pod
		if dbEntry := s.loadEntryFromDB(id); dbEntry != nil && dbEntry.Status != StatusPending {
			s.mu.Lock()
			delete(s.waiters, id)
			s.entries[id] = dbEntry
			s.mu.Unlock()
			return dbEntry
		}
		// Not resolved yet — re-enter wait via recursive tail call
		return s.WaitForResolutionCtx(ctx, id)
	case <-ctx.Done():
		// Check if approval was resolved concurrently (resolution wins over cancellation)
		select {
		case resolved := <-ch:
			return resolved
		default:
		}
		// Also check DB (another pod may have resolved)
		if dbEntry := s.loadEntryFromDB(id); dbEntry != nil && dbEntry.Status != StatusPending {
			s.mu.Lock()
			delete(s.waiters, id)
			s.entries[id] = dbEntry
			s.mu.Unlock()
			return dbEntry
		}
		s.mu.Lock()
		delete(s.waiters, id)
		if e.Status == StatusPending {
			e.Status = StatusDenied
			e.Reason = "task cancelled by client"
			e.ResolvedAt = time.Now()
			e.ResolvedBy = "system"
			if _, err := store.ExecDialect(s.db,
				`UPDATE approvals SET status = 'denied', reason = ?, resolved_at = ?, resolved_by = ? WHERE id = ?`,
				`UPDATE approvals SET status = 'denied', reason = $1, resolved_at = $2, resolved_by = $3 WHERE id = $4`,
				e.Reason, e.ResolvedAt, e.ResolvedBy, id); err != nil {
				slog.Warn("failed to persist task cancellation", "component", "approval", "id", id, "error", err)
			}
		}
		cp := *e
		s.mu.Unlock()
		return &cp
	}
}

func (s *SQLiteStore) notifyWaiter(id string, e *Entry) {
	if channels, ok := s.waiters[id]; ok {
		for _, ch := range channels {
			cp := *e
			ch <- &cp
		}
		delete(s.waiters, id)
	}
}

func (s *SQLiteStore) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *SQLiteStore) cleanup() {
	now := time.Now()

	s.mu.Lock()
	// Expire pending entries past their TTL
	for id, e := range s.entries {
		if e.Status == StatusPending && now.After(e.ExpiresAt) {
			e.Status = StatusExpired
			store.ExecDialect(s.db, //nolint:errcheck
				`UPDATE approvals SET status = 'expired' WHERE id = ?`,
				`UPDATE approvals SET status = 'expired' WHERE id = $1`,
				id)
			s.notifyWaiter(id, e)
		}
	}
	// Remove resolved entries older than 1 hour from memory (keep in SQLite)
	for id, e := range s.entries {
		if e.Status != StatusPending && !e.ResolvedAt.IsZero() && now.Sub(e.ResolvedAt) > 1*time.Hour {
			delete(s.entries, id)
		}
	}
	s.mu.Unlock()

	// Delete entries older than 7 days from DB
	store.ExecDialect(s.db, //nolint:errcheck
		`DELETE FROM approvals WHERE created_at < ?`,
		`DELETE FROM approvals WHERE created_at < $1`,
		now.Add(-7*24*time.Hour).Format(time.RFC3339Nano))
}

// SetTTL updates the approval TTL at runtime.
func (s *SQLiteStore) SetTTL(d time.Duration) { s.mu.Lock(); s.ttl = d; s.mu.Unlock() }

func (s *SQLiteStore) Close() {
	s.closed.Do(func() {
		close(s.stop)
		if s.db != nil && s.ownsDB {
			s.db.Close()
		}
	})
}

// generateApprovalID is defined in store.go and shared by both stores.
