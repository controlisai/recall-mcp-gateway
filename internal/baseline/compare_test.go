package baseline

import "testing"

func TestCompare_DetectsOutlier(t *testing.T) {
	baselines := map[string]*Baseline{
		"agent1:query": {MeanCallsPerHour: 10},
		"agent2:query": {MeanCallsPerHour: 12},
		"agent3:query": {MeanCallsPerHour: 120}, // outlier
	}

	results := Compare(baselines)
	if len(results) == 0 {
		t.Fatal("expected comparison results")
	}

	found := false
	for _, r := range results {
		if r.Tool == "query" {
			if len(r.Outliers) > 0 {
				found = true
			}
		}
	}
	if !found {
		t.Error("agent3 should be flagged as outlier")
	}
}

func TestCompare_NoOutlierWhenSimilar(t *testing.T) {
	baselines := map[string]*Baseline{
		"agent1:query": {MeanCallsPerHour: 10},
		"agent2:query": {MeanCallsPerHour: 12},
		"agent3:query": {MeanCallsPerHour: 11},
	}

	results := Compare(baselines)
	for _, r := range results {
		if len(r.Outliers) > 0 {
			t.Errorf("no outliers expected, got %v", r.Outliers)
		}
	}
}

func TestCompare_NeedsMultipleAgents(t *testing.T) {
	baselines := map[string]*Baseline{
		"agent1:query": {MeanCallsPerHour: 10},
	}

	results := Compare(baselines)
	if len(results) != 0 {
		t.Errorf("expected no results with single agent, got %d", len(results))
	}
}

func TestCompare_HighErrorRateInsight(t *testing.T) {
	baselines := map[string]*Baseline{
		"agent1:query": {MeanCallsPerHour: 10, MeanErrorRate: 0.02},
		"agent2:query": {MeanCallsPerHour: 10, MeanErrorRate: 0.25}, // high error
	}

	results := Compare(baselines)
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	hasInsight := false
	for _, r := range results {
		for _, insight := range r.Insights {
			if len(insight) > 0 {
				hasInsight = true
			}
		}
	}
	if !hasInsight {
		t.Error("expected insight about high error rate")
	}
}
