package admin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

// serverConfigNotifier broadcasts server mutations to other pods via PG LISTEN/NOTIFY.
type serverConfigNotifier interface {
	NotifyChange(configType string, data []byte) error
}

// ServerManagementHandlers provides HTTP handlers for server import/add/remove.
type ServerManagementHandlers struct {
	pool           *upstream.Pool
	store          ServerStore            // DB-backed server persistence
	onChanged      func()                 // callback after servers change (refresh tools)
	configNotifier serverConfigNotifier   // PG LISTEN/NOTIFY for multi-pod (nil = standalone)
}

// ServerStore is the interface for persisting server configs to gateway.db.
type ServerStore interface {
	SaveServer(config.ServerConfig) error
	RemoveServer(name string) error
}

// NewServerManagementHandlers creates server management handlers.
func NewServerManagementHandlers(pool *upstream.Pool, store ServerStore, onChanged func()) *ServerManagementHandlers {
	return &ServerManagementHandlers{pool: pool, store: store, onChanged: onChanged}
}

// SetConfigNotifier wires PG LISTEN/NOTIFY for cross-pod server change notification.
func (h *ServerManagementHandlers) SetConfigNotifier(cn serverConfigNotifier) {
	h.configNotifier = cn
}

// RegisterRoutes mounts server management routes on the mux.
func (h *ServerManagementHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/servers/add", h.handleAdd)
	mux.HandleFunc("/admin/v1/servers/remove/", h.handleRemove)
	mux.HandleFunc("/admin/v1/servers/config/", h.handleConfig)
	mux.HandleFunc("/admin/v1/servers/update/", h.handleUpdate)
}

func (h *ServerManagementHandlers) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var sc config.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&sc); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
		return
	}

	if sc.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "name is required"})
		return
	}
	if sc.Transport == "" {
		if sc.URL != "" {
			sc.Transport = "http"
		} else if sc.Command != "" {
			sc.Transport = "stdio"
		} else {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "transport, url, or command is required"})
			return
		}
	}

	ctx := context.Background()
	if err := h.pool.AddServer(ctx, sc); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Persist to DB
	h.saveServer(sc)

	// Notify proxy
	if h.onChanged != nil {
		h.onChanged()
	}

	// Get tool count
	toolCount := 0
	for _, s := range h.pool.Statuses() {
		if s.Name == sc.Name {
			toolCount = s.ToolCount
			break
		}
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "connected",
		"name":      sc.Name,
		"toolCount": toolCount,
	})
}

func (h *ServerManagementHandlers) handleRemove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	name := strings.TrimPrefix(r.URL.Path, "/admin/v1/servers/remove/")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "server name required"})
		return
	}

	if err := h.pool.RemoveServer(name); err != nil {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Remove from DB
	h.removeServer(name)

	// Notify proxy
	if h.onChanged != nil {
		h.onChanged()
	}

	json.NewEncoder(w).Encode(map[string]string{"status": "removed", "name": name})
}

func (h *ServerManagementHandlers) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	name := strings.TrimPrefix(r.URL.Path, "/admin/v1/servers/config/")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "server name required"})
		return
	}

	cfg, ok := h.pool.GetServerConfig(name)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "server not found"})
		return
	}

	// Redact secrets
	redacted := redactConfig(cfg)
	json.NewEncoder(w).Encode(redacted)
}

func (h *ServerManagementHandlers) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	name := strings.TrimPrefix(r.URL.Path, "/admin/v1/servers/update/")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "server name required"})
		return
	}

	var partial config.ServerConfig
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid body"})
		return
	}

	ctx := context.Background()
	if err := h.pool.UpdateServer(ctx, name, partial); err != nil {
		if strings.Contains(err.Error(), "not found") {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	// Persist updated config to DB
	if updated, ok := h.pool.GetServerConfig(name); ok {
		h.saveServer(updated)
	}

	// Notify proxy
	if h.onChanged != nil {
		h.onChanged()
	}

	// Get updated status
	toolCount := 0
	status := "connected"
	for _, s := range h.pool.Statuses() {
		if s.Name == name {
			toolCount = s.ToolCount
			status = s.Status
			break
		}
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status":    status,
		"name":      name,
		"toolCount": toolCount,
	})
}

// redactConfig returns a copy of the config with secrets replaced by "****".
func redactConfig(cfg config.ServerConfig) config.ServerConfig {
	if cfg.Auth == nil {
		return cfg
	}
	redacted := cfg
	authCopy := *cfg.Auth
	if authCopy.Token != "" {
		authCopy.Token = "****"
	}
	if authCopy.Password != "" {
		authCopy.Password = "****"
	}
	if authCopy.ClientSecret != "" {
		authCopy.ClientSecret = "****"
	}
	if len(authCopy.Headers) > 0 {
		masked := make(map[string]string, len(authCopy.Headers))
		for k := range authCopy.Headers {
			masked[k] = "****"
		}
		authCopy.Headers = masked
	}
	redacted.Auth = &authCopy
	return redacted
}

func (h *ServerManagementHandlers) saveServer(sc config.ServerConfig) {
	if h.store == nil {
		return
	}
	if err := h.store.SaveServer(sc); err != nil {
		slog.Error("failed to persist server", "component", "server-mgmt", "server", sc.Name, "error", err)
	}
	h.notifyPeers()
}

func (h *ServerManagementHandlers) removeServer(name string) {
	if h.store == nil {
		return
	}
	if err := h.store.RemoveServer(name); err != nil {
		slog.Error("failed to remove server from DB", "component", "server-mgmt", "server", name, "error", err)
	}
	h.notifyPeers()
}

func (h *ServerManagementHandlers) notifyPeers() {
	if h.configNotifier != nil {
		if err := h.configNotifier.NotifyChange("servers", nil); err != nil {
			slog.Warn("failed to notify server change", "component", "server-mgmt", "error", err)
		}
	}
}
