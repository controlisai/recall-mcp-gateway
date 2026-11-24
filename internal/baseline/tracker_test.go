package baseline

import (
	"testing"
	"time"
)

func TestTracker_Record_CreatesBaseline(t *testing.T) {
	tracker := NewTracker()
	tracker.Record(Observation{
		AgentID: "agent1", Tool: "query",
		Args: map[string]any{"sql": "SELECT 1"}, Timestamp: time.Now(),
	})

	bl := tracker.Get("agent1", "query")
	if bl == nil {
		t.Fatal("baseline not created")
	}
	if bl.SampleCount != 1 {
		t.Errorf("sampleCount = %d, want 1", bl.SampleCount)
	}
}

func TestTracker_Record_TracksArgFields(t *testing.T) {
	tracker := NewTracker()
	tracker.Record(Observation{
		AgentID: "agent1", Tool: "query",
		Args: map[string]any{"sql": "SELECT 1", "limit": 10}, Timestamp: time.Now(),
	})

	bl := tracker.Get("agent1", "query")
	if !bl.KnownArgFields["sql"] {
		t.Error("sql field not tracked")
	}
	if !bl.KnownArgFields["limit"] {
		t.Error("limit field not tracked")
	}
}

func TestTracker_Record_TracksActiveHours(t *testing.T) {
	tracker := NewTracker()
	now := time.Date(2026, 8, 21, 14, 30, 0, 0, time.UTC) // 2pm
	tracker.Record(Observation{
		AgentID: "agent1", Tool: "query", Timestamp: now,
	})

	bl := tracker.Get("agent1", "query")
	if !bl.ActiveHours[14] {
		t.Error("hour 14 not tracked")
	}
}

func TestTracker_BaselineReady_AfterMinSamples(t *testing.T) {
	tracker := NewTracker()
	for i := 0; i < MinSamplesForBaseline; i++ {
		tracker.Record(Observation{
			AgentID: "agent1", Tool: "query", Timestamp: time.Now(),
		})
	}

	bl := tracker.Get("agent1", "query")
	if !bl.BaselineReady {
		t.Error("baseline should be ready after min samples")
	}
}

func TestTracker_EWMA_Converges(t *testing.T) {
	tracker := NewTracker()
	for i := 0; i < 100; i++ {
		tracker.Record(Observation{
			AgentID: "agent1", Tool: "query", Timestamp: time.Now(),
		})
	}

	bl := tracker.Get("agent1", "query")
	if bl.MeanCallsPerHour == 0 {
		t.Error("mean calls per hour should not be zero after 100 observations")
	}
}

func TestTracker_ErrorRate(t *testing.T) {
	tracker := NewTracker()
	for i := 0; i < 10; i++ {
		tracker.Record(Observation{
			AgentID: "agent1", Tool: "query", IsError: i%5 == 0, Timestamp: time.Now(),
		})
	}

	bl := tracker.Get("agent1", "query")
	if bl.MeanErrorRate == 0 {
		t.Error("error rate should not be zero")
	}
}

func TestTracker_Status(t *testing.T) {
	tracker := NewTracker()
	status := tracker.Status()
	if status != "Waiting for observations..." {
		t.Errorf("status = %q, want waiting", status)
	}

	tracker.Record(Observation{
		AgentID: "agent1", Tool: "query", Timestamp: time.Now(),
	})
	status = tracker.Status()
	if status == "Waiting for observations..." {
		t.Error("status should change after observation")
	}
}

func TestTracker_HasBaseline(t *testing.T) {
	tracker := NewTracker()
	if tracker.HasBaseline("agent1") {
		t.Error("should not have baseline")
	}

	tracker.Record(Observation{AgentID: "agent1", Tool: "query", Timestamp: time.Now()})
	if !tracker.HasBaseline("agent1") {
		t.Error("should have baseline after observation")
	}
	if tracker.HasBaseline("agent2") {
		t.Error("agent2 should not have baseline")
	}
}
