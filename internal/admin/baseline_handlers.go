package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
)

// BaselineHandlers serves baseline data endpoints.
type BaselineHandlers struct {
	tracker *baseline.Tracker
}

// NewBaselineHandlers creates baseline API handlers.
func NewBaselineHandlers(tracker *baseline.Tracker) *BaselineHandlers {
	return &BaselineHandlers{tracker: tracker}
}

// RegisterRoutes registers GET /admin/v1/baselines and /admin/v1/baselines/{agent}.
func (h *BaselineHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/baselines", h.handleList)
	mux.HandleFunc("/admin/v1/baselines/", h.handleByAgent)
}

// baselineEntry is the JSON response shape for a single baseline.
type baselineEntry struct {
	AgentID  string             `json:"agentId"`
	Tool     string             `json:"tool"`
	Baseline *baseline.Baseline `json:"baseline"`
}

func (h *BaselineHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.tracker == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"baselines": []any{}, "count": 0})
		return
	}

	all := h.tracker.All()
	entries := make([]baselineEntry, 0, len(all))
	for key, bl := range all {
		agent, tool := parseBaselineKey(key)
		entries = append(entries, baselineEntry{AgentID: agent, Tool: tool, Baseline: bl})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"baselines": entries,
		"count":     len(entries),
		"status":    h.tracker.Status(),
	})
}

func (h *BaselineHandlers) handleByAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if h.tracker == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"baselines": []any{}, "count": 0})
		return
	}

	// Extract agent ID from path: /admin/v1/baselines/{agentId}
	agentID := strings.TrimPrefix(r.URL.Path, "/admin/v1/baselines/")
	if agentID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "agent ID required"})
		return
	}

	all := h.tracker.All()
	entries := make([]baselineEntry, 0)
	prefix := agentID + ":"
	for key, bl := range all {
		if strings.HasPrefix(key, prefix) {
			_, tool := parseBaselineKey(key)
			entries = append(entries, baselineEntry{AgentID: agentID, Tool: tool, Baseline: bl})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"baselines": entries,
		"count":     len(entries),
		"agentId":   agentID,
	})
}

// parseBaselineKey splits "agent:tool" into (agent, tool).
func parseBaselineKey(key string) (string, string) {
	idx := strings.Index(key, ":")
	if idx < 0 {
		return key, ""
	}
	return key[:idx], key[idx+1:]
}
