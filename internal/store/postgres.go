package store

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// OpenPostgres opens a PostgreSQL-backed gateway database.
// connStr is a PostgreSQL connection URL (e.g. "postgres://user:pass@host:5432/recall_gateway").
func OpenPostgres(connStr string, maxConns int) (*GatewayDB, error) {
	db, err := sql.Open("pgx", connStr)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if maxConns <= 0 {
		maxConns = 25
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns / 2)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	postgresFlag = true
	g := &GatewayDB{db: db, isPostgres: true}
	if err := g.createSchemaPG(); err != nil {
		db.Close()
		return nil, fmt.Errorf("create pg schema: %w", err)
	}

	slog.Info("gateway database opened", "backend", "postgres")
	return g, nil
}

// postgresFlag is set once when OpenPostgres is called. Sub-stores check this
// via IsPostgres() without needing a query on every call.
var postgresFlag bool

// IsPostgres returns true if the gateway is using PostgreSQL.
// This is a package-level flag set by OpenPostgres — safe for sub-stores
// that only receive *sql.DB without a GatewayDB reference.
func IsPostgres(db *sql.DB) bool {
	return postgresFlag
}

// ExecDialect executes the appropriate SQL statement based on the database backend.
func ExecDialect(db *sql.DB, sqliteSQL, pgSQL string, args ...any) (sql.Result, error) {
	if IsPostgres(db) {
		return db.Exec(pgSQL, args...)
	}
	return db.Exec(sqliteSQL, args...)
}

// ExecDialectTx executes the appropriate SQL statement in a transaction.
func ExecDialectTx(tx *sql.Tx, sqliteSQL, pgSQL string, isPostgres bool, args ...any) (sql.Result, error) {
	if isPostgres {
		return tx.Exec(pgSQL, args...)
	}
	return tx.Exec(sqliteSQL, args...)
}

// RebindQuery converts a SQLite-style query with `?` placeholders to
// PostgreSQL-style `$1, $2, ...` placeholders when running against PostgreSQL.
// When running against SQLite the query is returned unchanged.
func RebindQuery(db *sql.DB, query string) string {
	if !IsPostgres(db) {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 10)
	n := 1
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			fmt.Fprintf(&b, "$%d", n)
			n++
		} else {
			b.WriteByte(query[i])
		}
	}
	return b.String()
}

func (g *GatewayDB) createSchemaPG() error {
	// Split into individual statements — PostgreSQL doesn't support multi-statement exec
	// the same way SQLite does with CREATE TABLE IF NOT EXISTS in a batch.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS audit (
			id                   BIGSERIAL PRIMARY KEY,
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
			cost_per_call        DOUBLE PRECISION DEFAULT 0,
			engine_decision_id   TEXT DEFAULT '',
			signals              TEXT DEFAULT '',
			identity_source      TEXT DEFAULT '',
			protocol_era         TEXT DEFAULT '',
			trace_id             TEXT DEFAULT '',
			hash                 TEXT DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_agent ON audit(agent_id)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_tool ON audit(tool)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_decision ON audit(decision)`,

		`CREATE TABLE IF NOT EXISTS compliance_scores (
			date TEXT PRIMARY KEY,
			score INTEGER NOT NULL,
			band TEXT NOT NULL,
			recorded_at TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS approvals (
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_approvals_status ON approvals(status)`,
		`CREATE INDEX IF NOT EXISTS idx_approvals_args_hash ON approvals(args_hash)`,

		`CREATE TABLE IF NOT EXISTS revocations (
			hash TEXT PRIMARY KEY,
			expires_at TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS jti_cache (
			jti TEXT PRIMARY KEY,
			expires_at TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS rules (
			position INTEGER NOT NULL,
			tool TEXT NOT NULL,
			server TEXT DEFAULT '',
			action TEXT NOT NULL,
			agents TEXT DEFAULT '',
			data TEXT NOT NULL DEFAULT '{}',
			UNIQUE(tool, server, action, agents)
		)`,

		`CREATE TABLE IF NOT EXISTS agents (
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
			updated_at TEXT NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')::TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS reviews (
			tool TEXT PRIMARY KEY,
			status TEXT NOT NULL DEFAULT 'unreviewed',
			reviewed_at TEXT DEFAULT '',
			reviewed_by TEXT DEFAULT '',
			note TEXT DEFAULT '',
			acknowledged_signals TEXT DEFAULT ''
		)`,

		`CREATE TABLE IF NOT EXISTS baselines (
			key TEXT PRIMARY KEY,
			data TEXT NOT NULL,
			updated_at TEXT NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')::TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS server_health (
			name TEXT PRIMARY KEY,
			data TEXT NOT NULL
		)`,

		`CREATE TABLE IF NOT EXISTS oauth_tokens (
			token TEXT PRIMARY KEY,
			agent_id TEXT DEFAULT ''
		)`,

		`CREATE TABLE IF NOT EXISTS servers (
			name TEXT PRIMARY KEY,
			transport TEXT NOT NULL DEFAULT 'http',
			url TEXT DEFAULT '',
			command TEXT DEFAULT '',
			args TEXT DEFAULT '[]',
			env TEXT DEFAULT '{}',
			headers TEXT DEFAULT '{}',
			cost_per_call DOUBLE PRECISION DEFAULT 0,
			auth TEXT DEFAULT '',
			server_type TEXT DEFAULT '',
			mcp_header_mode TEXT DEFAULT '',
			tool_annotations TEXT DEFAULT '[]',
			updated_at TEXT NOT NULL DEFAULT (NOW() AT TIME ZONE 'UTC')::TEXT
		)`,

		`CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
	}

	for _, stmt := range stmts {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := g.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:min(60, len(stmt))], err)
		}
	}
	return nil
}
