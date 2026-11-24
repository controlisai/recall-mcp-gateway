package alerts

import "time"

// Alert represents a detected anomaly.
type Alert struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`      // rate_spike, new_tool, new_arg_pattern, off_hours, error_spike, signal_spike
	Severity  string    `json:"severity"`  // info, warning, critical
	AgentID   string    `json:"agentId"`
	Tool      string    `json:"tool"`
	Message   string    `json:"message"`
	Current   float64   `json:"current,omitempty"`
	Baseline  float64   `json:"baseline,omitempty"`
	Deviation float64   `json:"deviation,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// WebhookPayload is the JSON body sent to webhook URLs.
type WebhookPayload struct {
	Type         string       `json:"type"`
	Severity     string       `json:"severity"`
	AgentID      string       `json:"agentId"`
	Tool         string       `json:"tool"`
	Message      string       `json:"message"`
	Current      float64      `json:"current,omitempty"`
	Baseline     float64      `json:"baseline,omitempty"`
	Deviation    float64      `json:"deviation,omitempty"`
	DashboardURL string       `json:"dashboardUrl"`
	Timestamp    time.Time    `json:"timestamp"`
	RecentCalls  []RecentCall `json:"recentCalls,omitempty"`
}

// RecentCall is a summary of a recent tool call included in webhook payloads.
type RecentCall struct {
	Tool        string `json:"tool"`
	ArgsPreview string `json:"args_preview"`
	At          string `json:"at"`
}
