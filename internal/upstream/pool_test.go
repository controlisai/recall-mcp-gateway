package upstream

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// startTestServer creates a test MCP server with given tools, returning the server and its URL.
func startTestServer(t *testing.T, tools ...server.ServerTool) *mcptest.Server {
	t.Helper()
	srv, err := mcptest.NewServer(t, tools...)
	if err != nil {
		t.Fatalf("failed to create test server: %v", err)
	}
	return srv
}

func echoHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent("called: " + req.Params.Name),
		},
	}, nil
}

func TestPool_ConnectSingleServer(t *testing.T) {
	srv := startTestServer(t,
		server.ServerTool{Tool: mcp.NewTool("query", mcp.WithDescription("Run query")), Handler: echoHandler},
		server.ServerTool{Tool: mcp.NewTool("insert", mcp.WithDescription("Insert row")), Handler: echoHandler},
	)
	defer srv.Close()

	// Use the test server's client directly to verify it works
	client := srv.Client()
	tools, err := client.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 2 {
		t.Errorf("tools = %d, want 2", len(tools.Tools))
	}
}

func TestPool_ToolRegistry_NoCollisions(t *testing.T) {
	// Test the registry directly with tools from two virtual servers
	reg := NewRegistry()

	serverTools := map[string][]mcp.Tool{
		"database": {
			mcp.NewTool("query", mcp.WithDescription("Run SQL")),
			mcp.NewTool("insert", mcp.WithDescription("Insert row")),
		},
		"slack": {
			mcp.NewTool("send_message", mcp.WithDescription("Send message")),
			mcp.NewTool("list_channels", mcp.WithDescription("List channels")),
		},
	}

	collisions := reg.Build(serverTools)
	if collisions != 0 {
		t.Errorf("collisions = %d, want 0", collisions)
	}
	if reg.ToolCount() != 4 {
		t.Errorf("tool count = %d, want 4", reg.ToolCount())
	}

	// All tools accessible by original name
	for _, name := range []string{"query", "insert", "send_message", "list_channels"} {
		if _, ok := reg.Resolve(name); !ok {
			t.Errorf("tool %q should be resolvable", name)
		}
	}
}

func TestPool_ToolRegistry_WithCollisions(t *testing.T) {
	reg := NewRegistry()

	serverTools := map[string][]mcp.Tool{
		"database": {
			mcp.NewTool("query", mcp.WithDescription("SQL query")),
			mcp.NewTool("list", mcp.WithDescription("List tables")),
		},
		"search": {
			mcp.NewTool("query", mcp.WithDescription("Search query")),
			mcp.NewTool("index", mcp.WithDescription("Index doc")),
		},
	}

	collisions := reg.Build(serverTools)
	if collisions != 1 {
		t.Errorf("collisions = %d, want 1", collisions)
	}

	// Collided tool uses prefix
	dbRoute, ok := reg.Resolve("database__query")
	if !ok {
		t.Fatal("database__query not found")
	}
	if dbRoute.OriginalName != "query" {
		t.Errorf("original = %q, want query", dbRoute.OriginalName)
	}

	searchRoute, ok := reg.Resolve("search__query")
	if !ok {
		t.Fatal("search__query not found")
	}
	if searchRoute.ServerName != "search" {
		t.Errorf("server = %q, want search", searchRoute.ServerName)
	}

	// Non-collided tools keep original names
	if _, ok := reg.Resolve("list"); !ok {
		t.Error("list should resolve")
	}
	if _, ok := reg.Resolve("index"); !ok {
		t.Error("index should resolve")
	}
}

func TestPool_FailedConnection_MarksDown(t *testing.T) {
	pool := NewPool([]config.ServerConfig{
		{
			Name:      "bad-server",
			Transport: "http",
			URL:       "http://localhost:1", // Will fail to connect
		},
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool.Connect(ctx)

	statuses := pool.Statuses()
	if len(statuses) != 1 {
		t.Fatalf("statuses = %d, want 1", len(statuses))
	}
	if statuses[0].Status != "down" && statuses[0].Status != "reconnecting" {
		t.Errorf("status = %q, want down or reconnecting", statuses[0].Status)
	}

	// Pool should not be ready
	if pool.IsReady() {
		t.Error("pool should not be ready with all servers down")
	}

	// Tool calls should fail
	_, err := pool.CallTool(ctx, "anything", nil)
	if err == nil {
		t.Error("expected error calling tool on failed pool")
	}
}

func TestPool_CallTool_UnknownTool(t *testing.T) {
	pool := NewPool(nil, nil)

	_, err := pool.CallTool(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestPool_CallToolWithMRTP_UnknownTool(t *testing.T) {
	pool := NewPool(nil, nil)

	_, err := pool.CallToolWithMRTP(context.Background(), "nonexistent", nil, mcp.MultiRoundTripParams{})
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

// Compile-time verification that CallToolWithMRTP accepts MultiRoundTripParams
var _ = (*Client).CallToolWithMRTP
var _ = (*Pool).CallToolWithMRTP

func TestPool_CallToolWithMRTP_NotConnected(t *testing.T) {
	pool := NewPool(nil, nil)

	// Manually register a tool so route resolution succeeds but client lookup fails
	reg := pool.Registry()
	reg.Build(map[string][]mcp.Tool{
		"test-server": {mcp.NewTool("test_tool")},
	})

	_, err := pool.CallToolWithMRTP(context.Background(), "test_tool", nil, mcp.MultiRoundTripParams{
		RequestState: "some-state",
	})
	if err == nil {
		t.Error("expected error for not-connected server")
	}
}
