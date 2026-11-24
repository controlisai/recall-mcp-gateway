package audit

import "time"

// Entry represents a single audited tool call.
type Entry struct {
	Timestamp      time.Time      `json:"timestamp"`
	RequestID      string         `json:"requestId"`
	AgentID        string         `json:"agentId"`
	UserID          string         `json:"userId,omitempty"`         // human user identity from JWT sub (empty for M2M/anonymous)
	ActingFor       string         `json:"actingFor,omitempty"`      // delegation: who the agent is acting for (JWT act.sub)
	DelegationDepth int            `json:"delegationDepth,omitempty"` // delegation chain depth (0 = direct, 1+ = delegated)
	OrgID           string         `json:"orgId,omitempty"`
	Tool           string         `json:"tool"`
	Server         string         `json:"server"`
	Decision       string         `json:"decision"`
	DecisionReason string         `json:"decisionReason,omitempty"`
	MatchedRule       string         `json:"matchedRule,omitempty"`       // tool pattern of the matched rule
	MatchedRuleIndex  int            `json:"matchedRuleIndex"`            // index of matched rule (-1 = no match)
	MatchedRuleAction string         `json:"matchedRuleAction,omitempty"` // action of the matched rule
	LatencyMs      int64          `json:"latencyMs"`
	Arguments      map[string]any `json:"arguments,omitempty"`
	ResultPreview  string         `json:"result_preview,omitempty"`
	IsError        bool           `json:"isError,omitempty"`
	ErrorMessage   string         `json:"errorMessage,omitempty"`
	ErrorCategory  string         `json:"errorCategory,omitempty"` // session_error, auth_error, timeout, network, circuit_open, application_error
	Mode              string         `json:"mode"` // passthrough, rule, engine
	CostPerCall       float64        `json:"costPerCall,omitempty"`
	EngineDecisionID  string         `json:"engineDecisionId,omitempty"` // links to platform audit
	Signals           []SignalEntry  `json:"signals,omitempty"`
	IdentitySource    string         `json:"identitySource,omitempty"`    // bearer-token, meta-client-info, initialize-session, anonymous
	ProtocolEra       string         `json:"protocolEra,omitempty"`       // modern, legacy
	Archetype         string         `json:"archetype,omitempty"`         // autonomous, user_embedded, delegated
	TraceID           string         `json:"traceId,omitempty"`           // W3C trace ID from OTel context (links audit to distributed traces)
	Hash              string         `json:"hash,omitempty"`              // SHA-256 chain hash for tamper detection
	ConsentPhase      string         `json:"consentPhase,omitempty"`      // requested (elicitation sent), granted (user accepted), denied (user declined)
}

// SignalEntry is a signal attached to an audit entry.
type SignalEntry struct {
	Name     string `json:"name"`
	Category string `json:"category"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
