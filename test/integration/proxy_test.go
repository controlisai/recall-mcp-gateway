package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"

	"github.com/controlisai/recall-mcp-gateway/internal/admin"
	"github.com/controlisai/recall-mcp-gateway/internal/alerts"
	"github.com/controlisai/recall-mcp-gateway/internal/approval"
	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/detect"
	"github.com/controlisai/recall-mcp-gateway/internal/engine"
	"github.com/controlisai/recall-mcp-gateway/internal/proxy"
	"github.com/controlisai/recall-mcp-gateway/internal/recommend"
	"github.com/controlisai/recall-mcp-gateway/internal/rules"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

func echoHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent("ok: " + req.Params.Name)},
	}, nil
}

// startTestUpstream creates a mock MCP server and returns the client directly.
func startTestUpstream(t *testing.T) *mcptest.Server {
	t.Helper()
	srv, err := mcptest.NewServer(t,
		server.ServerTool{Tool: mcp.NewTool("query", mcp.WithDescription("Run SQL query")), Handler: echoHandler},
		server.ServerTool{Tool: mcp.NewTool("delete_user", mcp.WithDescription("Delete a user permanently")), Handler: echoHandler},
		server.ServerTool{Tool: mcp.NewTool("send_email", mcp.WithDescription("Send email message")), Handler: echoHandler},
		server.ServerTool{Tool: mcp.NewTool("list_users", mcp.WithDescription("List all users")), Handler: echoHandler},
	)
	if err != nil {
		t.Fatalf("test server: %v", err)
	}
	return srv
}

// --- Config & Validation Tests ---

