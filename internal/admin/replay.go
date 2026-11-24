package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

// ReplayDeps holds dependencies for the replay endpoint.
type ReplayDeps struct {
	Pool    *upstream.Pool
	Auditor audit.AuditStore
}

// ReplayHandler serves POST /admin/v1/calls/:requestId/replay.
func ReplayHandler(deps ReplayDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Extract requestId from path
		path := strings.TrimPrefix(r.URL.Path, "/admin/v1/calls/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) < 1 || parts[0] == "" {
			http.Error(w, `{"error":"missing requestId"}`, http.StatusBadRequest)
			return
		}
		requestID := parts[0]

		// Find the original call in audit history
		var original *audit.Entry
		for _, entry := range deps.Auditor.Recent() {
			if entry.RequestID == requestID {
				e := entry
				original = &e
				break
			}
		}
		if original == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(map[string]string{"error": "call not found in recent history"})
			return
		}

		// Parse optional modified args
		var body struct {
			ModifiedArgs map[string]any `json:"modifiedArgs"`
		}
		json.NewDecoder(r.Body).Decode(&body)

		args := original.Arguments
		if body.ModifiedArgs != nil {
			args = body.ModifiedArgs
		}

		// Replay to upstream (bypass rules)
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()

		result, err := deps.Pool.CallTool(ctx, original.Tool, args)

		w.Header().Set("Content-Type", "application/json")

		resp := map[string]any{
			"requestId":       requestID,
			"tool":            original.Tool,
			"server":          original.Server,
			"originalSuccess": !original.IsError,
			"argumentsUsed":   args,
		}

		if err != nil {
			resp["replaySuccess"] = false
			resp["replayError"] = err.Error()
		} else {
			resp["replaySuccess"] = !result.IsError
			if len(result.Content) > 0 {
				resp["replayResult"] = result.Content
			}
		}

		// Log replay as audit entry
		deps.Auditor.Log(audit.Entry{
			Timestamp: time.Now().UTC(),
			RequestID: requestID + "-replay",
			AgentID:   "admin",
			Tool:      original.Tool,
			Server:    original.Server,
			Decision:  "allow",
			Mode:      "replay",
			Arguments: args,
		})

		json.NewEncoder(w).Encode(resp)
	}
}

// ExportConfigHandler serves GET /admin/v1/export-config.
func ExportConfigHandler(port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		format := r.URL.Query().Get("for")
		w.Header().Set("Content-Type", "application/json")

		switch format {
		case "claude-desktop", "claude":
			json.NewEncoder(w).Encode(map[string]any{
				"mcpServers": map[string]any{
					"recall-mcp-gateway": map[string]any{
						"url": "http://localhost:" + strings.TrimLeft(r.Host, ":") + "/mcp",
					},
				},
			})
		case "cursor":
			json.NewEncoder(w).Encode(map[string]any{
				"mcpServers": map[string]any{
					"recall-mcp-gateway": map[string]any{
						"url": "http://localhost:" + strings.TrimLeft(r.Host, ":") + "/mcp",
					},
				},
			})
		default:
			json.NewEncoder(w).Encode(map[string]string{
				"error": "specify ?for=claude-desktop or ?for=cursor",
			})
		}
	}
}
