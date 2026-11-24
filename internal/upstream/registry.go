package upstream

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// ToolRoute maps a (possibly prefixed) tool name to its upstream server and original name.
type ToolRoute struct {
	ServerName   string
	OriginalName string
	Tool         mcp.Tool
}

// Registry merges tools from multiple upstream servers into a single namespace.
// It detects name collisions and prefixes tool names with the server name when needed.
type Registry struct {
	mu                  sync.RWMutex
	routes              map[string]ToolRoute // exposedName -> route
	tools               []mcp.Tool           // merged tool list for tools/list
	annotationOverrides map[string][]config.ToolAnnotationOverride // serverName -> overrides
}

// NewRegistry creates an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		routes: make(map[string]ToolRoute),
	}
}

// Build constructs the merged tool list from the given servers.
// It detects collisions and prefixes tool names when two servers expose the same tool name.
func (r *Registry) Build(servers map[string][]mcp.Tool) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Phase 1: detect collisions
	nameToServers := make(map[string][]string)
	for serverName, tools := range servers {
		for _, t := range tools {
			nameToServers[t.Name] = append(nameToServers[t.Name], serverName)
		}
	}

	collisions := make(map[string]bool)
	for name, srvs := range nameToServers {
		if len(srvs) > 1 {
			collisions[name] = true
		}
	}

	// Phase 2: build routes with prefix for collisions
	r.routes = make(map[string]ToolRoute)
	r.tools = nil

	for serverName, tools := range servers {
		for _, t := range tools {
			exposedName := t.Name
			if collisions[t.Name] {
				exposedName = serverName + "__" + t.Name
			}

			// Clone tool with exposed name
			exposed := t
			exposed.Name = exposedName
			if collisions[t.Name] {
				exposed.Description = fmt.Sprintf("[%s] %s", serverName, t.Description)
			}

			// Apply config annotation overrides for this server
			if overrides, ok := r.annotationOverrides[serverName]; ok {
				applyAnnotationOverrides(&exposed, overrides)
			}

			r.routes[exposedName] = ToolRoute{
				ServerName:   serverName,
				OriginalName: t.Name,
				Tool:         exposed,
			}
			r.tools = append(r.tools, exposed)
		}
	}

	return len(collisions)
}

// Tools returns the merged tool list for serving tools/list.
func (r *Registry) Tools() []mcp.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools
}

// Resolve looks up a tool by its exposed name and returns the routing info.
func (r *Registry) Resolve(exposedName string) (ToolRoute, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	route, ok := r.routes[exposedName]
	return route, ok
}

// ToolCount returns the total number of tools in the registry.
func (r *Registry) ToolCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// SetAnnotationOverrides sets per-server annotation override configs.
// Must be called before Build() for overrides to take effect.
func (r *Registry) SetAnnotationOverrides(overrides map[string][]config.ToolAnnotationOverride) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.annotationOverrides = overrides
}

// ToolAnnotations returns a map of exposed tool name to its annotation hints.
func (r *Registry) ToolAnnotations() map[string]mcp.ToolAnnotation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]mcp.ToolAnnotation, len(r.tools))
	for _, t := range r.tools {
		result[t.Name] = t.Annotations
	}
	return result
}

// applyAnnotationOverrides applies config-defined overrides to a tool's annotations.
// Nil fields in the override mean "keep upstream value".
func applyAnnotationOverrides(tool *mcp.Tool, overrides []config.ToolAnnotationOverride) {
	for _, ov := range overrides {
		matched, err := filepath.Match(ov.Tool, tool.Name)
		if err != nil {
			slog.Warn("invalid annotation override glob pattern", "component", "registry", "pattern", ov.Tool, "error", err)
			continue
		}
		if !matched {
			continue
		}
		if ov.Title != "" {
			tool.Annotations.Title = ov.Title
		}
		if ov.ReadOnlyHint != nil {
			tool.Annotations.ReadOnlyHint = ov.ReadOnlyHint
		}
		if ov.DestructiveHint != nil {
			tool.Annotations.DestructiveHint = ov.DestructiveHint
		}
		if ov.IdempotentHint != nil {
			tool.Annotations.IdempotentHint = ov.IdempotentHint
		}
		if ov.OpenWorldHint != nil {
			tool.Annotations.OpenWorldHint = ov.OpenWorldHint
		}
	}
}

// ServerTools returns all tools from a specific server.
func (r *Registry) ServerTools(serverName string) []mcp.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []mcp.Tool
	for _, route := range r.routes {
		if route.ServerName == serverName {
			result = append(result, route.Tool)
		}
	}
	return result
}
