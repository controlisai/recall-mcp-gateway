package detect

import (
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestSequenceDetector_DataExfiltration(t *testing.T) {
	profiler := NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("get_customers", mcp.WithDescription("Get customer data")),
	})
	profiler.ProfileTools("comms", []mcp.Tool{
		mcp.NewTool("send_email", mcp.WithDescription("Send email message")),
	})

	detector := NewSequenceDetector(profiler)

	// Step 1: read data
	signals := detector.Record("agent1", HistoryEntry{
		Tool: "get_customers", Server: "db", Timestamp: time.Now(),
	})
	if len(signals) != 0 {
		t.Errorf("step 1: expected 0 signals, got %d", len(signals))
	}

	// Step 2: external communication within 60s
	signals = detector.Record("agent1", HistoryEntry{
		Tool: "send_email", Server: "comms", Timestamp: time.Now(),
	})

	found := false
	for _, s := range signals {
		if s.Name == "data_exfiltration" {
			found = true
		}
	}
	if !found {
		t.Error("data_exfiltration sequence not detected")
	}
}

func TestSequenceDetector_NoFalsePositive(t *testing.T) {
	profiler := NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("list_users", mcp.WithDescription("List users")),
		mcp.NewTool("get_user", mcp.WithDescription("Get a user")),
	})

	detector := NewSequenceDetector(profiler)

	// Two reads in a row — should not trigger
	detector.Record("agent1", HistoryEntry{
		Tool: "list_users", Server: "db", Timestamp: time.Now(),
	})
	signals := detector.Record("agent1", HistoryEntry{
		Tool: "get_user", Server: "db", Timestamp: time.Now(),
	})

	for _, s := range signals {
		if s.Name == "data_exfiltration" {
			t.Error("false positive: data_exfiltration on two reads")
		}
	}
}

func TestSequenceDetector_PerAgent(t *testing.T) {
	profiler := NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("get_data", mcp.WithDescription("Get data")),
	})
	profiler.ProfileTools("comms", []mcp.Tool{
		mcp.NewTool("send_email", mcp.WithDescription("Send email")),
	})

	detector := NewSequenceDetector(profiler)

	// Agent 1 reads
	detector.Record("agent1", HistoryEntry{
		Tool: "get_data", Server: "db", Timestamp: time.Now(),
	})

	// Agent 2 sends email — should NOT trigger cross-agent sequence
	signals := detector.Record("agent2", HistoryEntry{
		Tool: "send_email", Server: "comms", Timestamp: time.Now(),
	})

	for _, s := range signals {
		if s.Name == "data_exfiltration" {
			t.Error("false positive: cross-agent sequence detected")
		}
	}
}

func TestSequenceDetector_History(t *testing.T) {
	profiler := NewProfiler()
	detector := NewSequenceDetector(profiler)

	for i := 0; i < 5; i++ {
		detector.Record("agent1", HistoryEntry{
			Tool: "tool", Timestamp: time.Now(),
		})
	}

	history := detector.History("agent1")
	if len(history) != 5 {
		t.Errorf("history = %d, want 5", len(history))
	}
}

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern, str string
		want         bool
	}{
		{"*", "anything", true},
		{"list_*", "list_users", true},
		{"list_*", "get_users", false},
		{"*delete*", "bulk_delete_users", true},
		{"*role*", "get_role", true},
		{"exact", "exact", true},
		{"exact", "other", false},
	}
	for _, tt := range tests {
		got := globMatch(tt.pattern, tt.str)
		if got != tt.want {
			t.Errorf("globMatch(%q, %q) = %v, want %v", tt.pattern, tt.str, got, tt.want)
		}
	}
}

func TestMatchToolPattern_Pipe(t *testing.T) {
	if !matchToolPattern("list_*|get_*", "list_users") {
		t.Error("should match list_*")
	}
	if !matchToolPattern("list_*|get_*", "get_user") {
		t.Error("should match get_*")
	}
	if matchToolPattern("list_*|get_*", "delete_user") {
		t.Error("should not match delete_user")
	}
}
