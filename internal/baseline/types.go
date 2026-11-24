package baseline

import "time"

// Baseline holds learned behavioral patterns for an (agent, tool) pair.
type Baseline struct {
	// Rate baseline (EWMA)
	MeanCallsPerHour   float64   `json:"meanCallsPerHour"`
	StdDevCallsPerHour float64   `json:"stdDevCallsPerHour"`
	LastUpdateAt       time.Time `json:"lastUpdateAt"`

	// Argument pattern baseline
	KnownArgFields map[string]bool     `json:"knownArgFields"`
	KnownArgValues map[string][]string `json:"knownArgValues"` // top 20 values per field

	// Timing baseline
	ActiveHours [24]bool `json:"activeHours"`
	ActiveDays  [7]bool  `json:"activeDays"`

	// Error baseline
	MeanErrorRate float64 `json:"meanErrorRate"`

	// Signal baseline
	MeanSignalsPerHour float64 `json:"meanSignalsPerHour"`

	// Status
	SampleCount    int       `json:"sampleCount"`
	BaselineReady  bool      `json:"baselineReady"`
	BaselineSource string    `json:"baselineSource,omitempty"` // "default" (from profile) or "learned" (from real data)
	CreatedAt      time.Time `json:"createdAt"`
	LastSeen       time.Time `json:"lastSeen"`
}

// ServerHealth tracks latency and error rates for an upstream MCP server.
type ServerHealth struct {
	Name             string  `json:"name"`
	MeanLatencyMs    float64 `json:"meanLatencyMs"`
	CurrentLatencyMs float64 `json:"currentLatencyMs"`
	ErrorRate        float64 `json:"errorRate"`
	Status           string  `json:"status"` // healthy, degraded, down
	SampleCount      int     `json:"sampleCount"`
}

const (
	// AlphaResponsive is EWMA alpha during first 24h (responsive to new data).
	AlphaResponsive = 0.3
	// AlphaStable is EWMA alpha after 24h (stable, drift-resistant).
	AlphaStable = 0.1

	// MinSamplesForBaseline is the minimum observations before baseline is ready.
	MinSamplesForBaseline = 50
	// MinHoursForBaseline is the minimum hours of observation.
	MinHoursForBaseline = 24

	// DeviationThreshold is the default z-score threshold for anomaly detection.
	DeviationThreshold = 3.0

	// MaxKnownValues is the max tracked unique values per argument field.
	MaxKnownValues = 20
)
