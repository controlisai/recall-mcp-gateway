package detect

import "testing"

func TestErrorAnalyzer_DetectsCorrelation(t *testing.T) {
	analyzer := NewErrorAnalyzer()

	// Build history: send_email often fails after big_query
	var history []HistoryEntry

	// Normal pattern: query → send_email (success)
	for i := 0; i < 20; i++ {
		history = append(history,
			HistoryEntry{Tool: "query"},
			HistoryEntry{Tool: "send_email", IsError: false},
		)
	}

	// Correlated pattern: big_query → send_email (fails 80%)
	for i := 0; i < 10; i++ {
		history = append(history,
			HistoryEntry{Tool: "big_query"},
			HistoryEntry{Tool: "send_email", IsError: i < 8},
		)
	}

	patterns := analyzer.Analyze(history)

	found := false
	for _, p := range patterns {
		if p.FailingTool == "send_email" && p.CorrelatedWith == "big_query" {
			found = true
			if p.Correlation < 1.5 {
				t.Errorf("correlation = %.1f, want >= 1.5", p.Correlation)
			}
		}
	}
	if !found {
		t.Error("expected error correlation: send_email fails after big_query")
	}
}

func TestErrorAnalyzer_NoPatternForRandomErrors(t *testing.T) {
	analyzer := NewErrorAnalyzer()

	var history []HistoryEntry
	for i := 0; i < 100; i++ {
		history = append(history, HistoryEntry{
			Tool:    "query",
			IsError: i%20 == 0, // low random error rate
		})
	}

	patterns := analyzer.Analyze(history)
	if len(patterns) > 0 {
		t.Errorf("expected no patterns for random errors, got %d", len(patterns))
	}
}

func TestErrorAnalyzer_InsufficientData(t *testing.T) {
	analyzer := NewErrorAnalyzer()
	patterns := analyzer.Analyze([]HistoryEntry{
		{Tool: "a"}, {Tool: "b", IsError: true},
	})
	if len(patterns) != 0 {
		t.Error("expected no patterns with insufficient data")
	}
}
