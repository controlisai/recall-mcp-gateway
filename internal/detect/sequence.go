package detect

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// SequenceStep defines one step in a suspicious sequence pattern.
type SequenceStep struct {
	ToolPattern    string        // glob pattern for tool name (e.g., "list_*")
	Classification string        // classification match (e.g., "DESTRUCTIVE")
	Within         time.Duration // max time since previous step
	CountGt        int           // require more than N occurrences of this step
}

// SequencePatternDef defines a complete suspicious sequence pattern.
type SequencePatternDef struct {
	Name     string
	Desc     string
	Steps    []SequenceStep
	Severity string
}

// BuiltInSequences are the default suspicious sequence patterns.
var BuiltInSequences = []SequencePatternDef{
	{
		Name: "data_exfiltration",
		Desc: "Read sensitive data then transferred externally",
		Steps: []SequenceStep{
			{Classification: "READ_ONLY"},
			{Classification: "EXTERNAL_COMM", Within: 60 * time.Second},
		},
		Severity: "critical",
	},
	{
		Name: "bulk_delete",
		Desc: "Enumerated records then deleted multiple",
		Steps: []SequenceStep{
			{ToolPattern: "list_*|get_*|search_*"},
			{Classification: "DESTRUCTIVE", Within: 30 * time.Second, CountGt: 2},
		},
		Severity: "critical",
	},
	{
		Name: "privilege_escalation",
		Desc: "Queried permissions then attempted to modify access",
		Steps: []SequenceStep{
			{ToolPattern: "*role*|*permission*|*access*|*auth*"},
			{ToolPattern: "*update*|*set*|*grant*|*create*admin*", Within: 120 * time.Second},
		},
		Severity: "critical",
	},
}

// SequenceDetector tracks per-agent call history and matches suspicious patterns.
type SequenceDetector struct {
	mu       sync.RWMutex
	history  map[string][]HistoryEntry // agentID -> recent calls
	maxSize  int
	patterns []SequencePatternDef
	profiler *Profiler
}

// NewSequenceDetector creates a detector with built-in patterns.
func NewSequenceDetector(profiler *Profiler) *SequenceDetector {
	return &SequenceDetector{
		history:  make(map[string][]HistoryEntry),
		maxSize:  50,
		patterns: BuiltInSequences,
		profiler: profiler,
	}
}

// Record adds a call to the agent's history and checks for sequence matches.
func (d *SequenceDetector) Record(agentID string, entry HistoryEntry) []Signal {
	d.mu.Lock()
	history := d.history[agentID]
	history = append(history, entry)
	if len(history) > d.maxSize {
		history = history[len(history)-d.maxSize:]
	}
	d.history[agentID] = history
	d.mu.Unlock()

	return d.checkPatterns(agentID, history)
}

// History returns the recent call history for an agent.
func (d *SequenceDetector) History(agentID string) []HistoryEntry {
	d.mu.RLock()
	defer d.mu.RUnlock()
	h := d.history[agentID]
	result := make([]HistoryEntry, len(h))
	copy(result, h)
	return result
}

func (d *SequenceDetector) checkPatterns(agentID string, history []HistoryEntry) []Signal {
	var signals []Signal
	for _, pattern := range d.patterns {
		if d.matchPattern(pattern, history) {
			signals = append(signals, Signal{
				Name:     pattern.Name,
				Category: "sequence",
				Severity: pattern.Severity,
				Message:  fmt.Sprintf("Suspicious sequence detected: %s", pattern.Desc),
			})
		}
	}
	return signals
}

func (d *SequenceDetector) matchPattern(pattern SequencePatternDef, history []HistoryEntry) bool {
	if len(history) < len(pattern.Steps) {
		return false
	}

	// Try to match steps in order from the end of history backward
	stepIdx := len(pattern.Steps) - 1
	lastMatchTime := time.Time{}

	for i := len(history) - 1; i >= 0 && stepIdx >= 0; i-- {
		step := pattern.Steps[stepIdx]
		entry := history[i]

		if d.matchStep(step, entry, history[:i+1]) {
			if stepIdx < len(pattern.Steps)-1 && !lastMatchTime.IsZero() && step.Within > 0 {
				// Check time constraint
				if lastMatchTime.Sub(entry.Timestamp) > step.Within {
					continue
				}
			}
			lastMatchTime = entry.Timestamp
			stepIdx--
		}
	}

	return stepIdx < 0
}

func (d *SequenceDetector) matchStep(step SequenceStep, entry HistoryEntry, historyUpTo []HistoryEntry) bool {
	// Check tool pattern
	if step.ToolPattern != "" {
		if !matchToolPattern(step.ToolPattern, entry.Tool) {
			return false
		}
	}

	// Check classification
	if step.Classification != "" {
		profile := d.profiler.GetProfile(entry.Tool)
		if profile == nil {
			return false
		}
		classMatch := false
		for _, class := range strings.Split(step.Classification, "|") {
			if profile.Classification == class {
				classMatch = true
				break
			}
		}
		if !classMatch {
			return false
		}
	}

	// Check count threshold
	if step.CountGt > 0 {
		count := 0
		for _, h := range historyUpTo {
			if d.entryMatchesStep(step, h) {
				count++
			}
		}
		if count <= step.CountGt {
			return false
		}
	}

	return true
}

func (d *SequenceDetector) entryMatchesStep(step SequenceStep, entry HistoryEntry) bool {
	if step.ToolPattern != "" && !matchToolPattern(step.ToolPattern, entry.Tool) {
		return false
	}
	if step.Classification != "" {
		profile := d.profiler.GetProfile(entry.Tool)
		if profile == nil {
			return false
		}
		found := false
		for _, class := range strings.Split(step.Classification, "|") {
			if profile.Classification == class {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// matchToolPattern matches a tool name against a pipe-separated glob pattern.
func matchToolPattern(pattern, name string) bool {
	for _, p := range strings.Split(pattern, "|") {
		if globMatch(strings.TrimSpace(p), name) {
			return true
		}
	}
	return false
}

// globMatch is a simple glob matcher supporting * wildcard.
func globMatch(pattern, str string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == str
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		return strings.HasPrefix(str, parts[0]) && strings.HasSuffix(str, parts[1])
	}
	// Multi-star: check each part appears in order
	remaining := str
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(remaining, part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 {
			return false // first part must be prefix
		}
		remaining = remaining[idx+len(part):]
	}
	return true
}
