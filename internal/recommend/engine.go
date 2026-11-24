package recommend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

// AgentProvider returns current agent configs for access control checks.
type AgentProvider func() []config.AgentConfig

// Engine generates recommendations from observed behavior.
type Engine struct {
	mu             sync.RWMutex
	profiler       *detect.Profiler
	tracker        *baseline.Tracker
	ruleEngine     *rules.Engine
	agentProvider  AgentProvider
	recs           []Recommendation
	dismissed      map[string]time.Time // type -> dismissed at
	dismissTTL     time.Duration
	llm         *LLMRecommender
	generating  bool // true while Generate is running (blocks Apply/Dismiss)
}

// NewEngine creates a recommendation engine.
func NewEngine(profiler *detect.Profiler, tracker *baseline.Tracker, ruleEngine *rules.Engine) *Engine {
	return &Engine{
		profiler:   profiler,
		tracker:    tracker,
		ruleEngine: ruleEngine,
		dismissed:  make(map[string]time.Time),
		dismissTTL: 7 * 24 * time.Hour,
	}
}

// SetAgentProvider sets the function used to retrieve current agent configs.
func (e *Engine) SetAgentProvider(p AgentProvider) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.agentProvider = p
}

// toolCoverage holds per-agent protection analysis for a tool.
type toolCoverage struct {
	totalAgents      int
	protectedCount   int
	unprotectedCount int
}

// pseudoAgent returns true for internal proxy agent IDs.
func pseudoAgent(id string) bool {
	return id == "system" || id == "unauthorized" || id == "anonymous"
}

// getAgents returns configured agents excluding pseudo-agents.
func (e *Engine) getAgents() []config.AgentConfig {
	if e.agentProvider == nil {
		return nil
	}
	all := e.agentProvider()
	var real []config.AgentConfig
	for _, a := range all {
		if !pseudoAgent(a.ID) {
			real = append(real, a)
		}
	}
	return real
}

// ruleMatchesTool checks if a rule applies to a given tool, considering
// tool name pattern, classification scope, and server scope.
// Catch-all rules (tool="*") are always skipped.
func (e *Engine) ruleMatchesTool(r config.RuleConfig, toolName string) bool {
	if r.Tool == "*" {
		return false // catch-all doesn't count as targeted protection
	}
	if !matchesToolName(r.Tool, toolName) {
		return false
	}
	// Check classification scope: if rule has classification, tool must match
	if r.Classification != "" {
		profile := e.profiler.GetProfile(toolName)
		if profile == nil || !strings.EqualFold(profile.Classification, r.Classification) {
			return false
		}
	}
	// Check server scope: if rule has server, tool must be on that server
	if r.Server != "" {
		profile := e.profiler.GetProfile(toolName)
		if profile == nil || profile.Server != r.Server {
			return false
		}
	}
	return true
}

// computeToolCoverage checks how many agents are protected for a given tool.
// Only counts agents with access to the tool's server.
// Called after hasBroadRuleFor — so only checks agent-scoped rules, blockedTools,
// and acknowledgedTools.
func (e *Engine) computeToolCoverage(toolName, toolServer string, agents []config.AgentConfig, rules []config.RuleConfig) toolCoverage {
	if len(agents) == 0 {
		return toolCoverage{}
	}

	// Filter to agents with access to this tool's server
	var relevant []config.AgentConfig
	for _, a := range agents {
		if a.HasServerAccess(toolServer) {
			relevant = append(relevant, a)
		}
	}
	if len(relevant) == 0 {
		return toolCoverage{} // no agents can reach this server
	}

	// Build agent-scoped rule coverage map
	agentScopedRules := make(map[string]bool)
	for _, r := range rules {
		if len(r.Agents) == 0 {
			continue
		}
		if !e.ruleMatchesTool(r, toolName) {
			continue
		}
		for _, a := range r.Agents {
			agentScopedRules[a] = true
		}
	}

	protected := 0
	for _, a := range relevant {
		if agentScopedRules[a.ID] || containsTool(a.BlockedTools, toolName) || containsTool(a.AcknowledgedTools, toolName) {
			protected++
		}
	}

	return toolCoverage{
		totalAgents:      len(relevant),
		protectedCount:   protected,
		unprotectedCount: len(relevant) - protected,
	}
}

