package alerts

import (
	"testing"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
)

func buildReadyTracker(agentID, tool string, samples int) *baseline.Tracker {
	tracker := baseline.NewTracker()
	for i := 0; i < samples; i++ {
		tracker.Record(baseline.Observation{
			AgentID: agentID, Tool: tool,
			Timestamp: time.Now(),
		})
	}
	return tracker
}

func TestDetector_NewToolUsage(t *testing.T) {
	tracker := buildReadyTracker("agent1", "query", 60)
	detector := NewDetector(tracker)

	// Agent calls a tool it's never used before
	alerts := detector.Check("agent1", "delete_all", false, 0, nil)
	found := false
	for _, a := range alerts {
		if a.Type == "new_tool" {
			found = true
		}
	}
	if !found {
		t.Error("expected new_tool alert")
	}
}

func TestDetector_NewToolUsage_NoAlertForUnknownAgent(t *testing.T) {
	tracker := baseline.NewTracker()
	detector := NewDetector(tracker)

	// Agent with no baselines at all — shouldn't alert
	alerts := detector.Check("new-agent", "query", false, 0, nil)
	for _, a := range alerts {
		if a.Type == "new_tool" {
			t.Error("should not alert for agent with no baselines")
		}
	}
}

func TestDetector_NewArgPattern(t *testing.T) {
	tracker := baseline.NewTracker()
	// Build baseline with known args
	for i := 0; i < 60; i++ {
		tracker.Record(baseline.Observation{
			AgentID: "agent1", Tool: "query",
			Args:      map[string]any{"sql": "SELECT 1"},
			Timestamp: time.Now(),
		})
	}

	detector := NewDetector(tracker)

	// Call with a new argument field
	alerts := detector.Check("agent1", "query", false, 0, map[string]any{
		"sql": "SELECT 1", "admin_override": true,
	})
	found := false
	for _, a := range alerts {
		if a.Type == "new_arg_pattern" {
			found = true
		}
	}
	if !found {
		t.Error("expected new_arg_pattern alert for unknown field")
	}
}

func TestDetector_Cooldown(t *testing.T) {
	tracker := buildReadyTracker("agent1", "query", 60)
	detector := NewDetector(tracker)
	detector.cooldown = 100 * time.Millisecond

	// First alert should fire
	alerts1 := detector.Check("agent1", "delete_all", false, 0, nil)
	if len(alerts1) == 0 {
		t.Fatal("expected alert on first check")
	}

	// Same alert within cooldown should not fire
	alerts2 := detector.Check("agent1", "delete_all", false, 0, nil)
	for _, a := range alerts2 {
		if a.Type == "new_tool" {
			t.Error("should be in cooldown")
		}
	}

	// After cooldown
	time.Sleep(150 * time.Millisecond)
	alerts3 := detector.Check("agent1", "delete_all", false, 0, nil)
	found := false
	for _, a := range alerts3 {
		if a.Type == "new_tool" {
			found = true
		}
	}
	if !found {
		t.Error("expected alert after cooldown")
	}
}

func TestDetector_Recent(t *testing.T) {
	tracker := buildReadyTracker("agent1", "query", 60)
	detector := NewDetector(tracker)

	detector.Check("agent1", "tool1", false, 0, nil)
	detector.Check("agent1", "tool2", false, 0, nil)

	recent := detector.Recent()
	if len(recent) < 2 {
		t.Errorf("recent = %d, want >= 2", len(recent))
	}
}

func TestDetector_NoAlertWhenNotReady(t *testing.T) {
	tracker := baseline.NewTracker()
	// Only a few observations — not ready
	for i := 0; i < 3; i++ {
		tracker.Record(baseline.Observation{
			AgentID: "agent1", Tool: "query", Timestamp: time.Now(),
		})
	}

	detector := NewDetector(tracker)
	alerts := detector.Check("agent1", "query", true, 5, nil)
	if len(alerts) != 0 {
		t.Errorf("expected no alerts before baseline ready, got %d", len(alerts))
	}
}
