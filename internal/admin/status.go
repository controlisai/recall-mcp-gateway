package admin

import (
	"encoding/json"
	"net/http"

	"github.com/controlisai/recall-mcp-gateway/internal/alerts"
	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

// StatusDeps holds dependencies for the status endpoint.
type StatusDeps struct {
	Pool          *upstream.Pool
	RuleEngine    *rules.Engine
	Tracker       *baseline.Tracker
	Latency       *baseline.LatencyTracker
	AlertDetector *alerts.Detector
	AgentCount    int
}

// StatusHandler serves GET /admin/v1/status.
func StatusHandler(deps StatusDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		servers := deps.Pool.Statuses()
		serverHealth := make([]map[string]any, len(servers))
		for i, s := range servers {
			sh := map[string]any{
				"name":            s.Name,
				"status":          s.Status,
				"toolCount":       s.ToolCount,
				"circuit_state":   s.CircuitState,
				"reconnect_count": s.ReconnectCount,
			}
			if !s.LastReconnectAt.IsZero() {
				sh["last_reconnect_at"] = s.LastReconnectAt
			}
			if deps.Latency != nil {
				if health := deps.Latency.Get(s.Name); health != nil {
					sh["meanLatencyMs"] = health.MeanLatencyMs
					sh["currentLatencyMs"] = health.CurrentLatencyMs
					sh["healthStatus"] = health.Status
				}
			}
			serverHealth[i] = sh
		}

		status := map[string]any{
			"servers":        serverHealth,
			"serverCount":    len(servers),
			"toolCount":      deps.Pool.ToolCount(),
			"ruleCount":      deps.RuleEngine.RuleCount(),
			"agentCount":     deps.AgentCount,
			"baselineStatus": "unknown",
		}

		if deps.Tracker != nil {
			status["baselineStatus"] = deps.Tracker.Status()
		}
		if deps.AlertDetector != nil {
			status["alerts"] = deps.AlertDetector.Recent()
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(status)
	}
}
