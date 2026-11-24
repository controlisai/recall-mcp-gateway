package platform

import "time"

// AgentHealth holds agent compliance and trust information from the platform.
type AgentHealth struct {
	AgentID         string  `json:"agentId"`
	TrustLevel      string  `json:"trustLevel"`
	ComplianceScore int     `json:"complianceScore"`
	ComplianceBand  string  `json:"complianceBand"`
	ErrorRate       float64 `json:"errorRate,omitempty"`
}

// DriftEvent is a drift detection alert from the engine.
type DriftEvent struct {
	AgentID   string    `json:"agentId"`
	Dimension string    `json:"dimension"` // intent, temporal, error, volume
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	DetectedAt time.Time `json:"detectedAt"`
}

// OrgSummary holds per-org decision statistics.
type OrgSummary struct {
	OrgID        string `json:"orgId"`
	OrgName      string `json:"orgName,omitempty"`
	TotalCalls   int    `json:"totalCalls"`
	BlockedCalls int    `json:"blockedCalls"`
	ApprovalRate float64 `json:"approvalRate"`
}

// PolicySummary is a read-only view of an engine policy.
type PolicySummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	RuleCount int   `json:"ruleCount"`
	Status    string `json:"status"`
}

// PlatformSummary aggregates all platform data for the dashboard.
type PlatformSummary struct {
	Connected   bool            `json:"connected"`
	Agents      []AgentHealth   `json:"agents,omitempty"`
	DriftEvents []DriftEvent    `json:"driftEvents,omitempty"`
	Orgs        []OrgSummary    `json:"orgs,omitempty"`
	Policies    []PolicySummary `json:"policies,omitempty"`
	ConsoleURL  string          `json:"consoleUrl,omitempty"`
}

// ProductSummary is a product from the platform.
type ProductSummary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// AgentRegistration is the payload for registering an agent in the platform.
type AgentRegistration struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	TrustLevel string   `json:"trustLevel"`
	AgentType  string   `json:"agentType"`
	RateLimit  int      `json:"rateLimit"`
	Scope      []string `json:"scope,omitempty"`
}

// IntentRegistration is the payload for registering an intent (tool) in the platform.
type IntentRegistration struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	RiskClassification   string `json:"riskClassification"`
	DataSensitivity      string `json:"dataSensitivity"`
	AuthorityRequirement string `json:"authorityRequirement"`
	BlastRadius          string `json:"blastRadius"`
	Reversible           bool   `json:"reversible"`
	Category             string `json:"category,omitempty"`
}

// PolicyRuleRegistration is a single rule in a policy.
type PolicyRuleRegistration struct {
	Priority   int               `json:"priority"`
	Conditions map[string]any    `json:"conditions"`
	Decision   string            `json:"decision"`
	Reason     string            `json:"reason,omitempty"`
}

// PolicyRegistration is the payload for creating a policy in the platform.
type PolicyRegistration struct {
	Name        string                   `json:"name"`
	Description string                   `json:"description,omitempty"`
	Rules       []PolicyRuleRegistration `json:"rules"`
}

// BaselineSummary is an exported baseline for migration.
type BaselineSummary struct {
	AgentID          string    `json:"agentId"`
	Tool             string    `json:"tool"`
	MeanCallsPerHour float64   `json:"meanCallsPerHour"`
	ActiveHours      []int     `json:"activeHours,omitempty"`
	SampleCount      int       `json:"sampleCount"`
	BaselineReady    bool      `json:"baselineReady"`
}

// ServerHealthSummary is an exported server health snapshot.
type ServerHealthSummary struct {
	Name          string  `json:"name"`
	MeanLatencyMs float64 `json:"meanLatencyMs"`
	Status        string  `json:"status"`
}

// MigrationSummary is the payload for sending proxy observation data to the platform.
type MigrationSummary struct {
	SyncedAt        string                `json:"syncedAt"`
	ProxyVersion    string                `json:"proxyVersion"`
	AgentCount      int                   `json:"agents"`
	ToolCount       int                   `json:"tools"`
	RuleCount       int                   `json:"rules"`
	ObservationDays int                   `json:"observationDays"`
	Baselines       []BaselineSummary     `json:"baselines,omitempty"`
	ServerHealth    []ServerHealthSummary  `json:"serverHealth,omitempty"`
}
