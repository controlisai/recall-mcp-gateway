package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/proxy"
)

func TestAgentHandlers_GetIncludesToolFields(t *testing.T) {
	agents := []config.AgentConfig{
		{
			ID:           "bot1",
			TokenHash:    proxy.HashToken("t1"),
			BlockedTools: []string{"delete_record"},
			HiddenTools:  []string{"list_tables"},
		},
	}
	h := NewAgentHandlers(agents, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/agents/bot1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /agents/bot1 status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	blocked, ok := resp["blockedTools"].([]any)
	if !ok || len(blocked) != 1 || blocked[0] != "delete_record" {
		t.Errorf("blockedTools = %v, want [delete_record]", resp["blockedTools"])
	}
	hidden, ok := resp["hiddenTools"].([]any)
	if !ok || len(hidden) != 1 || hidden[0] != "list_tables" {
		t.Errorf("hiddenTools = %v, want [list_tables]", resp["hiddenTools"])
	}
}

func TestAgentHandlers_GetEmptyToolFields(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot1", TokenHash: proxy.HashToken("t1")},
	}
	h := NewAgentHandlers(agents, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/agents/bot1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	// Should return empty arrays, not null
	blocked, ok := resp["blockedTools"].([]any)
	if !ok || len(blocked) != 0 {
		t.Errorf("blockedTools = %v, want []", resp["blockedTools"])
	}
	hidden, ok := resp["hiddenTools"].([]any)
	if !ok || len(hidden) != 0 {
		t.Errorf("hiddenTools = %v, want []", resp["hiddenTools"])
	}
}

func TestAgentHandlers_UpdateTools(t *testing.T) {
	var updatedAgents []config.AgentConfig
	agents := []config.AgentConfig{
		{ID: "bot1", TokenHash: proxy.HashToken("t1"), Source: "dashboard"},
	}
	h := NewAgentHandlers(agents, func(a []config.AgentConfig) {
		updatedAgents = a
	})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"blockedTools":["delete_record","send_sms"],"hiddenTools":["list_tables"]}`
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/agents/bot1/tools", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT /agents/bot1/tools status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["status"] != "updated" {
		t.Errorf("status = %v, want updated", resp["status"])
	}

	// Verify onUpdate was called
	if len(updatedAgents) != 1 {
		t.Fatalf("onUpdate agents = %d, want 1", len(updatedAgents))
	}
	if len(updatedAgents[0].BlockedTools) != 2 {
		t.Errorf("blockedTools = %v, want [delete_record send_sms]", updatedAgents[0].BlockedTools)
	}
	if len(updatedAgents[0].HiddenTools) != 1 {
		t.Errorf("hiddenTools = %v, want [list_tables]", updatedAgents[0].HiddenTools)
	}
}

func TestAgentHandlers_UpdateToolsOverlap(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot1", TokenHash: proxy.HashToken("t1")},
	}
	h := NewAgentHandlers(agents, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Same tool in both blocked and hidden — should fail
	body := `{"blockedTools":["delete_record"],"hiddenTools":["delete_record"]}`
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/agents/bot1/tools", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT overlap status = %d, want 400", w.Code)
	}
}

func TestAgentHandlers_UpdateToolsNotFound(t *testing.T) {
	h := NewAgentHandlers(nil, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"blockedTools":["x"],"hiddenTools":[]}`
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/agents/nonexistent/tools", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("PUT not found status = %d, want 404", w.Code)
	}
}

func TestAgentHandlers_PutAgentOverlapValidation(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot1", TokenHash: proxy.HashToken("t1"), HiddenTools: []string{"tool_x"}},
	}
	h := NewAgentHandlers(agents, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Try to block a tool that's already hidden via PUT /agents/{id}
	body := `{"blockedTools":["tool_x"]}`
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/agents/bot1", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT agent overlap status = %d, want 400", w.Code)
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["error"] == "" {
		t.Error("expected overlap error message")
	}
}

func TestAgentHandlers_PutAgentBothToolFields(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot1", TokenHash: proxy.HashToken("t1")},
	}
	h := NewAgentHandlers(agents, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Both set, with overlap — should fail
	body := `{"blockedTools":["tool_x"],"hiddenTools":["tool_x"]}`
	req := httptest.NewRequest(http.MethodPut, "/admin/v1/agents/bot1", bytes.NewReader([]byte(body)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("PUT agent both-overlap status = %d, want 400", w.Code)
	}
}

func TestAgentHandlers_ListIncludesToolFields(t *testing.T) {
	agents := []config.AgentConfig{
		{
			ID:           "bot1",
			TokenHash:    proxy.HashToken("t1"),
			BlockedTools: []string{"x"},
			HiddenTools:  []string{"y"},
		},
	}
	h := NewAgentHandlers(agents, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/agents", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp struct {
		Agents []struct {
			ID           string   `json:"id"`
			BlockedTools []string `json:"blockedTools"`
			HiddenTools  []string `json:"hiddenTools"`
		} `json:"agents"`
	}
	json.NewDecoder(w.Body).Decode(&resp)

	if len(resp.Agents) != 1 {
		t.Fatalf("agents = %d, want 1", len(resp.Agents))
	}
	if len(resp.Agents[0].BlockedTools) != 1 || resp.Agents[0].BlockedTools[0] != "x" {
		t.Errorf("blockedTools = %v, want [x]", resp.Agents[0].BlockedTools)
	}
	if len(resp.Agents[0].HiddenTools) != 1 || resp.Agents[0].HiddenTools[0] != "y" {
		t.Errorf("hiddenTools = %v, want [y]", resp.Agents[0].HiddenTools)
	}
}
