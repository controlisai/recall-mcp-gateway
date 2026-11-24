package detect

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestDataFlowTracker_DetectsFlow(t *testing.T) {
	profiler := NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("query", mcp.WithDescription("Run query")),
	})
	profiler.ProfileTools("comms", []mcp.Tool{
		mcp.NewTool("send_email", mcp.WithDescription("Send email")),
	})

	tracker := NewDataFlowTracker()

	history := []HistoryEntry{
		{Tool: "query", Signals: []Signal{{Name: "ssn", Category: "pii"}}},
		{Tool: "format_data"},
		{Tool: "send_email", Signals: []Signal{{Name: "ssn", Category: "pii"}}},
	}

	tracker.Analyze(history, profiler)

	flows := tracker.Flows()
	if len(flows) == 0 {
		t.Fatal("expected at least one flow")
	}

	found := false
	for _, f := range flows {
		if f.DataType == "ssn" && f.EntryTool == "query" {
			found = true
			if len(f.ExitTools) == 0 || f.ExitTools[0] != "send_email" {
				t.Errorf("exit = %v, want [send_email]", f.ExitTools)
			}
		}
	}
	if !found {
		t.Error("ssn flow from query not found")
	}
}

func TestDataFlowTracker_HighRiskForExternalComm(t *testing.T) {
	profiler := NewProfiler()
	profiler.ProfileTools("comms", []mcp.Tool{
		mcp.NewTool("send_email", mcp.WithDescription("Send email")),
	})

	tracker := NewDataFlowTracker()

	history := []HistoryEntry{
		{Tool: "query", Signals: []Signal{{Name: "credit_card", Category: "pii"}}},
		{Tool: "send_email", Signals: []Signal{{Name: "credit_card", Category: "pii"}}},
	}

	tracker.Analyze(history, profiler)

	flows := tracker.Flows()
	for _, f := range flows {
		if f.DataType == "credit_card" && f.Risk != "high" {
			t.Errorf("risk = %q, want high for external comm", f.Risk)
		}
	}
}

func TestDataFlowTracker_NoFlowWithoutMatchingPII(t *testing.T) {
	tracker := NewDataFlowTracker()

	history := []HistoryEntry{
		{Tool: "query", Signals: []Signal{{Name: "ssn", Category: "pii"}}},
		{Tool: "send_email", Signals: []Signal{{Name: "email_address", Category: "pii"}}},
	}

	tracker.Analyze(history, nil)

	flows := tracker.Flows()
	for _, f := range flows {
		if f.DataType == "ssn" && len(f.ExitTools) > 0 {
			t.Error("should not detect flow when PII types differ")
		}
	}
}
