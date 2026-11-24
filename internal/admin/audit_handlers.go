package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
)

// AuditHandlers serves audit store endpoints (recent, info, purge, search, stats).
type AuditHandlers struct {
	store audit.AuditStore
}

// NewAuditHandlers creates audit API handlers.
func NewAuditHandlers(store audit.AuditStore) *AuditHandlers {
	return &AuditHandlers{store: store}
}

// RegisterRoutes registers audit endpoints.
func (h *AuditHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/audit/recent", h.handleRecent)
	mux.HandleFunc("/admin/v1/audit/info", h.handleInfo)
	mux.HandleFunc("/admin/v1/audit/purge", h.handlePurge)
	mux.HandleFunc("/admin/v1/audit/search", h.handleSearch)
	mux.HandleFunc("/admin/v1/audit/stats", h.handleStats)
	mux.HandleFunc("/admin/v1/audit/config", h.handleConfig)
	mux.HandleFunc("/admin/v1/audit/verify", h.handleVerify)
}

// GET /admin/v1/audit/recent?limit=100
func (h *AuditHandlers) handleRecent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	limit := 200
	if q := r.URL.Query().Get("limit"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	entries := h.store.RecentN(limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"count":   len(entries),
	})
}

// GET /admin/v1/audit/info
func (h *AuditHandlers) handleInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.store.Info())
}

// DELETE /admin/v1/audit/purge
func (h *AuditHandlers) handlePurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if err := h.store.Purge(); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "purged"})
}

// PUT /admin/v1/audit/config — update max_entries at runtime
func (h *AuditHandlers) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		MaxEntries *int `json:"maxEntries"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
		return
	}

	if body.MaxEntries != nil {
		h.store.SetMaxEntries(*body.MaxEntries)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(h.store.Info())
}

// GET /admin/v1/audit/search?tool=X&agent=Y&decision=Z&server=S&from=T&to=T&limit=N
func (h *AuditHandlers) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	filter := audit.SearchFilter{
		Tool:     q.Get("tool"),
		AgentID:  q.Get("agent"),
		Decision: q.Get("decision"),
		Server:   q.Get("server"),
	}

	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.Limit = n
		}
	}
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.From = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.To = t
		}
	}

	entries := h.store.Search(filter)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"entries": entries,
		"count":   len(entries),
	})
}

// GET /admin/v1/audit/stats?from=T&to=T
func (h *AuditHandlers) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	q := r.URL.Query()
	var filter audit.StatsFilter
	if v := q.Get("from"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.From = t
		}
	}
	if v := q.Get("to"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.To = t
		}
	}

	stats := h.store.Stats(filter)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// GET /admin/v1/audit/verify — verify audit log hash chain integrity.
func (h *AuditHandlers) handleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	entries := h.store.Recent()
	tampered := audit.VerifyChain(entries)

	w.Header().Set("Content-Type", "application/json")
	if tampered >= 0 {
		json.NewEncoder(w).Encode(map[string]any{
			"valid":         false,
			"tampered_at":   tampered,
			"total_entries": len(entries),
			"message":       "Hash chain verification failed — audit log may have been tampered with",
		})
	} else {
		json.NewEncoder(w).Encode(map[string]any{
			"valid":         true,
			"total_entries": len(entries),
			"message":       "Audit log hash chain is intact",
		})
	}
}
