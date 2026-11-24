package detect

import (
	"strings"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
)

// Profiler classifies tools by analyzing their name, description, and input schema.
type Profiler struct {
	mu       sync.RWMutex
	profiles map[string]*ToolProfile
}

// NewProfiler creates a new tool profiler.
func NewProfiler() *Profiler {
	return &Profiler{profiles: make(map[string]*ToolProfile)}
}

// ProfileTools analyzes all tools and stores their profiles.
func (p *Profiler) ProfileTools(serverName string, tools []mcp.Tool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, t := range tools {
		profile := classifyTool(serverName, t)
		p.profiles[t.Name] = profile
	}
}

// GetProfile returns the profile for a tool, or nil if not profiled.
func (p *Profiler) GetProfile(toolName string) *ToolProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.profiles[toolName]
}

// OverrideRiskLevel sets a manual risk level for a tool.
func (p *Profiler) OverrideRiskLevel(toolName, riskLevel string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	profile := p.profiles[toolName]
	if profile == nil {
		return false
	}
	profile.RiskLevel = riskLevel
	return true
}

// AllProfiles returns all tool profiles.
func (p *Profiler) AllProfiles() []*ToolProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]*ToolProfile, 0, len(p.profiles))
	for _, profile := range p.profiles {
		result = append(result, profile)
	}
	return result
}

// ProfilesByRisk returns profiles grouped by risk level.
func (p *Profiler) ProfilesByRisk() map[string][]*ToolProfile {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := map[string][]*ToolProfile{
		"CRITICAL": {},
		"HIGH":     {},
		"MEDIUM":   {},
		"LOW":      {},
	}
	for _, profile := range p.profiles {
		result[profile.RiskLevel] = append(result[profile.RiskLevel], profile)
	}
	return result
}

func classifyTool(serverName string, tool mcp.Tool) *ToolProfile {
	profile := &ToolProfile{
		Name:   tool.Name,
		Server: serverName,
	}

	text := strings.ToLower(tool.Name + " " + tool.Description)

	// Find the highest-priority classification that matches
	bestClass := ""
	bestPriority := -1

	for _, class := range ClassificationPriority {
		keywords := ClassificationKeywords[class]
		for _, kw := range keywords {
			if strings.Contains(text, kw) {
				priority := classificationIndex(class)
				if priority > bestPriority {
					bestClass = class
					bestPriority = priority
					profile.RiskReasons = append(profile.RiskReasons, "keyword: "+kw)
				}
				break // one match per classification is enough
			}
		}
	}

	if bestClass == "" {
		bestClass = "READ_ONLY" // default to safest
		profile.RiskReasons = append(profile.RiskReasons, "no risk keywords detected")
	}
	profile.Classification = bestClass
	profile.RiskLevel = RiskByClassification[bestClass]

	// Analyze input schema for unbounded inputs
	profile.InputRisk = analyzeInputRisk(tool)
	if profile.InputRisk == "UNBOUNDED" {
		profile.RiskReasons = append(profile.RiskReasons, "accepts freeform string input")
		profile.RiskLevel = elevateRisk(profile.RiskLevel)
	}

	return profile
}

func analyzeInputRisk(tool mcp.Tool) string {
	// Check if any property in the input schema is a freeform string
	for _, prop := range tool.InputSchema.Properties {
		propMap, ok := prop.(map[string]any)
		if !ok {
			continue
		}
		propType, _ := propMap["type"].(string)
		if propType == "string" {
			// Check if it has enum or pattern constraints
			if _, hasEnum := propMap["enum"]; hasEnum {
				continue // bounded
			}
			if _, hasPattern := propMap["pattern"]; hasPattern {
				continue // bounded
			}
			return "UNBOUNDED"
		}
		if propType == "array" {
			return "UNBOUNDED" // bulk potential
		}
	}
	return "BOUNDED"
}

func classificationIndex(class string) int {
	for i, c := range ClassificationPriority {
		if c == class {
			return i
		}
	}
	return -1
}

func elevateRisk(level string) string {
	switch level {
	case "LOW":
		return "MEDIUM"
	case "MEDIUM":
		return "HIGH"
	case "HIGH":
		return "CRITICAL"
	default:
		return level
	}
}
