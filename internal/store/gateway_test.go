package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

func TestServersRoundtrip(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Empty at start
	servers, err := db.LoadServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 0 {
		t.Fatalf("expected 0 servers, got %d", len(servers))
	}

	// Save one
	s := config.ServerConfig{
		Name:      "test-server",
		Transport: "http",
		URL:       "http://localhost:3000/mcp",
		Auth:      &config.ServerAuthConfig{Type: "bearer", Token: "secret"},
	}
	if err := db.SaveServer(s); err != nil {
		t.Fatal(err)
	}

	// Load it back
	servers, err = db.LoadServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Name != "test-server" {
		t.Fatalf("expected name test-server, got %s", servers[0].Name)
	}
	if servers[0].Auth == nil || servers[0].Auth.Token != "secret" {
		t.Fatal("auth not round-tripped")
	}

	// Remove
	if err := db.RemoveServer("test-server"); err != nil {
		t.Fatal(err)
	}
	servers, _ = db.LoadServers()
	if len(servers) != 0 {
		t.Fatalf("expected 0 after remove, got %d", len(servers))
	}
}

func TestSeedOnlyWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{
		Servers: []config.ServerConfig{
			{Name: "s1", Transport: "http", URL: "http://localhost:3001/mcp"},
			{Name: "s2", Transport: "http", URL: "http://localhost:3002/mcp"},
		},
		Agents: []config.AgentConfig{
			{ID: "agent-1", Archetype: "autonomous"},
		},
		Rules: []config.RuleConfig{
			{Tool: "*", Action: "allow"},
		},
	}

	// First seed populates
	db.SeedFromConfig(cfg)
	servers, _ := db.LoadServers()
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers after seed, got %d", len(servers))
	}

	// Second seed is a no-op
	cfg2 := &config.Config{
		Servers: []config.ServerConfig{
			{Name: "s3", Transport: "http", URL: "http://localhost:3003/mcp"},
		},
	}
	db.SeedFromConfig(cfg2)
	servers, _ = db.LoadServers()
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers after second seed (no-op), got %d", len(servers))
	}
}

func TestLoadAgentsAndRules(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := &config.Config{
		Agents: []config.AgentConfig{
			{ID: "a1", Archetype: "autonomous"},
			{ID: "a2", Archetype: "user_embedded"},
		},
		Rules: []config.RuleConfig{
			{Tool: "delete_*", Action: "block"},
			{Tool: "*", Action: "allow"},
		},
	}
	db.SeedFromConfig(cfg)

	agents, err := db.LoadAgents()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	rules, err := db.LoadRules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	if rules[0].Tool != "delete_*" {
		t.Fatalf("expected first rule tool delete_*, got %s", rules[0].Tool)
	}
}

func TestSavedConfigPathDir(t *testing.T) {
	// Verify the function returns a path in ~/.recall-mcp-gateway/
	path := config.SavedConfigPath()
	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".recall-mcp-gateway", "config.yaml")
	if path != expected {
		t.Fatalf("expected %s, got %s", expected, path)
	}
}
