package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
)

// ExportHandlers serves audit data export endpoints.
type ExportHandlers struct {
	auditor audit.AuditStore
}

// NewExportHandlers creates export handlers.
func NewExportHandlers(auditor audit.AuditStore) *ExportHandlers {
	return &ExportHandlers{auditor: auditor}
}

// RegisterRoutes registers export endpoints.
func (h *ExportHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/export/audit", h.handleAuditExport)
}

func (h *ExportHandlers) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	recent := h.auditor.Recent()

	filename := fmt.Sprintf("recall-mcp-gateway-audit-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))

	json.NewEncoder(w).Encode(map[string]any{
		"exportedAt": time.Now().UTC(),
		"count":      len(recent),
		"entries":    recent,
	})
}
