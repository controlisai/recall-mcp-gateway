package rules

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/store"
)

// Decision is the result of rule evaluation.
type Decision struct {
	Action        string                    // allow, block, hide, require_approval, require_consent, rate_limited
	Rule          int                       // index of matched rule (-1 for default)
	Reason        string
	Notify        string                    // webhook URL from matched rule (empty if none)
	RetryMethod   string                    // "poll" or "callback" — for require_approval rules
	RetryHintMs   int64                     // suggested retry interval in ms (poll mode)
	StatusWebhook string                    // webhook URL for approval resolution (callback mode)
	ConsentForm      *config.ConsentFormConfig // form definition for require_consent rules (nil for other actions)
	DelegationNotify string                    // webhook URL for delegated user notification
	RequiredScopes   []string                  // OAuth scopes required (triggers scope step-up)
}

// Engine evaluates tool calls against configured rules.
type Engine struct {
	rules          []config.RuleConfig
	limiter        RateLimiterInterface
	persistPath    string   // file path for rules persistence (empty = no file persistence)
	persistDB      *sql.DB  // shared DB for rules persistence (nil = use file)
	defaultAction  string   // "allow" (default) or "block" — action when no rule matches
	configNotifier configNotifier // PG LISTEN/NOTIFY for multi-pod (nil = standalone)
}

// configNotifier broadcasts rule mutations to other pods via PG LISTEN/NOTIFY.
type configNotifier interface {
	NotifyChange(configType string, data []byte) error
}

// NewEngine creates a rule engine from config rules.
func NewEngine(rules []config.RuleConfig) *Engine {
	return &Engine{
		rules:   rules,
		limiter: NewRateLimiter(),
	}
}

// SetRateLimiter replaces the rate limiter implementation.
// Call with a *proxyredis.RedisRateLimiter for multi-pod rate limiting.
// When not called, the engine uses an in-process sliding-window limiter.
func (e *Engine) SetRateLimiter(rl RateLimiterInterface) {
	e.limiter = rl
}

// SetConfigNotifier wires PG LISTEN/NOTIFY for cross-pod rule change notification.
func (e *Engine) SetConfigNotifier(cn configNotifier) {
	e.configNotifier = cn
}

// SetDefaultAction sets the action returned when no rule matches.
// Valid values: "allow" (default), "block".
func (e *Engine) SetDefaultAction(action string) {
	if action == "block" {
		e.defaultAction = "block"
	} else {
		e.defaultAction = "allow"
	}
}

// EvalContext provides optional context about the tool being evaluated.
type EvalContext struct {
	ServerName     string
	Classification string
	Archetype      string // agent archetype for per-archetype action resolution
}

// Evaluate checks a tool call against rules. Returns the decision.
// First match wins. If no rule matches, default is allow.
// args is optional — pass nil if arguments are not available.
func (e *Engine) Evaluate(agentID, toolName string, args ...map[string]any) Decision {
	return e.EvaluateWithContext(agentID, toolName, EvalContext{}, args...)
}

