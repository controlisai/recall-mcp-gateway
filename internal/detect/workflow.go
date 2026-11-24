package detect

import (
	"strings"
	"sync"
)

// WorkflowDetector discovers and tracks common tool call patterns per agent.
type WorkflowDetector struct {
	mu        sync.RWMutex
	expected  map[string][][]string // agentID -> list of expected step sequences
}

// NewWorkflowDetector creates a new workflow detector.
func NewWorkflowDetector() *WorkflowDetector {
	return &WorkflowDetector{
		expected: make(map[string][][]string),
	}
}

// DiscoverWorkflows analyzes call history and extracts recurring tool sequences.
// Looks at n-gram sequences (n=2..maxLen) and counts frequency.
func DiscoverWorkflows(history []HistoryEntry, minCount int, maxLen int) []WorkflowPattern {
	if len(history) < 2 {
		return nil
	}

	// Extract tool names
	tools := make([]string, len(history))
	for i, h := range history {
		tools[i] = h.Tool
	}

	// Count n-gram frequencies
	counts := make(map[string]int)
	for n := 2; n <= maxLen && n <= len(tools); n++ {
		for i := 0; i <= len(tools)-n; i++ {
			key := ngramKey(tools[i : i+n])
			counts[key]++
		}
	}

	// Build patterns from frequent n-grams
	total := len(tools)
	var patterns []WorkflowPattern
	for key, count := range counts {
		if count < minCount {
			continue
		}
		steps := ngramFromKey(key)
		patterns = append(patterns, WorkflowPattern{
			Steps:     steps,
			Count:     count,
			Frequency: float64(count) / float64(total),
		})
	}

	return patterns
}

// SetExpected marks a workflow as expected for an agent.
func (d *WorkflowDetector) SetExpected(agentID string, steps []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.expected[agentID] = append(d.expected[agentID], steps)
}

// RemoveExpected removes an expected workflow.
func (d *WorkflowDetector) RemoveExpected(agentID string, steps []string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	workflows := d.expected[agentID]
	for i, w := range workflows {
		if stepsEqual(w, steps) {
			d.expected[agentID] = append(workflows[:i], workflows[i+1:]...)
			return
		}
	}
}

// GetExpected returns expected workflows for an agent.
func (d *WorkflowDetector) GetExpected(agentID string) [][]string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.expected[agentID]
}

// CheckAssertion checks if recent calls match any expected workflow.
// Returns a signal if the workflow is unexpected, nil if it matches or no expectations are set.
func (d *WorkflowDetector) CheckAssertion(agentID string, recentTools []string) *Signal {
	d.mu.RLock()
	expected := d.expected[agentID]
	d.mu.RUnlock()

	if len(expected) == 0 || len(recentTools) < 2 {
		return nil
	}

	for _, exp := range expected {
		if subsequenceMatch(recentTools, exp) {
			return nil
		}
	}

	return &Signal{
		Name:     "unexpected_workflow",
		Category: "workflow",
		Severity: "warning",
		Message:  "Agent followed an unexpected workflow pattern",
	}
}

// subsequenceMatch checks if expected is an ordered subsequence of observed.
func subsequenceMatch(observed, expected []string) bool {
	ei := 0
	for _, tool := range observed {
		if ei < len(expected) && tool == expected[ei] {
			ei++
		}
	}
	return ei == len(expected)
}

const ngramSep = "|"

func ngramKey(tools []string) string {
	return strings.Join(tools, ngramSep)
}

func ngramFromKey(key string) []string {
	return strings.Split(key, ngramSep)
}

func stepsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
