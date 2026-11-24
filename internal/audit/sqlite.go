package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/store"
	_ "modernc.org/sqlite"
)

// DailyScore represents a single day's compliance score snapshot.
type DailyScore struct {
	Date  string `json:"date"`
	Score int    `json:"score"`
	Band  string `json:"band"`
}

// SQLiteStore persists audit entries in a SQLite database.
type SQLiteStore struct {
	db          *sql.DB
	path        string
	ownsDB      bool // true when NewSQLiteStore opened the DB (Close should close it)
	mu          sync.RWMutex
	subscribers map[int]Subscriber
	nextID      int

	retention  time.Duration
	maxEntries int
	stopPurge  chan struct{}
	closed     sync.Once
	hashChain  *HashChain
}

// SQLiteConfig configures the SQLite audit store.
type SQLiteConfig struct {
	Path       string        // database file path
	Retention  time.Duration // auto-purge entries older than this (0 = no time purge)
	MaxEntries int           // hard cap on entry count (0 = unlimited)
}

// NewSQLiteStoreWithDB creates a SQLite audit store using an existing *sql.DB connection.
// The caller owns the DB connection — Close() will NOT close the underlying DB.
// Tables are created via CREATE TABLE IF NOT EXISTS so they are safe to call on a shared DB.
func NewSQLiteStoreWithDB(db *sql.DB, cfg SQLiteConfig) (*SQLiteStore, error) {
	// Skip createSchema when using shared gateway.db — the schema was already
	// created by GatewayDB.createSchema() (SQLite) or createSchemaPG() (PostgreSQL).
	// Calling createSchema here would fail on PG due to AUTOINCREMENT syntax.
	if !store.IsPostgres(db) {
		if err := createSchema(db); err != nil {
			return nil, err
		}
	}
	migrateSchema(db)

	var lastHash string
	row := db.QueryRow("SELECT hash FROM audit ORDER BY id DESC LIMIT 1")
	row.Scan(&lastHash)

	s := &SQLiteStore{
		db:          db,
		path:        cfg.Path,
		subscribers: make(map[int]Subscriber),
		retention:   cfg.Retention,
		maxEntries:  cfg.MaxEntries,
		stopPurge:   make(chan struct{}),
		hashChain:   NewHashChainFrom(lastHash),
	}
	go s.runPurge()
	return s, nil
}

// NewSQLiteStore opens (or creates) a SQLite audit database.
func NewSQLiteStore(cfg SQLiteConfig) (*SQLiteStore, error) {
	if cfg.Path == "" {
		cfg.Path = filepath.Join(".", "audit.db")
	}

	// Ensure parent directory exists (important for Docker volume mounts)
	if dir := filepath.Dir(cfg.Path); dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create audit db dir %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("open audit db %s: %w", cfg.Path, err)
	}

	// SQLite is single-writer; serialize all access through one connection
	db.SetMaxOpenConns(1)

	// SQLite pragmas for performance
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA cache_size=-8000", // 8MB
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %s: %w", p, err)
		}
	}

	// Create schema + migrations
	if err := createSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	migrateSchema(db)

	// Initialize hash chain from last entry's hash (if any)
	var lastHash string
	row := db.QueryRow("SELECT hash FROM audit ORDER BY id DESC LIMIT 1")
	row.Scan(&lastHash) // ignore error — empty DB returns empty hash

	s := &SQLiteStore{
		db:          db,
		path:        cfg.Path,
		ownsDB:      true,
		subscribers: make(map[int]Subscriber),
		retention:   cfg.Retention,
		maxEntries:  cfg.MaxEntries,
		stopPurge:   make(chan struct{}),
		hashChain:   NewHashChainFrom(lastHash),
	}

	// Start background purge
	go s.runPurge()

	return s, nil
}

func createSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		request_id TEXT NOT NULL,
		agent_id TEXT NOT NULL DEFAULT '',
		org_id TEXT DEFAULT '',
		tool TEXT NOT NULL,
		server TEXT DEFAULT '',
		decision TEXT NOT NULL,
		decision_reason TEXT DEFAULT '',
		matched_rule TEXT DEFAULT '',
		matched_rule_index INTEGER DEFAULT -1,
		matched_rule_action TEXT DEFAULT '',
		mode TEXT DEFAULT '',
		latency_ms INTEGER DEFAULT 0,
		arguments TEXT DEFAULT '',
		result_preview TEXT DEFAULT '',
		is_error INTEGER DEFAULT 0,
		error_message TEXT DEFAULT '',
		error_category TEXT DEFAULT '',
		cost_per_call REAL DEFAULT 0,
		engine_decision_id TEXT DEFAULT '',
		signals TEXT DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_agent ON audit(agent_id);
	CREATE INDEX IF NOT EXISTS idx_audit_tool ON audit(tool);
	CREATE INDEX IF NOT EXISTS idx_audit_decision ON audit(decision);
	CREATE INDEX IF NOT EXISTS idx_audit_server ON audit(server);

	CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("create audit schema: %w", err)
	}
	return nil
}

// migrateSchema applies pending migrations based on schema_version table.
func migrateSchema(db *sql.DB) {
	var currentVersion int
	row := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version")
	if err := row.Scan(&currentVersion); err != nil {
		// schema_version table may not exist in very old databases
		currentVersion = 0
	}

	migrations := []struct {
		version int
		sql     string
	}{
		{1, "ALTER TABLE audit ADD COLUMN error_category TEXT DEFAULT ''"},
		{2, "ALTER TABLE audit ADD COLUMN engine_decision_id TEXT DEFAULT ''"},
		{3, `CREATE TABLE IF NOT EXISTS compliance_scores (
			date TEXT PRIMARY KEY,
			score INTEGER NOT NULL,
			band TEXT NOT NULL,
			recorded_at TEXT NOT NULL
		)`},
		{4, "ALTER TABLE audit ADD COLUMN user_id TEXT DEFAULT ''"},
		{5, "ALTER TABLE audit ADD COLUMN acting_for TEXT DEFAULT ''"},
		{6, "ALTER TABLE audit ADD COLUMN delegation_depth INTEGER DEFAULT 0"},
		{7, "ALTER TABLE audit ADD COLUMN identity_source TEXT DEFAULT ''"},
		{8, "ALTER TABLE audit ADD COLUMN protocol_era TEXT DEFAULT ''"},
		{9, "ALTER TABLE audit ADD COLUMN trace_id TEXT DEFAULT ''"},
		{10, "ALTER TABLE audit ADD COLUMN hash TEXT DEFAULT ''"},
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, m := range migrations {
		if m.version <= currentVersion {
			continue
		}
		_, err := db.Exec(m.sql)
		if err != nil {
			// Column may already exist from a prior run without version tracking — ignore
			slog.Debug("migration skipped (likely already applied)", "component", "audit", "version", m.version, "error", err)
		}
		store.ExecDialect(db, //nolint:errcheck
			"INSERT OR REPLACE INTO schema_version (version, applied_at) VALUES (?, ?)",
			"INSERT INTO schema_version (version, applied_at) VALUES ($1, $2) ON CONFLICT (version) DO UPDATE SET applied_at = $2",
			m.version, now)
	}
}

// Log inserts an audit entry and broadcasts to subscribers.
func (s *SQLiteStore) Log(entry Entry) {
	// Compute hash chain for tamper detection
	if s.hashChain != nil {
		s.hashChain.ComputeHash(&entry)
	}

	argsJSON := ""
	if entry.Arguments != nil {
		if data, err := json.Marshal(entry.Arguments); err == nil {
			argsJSON = string(data)
		}
	}
	signalsJSON := ""
	if entry.Signals != nil {
		if data, err := json.Marshal(entry.Signals); err == nil {
			signalsJSON = string(data)
		}
	}

	isErr := 0
	if entry.IsError {
		isErr = 1
	}

	_, err := store.ExecDialect(s.db,
		`INSERT INTO audit (
		timestamp, request_id, agent_id, user_id, acting_for, delegation_depth, org_id, tool, server,
		decision, decision_reason, matched_rule, matched_rule_index, matched_rule_action,
		mode, latency_ms, arguments, result_preview,
		is_error, error_message, error_category, cost_per_call, engine_decision_id, signals,
		identity_source, protocol_era, trace_id, hash
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		`INSERT INTO audit (
		timestamp, request_id, agent_id, user_id, acting_for, delegation_depth, org_id, tool, server,
		decision, decision_reason, matched_rule, matched_rule_index, matched_rule_action,
		mode, latency_ms, arguments, result_preview,
		is_error, error_message, error_category, cost_per_call, engine_decision_id, signals,
		identity_source, protocol_era, trace_id, hash
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28)`,
		entry.Timestamp.Format(time.RFC3339Nano),
		entry.RequestID, entry.AgentID, entry.UserID, entry.ActingFor, entry.DelegationDepth, entry.OrgID, entry.Tool, entry.Server,
		entry.Decision, entry.DecisionReason, entry.MatchedRule, entry.MatchedRuleIndex, entry.MatchedRuleAction,
		entry.Mode, entry.LatencyMs, argsJSON, entry.ResultPreview,
		isErr, entry.ErrorMessage, entry.ErrorCategory, entry.CostPerCall, entry.EngineDecisionID, signalsJSON,
		entry.IdentitySource, entry.ProtocolEra, entry.TraceID, entry.Hash,
	)
	if err != nil {
		slog.Error("sqlite insert error", "component", "audit", "error", err)
		return
	}

	// Broadcast to subscribers
	s.mu.RLock()
	subs := make(map[int]Subscriber, len(s.subscribers))
	for id, sub := range s.subscribers {
		subs[id] = sub
	}
	s.mu.RUnlock()

	for _, sub := range subs {
		func() {
			defer func() { recover() }()
			sub(entry)
		}()
	}
}

// Recent returns the most recent 200 audit entries.
func (s *SQLiteStore) Recent() []Entry {
	return s.recentN(200)
}

// RecentN returns at most limit recent entries (oldest first).
func (s *SQLiteStore) RecentN(limit int) []Entry {
	if limit <= 0 {
		limit = 200
	}
	return s.recentN(limit)
}

func (s *SQLiteStore) recentN(limit int) []Entry {
	q := store.RebindQuery(s.db, `SELECT
		timestamp, request_id, agent_id, user_id, acting_for, delegation_depth, org_id, tool, server,
		decision, decision_reason, matched_rule, matched_rule_index, matched_rule_action,
		mode, latency_ms, arguments, result_preview,
		is_error, error_message, error_category, cost_per_call, engine_decision_id, signals,
		identity_source, protocol_era, trace_id, hash
		FROM audit ORDER BY id DESC LIMIT ?`)
	rows, err := s.db.Query(q, limit)
	if err != nil {
		slog.Error("sqlite query error", "component", "audit", "error", err)
		return []Entry{}
	}
	defer rows.Close()

	entries := make([]Entry, 0)
	for rows.Next() {
		var (
			ts          string
			argsJSON    string
			signalsJSON string
			isErr       int
			e           Entry
		)
		err := rows.Scan(
			&ts, &e.RequestID, &e.AgentID, &e.UserID, &e.ActingFor, &e.DelegationDepth, &e.OrgID, &e.Tool, &e.Server,
			&e.Decision, &e.DecisionReason, &e.MatchedRule, &e.MatchedRuleIndex, &e.MatchedRuleAction,
			&e.Mode, &e.LatencyMs, &argsJSON, &e.ResultPreview,
			&isErr, &e.ErrorMessage, &e.ErrorCategory, &e.CostPerCall, &e.EngineDecisionID, &signalsJSON,
				&e.IdentitySource, &e.ProtocolEra, &e.TraceID, &e.Hash,
		)
		if err != nil {
			slog.Error("sqlite scan error", "component", "audit", "error", err)
			continue
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		e.IsError = isErr != 0
		if argsJSON != "" {
			json.Unmarshal([]byte(argsJSON), &e.Arguments)
		}
		if signalsJSON != "" {
			json.Unmarshal([]byte(signalsJSON), &e.Signals)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		slog.Error("sqlite rows error", "component", "audit", "error", err)
	}

	// Reverse so oldest is first (matches Logger.Recent() order)
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

// Search returns entries matching the filter criteria.
func (s *SQLiteStore) Search(filter SearchFilter) []Entry {
	query := `SELECT
		timestamp, request_id, agent_id, user_id, acting_for, delegation_depth, org_id, tool, server,
		decision, decision_reason, matched_rule, matched_rule_index, matched_rule_action,
		mode, latency_ms, arguments, result_preview,
		is_error, error_message, error_category, cost_per_call, engine_decision_id, signals,
		identity_source, protocol_era, trace_id, hash
		FROM audit WHERE 1=1`
	var args []any

	if filter.Tool != "" {
		if store.IsPostgres(s.db) {
			// PostgreSQL: LIKE with % wildcards (convert SQLite's * glob wildcard)
			query += " AND tool LIKE ?"
			args = append(args, strings.ReplaceAll(filter.Tool, "*", "%"))
		} else {
			query += " AND tool GLOB ?"
			args = append(args, filter.Tool)
		}
	}
	if filter.AgentID != "" {
		query += " AND agent_id = ?"
		args = append(args, filter.AgentID)
	}
	if filter.Decision != "" {
		query += " AND decision = ?"
		args = append(args, filter.Decision)
	}
	if filter.Server != "" {
		query += " AND server = ?"
		args = append(args, filter.Server)
	}
	if !filter.From.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, filter.From.Format(time.RFC3339Nano))
	}
	if !filter.To.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, filter.To.Format(time.RFC3339Nano))
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(store.RebindQuery(s.db, query), args...)
	if err != nil {
		slog.Error("sqlite search error", "component", "audit", "error", err)
		return []Entry{}
	}
	defer rows.Close()

	entries := make([]Entry, 0)
	for rows.Next() {
		var (
			ts, argsJSON, signalsJSON string
			isErr                     int
			e                         Entry
		)
		err := rows.Scan(
			&ts, &e.RequestID, &e.AgentID, &e.UserID, &e.ActingFor, &e.DelegationDepth, &e.OrgID, &e.Tool, &e.Server,
			&e.Decision, &e.DecisionReason, &e.MatchedRule, &e.MatchedRuleIndex, &e.MatchedRuleAction,
			&e.Mode, &e.LatencyMs, &argsJSON, &e.ResultPreview,
			&isErr, &e.ErrorMessage, &e.ErrorCategory, &e.CostPerCall, &e.EngineDecisionID, &signalsJSON,
				&e.IdentitySource, &e.ProtocolEra, &e.TraceID, &e.Hash,
		)
		if err != nil {
			slog.Error("sqlite search scan error", "component", "audit", "error", err)
			continue
		}
		e.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		e.IsError = isErr != 0
		if argsJSON != "" {
			json.Unmarshal([]byte(argsJSON), &e.Arguments)
		}
		if signalsJSON != "" {
			json.Unmarshal([]byte(signalsJSON), &e.Signals)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		slog.Error("sqlite search rows error", "component", "audit", "error", err)
	}

	// Reverse to oldest-first
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries
}

// Stats returns aggregated statistics, optionally filtered by time range.
func (s *SQLiteStore) Stats(filter StatsFilter) AuditStats {
	var stats AuditStats
	whereClause, whereArgs := buildTimeWhere(s.db, filter)

	// Total count
	s.db.QueryRow("SELECT COUNT(*) FROM audit"+whereClause, whereArgs...).Scan(&stats.TotalCount)

	// Error count
	s.db.QueryRow("SELECT COUNT(*) FROM audit"+whereClause+" AND is_error = 1", whereArgs...).Scan(&stats.ErrorCount)

	// Avg latency
	s.db.QueryRow("SELECT COALESCE(AVG(latency_ms), 0) FROM audit"+whereClause, whereArgs...).Scan(&stats.AvgLatencyMs)

	// By agent
	stats.ByAgent = s.groupBy("agent_id", whereClause, whereArgs)
	// By tool
	stats.ByTool = s.groupBy("tool", whereClause, whereArgs)
	// By decision
	stats.ByDecision = s.groupBy("decision", whereClause, whereArgs)
	// By server
	stats.ByServer = s.groupBy("server", whereClause, whereArgs)
	// By mode
	stats.ByMode = s.groupBy("mode", whereClause, whereArgs)

	// --- Dashboard-specific aggregations (exclude system/pseudo entries) ---
	dashWhere, dashArgs := buildDashboardWhere(s.db, filter)

	// Blocked count
	s.db.QueryRow(store.RebindQuery(s.db,
		"SELECT COUNT(*) FROM audit"+dashWhere+" AND decision IN ('block','rate_limited')"),
		dashArgs...).Scan(&stats.BlockedCount)

	// Unique tools & agents
	s.db.QueryRow(store.RebindQuery(s.db,
		"SELECT COUNT(DISTINCT tool) FROM audit"+dashWhere+" AND tool != 'tools/list'"),
		dashArgs...).Scan(&stats.UniqueTools)
	s.db.QueryRow(store.RebindQuery(s.db,
		"SELECT COUNT(DISTINCT agent_id) FROM audit"+dashWhere),
		dashArgs...).Scan(&stats.UniqueAgents)

	// Signal counts
	stats.SignalCounts = s.querySignalCounts(dashWhere, dashArgs)

	// Top agents (top 10 with error counts)
	stats.TopAgents = s.queryTopAgents(dashWhere, dashArgs)

	// Top tools (top 10 with server)
	stats.TopTools = s.queryTopTools(dashWhere, dashArgs)

	// Hourly activity (24 buckets)
	stats.HourlyActivity = s.queryHourlyActivity(dashWhere, dashArgs)

	return stats
}

// buildDashboardWhere creates a WHERE clause excluding system/pseudo entries.
func buildDashboardWhere(db *sql.DB, filter StatsFilter) (string, []any) {
	clause := " WHERE agent_id NOT IN ('system','anonymous','unauthorized') AND tool != '(server-lifecycle)'"
	var args []any
	if !filter.From.IsZero() {
		clause += " AND timestamp >= ?"
		args = append(args, filter.From.Format(time.RFC3339Nano))
	}
	if !filter.To.IsZero() {
		clause += " AND timestamp <= ?"
		args = append(args, filter.To.Format(time.RFC3339Nano))
	}
	return store.RebindQuery(db, clause), args
}

func (s *SQLiteStore) querySignalCounts(whereClause string, whereArgs []any) map[string]int64 {
	sc := map[string]int64{"pii": 0, "credential": 0, "destructive": 0}
	row := s.db.QueryRow(store.RebindQuery(s.db,
		`SELECT
			COALESCE(SUM(CASE WHEN signals LIKE '%pii%' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN signals LIKE '%credential%' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN signals LIKE '%destructive%' THEN 1 ELSE 0 END), 0)
		FROM audit`+whereClause), whereArgs...)
	var pii, cred, destr int64
	if err := row.Scan(&pii, &cred, &destr); err == nil {
		sc["pii"] = pii
		sc["credential"] = cred
		sc["destructive"] = destr
	}
	return sc
}

func (s *SQLiteStore) queryTopAgents(whereClause string, whereArgs []any) []TopAgentStat {
	rows, err := s.db.Query(store.RebindQuery(s.db,
		`SELECT agent_id, COUNT(*) as cnt,
			SUM(CASE WHEN is_error = 1 OR decision IN ('block','rate_limited') THEN 1 ELSE 0 END) as errs
		FROM audit`+whereClause+
			` GROUP BY agent_id ORDER BY cnt DESC LIMIT 10`), whereArgs...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []TopAgentStat
	for rows.Next() {
		var a TopAgentStat
		if err := rows.Scan(&a.ID, &a.Count, &a.Errors); err == nil {
			result = append(result, a)
		}
	}
	return result
}

func (s *SQLiteStore) queryTopTools(whereClause string, whereArgs []any) []TopToolStat {
	rows, err := s.db.Query(store.RebindQuery(s.db,
		`SELECT tool, server, COUNT(*) as cnt
		FROM audit`+whereClause+` AND tool != 'tools/list'
		GROUP BY tool, server ORDER BY cnt DESC LIMIT 10`), whereArgs...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []TopToolStat
	for rows.Next() {
		var t TopToolStat
		if err := rows.Scan(&t.Name, &t.Server, &t.Count); err == nil {
			result = append(result, t)
		}
	}
	return result
}

func (s *SQLiteStore) queryHourlyActivity(whereClause string, whereArgs []any) []HourlyBucket {
	// Use strftime for SQLite, to_char for PostgreSQL to truncate timestamps to the hour
	var query string
	if store.IsPostgres(s.db) {
		query = `SELECT to_char(timestamp::timestamptz, 'YYYY-MM-DD"T"HH24:00:00"Z"') as hr,
			COUNT(*) as total,
			SUM(CASE WHEN is_error = 1 OR decision IN ('block','rate_limited') THEN 1 ELSE 0 END) as errors
			FROM audit` + whereClause + ` AND tool != 'tools/list'
			GROUP BY hr ORDER BY hr ASC`
	} else {
		query = `SELECT strftime('%Y-%m-%dT%H:00:00Z', timestamp) as hr,
			COUNT(*) as total,
			SUM(CASE WHEN is_error = 1 OR decision IN ('block','rate_limited') THEN 1 ELSE 0 END) as errors
			FROM audit` + whereClause + ` AND tool != 'tools/list'
			GROUP BY hr ORDER BY hr ASC`
	}
	rows, err := s.db.Query(store.RebindQuery(s.db, query), whereArgs...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []HourlyBucket
	for rows.Next() {
		var b HourlyBucket
		if err := rows.Scan(&b.Hour, &b.Total, &b.Errors); err == nil {
			result = append(result, b)
		}
	}
	return result
}

func (s *SQLiteStore) groupBy(column, whereClause string, whereArgs []any) []CountBucket {
	rows, err := s.db.Query(
		"SELECT "+column+", COUNT(*) as cnt FROM audit"+whereClause+
			" GROUP BY "+column+" ORDER BY cnt DESC LIMIT 50",
		whereArgs...)
	if err != nil {
		return []CountBucket{}
	}
	defer rows.Close()
	buckets := make([]CountBucket, 0)
	for rows.Next() {
		var b CountBucket
		if err := rows.Scan(&b.Label, &b.Count); err == nil {
			buckets = append(buckets, b)
		}
	}
	if err := rows.Err(); err != nil {
		slog.Error("sqlite groupBy rows error", "component", "audit", "error", err)
	}
	return buckets
}

func buildTimeWhere(db *sql.DB, filter StatsFilter) (string, []any) {
	clause := " WHERE 1=1"
	var args []any
	if !filter.From.IsZero() {
		clause += " AND timestamp >= ?"
		args = append(args, filter.From.Format(time.RFC3339Nano))
	}
	if !filter.To.IsZero() {
		clause += " AND timestamp <= ?"
		args = append(args, filter.To.Format(time.RFC3339Nano))
	}
	return store.RebindQuery(db, clause), args
}

// Subscribe registers a live listener. Returns an unsubscribe function.
func (s *SQLiteStore) Subscribe(sub Subscriber) func() {
	s.mu.Lock()
	id := s.nextID
	s.nextID++
	s.subscribers[id] = sub
	s.mu.Unlock()

	return func() {
		s.mu.Lock()
		delete(s.subscribers, id)
		s.mu.Unlock()
	}
}

// SubscriberCount returns the number of active subscribers.
func (s *SQLiteStore) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subscribers)
}

// Info returns storage metadata.
func (s *SQLiteStore) Info() StoreInfo {
	info := StoreInfo{
		Storage:   "sqlite",
		Path:      s.path,
		SizeBytes: -1,
	}

	// Entry count + oldest
	var count int64
	var oldest sql.NullString
	row := s.db.QueryRow("SELECT COUNT(*), MIN(timestamp) FROM audit")
	if err := row.Scan(&count, &oldest); err == nil {
		info.EntryCount = count
		if oldest.Valid {
			info.OldestEntry = oldest.String
		}
	}

	// DB file size
	var pageCount, pageSize int64
	if err := s.db.QueryRow("PRAGMA page_count").Scan(&pageCount); err == nil {
		if err := s.db.QueryRow("PRAGMA page_size").Scan(&pageSize); err == nil {
			info.SizeBytes = pageCount * pageSize
		}
	}

	// Retention/max from config
	if s.retention > 0 {
		hours := int(s.retention.Hours())
		if hours >= 24 && hours%24 == 0 {
			info.Retention = fmt.Sprintf("%dd", hours/24)
		} else if hours > 0 {
			info.Retention = fmt.Sprintf("%dh", hours)
		} else {
			info.Retention = fmt.Sprintf("%dm", int(s.retention.Minutes()))
		}
	}
	s.mu.RLock()
	info.MaxEntries = s.maxEntries
	s.mu.RUnlock()

	return info
}

// SetMaxEntries updates the hard cap on entry count.
func (s *SQLiteStore) SetMaxEntries(n int) {
	s.mu.Lock()
	s.maxEntries = n
	s.mu.Unlock()
}

// Purge deletes all audit entries and reclaims disk space.
func (s *SQLiteStore) Purge() error {
	if _, err := s.db.Exec("DELETE FROM audit"); err != nil {
		return fmt.Errorf("purge audit: %w", err)
	}
	// VACUUM is SQLite-specific — reclaims disk space after bulk delete.
	// On PostgreSQL, autovacuum handles this automatically.
	if !store.IsPostgres(s.db) {
		if _, err := s.db.Exec("VACUUM"); err != nil {
			return fmt.Errorf("vacuum audit: %w", err)
		}
	}
	return nil
}

// RecordComplianceScore stores or updates the daily high score.
func (s *SQLiteStore) RecordComplianceScore(score int, band string) {
	date := time.Now().Format("2006-01-02")
	now := time.Now().Format(time.RFC3339)
	_, err := store.ExecDialect(s.db,
		`INSERT INTO compliance_scores (date, score, band, recorded_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET score = MAX(score, excluded.score), band = excluded.band, recorded_at = excluded.recorded_at
		WHERE excluded.score > compliance_scores.score`,
		`INSERT INTO compliance_scores (date, score, band, recorded_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT(date) DO UPDATE SET score = GREATEST(compliance_scores.score, excluded.score), band = excluded.band, recorded_at = excluded.recorded_at
		WHERE excluded.score > compliance_scores.score`,
		date, score, band, now)
	if err != nil {
		slog.Error("record compliance score error", "component", "audit", "error", err)
	}
}

// ComplianceScoreTrend returns daily scores for the last N days.
func (s *SQLiteStore) ComplianceScoreTrend(days int) []DailyScore {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	q := store.RebindQuery(s.db, "SELECT date, score, band FROM compliance_scores WHERE date >= ? ORDER BY date ASC")
	rows, err := s.db.Query(q, cutoff)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var result []DailyScore
	for rows.Next() {
		var d DailyScore
		if err := rows.Scan(&d.Date, &d.Score, &d.Band); err != nil {
			slog.Error("compliance trend scan error", "component", "audit", "error", err)
			continue
		}
		result = append(result, d)
	}
	return result
}

// Close stops the purge goroutine and closes the database. Safe to call multiple times.
// When the store was created with NewSQLiteStoreWithDB, the DB is NOT closed (caller owns it).
func (s *SQLiteStore) Close() error {
	var dbErr error
	s.closed.Do(func() {
		close(s.stopPurge)
		if s.ownsDB {
			dbErr = s.db.Close()
		}
	})
	return dbErr
}

func (s *SQLiteStore) runPurge() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopPurge:
			return
		case <-ticker.C:
			s.purgeOnce()
		}
	}
}

func (s *SQLiteStore) purgeOnce() {
	// Time-based purge
	if s.retention > 0 {
		cutoff := time.Now().Add(-s.retention).Format(time.RFC3339Nano)
		if _, err := store.ExecDialect(s.db,
			"DELETE FROM audit WHERE timestamp < ?",
			"DELETE FROM audit WHERE timestamp < $1",
			cutoff); err != nil {
			slog.Error("purge by time error", "component", "audit", "error", err)
		}
	}
	// Count-based purge (read under lock since SetMaxEntries can change it)
	s.mu.RLock()
	max := s.maxEntries
	s.mu.RUnlock()
	if max > 0 {
		if _, err := store.ExecDialect(s.db,
			`DELETE FROM audit WHERE id NOT IN (SELECT id FROM audit ORDER BY id DESC LIMIT ?)`,
			`DELETE FROM audit WHERE id NOT IN (SELECT id FROM audit ORDER BY id DESC LIMIT $1)`,
			max); err != nil {
			slog.Error("purge by count error", "component", "audit", "error", err)
		}
	}
}
