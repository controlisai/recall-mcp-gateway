package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/review"
)

// ReviewHandlers provides HTTP handlers for tool review management.
type ReviewHandlers struct {
	store *review.Store
}

// NewReviewHandlers creates review handlers.
func NewReviewHandlers(store *review.Store) *ReviewHandlers {
	return &ReviewHandlers{store: store}
}

// RegisterRoutes mounts review routes on the mux.
func (h *ReviewHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/tools/reviews", h.handleList)
	mux.HandleFunc("/admin/v1/tools/", h.handleToolAction)
}

// handleList returns all tool review statuses.
func (h *ReviewHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"reviews": h.store.AllReviews(),
	})
}

// handleToolAction routes tool-specific actions (e.g., /admin/v1/tools/{name}/review).
func (h *ReviewHandlers) handleToolAction(w http.ResponseWriter, r *http.Request) {
	// Parse: /admin/v1/tools/{name}/review
	path := strings.TrimPrefix(r.URL.Path, "/admin/v1/tools/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || parts[1] != "review" {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	toolName := parts[0]
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodPost:
		var body struct {
			Note                string   `json:"note"`
			AcknowledgedSignals []string `json:"acknowledged_signals"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		entry := h.store.MarkReviewed(toolName, "admin-api", body.Note, body.AcknowledgedSignals)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{
			"status": "reviewed",
			"review": entry,
		})

	case http.MethodGet:
		entry := h.store.GetReview(toolName)
		if entry == nil {
			json.NewEncoder(w).Encode(map[string]any{
				"status": "unreviewed",
				"tool":   toolName,
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status": string(entry.Status),
			"review": entry,
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
