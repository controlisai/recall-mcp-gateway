package store

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// SeedServers populates the servers table from YAML config if the table is empty.
// Returns the number of servers seeded (0 if table already had data).
func (g *GatewayDB) SeedServers(servers []config.ServerConfig) int {
	if len(servers) == 0 || g.ServerCount() > 0 {
		return 0
	}
	if err := g.SaveAllServers(servers); err != nil {
		slog.Error("failed to seed servers", "component", "store", "error", err)
		return 0
	}
	slog.Info("seeded servers from config", "component", "store", "count", len(servers))
	return len(servers)
}

// SeedAgents populates the agents table from YAML config if the table is empty.
// Returns the number of agents seeded (0 if table already had data).
func (g *GatewayDB) SeedAgents(agents []config.AgentConfig) int {
	if len(agents) == 0 {
		return 0
	}
	var count int
	g.db.QueryRow("SELECT COUNT(*) FROM agents").Scan(&count)
	if count > 0 {
		return 0
	}

	tx, err := g.db.Begin()
	if err != nil {
		slog.Error("failed to seed agents", "component", "store", "error", err)
		return 0
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC().Format(time.RFC3339)
	for _, a := range agents {
		data, _ := json.Marshal(a)
		allowedJSON, _ := json.Marshal(a.AllowedServers)
		blockedJSON, _ := json.Marshal(a.BlockedTools)
		hiddenJSON, _ := json.Marshal(a.HiddenTools)
		acknowledgedJSON, _ := json.Marshal(a.AcknowledgedTools)
		restricted := 0
		if a.ServerAccessRestricted {
			restricted = 1
		}
		source := a.Source
		if source == "" {
			source = "config"
		}
		ExecDialectTx(tx, //nolint:errcheck
			`INSERT OR IGNORE INTO agents
			(id, token_hash, archetype, product, org_id, source, expires_at,
			 server_access_restricted, allowed_servers, blocked_tools, hidden_tools,
			 acknowledged_tools, data, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO agents
			(id, token_hash, archetype, product, org_id, source, expires_at,
			 server_access_restricted, allowed_servers, blocked_tools, hidden_tools,
			 acknowledged_tools, data, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			ON CONFLICT (id) DO NOTHING`,
			g.isPostgres,
			a.ID, a.TokenHash, a.Archetype, a.Product, a.OrgID, source, a.ExpiresAt,
			restricted, string(allowedJSON), string(blockedJSON), string(hiddenJSON),
			string(acknowledgedJSON), string(data), now)
	}
	if err := tx.Commit(); err != nil {
		slog.Error("failed to seed agents", "component", "store", "error", err)
		return 0
	}
	slog.Info("seeded agents from config", "component", "store", "count", len(agents))
	return len(agents)
}

// SeedRules populates the rules table from YAML config if the table is empty.
// Returns the number of rules seeded (0 if table already had data).
func (g *GatewayDB) SeedRules(rules []config.RuleConfig) int {
	if len(rules) == 0 {
		return 0
	}
	var count int
	g.db.QueryRow("SELECT COUNT(*) FROM rules").Scan(&count)
	if count > 0 {
		return 0
	}

	tx, err := g.db.Begin()
	if err != nil {
		slog.Error("failed to seed rules", "component", "store", "error", err)
		return 0
	}
	defer tx.Rollback() //nolint:errcheck

	for i, r := range rules {
		data, _ := json.Marshal(r)
		agentsJSON, _ := json.Marshal(r.Agents)
		ExecDialectTx(tx, //nolint:errcheck
			`INSERT OR IGNORE INTO rules (position, tool, server, action, agents, data) VALUES (?, ?, ?, ?, ?, ?)`,
			`INSERT INTO rules (position, tool, server, action, agents, data) VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`,
			g.isPostgres,
			i, r.Tool, r.Server, r.Action, string(agentsJSON), string(data))
	}
	if err := tx.Commit(); err != nil {
		slog.Error("failed to seed rules", "component", "store", "error", err)
		return 0
	}
	slog.Info("seeded rules from config", "component", "store", "count", len(rules))
	return len(rules)
}

// SeedFromConfig seeds all empty tables from YAML config. Called once at startup.
func (g *GatewayDB) SeedFromConfig(cfg *config.Config) {
	g.SeedServers(cfg.Servers)
	g.SeedAgents(cfg.Agents)
	g.SeedRules(cfg.Rules)
}
