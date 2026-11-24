package detect

import "time"

// Signal represents something detected during a tool call — risk, anomaly, or pattern match.
type Signal struct {
	Name     string `json:"name"`
	Category string `json:"category"` // pii, credential, destructive, sensitive_path, sequence, workflow
	Severity string `json:"severity"` // info, warning, high, critical
	Message  string `json:"message"`
	Detail   string `json:"detail,omitempty"`
}

// ToolProfile holds the auto-classification of a tool from its schema.
type ToolProfile struct {
	Name           string   `json:"name"`
	Server         string   `json:"server"`
	Classification string   `json:"classification"` // READ_ONLY, WRITE, DESTRUCTIVE, EXTERNAL_COMM, CODE_EXECUTION, DATA_TRANSFER
	RiskLevel      string   `json:"riskLevel"`      // LOW, MEDIUM, HIGH, CRITICAL
	RiskReasons    []string `json:"riskReasons"`
	InputRisk      string   `json:"inputRisk"` // BOUNDED or UNBOUNDED
}

// HistoryEntry is a minimal record stored in the per-agent call history buffer.
type HistoryEntry struct {
	Tool      string         `json:"tool"`
	Server    string         `json:"server"`
	Arguments map[string]any `json:"arguments,omitempty"`
	IsError   bool           `json:"isError"`
	Timestamp time.Time      `json:"timestamp"`
	Signals   []Signal       `json:"signals,omitempty"`
}

// WorkflowPattern is a discovered recurring tool call sequence.
type WorkflowPattern struct {
	Steps     []string      `json:"steps"`
	Count     int           `json:"count"`
	Frequency float64       `json:"frequency"` // 0.0 to 1.0
	AvgMs     int64         `json:"avgMs"`
}

// ArgumentDiff shows what changed between two calls to the same tool.
type ArgumentDiff struct {
	Tool    string                     `json:"tool"`
	Added   map[string]any             `json:"added,omitempty"`
	Removed map[string]any             `json:"removed,omitempty"`
	Changed map[string]ArgumentChange  `json:"changed,omitempty"`
}

// ArgumentChange records a before/after value for a changed argument.
type ArgumentChange struct {
	Before any `json:"before"`
	After  any `json:"after"`
}
