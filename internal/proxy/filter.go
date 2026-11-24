package proxy

import (
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
	"github.com/controlisai/recall-mcp-gateway/internal/upstream"
)

// ToolFilter filters the tools/list response per agent based on allowed_servers,
// blocked_tools, and hidden_tools config.
type ToolFilter struct {
	agentServers      map[string]map[string]bool // agentID -> set of allowed server names
	blockedTools      map[string]map[string]bool // agentID -> set of blocked tool names
	hiddenTools       map[string]map[string]bool // agentID -> set of hidden tool names
	acknowledgedTools map[string]map[string]bool // agentID -> set of acknowledged tool names
}

// NewToolFilter creates a filter from agent configs.
// Agents with no allowed_servers see all tools.
func NewToolFilter(agents []config.AgentConfig) *ToolFilter {
	as := make(map[string]map[string]bool)
	bt := make(map[string]map[string]bool)
	ht := make(map[string]map[string]bool)
	at := make(map[string]map[string]bool)
	for _, a := range agents {
		if len(a.AllowedServers) > 0 {
			set := make(map[string]bool, len(a.AllowedServers))
			for _, s := range a.AllowedServers {
				set[s] = true
			}
			as[a.ID] = set
		}
		if len(a.BlockedTools) > 0 {
			set := make(map[string]bool, len(a.BlockedTools))
			for _, t := range a.BlockedTools {
				set[t] = true
			}
			bt[a.ID] = set
		}
		if len(a.HiddenTools) > 0 {
			set := make(map[string]bool, len(a.HiddenTools))
			for _, t := range a.HiddenTools {
				set[t] = true
			}
			ht[a.ID] = set
		}
		if len(a.AcknowledgedTools) > 0 {
			set := make(map[string]bool, len(a.AcknowledgedTools))
			for _, t := range a.AcknowledgedTools {
				set[t] = true
			}
			at[a.ID] = set
		}
	}
	return &ToolFilter{agentServers: as, blockedTools: bt, hiddenTools: ht, acknowledgedTools: at}
}

// IsBlocked returns true if the tool is in the agent's blockedTools list.
func (f *ToolFilter) IsBlocked(agentID, toolName string) bool {
	if blocked, ok := f.blockedTools[agentID]; ok {
		return blocked[toolName]
	}
	return false
}

// Filter returns tools visible to the given agent.
// If the agent has allowed_servers configured, only tools from those servers are included.
// If no allowed_servers, all tools are returned.
func (f *ToolFilter) Filter(agentID string, allTools []mcp.Tool, registry *upstream.Registry) []mcp.Tool {
	allowed, hasRestriction := f.agentServers[agentID]
	if !hasRestriction {
		return allTools // no restriction — see everything
	}

	var visible []mcp.Tool
	for _, t := range allTools {
		route, ok := registry.Resolve(t.Name)
		if !ok {
			continue
		}
		if allowed[route.ServerName] {
			visible = append(visible, t)
		}
	}
	return visible
}
