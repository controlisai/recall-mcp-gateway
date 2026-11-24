package admin

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/approval"
	"github.com/controlisai/recall-mcp-gateway/internal/proxy"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

// ConfigHandlers provides runtime config update endpoints.
type ConfigHandlers struct {
	proxyServer    *proxy.Server
	pool           *upstream.Pool
	approvalStore  approval.StoreInterface
	DashUpdater    func(key, value string) // callback to update dashboard SSE init values
	OriginsUpdater func(origins []string)  // callback to update origin middleware
}

// ConfigHandlersConfig holds dependencies for config handlers.
type ConfigHandlersConfig struct {
	ProxyServer    *proxy.Server
	Pool           *upstream.Pool
	ApprovalStore  approval.StoreInterface
	DashUpdater    func(key, value string)
	OriginsUpdater func(origins []string)
}

// NewConfigHandlers creates config handlers.
func NewConfigHandlers(cfg ConfigHandlersConfig) *ConfigHandlers {
	return &ConfigHandlers{
		proxyServer:    cfg.ProxyServer,
		pool:           cfg.Pool,
		approvalStore:  cfg.ApprovalStore,
		DashUpdater:    cfg.DashUpdater,
		OriginsUpdater: cfg.OriginsUpdater,
	}
}

// RegisterRoutes mounts config routes.
func (h *ConfigHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/config/runtime", h.handleRuntimeConfig)
}

func (h *ConfigHandlers) handleRuntimeConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		resp := map[string]any{}
		if h.pool != nil {
			resp["mcpHeaderMode"] = h.pool.GlobalHeaderMode()
		}
		json.NewEncoder(w).Encode(resp)

	case http.MethodPut:
		var body struct {
			McpHeaderMode  *string  `json:"mcpHeaderMode,omitempty"`
			ApprovalTTL    *string  `json:"approvalTtl,omitempty"`
			AllowedOrigins []string `json:"allowedOrigins,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid JSON"})
			return
		}

		updated := map[string]string{}

		if body.McpHeaderMode != nil {
			mode := *body.McpHeaderMode
			valid := map[string]bool{"passthrough": true, "generate": true, "managed": true}
			if !valid[mode] {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "mcpHeaderMode must be passthrough, generate, or managed"})
				return
			}
			if h.pool != nil {
				h.pool.SetGlobalHeaderMode(mode)
			}
			if h.DashUpdater != nil {
				h.DashUpdater("mcpHeaderMode", mode)
			}
			updated["mcpHeaderMode"] = mode
		}

		if body.ApprovalTTL != nil {
			d, err := time.ParseDuration(*body.ApprovalTTL)
			if err != nil || d < time.Minute || d > 24*time.Hour {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "approvalTtl must be a valid duration between 1m and 24h"})
				return
			}
			if h.approvalStore != nil {
				h.approvalStore.SetTTL(d)
			}
			if h.DashUpdater != nil {
				h.DashUpdater("approvalTtl", *body.ApprovalTTL)
			}
			updated["approvalTtl"] = *body.ApprovalTTL
		}

		if body.AllowedOrigins != nil {
			if h.OriginsUpdater != nil {
				h.OriginsUpdater(body.AllowedOrigins)
			}
			if h.DashUpdater != nil {
				h.DashUpdater("allowedOrigins", "updated")
			}
			updated["allowedOrigins"] = "updated"
		}

		json.NewEncoder(w).Encode(map[string]any{"status": "updated", "changes": updated})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
