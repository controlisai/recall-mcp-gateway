package store

import (
	"encoding/json"
	"fmt"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// LoadAgents returns all agents from the agents table.
func (g *GatewayDB) LoadAgents() ([]config.AgentConfig, error) {
	rows, err := g.db.Query("SELECT data FROM agents WHERE data != '' ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("query agents: %w", err)
	}
	defer rows.Close()

	var agents []config.AgentConfig
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan agent: %w", err)
		}
		var a config.AgentConfig
		if err := json.Unmarshal([]byte(data), &a); err != nil {
			continue // skip corrupt rows
		}
		agents = append(agents, a)
	}
	return agents, rows.Err()
}

// LoadRules returns all rules from the rules table, ordered by position.
func (g *GatewayDB) LoadRules() ([]config.RuleConfig, error) {
	rows, err := g.db.Query("SELECT data FROM rules ORDER BY position ASC")
	if err != nil {
		return nil, fmt.Errorf("query rules: %w", err)
	}
	defer rows.Close()

	var rules []config.RuleConfig
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, fmt.Errorf("scan rule: %w", err)
		}
		var r config.RuleConfig
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			continue // skip corrupt rows
		}
		rules = append(rules, r)
	}
	return rules, rows.Err()
}
