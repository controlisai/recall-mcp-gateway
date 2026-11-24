package store

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// MigrateFromLegacy imports data from old JSON files and SQLite databases
// into the unified gateway.db. Safe to call multiple times — skips if
// data already exists in the target tables.
func (g *GatewayDB) MigrateFromLegacy(dataDir string) {
	g.migrateRulesJSON(filepath.Join(dataDir, "rules.json"))
	g.migrateAgentsJSON(filepath.Join(dataDir, "agents.json"))
	g.migrateReviewsJSON(filepath.Join(dataDir, "reviews.json"))
	g.migrateBaselinesJSON(filepath.Join(dataDir, "baselines.json"))
	g.migrateSecurityDB(filepath.Join(dataDir, "security.db"))
	g.migrateOAuthTokensJSON(filepath.Join(dataDir, "oauth-tokens.json"))
}

func (g *GatewayDB) migrateRulesJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return // file doesn't exist — nothing to migrate
	}
	// Check if rules table already has data
	var count int
	g.db.QueryRow("SELECT COUNT(*) FROM rules").Scan(&count)
	if count > 0 {
		return // already migrated
	}
	var rules []json.RawMessage
	if json.Unmarshal(data, &rules) != nil {
		return
	}
	tx, err := g.db.Begin()
	if err != nil {
		return
	}
	for i, r := range rules {
		var parsed struct {
			Tool   string   `json:"tool"`
			Server string   `json:"server"`
			Action string   `json:"action"`
			Agents []string `json:"agents"`
		}
		json.Unmarshal(r, &parsed)
		agents, _ := json.Marshal(parsed.Agents)
		ExecDialectTx(tx, //nolint:errcheck
			"INSERT OR IGNORE INTO rules (position, tool, server, action, agents, data) VALUES (?, ?, ?, ?, ?, ?)",
			"INSERT INTO rules (position, tool, server, action, agents, data) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING",
			g.isPostgres,
			i, parsed.Tool, parsed.Server, parsed.Action, string(agents), string(r))
	}
	tx.Commit()
	os.Rename(path, path+".migrated")
	slog.Info("migrated rules from JSON", "count", len(rules), "source", path)
}

func (g *GatewayDB) migrateAgentsJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var count int
	g.db.QueryRow("SELECT COUNT(*) FROM agents").Scan(&count)
	if count > 0 {
		return
	}
	var agents []struct {
		ID                   string   `json:"id"`
		TokenHash            string   `json:"token_hash"`
		Archetype            string   `json:"archetype"`
		Product              string   `json:"product"`
		OrgID                string   `json:"orgId"`
		Source               string   `json:"source"`
		ExpiresAt            string   `json:"expires_at"`
		ServerAccessRestricted bool   `json:"serverAccessRestricted"`
		AllowedServers       []string `json:"allowedServers"`
		BlockedTools         []string `json:"blockedTools"`
		HiddenTools          []string `json:"hiddenTools"`
		AcknowledgedTools    []string `json:"acknowledgedTools"`
	}
	if json.Unmarshal(data, &agents) != nil {
		return
	}
	tx, err := g.db.Begin()
	if err != nil {
		return
	}
	for _, a := range agents {
		allowed, _ := json.Marshal(a.AllowedServers)
		blocked, _ := json.Marshal(a.BlockedTools)
		hidden, _ := json.Marshal(a.HiddenTools)
		acked, _ := json.Marshal(a.AcknowledgedTools)
		restricted := 0
		if a.ServerAccessRestricted {
			restricted = 1
		}
		ExecDialectTx(tx, //nolint:errcheck
			`INSERT OR IGNORE INTO agents (id, token_hash, archetype, product, org_id, source, expires_at,
			server_access_restricted, allowed_servers, blocked_tools, hidden_tools, acknowledged_tools)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO agents (id, token_hash, archetype, product, org_id, source, expires_at,
			server_access_restricted, allowed_servers, blocked_tools, hidden_tools, acknowledged_tools)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) ON CONFLICT (id) DO NOTHING`,
			g.isPostgres,
			a.ID, a.TokenHash, a.Archetype, a.Product, a.OrgID, a.Source, a.ExpiresAt,
			restricted, string(allowed), string(blocked), string(hidden), string(acked))
	}
	tx.Commit()
	os.Rename(path, path+".migrated")
	slog.Info("migrated agents from JSON", "count", len(agents), "source", path)
}

