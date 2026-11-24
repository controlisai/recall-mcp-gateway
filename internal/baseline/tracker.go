package baseline

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// Observation is a single data point recorded by the tracker.
type Observation struct {
	AgentID     string
	Tool        string
	IsError     bool
	SignalCount int
	Args        map[string]any
	Timestamp   time.Time
}

// Tracker maintains EWMA baselines per (agent, tool).
type Tracker struct {
	mu        sync.RWMutex
	baselines map[string]*Baseline // key: "agent:tool"
	startTime time.Time
}

// NewTracker creates a new baseline tracker.
func NewTracker() *Tracker {
	return &Tracker{
		baselines: make(map[string]*Baseline),
		startTime: time.Now(),
	}
}

// Record processes an observation and updates the baseline.
func (t *Tracker) Record(obs Observation) {
	key := baselineKey(obs.AgentID, obs.Tool)

	t.mu.Lock()
	defer t.mu.Unlock()

	bl, ok := t.baselines[key]
	if !ok {
		bl = &Baseline{
			KnownArgFields: make(map[string]bool),
			KnownArgValues: make(map[string][]string),
			CreatedAt:      time.Now(),
		}
		t.baselines[key] = bl
	}

	bl.SampleCount++
	bl.LastUpdateAt = obs.Timestamp
	bl.LastSeen = time.Now()

	alpha := t.alpha(bl)

	// Update call rate (calls per hour, estimated from inter-arrival)
	if bl.SampleCount > 1 {
		hoursSinceStart := time.Since(bl.CreatedAt).Hours()
		if hoursSinceStart > 0 {
			currentRate := float64(bl.SampleCount) / hoursSinceStart
			bl.MeanCallsPerHour = ewma(bl.MeanCallsPerHour, currentRate, alpha)
			// Update stddev approximation
			deviation := math.Abs(currentRate - bl.MeanCallsPerHour)
			bl.StdDevCallsPerHour = ewma(bl.StdDevCallsPerHour, deviation, alpha)
		}
	}

	// Update error rate
	errorVal := 0.0
	if obs.IsError {
		errorVal = 1.0
	}
	bl.MeanErrorRate = ewma(bl.MeanErrorRate, errorVal, alpha)

	// Update signal rate
	bl.MeanSignalsPerHour = ewma(bl.MeanSignalsPerHour, float64(obs.SignalCount), alpha)

	// Track argument fields and values
	for k, v := range obs.Args {
		bl.KnownArgFields[k] = true
		strVal := fmt.Sprintf("%v", v)
		if len(bl.KnownArgValues[k]) < MaxKnownValues {
			if !containsStr(bl.KnownArgValues[k], strVal) {
				bl.KnownArgValues[k] = append(bl.KnownArgValues[k], strVal)
			}
		}
	}

	// Track active hours and days (UTC for consistency; dashboard converts to local)
	utc := obs.Timestamp.UTC()
	hour := utc.Hour()
	day := utc.Weekday()
	bl.ActiveHours[hour] = true
	bl.ActiveDays[day] = true

	// Check if baseline is ready (for non-default baselines)
	if !bl.BaselineReady {
		hours := time.Since(bl.CreatedAt).Hours()
		if bl.SampleCount >= MinSamplesForBaseline || hours >= MinHoursForBaseline {
			bl.BaselineReady = true
		}
	}

	// Flip source from "default" to "learned" once enough real data accumulated
	if bl.BaselineSource == "default" && bl.SampleCount >= MinSamplesForBaseline {
		bl.BaselineSource = "learned"
	}
}

// Get returns the baseline for an (agent, tool) pair.
// Falls back to wildcard ("*") baseline if no agent-specific baseline exists
// (used for default profiles applied to all agents).
func (t *Tracker) Get(agentID, tool string) *Baseline {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if bl := t.baselines[baselineKey(agentID, tool)]; bl != nil {
		return bl
	}
	// Fall back to wildcard default
	return t.baselines[baselineKey("*", tool)]
}

// ApplyDefaults pre-populates baselines for a set of tools using a profile.
// This gives anomaly detection a starting point from minute one.
// Only applies to (agent, tool) pairs that don't already have a baseline.
// The default baselines are marked BaselineReady = true and Source = "default".
// As real observations arrive, EWMA blends them in. After MinSamplesForBaseline
// observations, the source flips to "learned".
func (t *Tracker) ApplyDefaults(agentID string, toolNames []string, profile *Profile) int {
	if profile == nil {
		return 0
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	applied := 0
	now := time.Now()
	for _, tool := range toolNames {
		key := baselineKey(agentID, tool)
		if _, exists := t.baselines[key]; exists {
			continue // don't overwrite existing baseline (learned or previously defaulted)
		}
		t.baselines[key] = &Baseline{
			MeanCallsPerHour:   profile.MeanCallsPerHour,
			StdDevCallsPerHour: profile.StdDevCallsPerHour,
			MeanErrorRate:      profile.MeanErrorRate,
			MeanSignalsPerHour: profile.MeanSignalsPerHour,
			KnownArgFields:     make(map[string]bool),
			KnownArgValues:     make(map[string][]string),
			BaselineReady:      true,
			BaselineSource:     "default",
			CreatedAt:          now,
		}
		applied++
	}
	return applied
}

// All returns all baselines.
func (t *Tracker) All() map[string]*Baseline {
	t.mu.RLock()
	defer t.mu.RUnlock()
	result := make(map[string]*Baseline, len(t.baselines))
	for k, v := range t.baselines {
		result[k] = v
	}
	return result
}

// PruneStale removes baselines not seen within maxAge.
func (t *Tracker) PruneStale(maxAge time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := time.Now().Add(-maxAge)
	pruned := 0
	for key, bl := range t.baselines {
		if !bl.LastSeen.IsZero() && bl.LastSeen.Before(cutoff) {
			delete(t.baselines, key)
			pruned++
		}
	}
	return pruned
}

// HasBaseline returns true if any baseline exists for this agent.
func (t *Tracker) HasBaseline(agentID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	for k := range t.baselines {
		if len(k) > len(agentID)+1 && k[:len(agentID)+1] == agentID+":" {
			return true
		}
	}
	return false
}

// Status returns a human-readable baseline status.
func (t *Tracker) Status() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	total := len(t.baselines)
	ready := 0
	for _, bl := range t.baselines {
		if bl.BaselineReady {
			ready++
		}
	}
	hours := time.Since(t.startTime).Hours()
	if total == 0 {
		return "Waiting for observations..."
	}
	if hours < 1 {
		return fmt.Sprintf("Learning... (%d observations)", t.totalSamples())
	}
	if ready == 0 {
		return fmt.Sprintf("Baselines forming... (%d observations, %d tools)", t.totalSamples(), total)
	}
	return fmt.Sprintf("Baselines active — %d of %d ready", ready, total)
}

// SetBaselines loads baselines (for persistence restore).
func (t *Tracker) SetBaselines(baselines map[string]*Baseline) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.baselines = baselines
}

func (t *Tracker) totalSamples() int {
	total := 0
	for _, bl := range t.baselines {
		total += bl.SampleCount
	}
	return total
}

func (t *Tracker) alpha(bl *Baseline) float64 {
	if time.Since(bl.CreatedAt).Hours() < MinHoursForBaseline {
		return AlphaResponsive
	}
	return AlphaStable
}

func ewma(prev, current, alpha float64) float64 {
	if prev == 0 {
		return current
	}
	return alpha*current + (1-alpha)*prev
}

func baselineKey(agentID, tool string) string {
	return agentID + ":" + tool
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