// EvaluateWithContext checks a tool call against rules with additional context.
func (e *Engine) EvaluateWithContext(agentID, toolName string, ctx EvalContext, args ...map[string]any) Decision {
	var toolArgs map[string]any
	if len(args) > 0 {
		toolArgs = args[0]
	}

	for i, rule := range e.rules {
		if !MatchToolGlob(rule.Tool, toolName) {
			continue
		}

		// Check server scope
		if rule.Server != "" && ctx.ServerName != "" && rule.Server != ctx.ServerName {
			continue
		}

		// Check classification scope
		if rule.Classification != "" && ctx.Classification != "" && rule.Classification != ctx.Classification {
			continue
		}

		// Check agent scope
		if len(rule.Agents) > 0 && !containsAgent(rule.Agents, agentID) {
			continue
		}

		// Check schedule (time-based activation)
		if rule.Schedule != nil && !CheckSchedule(rule.Schedule) {
			continue // rule not active at this time
		}

		// Check param_blocks (argument pattern matching).
		// Semantics: param_blocks act as a CONDITION — the rule only fires when
		// arguments match the patterns. If args don't match, the rule is skipped
		// and evaluation continues to the next rule. This allows writing rules like
		// "block query when sql contains DELETE" without blocking all queries.
		if len(rule.ParamBlocks) > 0 && toolArgs != nil {
			if matched, reason := CheckParamBlocks(rule.ParamBlocks, toolArgs); matched {
				resolvedAction := rule.ResolveAction(ctx.Archetype)
				return Decision{Action: resolvedAction, Rule: i, Reason: reason, Notify: rule.Notify,
					RetryMethod: rule.RetryMethod, RetryHintMs: rule.RetryHintMs, StatusWebhook: rule.StatusWebhook,
					ConsentForm: rule.ConsentForm, DelegationNotify: rule.DelegationNotify, RequiredScopes: rule.RequiredScopes}
			}
			continue // args didn't match param_blocks — skip to next rule
		}

		// Check rate limit
		if rule.RateLimit != "" {
			if maxCalls, window, err := parseRateLimit(rule.RateLimit); err == nil {
				key := agentID + ":" + toolName
				if !e.limiter.Allow(key, maxCalls, window) {
					return Decision{Action: "rate_limited", Rule: i, Reason: "rate limit exceeded: " + rule.RateLimit}
				}
			}
		}

		resolvedAction := rule.ResolveAction(ctx.Archetype)
		return Decision{
			Action:           resolvedAction,
			Rule:             i,
			Notify:           rule.Notify,
			Reason:           rule.Reason,
			RetryMethod:      rule.RetryMethod,
			RetryHintMs:      rule.RetryHintMs,
			StatusWebhook:    rule.StatusWebhook,
			ConsentForm:      rule.ConsentForm,
			DelegationNotify: rule.DelegationNotify,
			RequiredScopes:   rule.RequiredScopes,
		}
	}

	// No rule matched — use configured default action
	action := e.defaultAction
	if action == "" {
		action = "allow"
	}
	reason := "no matching rule (default " + action + ")"
	return Decision{Action: action, Rule: -1, Reason: reason}
}

