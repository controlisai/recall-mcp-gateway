package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

// ReauthDeps holds dependencies for the server re-auth endpoint.
type ReauthDeps struct {
	Pool         *upstream.Pool
	OnReconnect  func() // called after successful reconnection (e.g. to refresh proxy tools)
}

// ReauthHandler serves POST /admin/v1/servers/{name}/reauth.
// It clears the OAuth token for the named server and triggers re-authorization.
func ReauthHandler(deps ReauthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Extract server name from path: /admin/v1/servers/{name}/reauth
		path := strings.TrimPrefix(r.URL.Path, "/admin/v1/servers/")
		name := strings.TrimSuffix(path, "/reauth")
		if name == "" || strings.Contains(name, "/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "missing server name"})
			return
		}

		slog.Info("re-auth requested", "component", "admin", "server", name)

		if err := deps.Pool.ReconnectServer(r.Context(), name, true); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error":  "re-auth failed",
				"detail": err.Error(),
			})
			return
		}

		if deps.OnReconnect != nil {
			deps.OnReconnect()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"server": name,
			"status": "connected",
		})
	}
}
