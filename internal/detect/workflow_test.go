package detect

import "testing"

func TestDiscoverWorkflows(t *testing.T) {
	// Create a repeating pattern
	history := []HistoryEntry{}
	pattern := []string{"get_customer", "query", "send_email"}

	for i := 0; i < 30; i++ {
		history = append(history, HistoryEntry{Tool: pattern[i%3]})
	}

	workflows := DiscoverWorkflows(history, 3, 3)
	if len(workflows) == 0 {
		t.Fatal("expected at least one workflow")
	}

	// Should find the 3-step pattern
	found := false
	for _, wf := range workflows {
		if len(wf.Steps) == 3 &&
			wf.Steps[0] == "get_customer" &&
			wf.Steps[1] == "query" &&
			wf.Steps[2] == "send_email" {
			found = true
			if wf.Count < 3 {
				t.Errorf("count = %d, want >= 3", wf.Count)
			}
		}
	}
	if !found {
		t.Error("expected pattern [get_customer, query, send_email] not discovered")
	}
}

func TestDiscoverWorkflows_TooFewEntries(t *testing.T) {
	workflows := DiscoverWorkflows([]HistoryEntry{{Tool: "a"}}, 2, 3)
	if len(workflows) != 0 {
		t.Errorf("expected 0 workflows for single entry, got %d", len(workflows))
	}
}

func TestWorkflowDetector_ExpectedMatch(t *testing.T) {
	wd := NewWorkflowDetector()
	wd.SetExpected("agent1", []string{"get_customer", "query", "send_email"})

	// Observed matches expected
	signal := wd.CheckAssertion("agent1", []string{"get_customer", "query", "send_email"})
	if signal != nil {
		t.Error("expected nil signal for matching workflow")
	}
}

func TestWorkflowDetector_SubsequenceMatch(t *testing.T) {
	wd := NewWorkflowDetector()
	wd.SetExpected("agent1", []string{"get_customer", "query", "send_email"})

	// Observed has extra steps but expected is subsequence
	signal := wd.CheckAssertion("agent1", []string{"get_customer", "log_access", "query", "format", "send_email"})
	if signal != nil {
		t.Error("expected nil signal for subsequence match")
	}
}

func TestWorkflowDetector_Unexpected(t *testing.T) {
	wd := NewWorkflowDetector()
	wd.SetExpected("agent1", []string{"get_customer", "query", "send_email"})

	// Completely different workflow
	signal := wd.CheckAssertion("agent1", []string{"delete_user", "drop_table"})
	if signal == nil {
		t.Fatal("expected signal for unexpected workflow")
	}
	if signal.Name != "unexpected_workflow" {
		t.Errorf("signal = %q, want unexpected_workflow", signal.Name)
	}
}

func TestWorkflowDetector_NoExpectations(t *testing.T) {
	wd := NewWorkflowDetector()

	// No expectations set — should not fire
	signal := wd.CheckAssertion("agent1", []string{"anything", "goes"})
	if signal != nil {
		t.Error("expected nil signal when no expectations set")
	}
}

func TestWorkflowDetector_RemoveExpected(t *testing.T) {
	wd := NewWorkflowDetector()
	wd.SetExpected("agent1", []string{"a", "b", "c"})
	wd.SetExpected("agent1", []string{"x", "y"})

	wd.RemoveExpected("agent1", []string{"a", "b", "c"})

	expected := wd.GetExpected("agent1")
	if len(expected) != 1 {
		t.Errorf("expected 1, got %d", len(expected))
	}
}

func TestSubsequenceMatch(t *testing.T) {
	tests := []struct {
		observed []string
		expected []string
		want     bool
	}{
		{[]string{"a", "b", "c"}, []string{"a", "b", "c"}, true},
		{[]string{"a", "x", "b", "y", "c"}, []string{"a", "b", "c"}, true},
		{[]string{"a", "c", "b"}, []string{"a", "b", "c"}, false},
		{[]string{"b", "c"}, []string{"a", "b", "c"}, false},
		{[]string{}, []string{"a"}, false},
	}
	for _, tt := range tests {
		got := subsequenceMatch(tt.observed, tt.expected)
		if got != tt.want {
			t.Errorf("subsequenceMatch(%v, %v) = %v, want %v", tt.observed, tt.expected, got, tt.want)
		}
	}
}
