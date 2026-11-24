package proxy

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
)

// listServersTool defines the built-in list_servers discovery tool.
func listServersTool() mcp.Tool {
	return mcp.NewTool(
		"list_servers",
		mcp.WithDescription("List all MCP servers connected through this proxy, their status, and the tools each server provides."),
	)
}

// serverInfo is the JSON structure returned by the list_servers tool.
type serverInfo struct {
	Name      string   `json:"name"`
	Status    string   `json:"status"`
	ToolCount int      `json:"tool_count"`
	Tools     []string `json:"tools"`
}

// listServersHandler handles the built-in list_servers tool call.
func (s *Server) listServersHandler(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	statuses := s.pool.Statuses()
	registry := s.pool.Registry()

	servers := make([]serverInfo, 0, len(statuses))
	for _, st := range statuses {
		tools := registry.ServerTools(st.Name)
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name
		}
		servers = append(servers, serverInfo{
			Name:      st.Name,
			Status:    st.Status,
			ToolCount: len(tools),
			Tools:     toolNames,
		})
	}

	data, err := json.MarshalIndent(servers, "", "  ")
	if err != nil {
		return governanceError(ErrCodeServerError, "", "marshal server list: %v", err), nil
	}

	return mcp.NewToolResultText(string(data)), nil
}
