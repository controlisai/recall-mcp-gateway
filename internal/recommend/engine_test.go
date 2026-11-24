package recommend

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

// testAgentProvider returns an agent provider with a single unrestricted agent.
func testAgentProvider() AgentProvider {
	return func() []config.AgentConfig {
		return []config.AgentConfig{{ID: "test-agent"}}
	}
}

func TestEngine_UnprotectedCriticalTool(t *testing.T) {
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete a user permanently")),
		mcp.NewTool("list_users", mcp.WithDescription("List all users")),
	})

	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine(nil)

	engine := NewEngine(profiler, tracker, ruleEngine)
	engine.SetAgentProvider(testAgentProvider())
	recs := engine.Generate(nil)

	found := false
	for _, r := range recs {
		if r.Type == "unprotected_critical_tool" && r.SuggestedRule.Tool == "delete_user" {
			found = true
			if r.Severity != "critical" {
				t.Errorf("severity = %q, want critical", r.Severity)
			}
		}
	}
	if !found {
		t.Error("expected recommendation for unprotected critical tool delete_user")
	}
}

func TestEngine_NoRecWhenNoAgentsHaveAccess(t *testing.T) {
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete a user permanently")),
	})

	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine(nil)

	// Agent restricted to "files" server only — no access to "db"
	engine := NewEngine(profiler, tracker, ruleEngine)
	engine.SetAgentProvider(func() []config.AgentConfig {
		return []config.AgentConfig{{ID: "files-agent", AllowedServers: []string{"files"}}}
	})
	recs := engine.Generate(nil)

	for _, r := range recs {
		if r.Type == "unprotected_critical_tool" && r.SuggestedRule.Tool == "delete_user" {
			t.Error("should not recommend for tool when no agent has server access")
		}
	}
}

func TestEngine_NoRecForProtectedTool(t *testing.T) {
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete a user permanently")),
	})

	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine([]config.RuleConfig{
		{Tool: "delete_user", Action: "block"},
	})

	engine := NewEngine(profiler, tracker, ruleEngine)
	recs := engine.Generate(nil)

	for _, r := range recs {
		if r.Type == "unprotected_critical_tool" && r.SuggestedRule.Tool == "delete_user" {
			t.Error("should not recommend for already-protected tool")
		}
	}
}

func TestEngine_PIIExposure(t *testing.T) {
	profiler := detect.NewProfiler()
	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine(nil)

	engine := NewEngine(profiler, tracker, ruleEngine)
	signals := []SignalSummary{
		{Category: "pii", Tool: "query", Count: 15},
	}

	recs := engine.Generate(signals)

	found := false
	for _, r := range recs {
		if r.Type == "pii_exposure" {
			found = true
		}
	}
	if !found {
		t.Error("expected PII exposure recommendation")
	}
}

func TestEngine_DestructiveSQL(t *testing.T) {
	profiler := detect.NewProfiler()
	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine(nil)

	engine := NewEngine(profiler, tracker, ruleEngine)
	signals := []SignalSummary{
		{Category: "destructive", Tool: "query", Count: 3},
	}

	recs := engine.Generate(signals)

	found := false
	for _, r := range recs {
		if r.Type == "destructive_sql" {
			found = true
			if r.Severity != "critical" {
				t.Errorf("severity = %q, want critical", r.Severity)
			}
		}
	}
	if !found {
		t.Error("expected destructive SQL recommendation")
	}
}

func TestEngine_SortedByImpact(t *testing.T) {
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete user")),
		mcp.NewTool("send_email", mcp.WithDescription("Send email message")),
	})

	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine(nil)

	engine := NewEngine(profiler, tracker, ruleEngine)
	engine.SetAgentProvider(testAgentProvider())
	recs := engine.Generate(nil)

	if len(recs) < 2 {
		t.Skipf("need at least 2 recs for sort test, got %d", len(recs))
	}

	for i := 1; i < len(recs); i++ {
		if recs[i].ImpactScore > recs[i-1].ImpactScore {
			t.Errorf("recs not sorted: [%d].impact=%.0f > [%d].impact=%.0f",
				i, recs[i].ImpactScore, i-1, recs[i-1].ImpactScore)
		}
	}
}

func TestEngine_ApplyAndDismiss(t *testing.T) {
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete user")),
	})
	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine(nil)

	engine := NewEngine(profiler, tracker, ruleEngine)
	engine.SetAgentProvider(testAgentProvider())
	recs := engine.Generate(nil)

	if len(recs) == 0 {
		t.Fatal("expected at least one recommendation")
	}

	// Apply
	rec, err := engine.Apply(recs[0].ID)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rec.Status != "applied" {
		t.Errorf("status = %q, want applied", rec.Status)
	}

	// Active should exclude applied
	active := engine.Active()
	for _, a := range active {
		if a.ID == recs[0].ID {
			t.Error("applied rec should not be in active list")
		}
	}
}

func TestEngine_DismissPreventsRegeneration(t *testing.T) {
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete user")),
	})
	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine(nil)

	engine := NewEngine(profiler, tracker, ruleEngine)
	engine.SetAgentProvider(testAgentProvider())
	recs := engine.Generate(nil)

	if len(recs) == 0 {
		t.Fatal("need recs")
	}

	// Dismiss
	engine.Dismiss(recs[0].ID)

	// Re-generate — should not include dismissed
	recs2 := engine.Generate(nil)
	for _, r := range recs2 {
		if r.SuggestedRule.Tool == "delete_user" && r.Type == "unprotected_critical_tool" {
			t.Error("dismissed recommendation should not reappear")
		}
	}
}
