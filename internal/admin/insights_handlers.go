package admin

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/review"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

// AgentProvider returns current agent configs for dynamic ops readiness checks.
type AgentProvider func() []config.AgentConfig

// InsightsDeps holds dependencies for the insights endpoint.
type InsightsDeps struct {
	Tracker       *baseline.Tracker
	Profiler      *detect.Profiler
	RuleEngine    *rules.Engine
	Auditor       audit.AuditStore
	ReviewStore   *review.Store
	// Operational readiness inputs
	AgentProvider   AgentProvider // dynamic agent config provider (includes dashboard + persisted agents)
	AuditPersistent bool         // true if audit uses sqlite/jsonl (not memory)
	MetricsEnabled  bool         // true if Prometheus /metrics is enabled
	TracingEnabled  bool         // true if OTel tracing is configured
}

// InsightsHandler serves GET /admin/v1/insights — upgrade-relevant insights.
func InsightsHandler(deps InsightsDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		var insights []map[string]any

		ruleCount := deps.RuleEngine.RuleCount()
		recent := deps.Auditor.Recent()

		// Don't show insights until we have enough data
		if len(recent) < 5 {
			json.NewEncoder(w).Encode(map[string]any{
				"insights": insights, "count": 0,
				"compliancePreview": map[string]any{
					"source": "proxy-preview",
					"note":   "Insufficient data. At least 5 tool calls needed for scoring.",
				},
			})
			return
		}

		// Count distinct agents
		agentSet := make(map[string]bool)
		blockedCount := 0
		signalCount := 0
		for _, e := range recent {
			agentSet[e.AgentID] = true
			if e.Decision == "block" || e.Decision == "rate_limited" {
				blockedCount++
			}
			signalCount += len(e.Signals)
		}
		agentCount := len(agentSet)

		// Insight: Multiple agents → governance profiles
		if agentCount > 1 {
			insights = append(insights, map[string]any{
				"type":        "multi_agent",
				"title":       "Multiple agents detected",
				"description": "You have " + itoa(agentCount) + " agents with different usage patterns. Platform governance profiles let you assign different policies per agent and per customer org.",
				"severity":    "info",
				"cta":         "Learn about governance profiles →",
			})
		}

		// Insight: Complex rules → policy priority ordering
		if ruleCount > 5 {
			insights = append(insights, map[string]any{
				"type":        "complex_rules",
				"title":       "Rule set growing (" + itoa(ruleCount) + " rules)",
				"description": "Local rules use first-match-wins with no priority ordering. Platform policies support priority levels, conditions, and conflict resolution.",
				"severity":    "info",
				"cta":         "Learn about platform policies →",
			})
		}

		// Insight: Blocking activity → compliance scoring
		if blockedCount > 10 {
			insights = append(insights, map[string]any{
				"type":        "compliance",
				"title":       "Active governance (" + itoa(blockedCount) + " blocked calls)",
				"description": "You're actively governing agent behavior. Platform compliance scoring (0-1000) tracks agent health over time with EWMA scoring and progressive autonomy.",
				"severity":    "info",
				"cta":         "Learn about compliance scoring →",
			})
		}

		// Insight: Signals detected → audit exports
		if signalCount > 5 {
			insights = append(insights, map[string]any{
				"type":        "audit",
				"title":       itoa(signalCount) + " signals detected",
				"description": "Platform provides full audit trail with compliance exports (PDF, CSV) for regulators. Every decision is logged with context, matched rules, and outcome.",
				"severity":    "info",
				"cta":         "Learn about compliance exports →",
			})
		}

		// Insight: Baselines ready → drift detection
		if deps.Tracker != nil {
			baselines := deps.Tracker.All()
			readyCount := 0
			for _, bl := range baselines {
				if bl.BaselineReady {
					readyCount++
				}
			}
			if readyCount > 3 {
				insights = append(insights, map[string]any{
					"type":        "drift",
					"title":       itoa(readyCount) + " baselines ready",
					"description": "Platform drift detection uses baselines to auto-detect behavioral anomalies and trigger adaptive governance responses.",
					"severity":    "info",
					"cta":         "Learn about drift detection →",
				})
			}
		}

		// === Compliance Score (3 GRC-aligned components, zero overlap) ===

		// Component 1: Tool Governance (400 pts) — % of HIGH/CRITICAL tools governed
		// A tool is "governed" when ALL agents with access to its server have coverage via:
		//   - Rule (broad or agent-scoped) covering the tool (any action: allow, block, require_approval, rate_limit)
		//   - blockedTools (tool blocked for the agent)
		//   - acknowledgedTools (risk explicitly accepted for the agent)
		// hiddenTools do NOT count as governance.
		// Per-agent, per-server — not global.
		totalHighCritical := 0
		governedHighCritical := 0

		// Per-route counters for frontend breakdown
		governedByRule := 0
		governedByBlocked := 0
		governedByAcknowledged := 0

		// Per-tool detail for frontend (tool name → coverage info)
		type toolGovDetail struct {
			Name           string `json:"name"`
			Server         string `json:"server"`
			RiskLevel      string `json:"riskLevel"`
			Governed       bool   `json:"governed"`
			RelevantAgents int    `json:"relevantAgents"`
			CoveredAgents  int    `json:"coveredAgents"`
			DominantRoute  string `json:"dominantRoute"` // "rule", "blocked", "acknowledged", or "" if ungoverned
		}
		var toolDetails []toolGovDetail

		// Build agent list for coverage checks (exclude pseudo-agents)
		var govAgents []config.AgentConfig
		if deps.AgentProvider != nil {
			for _, a := range deps.AgentProvider() {
				if a.ID != "system" && a.ID != "unauthorized" && a.ID != "anonymous" {
					govAgents = append(govAgents, a)
				}
			}
		}

		// Helper: does agent have coverage for a tool? Uses rules.MatchToolGlob for correct glob matching.
		agentHasCoverage := func(a config.AgentConfig, toolName string) (bool, string) {
			// Check rules (any action: allow, block, require_approval, rate_limit)
			for _, r := range deps.RuleEngine.Rules() {
				if r.Tool == "*" {
					continue
				}
				if !rules.MatchToolGlob(r.Tool, toolName) {
					continue
				}
				// Broad rule (no agent scope) or agent-scoped rule matching this agent
				if len(r.Agents) == 0 || containsStr(r.Agents, a.ID) {
					return true, "rule"
				}
			}
			// Check blockedTools
			if containsStr(a.BlockedTools, toolName) {
				return true, "blocked"
			}
			// Check acknowledgedTools
			if containsStr(a.AcknowledgedTools, toolName) {
				return true, "acknowledged"
			}
			return false, ""
		}

		if deps.Profiler != nil {
			for _, p := range deps.Profiler.AllProfiles() {
				if p.RiskLevel != "HIGH" && p.RiskLevel != "CRITICAL" {
					continue
				}
				totalHighCritical++

				// Find agents with access to this tool's server
				var relevantAgents []config.AgentConfig
				for _, a := range govAgents {
					if a.HasServerAccess(p.Server) {
						relevantAgents = append(relevantAgents, a)
					}
				}

				detail := toolGovDetail{
					Name:           p.Name,
					Server:         p.Server,
					RiskLevel:      p.RiskLevel,
					RelevantAgents: len(relevantAgents),
				}

				// No agents with access = tool is governed (no one can call it ungoverned)
				if len(relevantAgents) == 0 {
					governedHighCritical++
					governedByRule++
					detail.Governed = true
					detail.DominantRoute = "rule"
					toolDetails = append(toolDetails, detail)
					continue
				}

				// Check per-agent coverage
				allCovered := true
				routeCounts := map[string]int{"rule": 0, "blocked": 0, "acknowledged": 0}
				coveredCount := 0
				for _, a := range relevantAgents {
					covered, route := agentHasCoverage(a, p.Name)
					if covered {
						routeCounts[route]++
						coveredCount++
					} else {
						allCovered = false
					}
				}

				detail.CoveredAgents = coveredCount
				if allCovered {
					governedHighCritical++
					detail.Governed = true
					// Classify by dominant route
					if routeCounts["rule"] >= routeCounts["blocked"] && routeCounts["rule"] >= routeCounts["acknowledged"] {
						governedByRule++
						detail.DominantRoute = "rule"
					} else if routeCounts["blocked"] >= routeCounts["acknowledged"] {
						governedByBlocked++
						detail.DominantRoute = "blocked"
					} else {
						governedByAcknowledged++
						detail.DominantRoute = "acknowledged"
					}
				}
				toolDetails = append(toolDetails, detail)
			}
		}
		toolGovScore := 0
		if totalHighCritical > 0 {
			toolGovScore = int(float64(governedHighCritical) / float64(totalHighCritical) * 400)
		}

		// Component 2: Signal Safety (300 pts) — adjusted signal rate (excluding acknowledged)
		signalSafetyScore := 0
		if len(recent) > 0 {
			adjustedSignals := 0
			for _, e := range recent {
				for _, sig := range e.Signals {
					if deps.ReviewStore != nil && deps.ReviewStore.IsSignalAcknowledged(e.Tool, sig.Category) {
						continue // acknowledged signal — don't count
					}
					adjustedSignals++
				}
			}
			adjustedRate := float64(adjustedSignals) / float64(len(recent))
			signalSafetyScore = int((1.0 - min(adjustedRate, 1.0)) * 300)
		}
		// No data = 0 (not inflated)

		// Component 3: Operational Readiness (300 pts)
		// Required items (75 pts each, 4 items = 300 max):
		//   baselines, audit persistence, agent access scoped, observability
		// Optional items (0 pts, informational only):
		//   notifications, alert rules
		opsScore := 0
		opsChecklist := map[string]bool{}

		// Required 1: Baselines active (≥50% of tool pairs have baselines started)
		if deps.Tracker != nil {
			all := deps.Tracker.All()
			ready := 0
			for _, bl := range all {
				if bl.BaselineReady || bl.SampleCount > 0 {
					ready++
				}
			}
			opsChecklist["baselines_active"] = len(all) > 0 && float64(ready)/float64(len(all)) >= 0.5
		}
		// Required 2: Audit persistence (not in-memory)
		opsChecklist["audit_persistent"] = deps.AuditPersistent
		// Required 3: Agent access scoped (any agent with allowedServers or blockedTools)
		agentScoped := false
		if deps.AgentProvider != nil {
			for _, a := range deps.AgentProvider() {
				if a.ID == "system" || a.ID == "unauthorized" || a.ID == "anonymous" {
					continue
				}
				if len(a.AllowedServers) > 0 || len(a.BlockedTools) > 0 {
					agentScoped = true
					break
				}
			}
		}
		opsChecklist["agent_scoped"] = agentScoped
		// Required 4: Observability (tracing explicitly enabled, not just endpoint present)
		opsChecklist["observability"] = deps.TracingEnabled

		// Score from required items only (75 pts each)
		requiredKeys := []string{"baselines_active", "audit_persistent", "agent_scoped", "observability"}
		for _, k := range requiredKeys {
			if opsChecklist[k] {
				opsScore += 75
			}
		}

		// Optional items (informational, no score impact)
		for _, r := range deps.RuleEngine.Rules() {
			if r.Notify != "" || r.StatusWebhook != "" {
				opsChecklist["notifications_configured"] = true
				break
			}
		}
		alertsEnabled := false
		for _, r := range deps.RuleEngine.Rules() {
			if r.Notify != "" {
				alertsEnabled = true
				break
			}
		}
		opsChecklist["alerts_enabled"] = alertsEnabled

		score := toolGovScore + signalSafetyScore + opsScore
		if score > 1000 { score = 1000 }

		band := "CRITICAL"
		if score > 800 {
			band = "HEALTHY"
		} else if score > 500 {
			band = "DEGRADED"
		} else if score > 200 {
			band = "AT_RISK"
		}

		// Persist daily compliance score and fetch trend
		var scoreTrend []audit.DailyScore
		if sqlStore, ok := deps.Auditor.(*audit.SQLiteStore); ok {
			sqlStore.RecordComplianceScore(score, band)
			scoreTrend = sqlStore.ComplianceScoreTrend(7)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"insights":       insights,
			"count":          len(insights),
			"compliancePreview": map[string]any{
				"score":  score,
				"band":   band,
				"source": "proxy-preview",
				"note":   "GRC-aligned compliance score. Connect to Recall Platform for per-agent scoring.",
				"components": map[string]any{
					"toolGovernance":     toolGovScore,
					"signalSafety":       signalSafetyScore,
					"operationalReadiness": opsScore,
				},
				"scoreTrend": scoreTrend,
				"details": map[string]any{
					"totalHighCritical":      totalHighCritical,
					"governedHighCritical":   governedHighCritical,
					"governedByRule":         governedByRule,
					"governedByBlocked":      governedByBlocked,
					"governedByAcknowledged": governedByAcknowledged,
					"totalAgents":            len(govAgents),
					"toolDetails":            toolDetails,
					"opsChecklist":           opsChecklist,
				},
			},
		})
	}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
