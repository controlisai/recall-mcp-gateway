package baseline

import (
	"fmt"
	"math"
	"strings"
)

// AgentStat holds aggregated statistics for one agent on one tool.
type AgentStat struct {
	AgentID       string  `json:"agentId"`
	Tool          string  `json:"tool"`
	CallsPerHour  float64 `json:"callsPerHour"`
	ErrorRate     float64 `json:"errorRate"`
	SignalsPerHour float64 `json:"signalsPerHour"`
}

// ComparisonResult holds cross-agent comparison for a tool.
type ComparisonResult struct {
	Tool     string      `json:"tool"`
	Agents   []AgentStat `json:"agents"`
	Outliers []string    `json:"outliers,omitempty"` // agent IDs that are outliers
	Insights []string    `json:"insights,omitempty"`
}

// Compare generates cross-agent statistics and outlier detection for shared tools.
func Compare(baselines map[string]*Baseline) []ComparisonResult {
	// Group baselines by tool
	toolAgents := make(map[string][]AgentStat)

	for key, bl := range baselines {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		agentID, tool := parts[0], parts[1]

		stat := AgentStat{
			AgentID:       agentID,
			Tool:          tool,
			CallsPerHour:  bl.MeanCallsPerHour,
			ErrorRate:     bl.MeanErrorRate,
			SignalsPerHour: bl.MeanSignalsPerHour,
		}
		toolAgents[tool] = append(toolAgents[tool], stat)
	}

	var results []ComparisonResult
	for tool, agents := range toolAgents {
		if len(agents) < 2 {
			continue // need at least 2 agents for comparison
		}

		result := ComparisonResult{
			Tool:   tool,
			Agents: agents,
		}

		// Detect outliers using IQR on call rate
		rates := make([]float64, len(agents))
		for i, a := range agents {
			rates[i] = a.CallsPerHour
		}
		outlierThreshold := iqrOutlierThreshold(rates)

		for _, a := range agents {
			if a.CallsPerHour > outlierThreshold && outlierThreshold > 0 {
				result.Outliers = append(result.Outliers, a.AgentID)
				result.Insights = append(result.Insights, fmt.Sprintf(
					"%s calls %s at %.0fx the median rate",
					a.AgentID, tool, a.CallsPerHour/median(rates),
				))
			}
			if a.ErrorRate > 0.1 {
				result.Insights = append(result.Insights, fmt.Sprintf(
					"%s has %.0f%% error rate on %s",
					a.AgentID, a.ErrorRate*100, tool,
				))
			}
		}

		results = append(results, result)
	}

	return results
}

func iqrOutlierThreshold(values []float64) float64 {
	if len(values) < 4 {
		// With few values, use simple 3x median
		m := median(values)
		return m * 3
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sortFloat64s(sorted)

	q1 := sorted[len(sorted)/4]
	q3 := sorted[3*len(sorted)/4]
	iqr := q3 - q1
	return q3 + 1.5*iqr
}

func median(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sortFloat64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

func sortFloat64s(s []float64) {
	for i := 1; i < len(s); i++ {
		key := s[i]
		j := i - 1
		for j >= 0 && s[j] > key {
			s[j+1] = s[j]
			j--
		}
		s[j+1] = key
	}
}

// Unused but satisfies the import of math for potential future use.
var _ = math.Abs