// hasBroadRuleFor returns true if a non-catch-all, non-agent-scoped rule covers this tool.
func (e *Engine) hasBroadRuleFor(toolName string, rules []config.RuleConfig) bool {
	for _, r := range rules {
		if len(r.Agents) > 0 {
			continue // agent-scoped
		}
		if e.ruleMatchesTool(r, toolName) {
			return true
		}
	}
	return false
}

func matchesToolName(pattern, name string) bool {
	if pattern == name {
		return true
	}
	if strings.Contains(pattern, "*") {
		re := "^" + strings.ReplaceAll(regexp.QuoteMeta(pattern), "\\*", ".*") + "$"
		matched, _ := regexp.MatchString(re, name)
		return matched
	}
	return false
}

func containsTool(tools []string, name string) bool {
	for _, t := range tools {
		if t == name {
			return true
		}
	}
	return false
}

// SetLLM attaches an optional LLM recommender for async analysis.
func (e *Engine) SetLLM(llm *LLMRecommender) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.llm = llm
}

// Generate creates recommendations from current state and signal summaries.
func (e *Engine) Generate(signals []SignalSummary) []Recommendation {
	// Phase 1: deterministic generators under lock
	e.mu.Lock()
	e.generating = true
	locked := true // tracks whether we currently hold the lock
	defer func() {
		if !locked {
			e.mu.Lock()
		}
		e.generating = false
		e.mu.Unlock()
	}()
	llm := e.llm // capture under lock
	var recs []Recommendation

	// 1. Unprotected critical/high tools — per-agent coverage analysis
	agents := e.getAgents()
	allRules := e.ruleEngine.Rules()
	for _, profile := range e.profiler.AllProfiles() {
		if profile.RiskLevel != "CRITICAL" && profile.RiskLevel != "HIGH" {
			continue
		}
		if e.isDismissed("unprotected_critical_tool:" + profile.Name) {
			continue
		}

		// Check coverage: broad rule covers all agents regardless
		if e.hasBroadRuleFor(profile.Name, allRules) {
			continue
		}

		// Per-agent coverage check (only agents with access to tool's server)
		coverage := e.computeToolCoverage(profile.Name, profile.Server, agents, allRules)
		if coverage.totalAgents == 0 {
			continue // no agents can reach this tool's server — no risk
		}
		if coverage.unprotectedCount == 0 {
			continue // all agents protected
		}

		action := "require_approval"
		if profile.RiskLevel == "CRITICAL" {
			action = "block"
		}

		// Build description based on coverage
		var desc string
		if coverage.totalAgents == 0 || coverage.protectedCount == 0 {
			desc = fmt.Sprintf("%s is classified as %s. No rules currently protect it.",
				profile.Name, profile.Classification)
		} else {
			desc = fmt.Sprintf("%s is classified as %s. %d of %d agents protected, %d unprotected.",
				profile.Name, profile.Classification,
				coverage.protectedCount, coverage.totalAgents, coverage.unprotectedCount)
		}

		recs = append(recs, Recommendation{
			ID:          generateRecID(),
			Type:        "unprotected_critical_tool",
			Severity:    strings.ToLower(profile.RiskLevel),
			Title:       fmt.Sprintf("Protect %s tool (%s)", profile.Name, profile.Classification),
			Description: desc,
			Evidence:    fmt.Sprintf("Risk reasons: %s", strings.Join(profile.RiskReasons, ", ")),
			SuggestedRule: config.RuleConfig{
				Tool:   profile.Name,
				Action: action,
				Reason: fmt.Sprintf("%s tool — %s risk", profile.Classification, profile.RiskLevel),
			},
			ImpactScore: computeRiskScore(profile.RiskLevel),
			Source:      "deterministic",
			Status:      "active",
			CreatedAt:   time.Now(),
		})
	}

	// 2. PII exposure
	piiCount := countSignals(signals, "pii", "")
	if piiCount > 10 {
		topTool := topToolForCategory(signals, "pii")
		if !e.isDismissed("pii:" + topTool) {
			recs = append(recs, Recommendation{
				ID:          generateRecID(),
				Type:        "pii_exposure",
				Severity:    "high",
				Title:       fmt.Sprintf("PII detected %d times in %s", piiCount, topTool),
				Description: "Personal data (SSN, credit card, email) detected in tool arguments.",
				Evidence:    fmt.Sprintf("%d PII instances detected, primarily in %s.", piiCount, topTool),
				SuggestedRule: config.RuleConfig{
					Tool:   topTool,
					Action: "require_approval",
					Reason: "PII detected in arguments",
				},
				ImpactScore: 70 + math.Min(float64(piiCount), 15),
				Source:      "deterministic",
				Status:      "active",
				CreatedAt:   time.Now(),
			})
		}
	}

	// 3. Destructive SQL
	destructiveCount := countSignals(signals, "destructive", "")
	if destructiveCount > 0 && !e.isDismissed("destructive_sql") {
		recs = append(recs, Recommendation{
			ID:          generateRecID(),
			Type:        "destructive_sql",
			Severity:    "critical",
			Title:       "Block destructive SQL in query tools",
			Description: "DELETE, DROP, or TRUNCATE statements detected in query arguments.",
			Evidence:    fmt.Sprintf("%d destructive SQL calls detected.", destructiveCount),
			SuggestedRule: config.RuleConfig{
				Tool:   "*query*",
				Action: "block",
				Reason: "Destructive SQL blocked",
			},
			ImpactScore: 90,
			Source:      "deterministic",
			Status:      "active",
			CreatedAt:   time.Now(),
		})
	}

	// 4. Suspicious sequences — handled by Sequence Alerts section, not recommendations.
	// Rule engine cannot enforce sequence-level conditions, so no actionable recommendation here.

	// 5. Rate limit recommendations from baselines
	for key, bl := range e.tracker.All() {
		if !bl.BaselineReady || bl.MeanCallsPerHour < 50 {
			continue
		}
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		tool := parts[1]
		if e.ruleEngine.HasRuleFor(tool) || e.isDismissed("rate_limit:"+tool) {
			continue
		}

		limit := int(bl.MeanCallsPerHour * 2)
		recs = append(recs, Recommendation{
			ID:       generateRecID(),
			Type:     "rate_limit",
			Severity: "warning",
			Title:    fmt.Sprintf("Add rate limit for %s (%d/hour)", tool, limit),
			Description: fmt.Sprintf("Baseline: %.0f calls/hour. Suggested limit: %d/hour (2x) to catch runaway loops.",
				bl.MeanCallsPerHour, limit),
			Evidence: fmt.Sprintf("Observed over %d samples.", bl.SampleCount),
			SuggestedRule: config.RuleConfig{
				Tool:      tool,
				Action:    "allow",
				RateLimit: fmt.Sprintf("%d/hour", limit),
				Reason:    "Rate limit based on observed baseline",
			},
			ImpactScore: 40 + math.Min(bl.MeanCallsPerHour/10, 20),
			Source:      "deterministic",
			Status:      "active",
			CreatedAt:   time.Now(),
		})
	}

	// Phase 2: LLM call outside lock (can take 10-30s)
	e.mu.Unlock()
	locked = false
	if llm != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		llmRecs, err := llm.Generate(ctx, LLMInput{
			ToolProfiles:    e.profiler.AllProfiles(),
			BehaviorSummary: e.tracker.All(),
			SignalsSummary:  signals,
			CurrentRules:    e.ruleEngine.Rules(),
		})
		cancel()
		if err != nil {
			slog.Warn("LLM recommendation error (non-fatal)", "component", "recommend", "error", err)
		} else {
			recs = append(recs, llmRecs...)
		}
	}

	// Phase 3: filter out any items dismissed during the LLM call, sort, store
	e.mu.Lock()
	locked = true
	var filtered []Recommendation
	for _, r := range recs {
		key := r.Type + ":" + r.SuggestedRule.Tool
		if !e.isDismissed(key) {
			filtered = append(filtered, r)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ImpactScore > filtered[j].ImpactScore
	})
	e.recs = filtered
	e.mu.Unlock()
	locked = false
	// e.generating reset by defer
	return filtered
}

