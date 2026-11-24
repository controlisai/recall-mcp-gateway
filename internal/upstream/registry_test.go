package upstream

import (
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

func makeTool(name, desc string) mcp.Tool {
	return mcp.NewTool(name, mcp.WithDescription(desc))
}

func TestRegistry_NoCollisions(t *testing.T) {
	r := NewRegistry()

	servers := map[string][]mcp.Tool{
		"database": {makeTool("query", "Run SQL query"), makeTool("insert", "Insert row")},
		"slack":    {makeTool("send_message", "Send Slack message")},
	}

	collisions := r.Build(servers)
	if collisions != 0 {
		t.Errorf("collisions = %d, want 0", collisions)
	}
	if r.ToolCount() != 3 {
		t.Errorf("tool count = %d, want 3", r.ToolCount())
	}

	// All tools should have original names (no prefix)
	route, ok := r.Resolve("query")
	if !ok {
		t.Fatal("query not found")
	}
	if route.ServerName != "database" {
		t.Errorf("server = %q, want database", route.ServerName)
	}
	if route.OriginalName != "query" {
		t.Errorf("original = %q, want query", route.OriginalName)
	}

	_, ok = r.Resolve("send_message")
	if !ok {
		t.Fatal("send_message not found")
	}
}

func TestRegistry_WithCollisions(t *testing.T) {
	r := NewRegistry()

	servers := map[string][]mcp.Tool{
		"database": {makeTool("query", "Database query"), makeTool("list", "List tables")},
		"search":   {makeTool("query", "Search query"), makeTool("index", "Index document")},
	}

	collisions := r.Build(servers)
	if collisions != 1 {
		t.Errorf("collisions = %d, want 1", collisions)
	}
	if r.ToolCount() != 4 {
		t.Errorf("tool count = %d, want 4", r.ToolCount())
	}

	// "query" should be prefixed for both servers
	dbRoute, ok := r.Resolve("database__query")
	if !ok {
		t.Fatal("database__query not found")
	}
	if dbRoute.ServerName != "database" {
		t.Errorf("server = %q, want database", dbRoute.ServerName)
	}
	if dbRoute.OriginalName != "query" {
		t.Errorf("original = %q, want query", dbRoute.OriginalName)
	}

	searchRoute, ok := r.Resolve("search__query")
	if !ok {
		t.Fatal("search__query not found")
	}
	if searchRoute.ServerName != "search" {
		t.Errorf("server = %q, want search", searchRoute.ServerName)
	}

	// Non-colliding tools should keep original names
	_, ok = r.Resolve("list")
	if !ok {
		t.Fatal("list not found")
	}
	_, ok = r.Resolve("index")
	if !ok {
		t.Fatal("index not found")
	}

	// Original unprefixed "query" should NOT resolve
	_, ok = r.Resolve("query")
	if ok {
		t.Error("unprefixed 'query' should not resolve when there are collisions")
	}
}

func TestRegistry_CollisionDescription(t *testing.T) {
	r := NewRegistry()

	servers := map[string][]mcp.Tool{
		"db":     {makeTool("query", "Run SQL")},
		"search": {makeTool("query", "Search docs")},
	}

	r.Build(servers)

	route, _ := r.Resolve("db__query")
	if route.Tool.Description != "[db] Run SQL" {
		t.Errorf("desc = %q, want '[db] Run SQL'", route.Tool.Description)
	}
}

func TestRegistry_Resolve_Unknown(t *testing.T) {
	r := NewRegistry()
	r.Build(map[string][]mcp.Tool{
		"test": {makeTool("foo", "Foo tool")},
	})

	_, ok := r.Resolve("nonexistent")
	if ok {
		t.Error("should not resolve unknown tool")
	}
}

func TestRegistry_Tools_ReturnsList(t *testing.T) {
	r := NewRegistry()
	r.Build(map[string][]mcp.Tool{
		"a": {makeTool("t1", "Tool 1"), makeTool("t2", "Tool 2")},
		"b": {makeTool("t3", "Tool 3")},
	})

	tools := r.Tools()
	if len(tools) != 3 {
		t.Errorf("tools = %d, want 3", len(tools))
	}
}

func TestRegistry_ServerTools(t *testing.T) {
	r := NewRegistry()
	r.Build(map[string][]mcp.Tool{
		"a": {makeTool("t1", ""), makeTool("t2", "")},
		"b": {makeTool("t3", "")},
	})

	aTools := r.ServerTools("a")
	if len(aTools) != 2 {
		t.Errorf("server a tools = %d, want 2", len(aTools))
	}

	bTools := r.ServerTools("b")
	if len(bTools) != 1 {
		t.Errorf("server b tools = %d, want 1", len(bTools))
	}
}

func TestRegistry_AnnotationsPreserved(t *testing.T) {
	r := NewRegistry()

	readOnly := true
	tool := mcp.NewTool("read_file")
	tool.Annotations.ReadOnlyHint = &readOnly
	tool.Annotations.Title = "Read a file"

	r.Build(map[string][]mcp.Tool{
		"fs": {tool},
	})

	route, ok := r.Resolve("read_file")
	if !ok {
		t.Fatal("read_file not found")
	}
	if route.Tool.Annotations.Title != "Read a file" {
		t.Errorf("title = %q, want 'Read a file'", route.Tool.Annotations.Title)
	}
	if route.Tool.Annotations.ReadOnlyHint == nil || !*route.Tool.Annotations.ReadOnlyHint {
		t.Error("readOnlyHint should be true")
	}
}

func TestRegistry_AnnotationOverride(t *testing.T) {
	r := NewRegistry()

	destructive := true
	r.SetAnnotationOverrides(map[string][]config.ToolAnnotationOverride{
		"db": {
			{Tool: "db_delete*", DestructiveHint: &destructive},
		},
	})

	tool := mcp.NewTool("db_delete_rows")
	r.Build(map[string][]mcp.Tool{
		"db": {tool},
	})

	route, ok := r.Resolve("db_delete_rows")
	if !ok {
		t.Fatal("db_delete_rows not found")
	}
	if route.Tool.Annotations.DestructiveHint == nil || !*route.Tool.Annotations.DestructiveHint {
		t.Error("destructiveHint should be overridden to true")
	}
}

func TestRegistry_AnnotationOverrideNilFieldsPreserved(t *testing.T) {
	r := NewRegistry()

	// Override only destructiveHint — readOnlyHint should be untouched
	destructive := false
	r.SetAnnotationOverrides(map[string][]config.ToolAnnotationOverride{
		"db": {
			{Tool: "query", DestructiveHint: &destructive},
		},
	})

	readOnly := true
	tool := mcp.NewTool("query")
	tool.Annotations.ReadOnlyHint = &readOnly

	r.Build(map[string][]mcp.Tool{
		"db": {tool},
	})

	route, _ := r.Resolve("query")
	if route.Tool.Annotations.ReadOnlyHint == nil || !*route.Tool.Annotations.ReadOnlyHint {
		t.Error("readOnlyHint should be preserved from upstream")
	}
	if route.Tool.Annotations.DestructiveHint == nil || *route.Tool.Annotations.DestructiveHint {
		t.Error("destructiveHint should be overridden to false")
	}
}

func TestRegistry_ToolAnnotationsGetter(t *testing.T) {
	r := NewRegistry()

	readOnly := true
	tool := mcp.NewTool("read_file")
	tool.Annotations.ReadOnlyHint = &readOnly
	tool.Annotations.Title = "Read a file"

	r.Build(map[string][]mcp.Tool{
		"fs": {tool, mcp.NewTool("write_file")},
	})

	annots := r.ToolAnnotations()
	if len(annots) != 2 {
		t.Errorf("expected 2 annotations, got %d", len(annots))
	}
	if annots["read_file"].Title != "Read a file" {
		t.Errorf("read_file title = %q, want 'Read a file'", annots["read_file"].Title)
	}
}

func TestRegistry_Rebuild(t *testing.T) {
	r := NewRegistry()

	// Initial build
	r.Build(map[string][]mcp.Tool{
		"a": {makeTool("t1", "")},
	})
	if r.ToolCount() != 1 {
		t.Fatalf("initial count = %d, want 1", r.ToolCount())
	}

	// Rebuild with more tools (simulates reconnection adding tools)
	r.Build(map[string][]mcp.Tool{
		"a": {makeTool("t1", ""), makeTool("t2", "")},
		"b": {makeTool("t3", "")},
	})
	if r.ToolCount() != 3 {
		t.Errorf("rebuilt count = %d, want 3", r.ToolCount())
	}
}
