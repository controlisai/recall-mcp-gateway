package admin

import (
	"encoding/json"
	"net/http"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"

	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/platform"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

// SyncDeps holds all dependencies needed by the sync handler.
type SyncDeps struct {
	PlatformHandlers *PlatformHandlers
	AgentHandlers    *AgentHandlers
	Profiler         *detect.Profiler
	RuleEngine       *rules.Engine
	Tracker          *baseline.Tracker
	Latency          *baseline.LatencyTracker
	Version          string
}

// SyncHandlers serves the platform sync endpoint.
type SyncHandlers struct {
	deps SyncDeps
}

// NewSyncHandlers creates sync handlers.
func NewSyncHandlers(deps SyncDeps) *SyncHandlers {
	return &SyncHandlers{deps: deps}
}

// RegisterRoutes mounts sync routes on the mux.
func (h *SyncHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/platform/sync", h.handleSync)
}

func (h *SyncHandlers) handleSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	// Check platform connection
	client := h.deps.PlatformHandlers.Client()
	if client == nil || !client.IsConfigured() {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Not connected to platform. Connect first via Settings."})
		return
	}

	var body struct {
		ProductID string `json:"productId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}
	if body.ProductID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "productId is required"})
		return
	}

	// Store selected product
	h.deps.PlatformHandlers.SetProductID(body.ProductID)

	ctx := r.Context()

	// 1. Sync agents
	agents := h.deps.AgentHandlers.Agents()
	agentResult := platform.SyncAgents(ctx, client, body.ProductID, agents)

	// 2. Sync intents (tools)
	var profiles []*detect.ToolProfile
	if h.deps.Profiler != nil {
		profiles = h.deps.Profiler.AllProfiles()
	}
	rules := h.deps.RuleEngine.Rules()
	intentResult := platform.SyncIntents(ctx, client, body.ProductID, profiles, rules)

	// 3. Sync policy
	policyResult := platform.SyncPolicy(ctx, client, body.ProductID, rules)

	// 4. Sync baselines
	var baselineResult platform.SyncResult
	if h.deps.Tracker != nil {
		baselines := h.deps.Tracker.All()
		var serverHealth map[string]baseline.ServerHealth
		if h.deps.Latency != nil {
			serverHealth = h.deps.Latency.All()
		}
		baselineResult = platform.SyncBaselines(ctx, client, body.ProductID, h.deps.Version, len(rules), baselines, serverHealth)
	}

	// Build response
	totalCreated := agentResult.Created + intentResult.Created + policyResult.Created + baselineResult.Created
	totalSkipped := agentResult.Skipped + intentResult.Skipped + policyResult.Skipped + baselineResult.Skipped
	totalErrors := agentResult.Errors + intentResult.Errors + policyResult.Errors + baselineResult.Errors

	status := "complete"
	if totalErrors > 0 && totalCreated == 0 {
		status = "failed"
	} else if totalErrors > 0 {
		status = "partial"
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"synced": map[string]any{
			"agents":    agentResult,
			"intents":   intentResult,
			"policy":    policyResult,
			"baselines": baselineResult,
		},
		"totals": map[string]int{
			"created": totalCreated,
			"skipped": totalSkipped,
			"errors":  totalErrors,
		},
		"productId": body.ProductID,
		"consoleUrl": client.ConsoleAPIURL(),
	})
}

// SyncPreview returns a summary of what would be synced without actually syncing.
func (h *SyncHandlers) SyncPreview() map[string]any {
	agents := h.deps.AgentHandlers.Agents()
	// Filter out anonymous
	var realAgents []config.AgentConfig
	for _, a := range agents {
		if a.ID != "" && a.ID != "anonymous" {
			realAgents = append(realAgents, a)
		}
	}

	var toolCount int
	if h.deps.Profiler != nil {
		toolCount = len(h.deps.Profiler.AllProfiles())
	}

	ruleCount := len(h.deps.RuleEngine.Rules())

	var baselineCount int
	if h.deps.Tracker != nil {
		baselineCount = len(h.deps.Tracker.All())
	}

	return map[string]any{
		"agents":    len(realAgents),
		"tools":     toolCount,
		"rules":     ruleCount,
		"baselines": baselineCount,
	}
}