// Get returns a recommendation by ID.
func (e *Engine) Get(id string) *Recommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for i := range e.recs {
		if e.recs[i].ID == id {
			return &e.recs[i]
		}
	}
	return nil
}

// Apply marks a recommendation as applied.
// Returns error if Generate is currently running (prevents lost writes).
func (e *Engine) Apply(id string) (*Recommendation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.generating {
		return nil, fmt.Errorf("recommendations are being regenerated, try again shortly")
	}
	for i := range e.recs {
		if e.recs[i].ID == id {
			e.recs[i].Status = "applied"
			e.recs[i].ResolvedAt = time.Now()
			return &e.recs[i], nil
		}
	}
	return nil, fmt.Errorf("recommendation %s not found", id)
}

// Dismiss marks a recommendation as dismissed.
func (e *Engine) Dismiss(id string) (*Recommendation, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.generating {
		return nil, fmt.Errorf("recommendations are being regenerated, try again shortly")
	}
	for i := range e.recs {
		if e.recs[i].ID == id {
			e.recs[i].Status = "dismissed"
			e.recs[i].ResolvedAt = time.Now()
			e.dismissed[e.recs[i].Type+":"+e.recs[i].SuggestedRule.Tool] = time.Now()
			return &e.recs[i], nil
		}
	}
	return nil, fmt.Errorf("recommendation %s not found", id)
}

