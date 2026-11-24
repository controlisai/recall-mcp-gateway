package baseline

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPersist_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baselines.json")

	// Create tracker with data
	tracker := NewTracker()
	for i := 0; i < 5; i++ {
		tracker.Record(Observation{
			AgentID: "agent1", Tool: "query",
			Args: map[string]any{"sql": "SELECT 1"}, Timestamp: time.Now(),
		})
	}

	serverHealth := map[string]ServerHealth{
		"db": {Name: "db", MeanLatencyMs: 45, Status: "healthy", SampleCount: 100},
	}

	// Save
	err := SaveState(path, tracker, serverHealth)
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	// Load
	state, err := LoadState(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if state == nil {
		t.Fatal("state is nil")
	}

	if len(state.Baselines) != 1 {
		t.Errorf("baselines = %d, want 1", len(state.Baselines))
	}
	bl := state.Baselines["agent1:query"]
	if bl == nil {
		t.Fatal("baseline not found")
	}
	if bl.SampleCount != 5 {
		t.Errorf("sampleCount = %d, want 5", bl.SampleCount)
	}

	if len(state.ServerHealth) != 1 {
		t.Errorf("serverHealth = %d, want 1", len(state.ServerHealth))
	}
}

func TestPersist_LoadNonExistent(t *testing.T) {
	state, err := LoadState("/nonexistent/baselines.json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state != nil {
		t.Error("expected nil state for nonexistent file")
	}
}

func TestPersist_RestoreTracker(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baselines.json")

	// Create and save
	tracker1 := NewTracker()
	for i := 0; i < 10; i++ {
		tracker1.Record(Observation{
			AgentID: "agent1", Tool: "query", Timestamp: time.Now(),
		})
	}
	SaveState(path, tracker1, nil)

	// Load and restore
	state, _ := LoadState(path)
	tracker2 := NewTracker()
	tracker2.SetBaselines(state.Baselines)

	bl := tracker2.Get("agent1", "query")
	if bl == nil {
		t.Fatal("baseline not restored")
	}
	if bl.SampleCount != 10 {
		t.Errorf("sampleCount = %d, want 10", bl.SampleCount)
	}
}
