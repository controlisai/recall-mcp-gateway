package detect

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// HeaderAnnotation describes a single x-mcp-header annotation on a tool parameter.
type HeaderAnnotation struct {
	ParamPath  string // JSON property path (e.g., "tenant_id" or "config.region")
	HeaderName string // header suffix (e.g., "TenantId" → becomes "Mcp-Param-TenantId")
	ParamType  string // "string", "integer", "boolean"
}

// ToolHeaderMap maps tool names to their x-mcp-header annotations.
type ToolHeaderMap map[string][]HeaderAnnotation

// ParseHeaderAnnotations scans tool schemas for x-mcp-header annotations.
// Returns a map of toolName → annotations. Only includes tools that have annotations.
func ParseHeaderAnnotations(tools []mcp.Tool) ToolHeaderMap {
	result := make(ToolHeaderMap)
	for _, tool := range tools {
		annotations := parseToolAnnotations(tool)
		if len(annotations) > 0 {
			result[tool.Name] = annotations
		}
	}
	return result
}

// parseToolAnnotations extracts x-mcp-header annotations from a single tool's inputSchema.
func parseToolAnnotations(tool mcp.Tool) []HeaderAnnotation {
	props := tool.InputSchema.Properties
	if len(props) == 0 {
		return nil
	}

	var annotations []HeaderAnnotation
	for name, prop := range props {
		propMap, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		headerName, ok := propMap["x-mcp-header"].(string)
		if !ok || headerName == "" {
			continue
		}

		// Validate type: only string, integer, boolean allowed
		paramType, _ := propMap["type"].(string)
		switch paramType {
		case "string", "integer", "boolean":
			// valid
		default:
			slog.Warn("x-mcp-header on unsupported type — skipping", "tool", tool.Name, "param", name, "type", paramType)
			continue
		}

		annotations = append(annotations, HeaderAnnotation{
			ParamPath:  name,
			HeaderName: headerName,
			ParamType:  paramType,
		})
	}
	return annotations
}

// AnnotationSummary returns a human-readable summary of header annotations per server.
func AnnotationSummary(headerMap ToolHeaderMap) string {
	if len(headerMap) == 0 {
		return "no x-mcp-header annotations"
	}
	totalAnnotations := 0
	for _, anns := range headerMap {
		totalAnnotations += len(anns)
	}
	tools := make([]string, 0, len(headerMap))
	for name := range headerMap {
		tools = append(tools, name)
	}
	return strings.Join(tools, ", ") + " (" + fmt.Sprintf("%d", totalAnnotations) + " annotations)"
}
