package admin

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

func TestStatusHandler(t *testing.T) {
	pool := upstream.NewPool(nil, nil)
	ruleEngine := rules.NewEngine(nil)
	tracker := baseline.NewTracker()

	handler := StatusHandler(StatusDeps{
		Pool:       pool,
		RuleEngine: ruleEngine,
		Tracker:    tracker,
		AgentCount: 2,
	})

	req := httptest.NewRequest("GET", "/admin/v1/status", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)

	if body["agentCount"].(float64) != 2 {
		t.Errorf("agentCount = %v", body["agentCount"])
	}
}
