package rules

import (
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

func TestEngine_DefaultAllow(t *testing.T) {
	e := NewEngine(nil)
	d := e.Evaluate("agent1", "any_tool")
	if d.Action != "allow" {
		t.Errorf("action = %q, want allow", d.Action)
	}
}

func TestEngine_BlockRule(t *testing.T) {
	e := NewEngine([]config.RuleConfig{
		{Tool: "delete_*", Action: "block", Reason: "no destructive ops"},
	})

	d := e.Evaluate("agent1", "delete_user")
	if d.Action != "block" {
		t.Errorf("action = %q, want block", d.Action)
	}
	if d.Reason != "no destructive ops" {
		t.Errorf("reason = %q", d.Reason)
	}
}

func TestEngine_AllowRule(t *testing.T) {
	e := NewEngine([]config.RuleConfig{
		{Tool: "delete_*", Action: "block"},
		{Tool: "*", Action: "allow"},
	})

	d := e.Evaluate("agent1", "list_users")
	if d.Action != "allow" {
		t.Errorf("action = %q, want allow", d.Action)
	}
}

func TestEngine_FirstMatchWins(t *testing.T) {
	e := NewEngine([]config.RuleConfig{
		{Tool: "query", Action: "block"},
		{Tool: "*", Action: "allow"},
	})

	d := e.Evaluate("agent1", "query")
	if d.Action != "block" {
		t.Errorf("action = %q, want block (first match)", d.Action)
	}
}

func TestEngine_AgentScoping(t *testing.T) {
	e := NewEngine([]config.RuleConfig{
		{Tool: "delete_*", Action: "block", Agents: []string{"untrusted-bot"}},
		{Tool: "*", Action: "allow"},
	})

	// Scoped agent is blocked
	d := e.Evaluate("untrusted-bot", "delete_user")
	if d.Action != "block" {
		t.Errorf("scoped agent: action = %q, want block", d.Action)
	}

	// Other agent is allowed (rule doesn't match)
	d = e.Evaluate("trusted-bot", "delete_user")
	if d.Action != "allow" {
		t.Errorf("other agent: action = %q, want allow", d.Action)
	}
}

func TestEngine_RateLimit(t *testing.T) {
	e := NewEngine([]config.RuleConfig{
		{Tool: "*", Action: "allow", RateLimit: "3/minute"},
	})

	// First 3 calls should be allowed
	for i := 0; i < 3; i++ {
		d := e.Evaluate("agent1", "query")
		if d.Action != "allow" {
			t.Errorf("call %d: action = %q, want allow", i+1, d.Action)
		}
	}

	// 4th call should be rate limited
	d := e.Evaluate("agent1", "query")
	if d.Action != "rate_limited" {
		t.Errorf("4th call: action = %q, want rate_limited", d.Action)
	}
}

func TestEngine_FilterTools_Hide(t *testing.T) {
	e := NewEngine([]config.RuleConfig{
		{Tool: "admin_*", Action: "hide"},
	})

	tools := []string{"query", "admin_settings", "list_users", "admin_reset"}
	visible := e.FilterTools("agent1", tools)

	if len(visible) != 2 {
		t.Fatalf("visible = %d, want 2", len(visible))
	}
	for _, v := range visible {
		if v == "admin_settings" || v == "admin_reset" {
			t.Errorf("admin tool %q should be hidden", v)
		}
	}
}

func TestEngine_FilterTools_HideWithAgentScope(t *testing.T) {
	e := NewEngine([]config.RuleConfig{
		{Tool: "admin_*", Action: "hide", Agents: []string{"restricted-bot"}},
	})

	tools := []string{"query", "admin_settings"}

	// Restricted bot: admin tools hidden
	visible := e.FilterTools("restricted-bot", tools)
	if len(visible) != 1 {
		t.Errorf("restricted: visible = %d, want 1", len(visible))
	}

	// Other bot: admin tools visible
	visible = e.FilterTools("admin-bot", tools)
	if len(visible) != 2 {
		t.Errorf("admin: visible = %d, want 2", len(visible))
	}
}

func TestMatchToolGlob(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*", "anything", true},
		{"delete_*", "delete_user", true},
		{"delete_*", "list_users", false},
		{"*_delete", "bulk_delete", true},
		{"db_*_delete", "db_user_delete", true},
		{"exact", "exact", true},
		{"exact", "other", false},
	}
	for _, tt := range tests {
		got := MatchToolGlob(tt.pattern, tt.name)
		if got != tt.want {
			t.Errorf("MatchToolGlob(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}