// Active returns only active recommendations.
// DismissedCount returns the number of currently dismissed recommendations.
func (e *Engine) DismissedCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	count := 0
	for _, t := range e.dismissed {
		if time.Since(t) < e.dismissTTL {
			count++
		}
	}
	return count
}

func (e *Engine) Active() []Recommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var active []Recommendation
	for _, r := range e.recs {
		if r.Status == "active" {
			active = append(active, r)
		}
	}
	return active
}

// All returns all recommendations.
func (e *Engine) All() []Recommendation {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]Recommendation, len(e.recs))
	copy(result, e.recs)
	return result
}

func (e *Engine) isDismissed(key string) bool {
	if t, ok := e.dismissed[key]; ok {
		return time.Since(t) < e.dismissTTL
	}
	return false
}

func computeRiskScore(riskLevel string) float64 {
	switch riskLevel {
	case "CRITICAL":
		return 90
	case "HIGH":
		return 70
	case "MEDIUM":
		return 45
	default:
		return 20
	}
}

func countSignals(signals []SignalSummary, category, tool string) int {
	total := 0
	for _, s := range signals {
		if s.Category == category {
			if tool == "" || s.Tool == tool {
				total += s.Count
			}
		}
	}
	return total
}

func topToolForCategory(signals []SignalSummary, category string) string {
	counts := make(map[string]int)
	for _, s := range signals {
		if s.Category == category {
			counts[s.Tool] += s.Count
		}
	}
	best := ""
	bestCount := 0
	for tool, count := range counts {
		if count > bestCount {
			best = tool
			bestCount = count
		}
	}
	return best
}

func generateRecID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "rec-" + hex.EncodeToString(b)
}
