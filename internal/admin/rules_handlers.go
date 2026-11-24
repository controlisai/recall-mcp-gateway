package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

// RulesHandlers provides HTTP handlers for rules CRUD.
type RulesHandlers struct {
	ruleEngine *rules.Engine
	profiler   *detect.Profiler
	auth       AdminAuthChecker
}

// NewRulesHandlers creates rules handlers.
func NewRulesHandlers(ruleEngine *rules.Engine, profiler *detect.Profiler, adminKey string) *RulesHandlers {
	return &RulesHandlers{ruleEngine: ruleEngine, profiler: profiler, auth: AdminAuthChecker{AdminKey: adminKey}}
}

// SetSessionValidator configures dashboard session auth for rules endpoints.
func (h *RulesHandlers) SetSessionValidator(fn func(r *http.Request) bool) {
	h.auth.SessionValidator = fn
}

// RegisterRoutes mounts rules routes on the mux.
func (h *RulesHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/rules", h.handleRules)
	mux.HandleFunc("/admin/v1/rules/", h.handleRuleByIndex)
	mux.HandleFunc("/admin/v1/rules-preview", h.handlePreview)
	mux.HandleFunc("/admin/v1/tools/risk-override", h.handleRiskOverride)
}

func (h *RulesHandlers) handleRules(w http.ResponseWriter, r *http.Request) {
	if !h.auth.CheckAuth(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		allRules := h.ruleEngine.Rules()
		json.NewEncoder(w).Encode(map[string]any{
			"rules": allRules,
			"count": len(allRules),
		})

	case http.MethodPost:
		var rule config.RuleConfig
		if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid rule: " + err.Error()})
			return
		}
		if rule.Tool == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "tool is required"})
			return
		}
		if rule.Action == "" && len(rule.Actions) == 0 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "action or actions map is required"})
			return
		}
		validActions := map[string]bool{
			"allow": true, "block": true, "hide": true,
			"require_approval": true, "require_consent": true, "rate_limit": true,
		}
		if rule.Action != "" && !validActions[rule.Action] {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid action: " + rule.Action})
			return
		}
		for archetype, action := range rule.Actions {
			if !validActions[action] {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid action for archetype " + archetype + ": " + action})
				return
			}
		}
		if !h.ruleEngine.AddRule(rule) {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"error": "duplicate rule: same tool, server, action, and agents already exists"})
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "created",
			"rule":   rule,
			"index":  h.ruleEngine.RuleCount() - 1,
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *RulesHandlers) handleRuleByIndex(w http.ResponseWriter, r *http.Request) {
	if !h.auth.CheckAuth(w, r) {
		return
	}
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	indexStr := strings.TrimPrefix(r.URL.Path, "/admin/v1/rules/")
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid index"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if h.ruleEngine.RemoveRule(index) {
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
	} else {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "rule not found"})
	}
}

// handlePreview shows which tools a rule would affect (dry run).
func (h *RulesHandlers) handlePreview(w http.ResponseWriter, r *http.Request) {
	if !h.auth.CheckAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var rule config.RuleConfig
	if err := json.NewDecoder(r.Body).Decode(&rule); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid rule"})
		return
	}

	var matched []string
	if h.profiler != nil {
		for _, profile := range h.profiler.AllProfiles() {
			if rule.Tool != "" && !rules.MatchToolGlob(rule.Tool, profile.Name) {
				continue
			}
			if rule.Server != "" && rule.Server != profile.Server {
				continue
			}
			if rule.Classification != "" && rule.Classification != profile.Classification {
				continue
			}
			matched = append(matched, profile.Name)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"matchedTools": matched,
		"count":        len(matched),
	})
}

// handleRiskOverride allows overriding a tool's auto-detected risk level.
func (h *RulesHandlers) handleRiskOverride(w http.ResponseWriter, r *http.Request) {
	if !h.auth.CheckAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Tool      string `json:"tool"`
		RiskLevel string `json:"riskLevel"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tool == "" || body.RiskLevel == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "tool and riskLevel required"})
		return
	}
	valid := map[string]bool{"LOW": true, "MEDIUM": true, "HIGH": true, "CRITICAL": true}
	if !valid[body.RiskLevel] {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "riskLevel must be LOW, MEDIUM, HIGH, or CRITICAL"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if h.profiler != nil && h.profiler.OverrideRiskLevel(body.Tool, body.RiskLevel) {
		json.NewEncoder(w).Encode(map[string]any{"status": "updated", "tool": body.Tool, "riskLevel": body.RiskLevel})
	} else {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "tool not found"})
	}
}
