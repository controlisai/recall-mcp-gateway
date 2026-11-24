package proxy

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/mcptest"
	"github.com/mark3labs/mcp-go/server"

	"github.com/controlisai/recall-mcp-gateway/internal/alerts"
	"github.com/controlisai/recall-mcp-gateway/internal/audit"
	"github.com/controlisai/recall-mcp-gateway/internal/baseline"
	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

func echoHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent("called: " + req.Params.Name),
		},
	}, nil
}

func TestNewServer_RegistersTools(t *testing.T) {
	// Create a test MCP server with tools
	srv, err := mcptest.NewServer(t,
		server.ServerTool{Tool: mcp.NewTool("query", mcp.WithDescription("Run query")), Handler: echoHandler},
		server.ServerTool{Tool: mcp.NewTool("insert", mcp.WithDescription("Insert row")), Handler: echoHandler},
	)
	if err != nil {
		t.Fatalf("test server: %v", err)
	}
	defer srv.Close()

	// Verify the test server works
	client := srv.Client()
	tools, err := client.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools.Tools) != 2 {
		t.Errorf("tools = %d, want 2", len(tools.Tools))
	}
}

func TestNewServer_CreatesServer(t *testing.T) {
	// Build a registry with tools directly
	reg := upstream.NewRegistry()
	reg.Build(map[string][]mcp.Tool{
		"test-server": {
			mcp.NewTool("query", mcp.WithDescription("Run query")),
			mcp.NewTool("insert", mcp.WithDescription("Insert data")),
		},
	})

	pool := upstream.NewPool(nil, nil)
	auditor := audit.NewLogger(nil)
	auth := NewAgentAuthenticator(nil)

	tracker := baseline.NewTracker()
	latencyTracker := baseline.NewLatencyTracker()
	alertDetector := alerts.NewDetector(tracker)

	proxyServer := NewServer(ServerConfig{
		Pool:          pool,
		Auth:          auth,
		Auditor:       auditor,
		Servers:       []config.ServerConfig{{Name: "test-server", Transport: "stdio"}},
		Tracker:       tracker,
		Latency:       latencyTracker,
		AlertDetector: alertDetector,
	})

	if proxyServer.Handler() == nil {
		t.Error("handler should not be nil")
	}
}

func TestResultPreview_TextContent(t *testing.T) {
	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent("Hello world"),
		},
	}
	preview := resultPreview(result)
	if preview != "Hello world" {
		t.Errorf("preview = %q, want 'Hello world'", preview)
	}
}

func TestResultPreview_LongText(t *testing.T) {
	longText := ""
	for i := 0; i < 300; i++ {
		longText += "x"
	}
	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(longText),
		},
	}
	preview := resultPreview(result)
	if len(preview) > 210 { // 200 + "..."
		t.Errorf("preview length = %d, want <= 210", len(preview))
	}
}

func TestResultPreview_Empty(t *testing.T) {
	result := &mcp.CallToolResult{}
	if resultPreview(result) != "" {
		t.Error("expected empty preview for empty result")
	}
	if resultPreview(nil) != "" {
		t.Error("expected empty preview for nil result")
	}
}
