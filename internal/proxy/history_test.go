package proxy

import (
	"testing"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/detect"
)

func TestHistoryBuffer_AddAndGet(t *testing.T) {
	buf := NewHistoryBuffer(5)
	buf.Add("agent1", detect.HistoryEntry{Tool: "query", Timestamp: time.Now()})
	buf.Add("agent1", detect.HistoryEntry{Tool: "insert", Timestamp: time.Now()})

	history := buf.Get("agent1")
	if len(history) != 2 {
		t.Errorf("history = %d, want 2", len(history))
	}
}

func TestHistoryBuffer_RingBuffer(t *testing.T) {
	buf := NewHistoryBuffer(3)
	for i := 0; i < 10; i++ {
		buf.Add("agent1", detect.HistoryEntry{Tool: "tool"})
	}
	if len(buf.Get("agent1")) != 3 {
		t.Errorf("history = %d, want 3 (ring buffer)", len(buf.Get("agent1")))
	}
}

func TestHistoryBuffer_LastSuccessful(t *testing.T) {
	buf := NewHistoryBuffer(10)
	buf.Add("agent1", detect.HistoryEntry{Tool: "query", IsError: false, Arguments: map[string]any{"sql": "SELECT 1"}})
	buf.Add("agent1", detect.HistoryEntry{Tool: "query", IsError: true, Arguments: map[string]any{"sql": "SELECT bad"}})

	last := buf.LastSuccessful("agent1", "query")
	if last == nil {
		t.Fatal("expected last successful")
	}
	if last.Arguments["sql"] != "SELECT 1" {
		t.Errorf("last successful sql = %v", last.Arguments["sql"])
	}
}

func TestHistoryBuffer_RecentTools(t *testing.T) {
	buf := NewHistoryBuffer(10)
	buf.Add("agent1", detect.HistoryEntry{Tool: "a"})
	buf.Add("agent1", detect.HistoryEntry{Tool: "b"})
	buf.Add("agent1", detect.HistoryEntry{Tool: "c"})

	tools := buf.RecentTools("agent1")
	if len(tools) != 3 || tools[0] != "a" || tools[2] != "c" {
		t.Errorf("tools = %v", tools)
	}
}

func TestHistoryBuffer_AllAgents(t *testing.T) {
	buf := NewHistoryBuffer(10)
	buf.Add("agent1", detect.HistoryEntry{Tool: "a"})
	buf.Add("agent2", detect.HistoryEntry{Tool: "b"})

	agents := buf.AllAgents()
	if len(agents) != 2 {
		t.Errorf("agents = %d, want 2", len(agents))
	}
}