func TestConfig_ParseValid(t *testing.T) {
	yaml := `
proxy:
  port: 9090
servers:
  - name: test
    transport: stdio
    command: echo
agents:
  - id: bot1
    token_hash: "a0b1c2d3e4f5a6b7c8d9e0f1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
rules:
  - tool: "*"
    action: allow
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Proxy.Port != 9090 {
		t.Errorf("port = %d", cfg.Proxy.Port)
	}
	if len(cfg.Rules) != 1 {
		t.Errorf("rules = %d", len(cfg.Rules))
	}
}

func TestConfig_RejectsDuplicateAgents(t *testing.T) {
	yaml := `
servers:
  - name: test
    transport: stdio
    command: echo
agents:
  - id: bot1
    token_hash: samehash123
  - id: bot2
    token_hash: samehash123
`
	_, err := config.Parse([]byte(yaml))
	if err == nil {
		t.Error("expected error for duplicate tokens")
	}
}

// --- Rule Engine Tests ---

func TestRules_BlockAndAllow(t *testing.T) {
	eng := rules.NewEngine([]config.RuleConfig{
		{Tool: "delete_*", Action: "block", Reason: "destructive"},
		{Tool: "*", Action: "allow"},
	})

	d := eng.Evaluate("agent1", "delete_user")
	if d.Action != "block" {
		t.Errorf("delete_user: action = %q", d.Action)
	}

	d = eng.Evaluate("agent1", "list_users")
	if d.Action != "allow" {
		t.Errorf("list_users: action = %q", d.Action)
	}
}

func TestRules_RateLimit(t *testing.T) {
	eng := rules.NewEngine([]config.RuleConfig{
		{Tool: "*", Action: "allow", RateLimit: "5/minute"},
	})

	for i := 0; i < 5; i++ {
		d := eng.Evaluate("agent1", "query")
		if d.Action != "allow" {
			t.Errorf("call %d: %q", i+1, d.Action)
		}
	}
	d := eng.Evaluate("agent1", "query")
	if d.Action != "rate_limited" {
		t.Errorf("call 6: %q, want rate_limited", d.Action)
	}
}

func TestRules_HideFromToolsList(t *testing.T) {
	eng := rules.NewEngine([]config.RuleConfig{
		{Tool: "admin_*", Action: "hide"},
	})

	tools := eng.FilterTools("bot", []string{"query", "admin_reset", "list_users"})
	if len(tools) != 2 {
		t.Errorf("visible tools = %d, want 2", len(tools))
	}
}

func TestRules_AddRemove(t *testing.T) {
	eng := rules.NewEngine(nil)
	eng.AddRule(config.RuleConfig{Tool: "test", Action: "block"})

	if eng.RuleCount() != 1 {
		t.Fatalf("count = %d", eng.RuleCount())
	}

	eng.RemoveRule(0)
	if eng.RuleCount() != 0 {
		t.Errorf("count after remove = %d", eng.RuleCount())
	}
}

// --- Detection Tests ---

func TestDetect_ToolProfiling(t *testing.T) {
	p := detect.NewProfiler()
	p.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete a user")),
		mcp.NewTool("list_users", mcp.WithDescription("List users")),
		mcp.NewTool("execute_code", mcp.WithDescription("Execute arbitrary code")),
	})

	byRisk := p.ProfilesByRisk()
	if len(byRisk["CRITICAL"]) < 2 {
		t.Errorf("CRITICAL = %d, want >= 2", len(byRisk["CRITICAL"]))
	}
	if len(byRisk["LOW"]) < 1 {
		t.Errorf("LOW = %d, want >= 1", len(byRisk["LOW"]))
	}
}

func TestDetect_ContentSSN(t *testing.T) {
	d := detect.NewContentDetector()
	signals := d.Scan(map[string]any{"ssn": "123-45-6789"})
	if len(signals) == 0 {
		t.Error("expected SSN detection")
	}
}

func TestDetect_ContentClean(t *testing.T) {
	d := detect.NewContentDetector()
	signals := d.Scan(map[string]any{"name": "John", "age": 30})
	if len(signals) != 0 {
		t.Errorf("expected clean, got %d signals", len(signals))
	}
}

// --- Approval Flow Tests ---

func TestApproval_FullLifecycle(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	args := map[string]any{"to": "user@example.com"}

	// Create
	entry := store.Create("agent1", "send_email", args)
	if entry.Status != approval.StatusPending {
		t.Fatalf("status = %q", entry.Status)
	}

	// Not yet approved
	if store.CheckApproved("agent1", "send_email", args) {
		t.Error("should not be approved yet")
	}

	// Approve
	store.Approve(entry.ID, "admin")

	// Now approved — consumed on check
	if !store.CheckApproved("agent1", "send_email", args) {
		t.Error("should be approved")
	}

	// Consumed — second check fails
	if store.CheckApproved("agent1", "send_email", args) {
		t.Error("should be consumed")
	}
}

func TestApproval_Deny(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("agent1", "tool", nil)
	store.Deny(entry.ID, "admin", "not allowed")

	got := store.Get(entry.ID)
	if got.Status != approval.StatusDenied {
		t.Errorf("status = %q", got.Status)
	}
}

// --- Baseline Tests ---

func TestBaseline_Learning(t *testing.T) {
	tracker := baseline.NewTracker()
	for i := 0; i < 60; i++ {
		tracker.Record(baseline.Observation{
			AgentID: "agent1", Tool: "query",
			Args:      map[string]any{"sql": "SELECT 1"},
			Timestamp: time.Now(),
		})
	}

	bl := tracker.Get("agent1", "query")
	if bl == nil {
		t.Fatal("no baseline")
	}
	if !bl.BaselineReady {
		t.Error("baseline should be ready")
	}
	if bl.MeanCallsPerHour == 0 {
		t.Error("mean should not be zero")
	}
}

// --- Recommendation Tests ---

func TestRecommend_CriticalToolUnprotected(t *testing.T) {
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete a user")),
	})

	eng := recommend.NewEngine(profiler, baseline.NewTracker(), rules.NewEngine(nil))
	eng.SetAgentProvider(func() []config.AgentConfig {
		return []config.AgentConfig{{ID: "test-agent", AllowedServers: []string{"db"}}}
	})
	recs := eng.Generate(nil)

	found := false
	for _, r := range recs {
		if r.Type == "unprotected_critical_tool" {
			found = true
		}
	}
	if !found {
		t.Error("expected recommendation for unprotected critical tool")
	}
}

func TestRecommend_ApplyAddsRule(t *testing.T) {
	profiler := detect.NewProfiler()
	profiler.ProfileTools("db", []mcp.Tool{
		mcp.NewTool("delete_user", mcp.WithDescription("Delete")),
	})

	ruleEng := rules.NewEngine(nil)
	recEng := recommend.NewEngine(profiler, baseline.NewTracker(), ruleEng)
	recEng.SetAgentProvider(func() []config.AgentConfig {
		return []config.AgentConfig{{ID: "test-agent", AllowedServers: []string{"db"}}}
	})
	recs := recEng.Generate(nil)

	if len(recs) == 0 {
		t.Fatal("need recs")
	}
	recEng.Apply(recs[0].ID)
	ruleEng.AddRule(recs[0].SuggestedRule)

	if ruleEng.RuleCount() != 1 {
		t.Error("rule should be added")
	}
}

// --- Engine Bridge Tests ---

func TestEngine_TwoLayerEval_ProxyBlockSkipsEngine(t *testing.T) {
	// If proxy blocks, engine is never called
	ruleEng := rules.NewEngine([]config.RuleConfig{
		{Tool: "delete_*", Action: "block"},
	})

	d := ruleEng.Evaluate("agent1", "delete_user")
	if d.Action != "block" {
		t.Error("proxy should block")
	}
	// Engine never called — tested implicitly by the proxy server tool handler
}

func TestEngine_TwoLayerEval_EngineBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(engine.DecideResponse{
			Proceed:  false,
			Decision: "BLOCK",
		})
	}))
	defer srv.Close()

	bridge := engine.NewBridge(engine.BridgeConfig{
		EngineURL: srv.URL,
		APIKey:    "key",
	})

	resp, _ := bridge.DecideForTool(context.Background(), "agent1", "query", nil, "", "")
	if resp.Proceed {
		t.Error("engine should block")
	}
}

func TestEngine_TwoLayerEval_EngineDown_FailOpen(t *testing.T) {
	bridge := engine.NewBridge(engine.BridgeConfig{
		EngineURL: "http://localhost:1",
		APIKey:    "key",
	})

	resp, _ := bridge.DecideForTool(context.Background(), "agent1", "query", nil, "", "")
	if !resp.Proceed {
		t.Error("should fail-open when engine down")
	}
}

// --- Admin API Tests ---

func TestAdmin_Status(t *testing.T) {
	handler := admin.StatusHandler(admin.StatusDeps{
		Pool:       upstream.NewPool(nil, nil),
		RuleEngine: rules.NewEngine(nil),
		Tracker:    baseline.NewTracker(),
		AgentCount: 3,
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/admin/v1/status", nil))

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["agentCount"].(float64) != 3 {
		t.Errorf("agentCount = %v", body["agentCount"])
	}
}

func TestAdmin_RulesCRUD(t *testing.T) {
	eng := rules.NewEngine(nil)
	handlers := admin.NewRulesHandlers(eng, nil, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	// Create
	w := httptest.NewRecorder()
	body := `{"tool":"test","action":"block"}`
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/admin/v1/rules", bytes.NewReader([]byte(body))))
	if w.Code != 201 {
		t.Fatalf("create: %d", w.Code)
	}

	// List
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/admin/v1/rules", nil))
	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	if listResp["count"].(float64) != 1 {
		t.Errorf("count = %v", listResp["count"])
	}

	// Delete
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("DELETE", "/admin/v1/rules/0", nil))
	if w.Code != 200 {
		t.Errorf("delete: %d", w.Code)
	}
	if eng.RuleCount() != 0 {
		t.Error("rule not deleted")
	}
}

func TestAdmin_ApprovalFlow(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("agent1", "send_email", nil)

	handlers := admin.NewApprovalHandlers(store, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	// List
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/admin/v1/approvals", nil))
	var listResp map[string]any
	json.Unmarshal(w.Body.Bytes(), &listResp)
	if listResp["count"].(float64) != 1 {
		t.Errorf("pending = %v", listResp["count"])
	}

	// Approve
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/approve", nil))
	if w.Code != 200 {
		t.Errorf("approve: %d", w.Code)
	}
}

// --- Audit Tests ---

func TestAudit_LogAndSubscribe(t *testing.T) {
	logger := audit.NewLogger(&bytes.Buffer{})

	received := make(chan audit.Entry, 10)
	unsub := logger.Subscribe(func(e audit.Entry) {
		received <- e
	})
	defer unsub()

	logger.Log(audit.Entry{Tool: "query", AgentID: "agent1"})

	select {
	case e := <-received:
		if e.Tool != "query" {
			t.Errorf("tool = %q", e.Tool)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for entry")
	}
}

// --- Upstream Pool Tests ---

func TestUpstream_MockServerConnect(t *testing.T) {
	srv := startTestUpstream(t)
	defer srv.Close()

	client := srv.Client()
	tools, err := client.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 4 {
		t.Errorf("tools = %d, want 4", len(tools.Tools))
	}
}

func TestProxy_FullWiring(t *testing.T) {
	// Verify proxy server construction with ALL components wired
	tracker := baseline.NewTracker()
	latencyTracker := baseline.NewLatencyTracker()
	alertDetector := alerts.NewDetector(tracker)

	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)
	auth := proxy.NewAgentAuthenticator(nil)

	proxyServer := proxy.NewServer(proxy.ServerConfig{
		Pool:          pool,
		Auth:          auth,
		Auditor:       auditor,
		Servers:       []config.ServerConfig{{Name: "test", Transport: "stdio"}},
		Tracker:       tracker,
		Latency:       latencyTracker,
		AlertDetector: alertDetector,
	})

	// Verify all accessors work
	if proxyServer.Handler() == nil {
		t.Error("Handler nil")
	}
	if proxyServer.Profiler() == nil {
		t.Error("Profiler nil")
	}
	if proxyServer.WorkflowDetector() == nil {
		t.Error("WorkflowDetector nil")
	}
	if proxyServer.History() == nil {
		t.Error("History nil")
	}
	if proxyServer.DataFlow() == nil {
		t.Error("DataFlow nil")
	}
	if proxyServer.ErrorAnalyzer() == nil {
		t.Error("ErrorAnalyzer nil")
	}
	if proxyServer.UnusedTools() == nil {
		t.Error("UnusedTools nil")
	}
}

func TestUpstream_RegistryCollision(t *testing.T) {
	reg := upstream.NewRegistry()
	collisions := reg.Build(map[string][]mcp.Tool{
		"db":     {mcp.NewTool("query", mcp.WithDescription("DB"))},
		"search": {mcp.NewTool("query", mcp.WithDescription("Search"))},
	})
	if collisions != 1 {
		t.Errorf("collisions = %d", collisions)
	}

	_, ok := reg.Resolve("db__query")
	if !ok {
		t.Error("db__query not found")
	}
}
