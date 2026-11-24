package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

// ReloadDeps holds dependencies for hot-reload.
type ReloadDeps struct {
	ConfigPath string
	RuleEngine *rules.Engine
	OnReload   func(cfg *config.Config) // optional callback for reloading agents, alerts, etc.
}

// ReloadHandler serves POST /admin/v1/reload — hot-reloads config from file.
// Reloads: rules, agents, alerts. Does NOT reload: servers, port, engine_url (require restart).
func ReloadHandler(deps ReloadDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if deps.ConfigPath == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "no config path — started in dashboard-only mode"})
			return
		}

		cfg, err := config.Load(deps.ConfigPath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		// Reload rules
		deps.RuleEngine.SetRules(cfg.Rules)

		// Notify other components
		if deps.OnReload != nil {
			deps.OnReload(cfg)
		}

		slog.Info("reloaded config", "component", "admin", "ruleCount", len(cfg.Rules), "agentCount", len(cfg.Agents), "alertCount", len(cfg.Alerts))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "reloaded",
			"ruleCount":  len(cfg.Rules),
			"agentCount": len(cfg.Agents),
			"alertCount": len(cfg.Alerts),
		})
	}
}
