package store

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// LoadServers returns all servers from the servers table.
func (g *GatewayDB) LoadServers() ([]config.ServerConfig, error) {
	rows, err := g.db.Query(`SELECT name, transport, url, command, args, env, headers,
		cost_per_call, auth, server_type, mcp_header_mode, tool_annotations
		FROM servers ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query servers: %w", err)
	}
	defer rows.Close()

	var servers []config.ServerConfig
	for rows.Next() {
		var (
			s                                          config.ServerConfig
			argsJSON, envJSON, headersJSON, authJSON    string
			annotJSON                                   string
		)
		if err := rows.Scan(&s.Name, &s.Transport, &s.URL, &s.Command,
			&argsJSON, &envJSON, &headersJSON,
			&s.CostPerCall, &authJSON, &s.ServerType, &s.McpHeaderMode, &annotJSON); err != nil {
			return nil, fmt.Errorf("scan server: %w", err)
		}
		if argsJSON != "" && argsJSON != "[]" {
			json.Unmarshal([]byte(argsJSON), &s.Args)
		}
		if envJSON != "" && envJSON != "{}" {
			json.Unmarshal([]byte(envJSON), &s.Env)
		}
		if headersJSON != "" && headersJSON != "{}" {
			json.Unmarshal([]byte(headersJSON), &s.Headers)
		}
		if authJSON != "" {
			var auth config.ServerAuthConfig
			if json.Unmarshal([]byte(authJSON), &auth) == nil && auth.Type != "" {
				s.Auth = &auth
			}
		}
		if annotJSON != "" && annotJSON != "[]" {
			json.Unmarshal([]byte(annotJSON), &s.ToolAnnotations)
		}
		servers = append(servers, s)
	}
	return servers, rows.Err()
}

// SaveServer upserts a single server into the servers table.
func (g *GatewayDB) SaveServer(s config.ServerConfig) error {
	argsJSON, _ := json.Marshal(s.Args)
	envJSON, _ := json.Marshal(s.Env)
	headersJSON, _ := json.Marshal(s.Headers)
	var authJSON []byte
	if s.Auth != nil {
		authJSON, _ = json.Marshal(s.Auth)
	}
	annotJSON, _ := json.Marshal(s.ToolAnnotations)
	now := time.Now().UTC().Format(time.RFC3339)

	args := []any{s.Name, s.Transport, s.URL, s.Command,
		string(argsJSON), string(envJSON), string(headersJSON),
		s.CostPerCall, string(authJSON), s.ServerType, s.McpHeaderMode, string(annotJSON), now}
	_, err := ExecDialect(g.db,
		`INSERT OR REPLACE INTO servers
			(name, transport, url, command, args, env, headers,
			 cost_per_call, auth, server_type, mcp_header_mode, tool_annotations, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		`INSERT INTO servers
			(name, transport, url, command, args, env, headers,
			 cost_per_call, auth, server_type, mcp_header_mode, tool_annotations, updated_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
			ON CONFLICT (name) DO UPDATE SET
			transport=$2, url=$3, command=$4, args=$5, env=$6, headers=$7,
			cost_per_call=$8, auth=$9, server_type=$10, mcp_header_mode=$11,
			tool_annotations=$12, updated_at=$13`,
		args...)
	if err != nil {
		return fmt.Errorf("save server %s: %w", s.Name, err)
	}
	return nil
}

// RemoveServer deletes a server from the servers table.
func (g *GatewayDB) RemoveServer(name string) error {
	_, err := ExecDialect(g.db,
		"DELETE FROM servers WHERE name = ?",
		"DELETE FROM servers WHERE name = $1",
		name)
	return err
}

// SaveAllServers replaces all servers in the servers table.
func (g *GatewayDB) SaveAllServers(servers []config.ServerConfig) error {
	tx, err := g.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec("DELETE FROM servers"); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range servers {
		argsJSON, _ := json.Marshal(s.Args)
		envJSON, _ := json.Marshal(s.Env)
		headersJSON, _ := json.Marshal(s.Headers)
		var authJSON []byte
		if s.Auth != nil {
			authJSON, _ = json.Marshal(s.Auth)
		}
		annotJSON, _ := json.Marshal(s.ToolAnnotations)

		args := []any{s.Name, s.Transport, s.URL, s.Command,
			string(argsJSON), string(envJSON), string(headersJSON),
			s.CostPerCall, string(authJSON), s.ServerType, s.McpHeaderMode, string(annotJSON), now}
		if _, err := ExecDialectTx(tx,
			`INSERT INTO servers
				(name, transport, url, command, args, env, headers,
				 cost_per_call, auth, server_type, mcp_header_mode, tool_annotations, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			`INSERT INTO servers
				(name, transport, url, command, args, env, headers,
				 cost_per_call, auth, server_type, mcp_header_mode, tool_annotations, updated_at)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			g.isPostgres, args...); err != nil {
			return fmt.Errorf("insert server %s: %w", s.Name, err)
		}
	}
	return tx.Commit()
}

// ServerCount returns the number of servers in the table.
func (g *GatewayDB) ServerCount() int {
	var count int
	g.db.QueryRow("SELECT COUNT(*) FROM servers").Scan(&count)
	return count
}