func (g *GatewayDB) migrateReviewsJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var count int
	g.db.QueryRow("SELECT COUNT(*) FROM reviews").Scan(&count)
	if count > 0 {
		return
	}
	var reviews map[string]struct {
		Tool               string   `json:"tool"`
		Status             string   `json:"status"`
		ReviewedAt         string   `json:"reviewedAt"`
		ReviewedBy         string   `json:"reviewedBy"`
		Note               string   `json:"note"`
		AcknowledgedSignals []string `json:"acknowledgedSignals"`
	}
	if json.Unmarshal(data, &reviews) != nil {
		return
	}
	tx, err := g.db.Begin()
	if err != nil {
		return
	}
	for _, r := range reviews {
		signals, _ := json.Marshal(r.AcknowledgedSignals)
		ExecDialectTx(tx, //nolint:errcheck
			"INSERT OR IGNORE INTO reviews (tool, status, reviewed_at, reviewed_by, note, acknowledged_signals) VALUES (?, ?, ?, ?, ?, ?)",
			"INSERT INTO reviews (tool, status, reviewed_at, reviewed_by, note, acknowledged_signals) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (tool) DO NOTHING",
			g.isPostgres,
			r.Tool, r.Status, r.ReviewedAt, r.ReviewedBy, r.Note, string(signals))
	}
	tx.Commit()
	os.Rename(path, path+".migrated")
	slog.Info("migrated reviews from JSON", "count", len(reviews), "source", path)
}

func (g *GatewayDB) migrateBaselinesJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var count int
	g.db.QueryRow("SELECT COUNT(*) FROM baselines").Scan(&count)
	if count > 0 {
		return
	}
	var state struct {
		Baselines map[string]json.RawMessage `json:"baselines"`
	}
	if json.Unmarshal(data, &state) != nil {
		return
	}
	tx, err := g.db.Begin()
	if err != nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for key, bl := range state.Baselines {
		ExecDialectTx(tx, //nolint:errcheck
			"INSERT OR IGNORE INTO baselines (key, data, updated_at) VALUES (?, ?, ?)",
			"INSERT INTO baselines (key, data, updated_at) VALUES ($1, $2, $3) ON CONFLICT (key) DO NOTHING",
			g.isPostgres,
			key, string(bl), now)
	}
	tx.Commit()
	os.Rename(path, path+".migrated")
	slog.Info("migrated baselines from JSON", "count", len(state.Baselines), "source", path)
}

func (g *GatewayDB) migrateSecurityDB(path string) {
	if g.isPostgres {
		return // ATTACH DATABASE is SQLite-only; security.db migration skipped for PostgreSQL
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return
	}
	// Check if we already have data
	var revCount, jtiCount int
	g.db.QueryRow("SELECT COUNT(*) FROM revocations").Scan(&revCount)
	g.db.QueryRow("SELECT COUNT(*) FROM jti_cache").Scan(&jtiCount)
	if revCount > 0 || jtiCount > 0 {
		return
	}
	// Attach old DB and copy
	_, err := g.db.Exec("ATTACH DATABASE ? AS old_security", path)
	if err != nil {
		slog.Warn("failed to attach security.db for migration", "error", err)
		return
	}
	defer g.db.Exec("DETACH DATABASE old_security")

	result, _ := g.db.Exec("INSERT OR IGNORE INTO revocations SELECT * FROM old_security.revocations")
	if result != nil {
		n, _ := result.RowsAffected()
		if n > 0 {
			slog.Info("migrated revocations from security.db", "count", n)
		}
	}
	result, _ = g.db.Exec("INSERT OR IGNORE INTO jti_cache SELECT * FROM old_security.jti_cache")
	if result != nil {
		n, _ := result.RowsAffected()
		if n > 0 {
			slog.Info("migrated JTI cache from security.db", "count", n)
		}
	}
	os.Rename(path, path+".migrated")
}

func (g *GatewayDB) migrateOAuthTokensJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var count int
	g.db.QueryRow("SELECT COUNT(*) FROM oauth_tokens").Scan(&count)
	if count > 0 {
		return
	}
	var stored struct {
		Tokens      []string          `json:"tokens"`
		TokenAgents map[string]string `json:"tokenAgents"`
	}
	if json.Unmarshal(data, &stored) != nil {
		return
	}
	tx, err := g.db.Begin()
	if err != nil {
		return
	}
	for _, tok := range stored.Tokens {
		agent := stored.TokenAgents[tok]
		ExecDialectTx(tx, //nolint:errcheck
			"INSERT OR IGNORE INTO oauth_tokens (token, agent_id) VALUES (?, ?)",
			"INSERT INTO oauth_tokens (token, agent_id) VALUES ($1, $2) ON CONFLICT (token) DO NOTHING",
			g.isPostgres,
			tok, agent)
	}
	tx.Commit()
	os.Rename(path, path+".migrated")
	slog.Info("migrated OAuth tokens from JSON", "count", len(stored.Tokens), "source", path)
}