// ruleHasAction checks if a rule has the given action in either Action or Actions map.
func ruleHasAction(rule config.RuleConfig, action string) bool {
	if rule.Action == action {
		return true
	}
	for _, a := range rule.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// FilterTools returns tools visible to an agent, removing hidden tools.
func (e *Engine) FilterTools(agentID string, toolNames []string) []string {
	var visible []string
	for _, name := range toolNames {
		hidden := false
		for _, rule := range e.rules {
			if !ruleHasAction(rule, "hide") {
				continue
			}
			if !MatchToolGlob(rule.Tool, name) {
				continue
			}
			if len(rule.Agents) > 0 && !containsAgent(rule.Agents, agentID) {
				continue
			}
			hidden = true
			break
		}
		if !hidden {
			visible = append(visible, name)
		}
	}
	return visible
}

// RuleCount returns the number of configured rules.
func (e *Engine) RuleCount() int {
	return len(e.rules)
}

// Rules returns all configured rules.
func (e *Engine) Rules() []config.RuleConfig {
	return e.rules
}

// AddRule appends a rule if no duplicate (same tool+server+action+agents) exists.
// Returns true if added, false if duplicate found.
func (e *Engine) AddRule(rule config.RuleConfig) bool {
	for _, existing := range e.rules {
		if existing.Tool == rule.Tool && existing.Server == rule.Server &&
			existing.Action == rule.Action && sameAgents(existing.Agents, rule.Agents) {
			return false
		}
	}
	e.rules = append(e.rules, rule)
	e.persist()
	return true
}

func sameAgents(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// RemoveRule removes a rule by index and persists.
func (e *Engine) RemoveRule(index int) bool {
	if index < 0 || index >= len(e.rules) {
		return false
	}
	e.rules = append(e.rules[:index], e.rules[index+1:]...)
	e.persist()
	return true
}

// SetPersistPath sets the file path for rules persistence.
func (e *Engine) SetPersistPath(path string) {
	e.persistPath = path
	// Load persisted rules if file exists (skip duplicates from config)
	if path != "" {
		if loaded, err := loadRulesFromFile(path); err == nil && len(loaded) > 0 {
			existing := make(map[string]bool)
			for _, r := range e.rules {
				existing[r.Tool+"|"+r.Action+"|"+r.Server] = true
			}
			added := 0
			for _, r := range loaded {
				key := r.Tool + "|" + r.Action + "|" + r.Server
				if !existing[key] {
					e.rules = append(e.rules, r)
					added++
				}
			}
			if added > 0 {
				slog.Info("persisted rules loaded", "component", "rules", "count", added, "path", path)
			}
		}
	}
}

// SetPersistDB configures the engine to persist rules in the shared gateway DB.
// In DB-only mode, rules are already loaded from DB before construction —
// this just sets the handle for future writes.
func (e *Engine) SetPersistDB(db *sql.DB) {
	e.persistDB = db
}

func (e *Engine) persist() {
	data, marshalErr := json.Marshal(e.rules)
	if marshalErr != nil {
		slog.Error("failed to marshal rules for persistence", "component", "rules", "error", marshalErr)
		return
	}
	if e.persistDB != nil {
		if err := saveRulsToDB(e.persistDB, e.rules); err != nil {
			slog.Error("failed to persist rules to gateway.db", "component", "rules", "error", err)
		}
	} else if e.persistPath != "" {
		if err := saveRulesToFile(e.persistPath, e.rules); err != nil {
			slog.Error("failed to persist rules to file", "component", "rules", "error", err)
		}
	}
	if e.configNotifier != nil {
		if err := e.configNotifier.NotifyChange("rules", data); err != nil {
			slog.Warn("failed to notify rules change", "component", "rules", "error", err)
		}
	}
}

func saveRulsToDB(db *sql.DB, rules []config.RuleConfig) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec("DELETE FROM rules"); err != nil {
		return err
	}
	isPG := store.IsPostgres(db)
	for i, r := range rules {
		data, err := json.Marshal(r)
		if err != nil {
			continue
		}
		agentsJSON, _ := json.Marshal(r.Agents)
		if _, err = store.ExecDialectTx(tx,
			"INSERT INTO rules (position, tool, server, action, agents, data) VALUES (?, ?, ?, ?, ?, ?)",
			"INSERT INTO rules (position, tool, server, action, agents, data) VALUES ($1, $2, $3, $4, $5, $6)",
			isPG, i, r.Tool, r.Server, r.Action, string(agentsJSON), string(data),
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func saveRulesToFile(path string, rules []config.RuleConfig) error {
	data, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func loadRulesFromFile(path string) ([]config.RuleConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rules []config.RuleConfig
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return rules, nil
}

// HasRuleFor returns true if any non-wildcard rule matches this tool.
func (e *Engine) HasRuleFor(toolName string) bool {
	for _, r := range e.rules {
		if r.Tool == "*" {
			continue // skip catch-all
		}
		if MatchToolGlob(r.Tool, toolName) {
			return true
		}
	}
	return false
}

// SetRules replaces all rules (for hot-reload).
func (e *Engine) SetRules(rules []config.RuleConfig) {
	e.rules = rules
}

// MatchToolGlob checks if a tool name matches a glob pattern.
func MatchToolGlob(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return pattern == name
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 2 {
		return strings.HasPrefix(name, parts[0]) && strings.HasSuffix(name, parts[1])
	}
	remaining := name
	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(remaining, part)
		if idx < 0 {
			return false
		}
		if i == 0 && idx != 0 {
			return false
		}
		remaining = remaining[idx+len(part):]
	}
	return true
}

func containsAgent(agents []string, agentID string) bool {
	for _, a := range agents {
		if a == agentID {
			return true
		}
	}
	return false
}
