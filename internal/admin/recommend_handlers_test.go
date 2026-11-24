package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/recommend"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
)

func setupRecommendTest(t *testing.T) (*recommend.Engine, *rules.Engine, *http.ServeMux) {
	t.Helper()
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete user permanently")),
	})

	tracker := baseline.NewTracker()
	ruleEngine := rules.NewEngine(nil)
	recEngine := recommend.NewEngine(profiler, tracker, ruleEngine)

	// Provide an agent with server access so coverage analysis produces recommendations
	recEngine.SetAgentProvider(func() []config.AgentConfig {
		return []config.AgentConfig{
			{ID: "test-agent", AllowedServers: []string{"db"}},
		}
	})

	recEngine.Generate(nil) // generate initial recs

	handlers := NewRecommendHandlers(recEngine, ruleEngine)
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	return recEngine, ruleEngine, mux
}

func TestRecommendHandlers_List(t *testing.T) {
	_, _, mux := setupRecommendTest(t)

	req := httptest.NewRequest("GET", "/admin/v1/recommendations", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	count := int(body["count"].(float64))
	if count == 0 {
		t.Error("expected at least one recommendation")
	}
}

func TestRecommendHandlers_Apply(t *testing.T) {
	recEngine, ruleEngine, mux := setupRecommendTest(t)

	recs := recEngine.Active()
	if len(recs) == 0 {
		t.Fatal("need recs")
	}

	req := httptest.NewRequest("POST", "/admin/v1/recommendations/"+recs[0].ID+"/apply", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}

	// Rule should be added
	if ruleEngine.RuleCount() == 0 {
		t.Error("expected rule to be added after apply")
	}
}

func TestRecommendHandlers_Dismiss(t *testing.T) {
	recEngine, _, mux := setupRecommendTest(t)

	recs := recEngine.Active()
	if len(recs) == 0 {
		t.Fatal("need recs")
	}

	req := httptest.NewRequest("POST", "/admin/v1/recommendations/"+recs[0].ID+"/dismiss", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "dismissed" {
		t.Errorf("status = %v", body["status"])
	}
}

func TestRecommendHandlers_NotFound(t *testing.T) {
	_, _, mux := setupRecommendTest(t)

	req := httptest.NewRequest("POST", "/admin/v1/recommendations/nonexistent/apply", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}
