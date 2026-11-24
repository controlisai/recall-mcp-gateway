package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

func TestRulesHandlers_List(t *testing.T) {
	engine := rules.NewEngine([]config.RuleConfig{
		{Tool: "*", Action: "allow"},
	})

	handlers := NewRulesHandlers(engine, nil, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/admin/v1/rules", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["count"].(float64) != 1 {
		t.Errorf("count = %v", body["count"])
	}
}

func TestRulesHandlers_Create(t *testing.T) {
	engine := rules.NewEngine(nil)
	handlers := NewRulesHandlers(engine, nil, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	body := `{"tool":"delete_*","action":"block","reason":"no destructive ops"}`
	req := httptest.NewRequest("POST", "/admin/v1/rules", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Errorf("status = %d, want 201", w.Code)
	}

	if engine.RuleCount() != 1 {
		t.Errorf("rules = %d, want 1", engine.RuleCount())
	}
}

func TestRulesHandlers_Delete(t *testing.T) {
	engine := rules.NewEngine([]config.RuleConfig{
		{Tool: "a", Action: "allow"},
		{Tool: "b", Action: "block"},
	})

	handlers := NewRulesHandlers(engine, nil, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/admin/v1/rules/0", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
	if engine.RuleCount() != 1 {
		t.Errorf("rules = %d, want 1", engine.RuleCount())
	}
}

func TestRulesHandlers_CreateWithActionsMap(t *testing.T) {
	engine := rules.NewEngine(nil)
	handlers := NewRulesHandlers(engine, nil, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	body := `{"tool":"delete_*","actions":{"autonomous":"require_approval","user_embedded":"require_consent","delegated":"require_approval"}}`
	req := httptest.NewRequest("POST", "/admin/v1/rules", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 201 {
		t.Errorf("status = %d, want 201", w.Code)
	}
	if engine.RuleCount() != 1 {
		t.Errorf("rules = %d, want 1", engine.RuleCount())
	}
}

func TestRulesHandlers_CreateMissingFields(t *testing.T) {
	engine := rules.NewEngine(nil)
	handlers := NewRulesHandlers(engine, nil, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	body := `{"tool":"test"}`
	req := httptest.NewRequest("POST", "/admin/v1/rules", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
