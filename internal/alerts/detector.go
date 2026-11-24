package alerts

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
)

// Detector monitors baselines and generates alerts for anomalies.
type Detector struct {
	mu        sync.RWMutex
	tracker   *baseline.Tracker
	cooldowns map[string]time.Time // alertKey -> last fired
	cooldown  time.Duration
	threshold float64
	recent    []Alert // ring buffer of recent alerts
	maxRecent int
}

// NewDetector creates a new anomaly detector.
func NewDetector(tracker *baseline.Tracker) *Detector {
	return &Detector{
		tracker:   tracker,
		cooldowns: make(map[string]time.Time),
		cooldown:  5 * time.Minute,
		threshold: baseline.DeviationThreshold,
		maxRecent: 50,
	}
}

// Check evaluates an observation against baselines and returns any alerts.
func (d *Detector) Check(agentID, tool string, isError bool, signalCount int, args map[string]any) []Alert {
	bl := d.tracker.Get(agentID, tool)
	now := time.Now()
	var alerts []Alert

	// 1. New tool usage
	if bl == nil {
		if d.tracker.HasBaseline(agentID) {
			if alert := d.fireIfCooled("new_tool:"+agentID+":"+tool, Alert{
				Type:     "new_tool",
				Severity: "warning",
				AgentID:  agentID,
				Tool:     tool,
				Message:  fmt.Sprintf("Agent %s called %s for the first time", agentID, tool),
			}); alert != nil {
				alerts = append(alerts, *alert)
			}
		}
		return alerts
	}

	if !bl.BaselineReady {
		return nil // not enough data yet
	}

	// 2. Rate spike
	hoursSinceStart := time.Since(bl.CreatedAt).Hours()
	if hoursSinceStart > 0 && bl.MeanCallsPerHour > 0 {
		currentRate := float64(bl.SampleCount) / hoursSinceStart
		if bl.StdDevCallsPerHour > 0 {
			zScore := (currentRate - bl.MeanCallsPerHour) / bl.StdDevCallsPerHour
			if zScore > d.threshold {
				deviation := currentRate / bl.MeanCallsPerHour
				severity := "warning"
				if deviation > 5 {
					severity = "critical"
				}
				if alert := d.fireIfCooled("rate_spike:"+agentID+":"+tool, Alert{
					Type:      "rate_spike",
					Severity:  severity,
					AgentID:   agentID,
					Tool:      tool,
					Message:   fmt.Sprintf("Agent %s calling %s at %.0fx normal rate", agentID, tool, deviation),
					Current:   currentRate,
					Baseline:  bl.MeanCallsPerHour,
					Deviation: deviation,
				}); alert != nil {
					alerts = append(alerts, *alert)
				}
			}
		}
	}

	// 3. New argument pattern
	for k := range args {
		if !bl.KnownArgFields[k] {
			if alert := d.fireIfCooled("new_arg:"+agentID+":"+tool+":"+k, Alert{
				Type:     "new_arg_pattern",
				Severity: "info",
				AgentID:  agentID,
				Tool:     tool,
				Message:  fmt.Sprintf("New argument field %q in %s (never seen before)", k, tool),
			}); alert != nil {
				alerts = append(alerts, *alert)
			}
		}
	}

	// 4. Off-hours activity
	hour := now.Hour()
	day := now.Weekday()
	if !bl.ActiveHours[hour] && countActiveHours(bl) >= 4 {
		if alert := d.fireIfCooled("off_hours:"+agentID, Alert{
			Type:     "off_hours",
			Severity: "warning",
			AgentID:  agentID,
			Tool:     tool,
			Message:  fmt.Sprintf("Agent %s active at %02d:00 (outside normal hours)", agentID, hour),
		}); alert != nil {
			alerts = append(alerts, *alert)
		}
	}
	_ = day // days tracked but not alerted on yet

	// 5. Error spike
	if isError && bl.MeanErrorRate > 0 {
		// Use a simple threshold: current error rate significantly above baseline
		if bl.MeanErrorRate < 0.1 { // baseline has low error rate
			if alert := d.fireIfCooled("error_spike:"+agentID+":"+tool, Alert{
				Type:      "error_spike",
				Severity:  "warning",
				AgentID:   agentID,
				Tool:      tool,
				Message:   fmt.Sprintf("Error rate for %s on %s exceeds baseline", agentID, tool),
				Current:   1.0,
				Baseline:  bl.MeanErrorRate,
				Deviation: 1.0 / math.Max(bl.MeanErrorRate, 0.01),
			}); alert != nil {
				alerts = append(alerts, *alert)
			}
		}
	}

	// 6. Signal spike
	if signalCount > 0 && bl.MeanSignalsPerHour > 0 {
		if float64(signalCount) > bl.MeanSignalsPerHour*d.threshold {
			if alert := d.fireIfCooled("signal_spike:"+agentID+":"+tool, Alert{
				Type:      "signal_spike",
				Severity:  "critical",
				AgentID:   agentID,
				Tool:      tool,
				Message:   fmt.Sprintf("Signal spike for %s on %s: %d signals (baseline: %.1f/hr)", agentID, tool, signalCount, bl.MeanSignalsPerHour),
				Current:   float64(signalCount),
				Baseline:  bl.MeanSignalsPerHour,
				Deviation: float64(signalCount) / bl.MeanSignalsPerHour,
			}); alert != nil {
				alerts = append(alerts, *alert)
			}
		}
	}

	return alerts
}

// Recent returns the most recent alerts.
func (d *Detector) Recent() []Alert {
	d.mu.RLock()
	defer d.mu.RUnlock()
	result := make([]Alert, len(d.recent))
	copy(result, d.recent)
	return result
}

func (d *Detector) fireIfCooled(key string, alert Alert) *Alert {
	d.mu.Lock()
	defer d.mu.Unlock()

	if lastFired, ok := d.cooldowns[key]; ok {
		if time.Since(lastFired) < d.cooldown {
			return nil // in cooldown
		}
	}

	alert.ID = generateAlertID()
	alert.Timestamp = time.Now()
	d.cooldowns[key] = time.Now()

	// Add to recent
	if len(d.recent) >= d.maxRecent {
		d.recent = d.recent[1:]
	}
	d.recent = append(d.recent, alert)

	return &alert
}

func countActiveHours(bl *baseline.Baseline) int {
	count := 0
	for _, active := range bl.ActiveHours {
		if active {
			count++
		}
	}
	return count
}

func generateAlertID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "alert-" + hex.EncodeToString(b)
}
