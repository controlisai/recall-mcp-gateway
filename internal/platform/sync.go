package platform

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/sanitize"
)

// SyncResult tracks the outcome of a sync operation.
type SyncResult struct {
	Created int `json:"created"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// SyncAgents registers proxy agents in the platform's agent registry.
func SyncAgents(ctx context.Context, client *Client, productID string, agents []config.AgentConfig) SyncResult {
	var result SyncResult
	for _, agent := range agents {
		if agent.ID == "" || agent.ID == "anonymous" {
			result.Skipped++
			continue
		}

		reg := AgentRegistration{
			ID:         agent.ID,
			Name:       agent.ID,
			TrustLevel: "LOW",
			AgentType:  "FULLY_AUTONOMOUS",
			RateLimit:  100,
			Scope:      agent.AllowedServers,
		}

		err := client.RegisterAgent(ctx, productID, reg)
		if err != nil {
			slog.Error("agent registration failed", "component", "sync", "agent_id", agent.ID, "error", sanitize.Error(err.Error()))
			result.Errors++
		} else {
			result.Created++
		}
	}
	slog.Info("agents synced", "component", "sync", "created", result.Created, "skipped", result.Skipped, "errors", result.Errors)
	return result
}

// SyncIntents registers proxy tool profiles as intents in the platform.
// Uses risk mapping tables to convert proxy classifications to platform equivalents.
// Rules are checked to infer authority requirements per tool.
func SyncIntents(ctx context.Context, client *Client, productID string, profiles []*detect.ToolProfile, rules []config.RuleConfig) SyncResult {
	// Build rule lookup: tool name → most restrictive action
	toolActions := buildToolActionMap(rules)

	var result SyncResult
	for _, profile := range profiles {
		if profile.Name == "" {
			result.Skipped++
			continue
		}

		reg := IntentRegistration{
			ID:                   sanitizeID(profile.Name),
			Name:                 profile.Name,
			RiskClassification:   mapRiskClassification(profile.RiskLevel),
			DataSensitivity:      mapDataSensitivity(profile.Classification),
			AuthorityRequirement: inferAuthorityRequirement(profile.Name, toolActions),
			BlastRadius:          mapBlastRadius(profile.RiskLevel),
			Reversible:           true,
			Category:             mapCategory(profile.Classification),
		}

		err := client.RegisterIntent(ctx, productID, reg)
		if err != nil {
			slog.Error("intent registration failed", "component", "sync", "intent", profile.Name, "error", sanitize.Error(err.Error()))
			result.Errors++
		} else {
			result.Created++
		}
	}
	slog.Info("intents synced", "component", "sync", "created", result.Created, "skipped", result.Skipped, "errors", result.Errors)
	return result
}

// SyncPolicy converts proxy rules into a platform admissibility policy.
// Creates a single policy with all rules as priority-ordered entries.
func SyncPolicy(ctx context.Context, client *Client, productID string, rules []config.RuleConfig) SyncResult {
	var result SyncResult

	// Filter syncable rules (skip hide — proxy-local concept)
	var policyRules []PolicyRuleRegistration
	priority := 1
	for _, r := range rules {
		if r.Tool == "" || r.Action == "hide" {
			result.Skipped++
			continue
		}

		decision := mapRuleAction(r.Action)
		if decision == "" {
			result.Skipped++
			continue
		}

		conditions := map[string]any{
			"intent:name": map[string]string{"glob": r.Tool},
		}

		// Agent-scoped rules add agent condition
		if len(r.Agents) > 0 {
			if len(r.Agents) == 1 {
				conditions["agent:id"] = r.Agents[0]
			} else {
				conditions["agent:id"] = map[string]any{"in": r.Agents}
			}
		}

		// Classification-scoped rules
		if r.Classification != "" {
			conditions["intent:riskClassification"] = mapRiskClassification(classificationToRiskLevel(r.Classification))
		}

		policyRules = append(policyRules, PolicyRuleRegistration{
			Priority:   priority,
			Conditions: conditions,
			Decision:   decision,
			Reason:     r.Reason,
		})
		priority++
	}

	if len(policyRules) == 0 {
		slog.Info("policy sync skipped: no syncable rules", "component", "sync")
		return result
	}

	policy := PolicyRegistration{
		Name:        fmt.Sprintf("Gateway Migration Rules (%s)", time.Now().Format("2006-01-02")),
		Description: "Auto-imported from Recall MCP Gateway standalone rules",
		Rules:       policyRules,
	}

	err := client.CreatePolicy(ctx, productID, policy)
	if err != nil {
		slog.Error("policy creation failed", "component", "sync", "error", sanitize.Error(err.Error()))
		result.Errors++
	} else {
		result.Created = len(policyRules)
	}

	slog.Info("policy synced", "component", "sync", "rules_synced", result.Created, "skipped", result.Skipped, "errors", result.Errors)
	return result
}

// SyncBaselines exports proxy baseline data to the platform as a migration summary.
func SyncBaselines(ctx context.Context, client *Client, productID, proxyVersion string, ruleCount int, baselines map[string]*baseline.Baseline, serverHealth map[string]baseline.ServerHealth) SyncResult {
	var result SyncResult

	// Convert baselines
	var blSummaries []BaselineSummary
	var oldestCreated time.Time
	for key, bl := range baselines {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		agentID, tool := parts[0], parts[1]

		// Extract active hours as int array
		var activeHours []int
		for h, active := range bl.ActiveHours {
			if active {
				activeHours = append(activeHours, h)
			}
		}

		blSummaries = append(blSummaries, BaselineSummary{
			AgentID:          agentID,
			Tool:             tool,
			MeanCallsPerHour: math.Round(bl.MeanCallsPerHour*100) / 100,
			ActiveHours:      activeHours,
			SampleCount:      bl.SampleCount,
			BaselineReady:    bl.BaselineReady,
		})

		if oldestCreated.IsZero() || bl.CreatedAt.Before(oldestCreated) {
			oldestCreated = bl.CreatedAt
		}
	}

	// Convert server health
	var shSummaries []ServerHealthSummary
	for _, sh := range serverHealth {
		shSummaries = append(shSummaries, ServerHealthSummary{
			Name:          sh.Name,
			MeanLatencyMs: math.Round(sh.MeanLatencyMs*100) / 100,
			Status:        sh.Status,
		})
	}

	// Calculate observation days
	observationDays := 0
	if !oldestCreated.IsZero() {
		observationDays = int(time.Since(oldestCreated).Hours() / 24)
	}

	// Count unique agents and tools
	agentSet := make(map[string]bool)
	toolSet := make(map[string]bool)
	for key := range baselines {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			agentSet[parts[0]] = true
			toolSet[parts[1]] = true
		}
	}

	summary := MigrationSummary{
		SyncedAt:        time.Now().UTC().Format(time.RFC3339),
		ProxyVersion:    proxyVersion,
		AgentCount:      len(agentSet),
		ToolCount:       len(toolSet),
		RuleCount:       ruleCount,
		ObservationDays: observationDays,
		Baselines:       blSummaries,
		ServerHealth:    shSummaries,
	}

	err := client.PostMigrationSummary(ctx, productID, summary)
	if err != nil {
		slog.Error("baseline migration export failed", "component", "sync", "error", sanitize.Error(err.Error()))
		result.Errors++
	} else {
		result.Created = len(blSummaries)
	}

	slog.Info("baselines exported", "component", "sync", "baselines", len(blSummaries), "server_health_records", len(shSummaries))
	return result
}

// --- Rule Action Mapping ---

// mapRuleAction converts proxy rule action to platform decision.
func mapRuleAction(action string) string {
	switch action {
	case "block":
		return "NO_EXECUTION"
	case "require_approval":
		return "PENDING_AUTHORIZATION"
	case "allow":
		return "EXECUTE"
	default:
		return "" // rate_limit, hide — not directly mappable to a decision
	}
}

// classificationToRiskLevel converts proxy tool classification to a risk level for conditions.
func classificationToRiskLevel(classification string) string {
	switch classification {
	case "DESTRUCTIVE":
		return "CRITICAL"
	case "CREDENTIAL", "CODE_EXECUTION":
		return "HIGH"
	case "WRITE", "DATABASE", "EXTERNAL_COMM":
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// --- Risk Mapping ---

// mapRiskClassification converts proxy riskLevel to platform riskClassification.
// CRITICAL → DESTRUCTIVE, HIGH → SENSITIVE, MEDIUM/LOW → LOW
func mapRiskClassification(riskLevel string) string {
	switch riskLevel {
	case "CRITICAL":
		return "DESTRUCTIVE"
	case "HIGH":
		return "SENSITIVE"
	default:
		return "LOW"
	}
}

// mapBlastRadius converts proxy riskLevel to platform blastRadius.
func mapBlastRadius(riskLevel string) string {
	switch riskLevel {
	case "CRITICAL":
		return "CRITICAL"
	case "HIGH":
		return "HIGH"
	case "MEDIUM":
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// mapDataSensitivity converts proxy classification to platform dataSensitivity.
// CREDENTIAL/DATABASE → PII, everything else → MINIMAL
func mapDataSensitivity(classification string) string {
	switch classification {
	case "CREDENTIAL", "DATABASE":
		return "PII"
	default:
		return "MINIMAL"
	}
}

// mapCategory converts proxy classification to a human-readable intent category.
func mapCategory(classification string) string {
	switch classification {
	case "READ_ONLY":
		return "rag"
	case "WRITE", "DESTRUCTIVE":
		return "generative"
	case "EXTERNAL_COMM":
		return "conversational"
	case "DATABASE":
		return "extraction"
	default:
		return ""
	}
}

// inferAuthorityRequirement checks if any proxy rule requires approval or blocks this tool.
func inferAuthorityRequirement(toolName string, toolActions map[string]string) string {
	action, ok := toolActions[toolName]
	if !ok {
		return "NONE"
	}
	switch action {
	case "require_approval":
		return "ALWAYS"
	case "block":
		return "CONDITIONAL"
	default:
		return "NONE"
	}
}

// buildToolActionMap builds a map of tool name → most restrictive action from rules.
// Handles glob patterns by expanding them against known tool names (not done here —
// only exact matches and simple * prefix/suffix).
func buildToolActionMap(rules []config.RuleConfig) map[string]string {
	result := make(map[string]string)
	for _, r := range rules {
		if r.Tool == "" || r.Tool == "*" {
			continue // skip wildcard-all rules for per-tool mapping
		}
		existing, ok := result[r.Tool]
		if !ok || actionPriority(r.Action) > actionPriority(existing) {
			result[r.Tool] = r.Action
		}
	}
	return result
}

func actionPriority(action string) int {
	switch action {
	case "block":
		return 3
	case "require_approval":
		return 2
	case "rate_limit":
		return 1
	default:
		return 0
	}
}

// sanitizeID converts a tool name to a valid platform intent ID.
// Replaces non-alphanumeric characters with hyphens, lowercases.
func sanitizeID(name string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
