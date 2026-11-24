// Package store provides the unified database for all gateway runtime state.
// Supports two backends: SQLite (standalone) and PostgreSQL (single-pod or multi-pod).
package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// GatewayDB wraps the gateway's durable store (SQLite or PostgreSQL).
type GatewayDB struct {
	db         *sql.DB
	path       string // SQLite file path (empty for PostgreSQL)
	isPostgres bool
}

// IsPostgresMode returns true if this GatewayDB is backed by PostgreSQL.
func (g *GatewayDB) IsPostgresMode() bool { return g.isPostgres }

// Open creates or opens the gateway database at the given path.
// Creates all tables if they don't exist.
func Open(dbPath string) (*GatewayDB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("create dir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	g := &GatewayDB{db: db, path: dbPath}
	if err := g.createSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}

	slog.Info("gateway database opened", "path", dbPath)
	return g, nil
}

// DB returns the underlying *sql.DB for sub-stores to use.
func (g *GatewayDB) DB() *sql.DB { return g.db }

// Path returns the database file path.
func (g *GatewayDB) Path() string { return g.path }

// Close shuts down the database.
func (g *GatewayDB) Close() error {
	if g.db != nil {
		return g.db.Close()
	}
	return nil
}

func (g *GatewayDB) createSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS audit (
		id                   INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp            TEXT NOT NULL,
		request_id           TEXT NOT NULL,
		agent_id             TEXT NOT NULL DEFAULT '',
		user_id              TEXT DEFAULT '',
		acting_for           TEXT DEFAULT '',
		delegation_depth     INTEGER DEFAULT 0,
		org_id               TEXT DEFAULT '',
		tool                 TEXT NOT NULL,
		server               TEXT DEFAULT '',
		decision             TEXT NOT NULL,
		decision_reason      TEXT DEFAULT '',
		matched_rule         TEXT DEFAULT '',
		matched_rule_index   INTEGER DEFAULT -1,
		matched_rule_action  TEXT DEFAULT '',
		mode                 TEXT DEFAULT '',
		latency_ms           INTEGER DEFAULT 0,
		arguments            TEXT DEFAULT '',
		result_preview       TEXT DEFAULT '',
		is_error             INTEGER DEFAULT 0,
		error_message        TEXT DEFAULT '',
		error_category       TEXT DEFAULT '',
		cost_per_call        REAL DEFAULT 0,
		engine_decision_id   TEXT DEFAULT '',
		signals              TEXT DEFAULT '',
		identity_source      TEXT DEFAULT '',
		protocol_era         TEXT DEFAULT '',
		trace_id             TEXT DEFAULT '',
		hash                 TEXT DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_agent ON audit(agent_id);
	CREATE INDEX IF NOT EXISTS idx_audit_tool ON audit(tool);
	CREATE INDEX IF NOT EXISTS idx_audit_decision ON audit(decision);

	CREATE TABLE IF NOT EXISTS compliance_scores (
		date TEXT PRIMARY KEY,
		score INTEGER NOT NULL,
		band TEXT NOT NULL,
		recorded_at TEXT NOT NULL
	);

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

	CREATE TABLE IF NOT EXISTS revocations (
		hash TEXT PRIMARY KEY,
		expires_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS jti_cache (
		jti TEXT PRIMARY KEY,
		expires_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS rules (
		position INTEGER NOT NULL,
		tool TEXT NOT NULL,
		server TEXT DEFAULT '',
		action TEXT NOT NULL,
		agents TEXT DEFAULT '',
		data TEXT NOT NULL DEFAULT '{}',
		UNIQUE(tool, server, action, agents)
	);

	CREATE TABLE IF NOT EXISTS agents (
		id TEXT PRIMARY KEY,
		token_hash TEXT DEFAULT '',
		archetype TEXT DEFAULT 'autonomous',
		product TEXT DEFAULT '',
		org_id TEXT DEFAULT '',
		source TEXT DEFAULT '',
		expires_at TEXT DEFAULT '',
		server_access_restricted INTEGER DEFAULT 0,
		allowed_servers TEXT DEFAULT '',
		blocked_tools TEXT DEFAULT '',
		hidden_tools TEXT DEFAULT '',
		acknowledged_tools TEXT DEFAULT '',
		data TEXT DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
	);

	CREATE TABLE IF NOT EXISTS reviews (
		tool TEXT PRIMARY KEY,
		status TEXT NOT NULL DEFAULT 'unreviewed',
		reviewed_at TEXT DEFAULT '',
		reviewed_by TEXT DEFAULT '',
		note TEXT DEFAULT '',
		acknowledged_signals TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS baselines (
		key TEXT PRIMARY KEY,
		data TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
	);

	CREATE TABLE IF NOT EXISTS server_health (
		name TEXT PRIMARY KEY,
		data TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS oauth_tokens (
		token TEXT PRIMARY KEY,
		agent_id TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS servers (
		name TEXT PRIMARY KEY,
		transport TEXT NOT NULL DEFAULT 'http',
		url TEXT DEFAULT '',
		command TEXT DEFAULT '',
		args TEXT DEFAULT '[]',
		env TEXT DEFAULT '{}',
		headers TEXT DEFAULT '{}',
		cost_per_call REAL DEFAULT 0,
		auth TEXT DEFAULT '',
		server_type TEXT DEFAULT '',
		mcp_header_mode TEXT DEFAULT '',
		tool_annotations TEXT DEFAULT '[]',
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
	);

	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	);
	`
	_, err := g.db.Exec(schema)
	return err
}
