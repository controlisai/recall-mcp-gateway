package detect

import (
	"fmt"
	"sync"
)

// ErrorPattern represents a detected error correlation.
type ErrorPattern struct {
	FailingTool    string  `json:"failingTool"`
	ErrorRate      float64 `json:"errorRate"`
	CorrelatedWith string  `json:"correlatedWith"`
	Correlation    float64 `json:"correlation"` // 0.0 to 1.0
	Suggestion     string  `json:"suggestion"`
}

// ErrorAnalyzer finds correlations between tool call failures and preceding calls.
type ErrorAnalyzer struct {
	mu       sync.RWMutex
	patterns []ErrorPattern
}

// NewErrorAnalyzer creates a new error correlation analyzer.
func NewErrorAnalyzer() *ErrorAnalyzer {
	return &ErrorAnalyzer{}
}

// Analyze examines history for error patterns.
// Looks at what tool was called before each failed call.
func (ea *ErrorAnalyzer) Analyze(history []HistoryEntry) []ErrorPattern {
	if len(history) < 5 {
		return nil
	}

	// Count total calls and error calls per tool
	totalByTool := make(map[string]int)
	errorByTool := make(map[string]int)
	for _, h := range history {
		totalByTool[h.Tool]++
		if h.IsError {
			errorByTool[h.Tool]++
		}
	}

	// For each tool with errors, count what preceded it
	// precededBy[failTool][precedingTool] = count of failures when preceded by that tool
	precededBy := make(map[string]map[string]int)
	precededByTotal := make(map[string]map[string]int)

	for i := 1; i < len(history); i++ {
		current := history[i]
		prev := history[i-1]

		if precededByTotal[current.Tool] == nil {
			precededByTotal[current.Tool] = make(map[string]int)
		}
		precededByTotal[current.Tool][prev.Tool]++

		if current.IsError {
			if precededBy[current.Tool] == nil {
				precededBy[current.Tool] = make(map[string]int)
			}
			precededBy[current.Tool][prev.Tool]++
		}
	}

	// Find significant correlations
	var patterns []ErrorPattern
	for failTool, errorCount := range errorByTool {
		if errorCount < 3 || totalByTool[failTool] < 5 {
			continue // not enough data
		}

		overallErrorRate := float64(errorCount) / float64(totalByTool[failTool])
		if overallErrorRate < 0.05 {
			continue // too low to be interesting
		}

		for prevTool, failsAfter := range precededBy[failTool] {
			totalAfter := precededByTotal[failTool][prevTool]
			if totalAfter < 3 {
				continue
			}
			conditionalErrorRate := float64(failsAfter) / float64(totalAfter)

			// Is the error rate significantly higher when preceded by this tool?
			if conditionalErrorRate > overallErrorRate*1.5 && conditionalErrorRate > 0.1 {
				patterns = append(patterns, ErrorPattern{
					FailingTool:    failTool,
					ErrorRate:      conditionalErrorRate,
					CorrelatedWith: prevTool,
					Correlation:    conditionalErrorRate / overallErrorRate,
					Suggestion:     fmt.Sprintf("%s fails %.0f%% when preceded by %s (overall: %.0f%%)", failTool, conditionalErrorRate*100, prevTool, overallErrorRate*100),
				})
			}
		}
	}

	ea.mu.Lock()
	ea.patterns = patterns
	ea.mu.Unlock()

	return patterns
}

// Patterns returns the most recently detected error patterns.
func (ea *ErrorAnalyzer) Patterns() []ErrorPattern {
	ea.mu.RLock()
	defer ea.mu.RUnlock()
	result := make([]ErrorPattern, len(ea.patterns))
	copy(result, ea.patterns)
	return result
}
