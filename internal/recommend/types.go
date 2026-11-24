package recommend

import (
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// Recommendation is a suggested action based on observed behavior.
type Recommendation struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"` // unprotected_critical_tool, pii_exposure, destructive_sql, suspicious_sequence, rate_limit
	Severity      string            `json:"severity"`
	Title         string            `json:"title"`
	Description   string            `json:"description"`
	Evidence      string            `json:"evidence"`
	SuggestedRule config.RuleConfig `json:"suggestedRule"`
	ImpactScore   float64           `json:"impactScore"`
	Source        string            `json:"source"`   // deterministic, llm
	Status        string            `json:"status"`   // active, applied, dismissed
	ActionMode    string            `json:"actionMode"` // "rule" (default, show action buttons) or "monitor" (alert/observe only)
	CreatedAt     time.Time         `json:"createdAt"`
	ResolvedAt    time.Time         `json:"resolvedAt,omitempty"`
}

// ImpactComponents breaks down the impact score.
type ImpactComponents struct {
	RiskLevel    float64 `json:"riskLevel"`
	CallVolume   float64 `json:"callVolume"`
	PIIExposure  float64 `json:"piiExposure"`
	ErrorRate    float64 `json:"errorRate"`
	AgentBreadth float64 `json:"agentBreadth"`
}

// SignalSummary provides signal counts by category for the recommendation engine.
type SignalSummary struct {
	Category string
	Tool     string
	Count    int
}
