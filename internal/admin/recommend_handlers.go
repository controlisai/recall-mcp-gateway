package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/recommend"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

// RecommendHandlers provides HTTP handlers for the recommendations API.
type RecommendHandlers struct {
	engine     *recommend.Engine
	ruleEngine *rules.Engine
}

// NewRecommendHandlers creates recommendation handlers.
func NewRecommendHandlers(engine *recommend.Engine, ruleEngine *rules.Engine) *RecommendHandlers {
	return &RecommendHandlers{engine: engine, ruleEngine: ruleEngine}
}

// RegisterRoutes mounts recommendation routes on the mux.
func (h *RecommendHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/recommendations", h.handleList)
	mux.HandleFunc("/admin/v1/recommendations/", h.handleAction)
}

func (h *RecommendHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	recs := h.engine.Active()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"recommendations":  recs,
		"count":            len(recs),
		"dismissedCount":   h.engine.DismissedCount(),
	})
}

func (h *RecommendHandlers) handleAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/admin/v1/recommendations/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		http.Error(w, `{"error":"invalid path"}`, http.StatusBadRequest)
		return
	}

	id := parts[0]
	action := parts[1]
	w.Header().Set("Content-Type", "application/json")

	switch action {
	case "apply":
		rec, err := h.engine.Apply(id)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		// Add the suggested rule to the rule engine
		h.ruleEngine.AddRule(rec.SuggestedRule)
		json.NewEncoder(w).Encode(map[string]any{
			"status":         "applied",
			"recommendation": rec,
			"ruleAdded":      rec.SuggestedRule,
		})

	case "dismiss":
		rec, err := h.engine.Dismiss(id)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status":         "dismissed",
			"recommendation": rec,
		})

	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusBadRequest)
	}
}
