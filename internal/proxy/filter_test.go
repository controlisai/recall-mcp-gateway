package proxy

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

func TestToolFilter_NoRestriction(t *testing.T) {
	filter := NewToolFilter(nil)
	reg := upstream.NewRegistry()
	reg.Build(map[string][]mcp.Tool{
		"db": {mcp.NewTool("query", mcp.WithDescription("q"))},
	})

	tools := reg.Tools()
	visible := filter.Filter("any-agent", tools, reg)
	if len(visible) != 1 {
		t.Errorf("visible = %d, want 1", len(visible))
	}
}

func TestToolFilter_AllowedServers(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot1", TokenHash: HashToken("t1"), AllowedServers: []string{"db"}},
	}
	filter := NewToolFilter(agents)

	reg := upstream.NewRegistry()
	reg.Build(map[string][]mcp.Tool{
		"db":    {mcp.NewTool("query", mcp.WithDescription("q"))},
		"slack": {mcp.NewTool("send_msg", mcp.WithDescription("s"))},
	})

	tools := reg.Tools()

	// bot1 only sees db tools
	visible := filter.Filter("bot1", tools, reg)
	if len(visible) != 1 {
		t.Errorf("bot1 visible = %d, want 1", len(visible))
	}
	if visible[0].Name != "query" {
		t.Errorf("visible tool = %q, want query", visible[0].Name)
	}

	// unknown agent sees everything
	visible2 := filter.Filter("other-bot", tools, reg)
	if len(visible2) != 2 {
		t.Errorf("other-bot visible = %d, want 2", len(visible2))
	}
}

func TestToolFilter_BlockedTools(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "bot1", TokenHash: HashToken("t1"), BlockedTools: []string{"delete_record"}},
	}
	filter := NewToolFilter(agents)

	if !filter.IsBlocked("bot1", "delete_record") {
		t.Error("expected delete_record to be blocked for bot1")
	}
	if filter.IsBlocked("bot1", "query_records") {
		t.Error("expected query_records to NOT be blocked for bot1")
	}
	if filter.IsBlocked("other-bot", "delete_record") {
		t.Error("expected delete_record to NOT be blocked for other-bot")
	}
}

func TestToolFilter_HiddenTools(t *testing.T) {
	agents := []config.AgentConfig{
		{
			ID:          "bot1",
			TokenHash:   HashToken("t1"),
			HiddenTools: []string{"list_tables"},
		},
	}
	filter := NewToolFilter(agents)

	// Hidden tools should be in the hiddenTools map
	if hidden, ok := filter.hiddenTools["bot1"]; !ok || !hidden["list_tables"] {
		t.Error("expected list_tables to be hidden for bot1")
	}

	// Hidden tools should NOT be blocked (they're callable, just not listed)
	if filter.IsBlocked("bot1", "list_tables") {
		t.Error("hidden tools should not be blocked")
	}
}

func TestToolFilter_BlockedAndHiddenSeparate(t *testing.T) {
	agents := []config.AgentConfig{
		{
			ID:           "bot1",
			TokenHash:    HashToken("t1"),
			AllowedServers: []string{"db"},
			BlockedTools: []string{"delete_record"},
			HiddenTools:  []string{"list_tables"},
		},
	}
	filter := NewToolFilter(agents)

	reg := upstream.NewRegistry()
	reg.Build(map[string][]mcp.Tool{
		"db": {
			mcp.NewTool("query_records", mcp.WithDescription("q")),
			mcp.NewTool("delete_record", mcp.WithDescription("d")),
			mcp.NewTool("list_tables", mcp.WithDescription("l")),
		},
	})

	// Blocked → IsBlocked true, hidden → IsBlocked false
	if !filter.IsBlocked("bot1", "delete_record") {
		t.Error("delete_record should be blocked")
	}
	if filter.IsBlocked("bot1", "list_tables") {
		t.Error("list_tables should NOT be blocked (it's hidden)")
	}
	if filter.IsBlocked("bot1", "query_records") {
		t.Error("query_records should NOT be blocked")
	}
}
