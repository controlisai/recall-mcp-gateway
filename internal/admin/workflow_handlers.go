package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/proxy"
)

// WorkflowHandlers provides HTTP handlers for workflow management.
type WorkflowHandlers struct {
	workflow *detect.WorkflowDetector
	history  *proxy.HistoryBuffer
}

// NewWorkflowHandlers creates workflow handlers.
func NewWorkflowHandlers(workflow *detect.WorkflowDetector, history *proxy.HistoryBuffer) *WorkflowHandlers {
	return &WorkflowHandlers{workflow: workflow, history: history}
}

// RegisterRoutes mounts workflow routes on the mux.
func (h *WorkflowHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/workflows/expected", h.handleExpected)
	mux.HandleFunc("/admin/v1/workflows/observed", h.handleObserved)
}

func (h *WorkflowHandlers) handleExpected(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		// List all expected workflows across all agents
		result := make(map[string][][]string)
		for _, agentID := range h.history.AllAgents() {
			expected := h.workflow.GetExpected(agentID)
			if len(expected) > 0 {
				result[agentID] = expected
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"workflows": result})

	case http.MethodPost:
		var body struct {
			AgentID string   `json:"agentId"`
			Steps   []string `json:"steps"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
			return
		}
		if body.AgentID == "" || len(body.Steps) < 2 {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "agentId and at least 2 steps required"})
			return
		}
		h.workflow.SetExpected(body.AgentID, body.Steps)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"status": "created", "agentId": body.AgentID, "steps": body.Steps})

	case http.MethodDelete:
		var body struct {
			AgentID string   `json:"agentId"`
			Steps   []string `json:"steps"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
			return
		}
		if body.AgentID == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "agentId is required"})
			return
		}
		h.workflow.RemoveExpected(body.AgentID, body.Steps)
		json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *WorkflowHandlers) handleObserved(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	agentID := r.URL.Query().Get("agentId")
	result := make(map[string][]detect.WorkflowPattern)

	agents := h.history.AllAgents()
	for _, a := range agents {
		if agentID != "" && a != agentID {
			continue
		}
		history := h.history.Get(a)
		if len(history) < 5 {
			continue
		}
		workflows := detect.DiscoverWorkflows(history, 2, 4)
		if len(workflows) > 0 {
			result[a] = workflows
		}
	}

	json.NewEncoder(w).Encode(map[string]any{"workflows": result})
}

// DiffHandler serves GET /admin/v1/calls/:requestId/diff.
func DiffHandler(history *proxy.HistoryBuffer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Extract requestId — path is /admin/v1/calls/:id/diff
		path := strings.TrimPrefix(r.URL.Path, "/admin/v1/calls/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) < 2 || parts[1] != "diff" {
			http.Error(w, `{"error":"invalid path — use /admin/v1/calls/:requestId/diff"}`, http.StatusBadRequest)
			return
		}

		// Search all agents' history for the requestId
		// (requestId is not stored in HistoryEntry — we match by searching audit log)
		// For now, find the most recent pair of calls to the same tool for the agent
		agentID := r.URL.Query().Get("agentId")
		tool := r.URL.Query().Get("tool")

		if agentID == "" || tool == "" {
			http.Error(w, `{"error":"agentId and tool query params required"}`, http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		entries := history.Get(agentID)
		var prev, current *detect.HistoryEntry
		for i := len(entries) - 1; i >= 0; i-- {
			if entries[i].Tool == tool {
				if current == nil {
					e := entries[i]
					current = &e
				} else {
					e := entries[i]
					prev = &e
					break
				}
			}
		}

		if prev == nil || current == nil {
			json.NewEncoder(w).Encode(map[string]any{
				"tool":  tool,
				"agent": agentID,
				"diff":  nil,
				"note":  "need at least 2 calls to the same tool for diff",
			})
			return
		}

		diff := detect.DiffArguments(prev.Arguments, current.Arguments)
		json.NewEncoder(w).Encode(map[string]any{
			"tool":  tool,
			"agent": agentID,
			"diff":  diff,
		})
	}
}
