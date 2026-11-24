package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/metrics"
	"github.com/controlisai/recall-mcp-gateway/internal/sanitize"
)

const (
	maxAutoRegisteredIntents = 100
	maxIntentNameLen         = 128
	maxAgentIDLen            = 64
)

var validEntityNameRe = regexp.MustCompile(`^[a-zA-Z0-9_\-\.]+$`)

// BridgeConfig holds the engine connection parameters.
type BridgeConfig struct {
	EngineURL                string
	ConsoleAPIURL            string
	APIKey                   string
	AgentID                  string
	OrgID                    string
	ProductID                string
	BlockOnEngineUnavailable bool
	AutoRegister             bool
}

// DecideResponse holds the relevant fields from the engine's /api/v1/decide response.
type DecideResponse struct {
	Proceed   bool   `json:"proceed"`
	Decision  string `json:"decision"`
	Cause     string `json:"cause"`
	Narrative struct {
		Summary  string `json:"summary"`
		Guidance string `json:"guidance"`
	} `json:"narrative"`
	DecisionID string `json:"decisionId,omitempty"`
}

// CheckToolCallOpts carries optional context for a governance check.
type CheckToolCallOpts struct {
	FlowID       string
	ParentFlowID string
}

// Bridge handles communication with the Recall engine for governance decisions.
type Bridge struct {
	cfg                BridgeConfig
	client             *http.Client
	registeredIntents  atomic.Int32
}

// NewBridge creates a new engine bridge.
func NewBridge(cfg BridgeConfig) *Bridge {
	return &Bridge{
		cfg:    cfg,
		client: &http.Client{Timeout: 100 * time.Millisecond},
	}
}

// IsConfigured returns true if an engine URL is set.
func (b *Bridge) IsConfigured() bool {
	return b.cfg.EngineURL != ""
}

// DecideForTool evaluates a tool call with product-scoped API key routing.
// This is the two-layer evaluation entry point for connected mode.
func (b *Bridge) DecideForTool(ctx context.Context, agentID, toolName string, args map[string]any, apiKey, orgID string) (*DecideResponse, error) {
	if apiKey == "" {
		apiKey = b.cfg.APIKey
	}
	if orgID == "" {
		orgID = b.cfg.OrgID
	}

	payload := map[string]any{
		"agentId":     agentID,
		"intent":      toolName,
		"actionType":  "tool_invoke",
		"orgId":       orgID,
		"clientAttrs": args,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return b.fallback("marshal error: " + err.Error()), nil
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.cfg.EngineURL+"/api/v1/decide", bytes.NewReader(body))
	if err != nil {
		return b.fallback("request error: " + err.Error()), nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := b.client.Do(req)
	if err != nil {
		slog.Error("engine unavailable for two-layer eval", "component", "engine", "error", sanitize.Error(err.Error()))
		return b.fallback("engine unavailable"), nil
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// 4xx = real rejection, don't fall back
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		var result DecideResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return &DecideResponse{
				Proceed:  false,
				Decision: "BLOCK",
				Narrative: struct {
					Summary  string `json:"summary"`
					Guidance string `json:"guidance"`
				}{
					Summary:  fmt.Sprintf("Engine rejected: %d", resp.StatusCode),
					Guidance: string(respBody),
				},
			}, nil
		}
		return &result, nil
	}

	if resp.StatusCode != http.StatusOK {
		return b.fallback(fmt.Sprintf("engine returned %d", resp.StatusCode)), nil
	}

	var result DecideResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return b.fallback("decode error: " + err.Error()), nil
	}
	return &result, nil
}

// decideResult wraps the HTTP response from the engine for internal routing.
type decideResult struct {
	response   *DecideResponse
	statusCode int
	rawBody    string
}

// CheckToolCall evaluates whether a tool call should proceed.
func (b *Bridge) CheckToolCall(ctx context.Context, toolName string, args map[string]any, opts *CheckToolCallOpts) (*DecideResponse, error) {
	dr := b.doDecide(ctx, toolName, args, opts)

	// Auto-registration: if engine returns 404 (snapshot not found / unknown entity),
	// validate names, check caps, register the intent and retry once.
	if b.cfg.AutoRegister && b.cfg.ConsoleAPIURL != "" && b.cfg.ProductID != "" {
		if dr.statusCode == http.StatusNotFound {
			if !isValidEntityName(toolName, maxIntentNameLen) {
				slog.Warn("skipping auto-register: invalid intent name", "component", "engine", "intent", toolName)
			} else if !isValidEntityName(b.cfg.AgentID, maxAgentIDLen) {
				slog.Warn("skipping auto-register: invalid agent ID", "component", "engine", "agentId", b.cfg.AgentID)
			} else if int(b.registeredIntents.Load()) >= maxAutoRegisteredIntents {
				slog.Warn("auto-register intent cap reached", "component", "engine", "cap", maxAutoRegisteredIntents)
			} else {
				slog.Info("auto-registering intent and retrying", "component", "engine", "intent", toolName)
				b.autoRegisterIntent(ctx, toolName)
				b.autoRegisterAgent(ctx)
				dr = b.doDecide(ctx, toolName, args, opts)
			}
		}
	}

	return dr.response, nil
}

func (b *Bridge) doDecide(ctx context.Context, toolName string, args map[string]any, opts *CheckToolCallOpts) *decideResult {
	payload := map[string]any{
		"agentId":     b.cfg.AgentID,
		"intent":      toolName,
		"actionType":  "tool_invoke",
		"orgId":       b.cfg.OrgID,
		"clientAttrs": args,
	}
	if opts != nil {
		if opts.FlowID != "" {
			payload["flowId"] = opts.FlowID
		}
		if opts.ParentFlowID != "" {
			payload["parentFlowId"] = opts.ParentFlowID
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return &decideResult{response: b.fallback("marshal error: " + err.Error()), statusCode: 0}
	}

	req, err := http.NewRequestWithContext(ctx, "POST", b.cfg.EngineURL+"/api/v1/decide", bytes.NewReader(body))
	if err != nil {
		return &decideResult{response: b.fallback("request error: " + err.Error()), statusCode: 0}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.cfg.APIKey)

	resp, err := b.client.Do(req)
	if err != nil {
		slog.Error("engine unavailable", "component", "engine", "error", sanitize.Error(err.Error()))
		metrics.EngineRequestsTotal.WithLabelValues("error").Inc()
		return &decideResult{response: b.fallback("engine unavailable"), statusCode: 0}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		slog.Warn("engine returned non-200", "component", "engine", "status", resp.StatusCode, "body", string(respBody))
		metrics.EngineRequestsTotal.WithLabelValues("error").Inc()
		return &decideResult{
			response:   b.fallback(fmt.Sprintf("engine returned %d", resp.StatusCode)),
			statusCode: resp.StatusCode,
			rawBody:    string(respBody),
		}
	}

	metrics.EngineRequestsTotal.WithLabelValues("success").Inc()
	var result DecideResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return &decideResult{response: b.fallback("decode error: " + err.Error()), statusCode: resp.StatusCode}
	}
	return &decideResult{response: &result, statusCode: resp.StatusCode}
}

// autoRegisterIntent calls console-api to register a new intent with UNCLASSIFIED defaults.
func (b *Bridge) autoRegisterIntent(ctx context.Context, intentName string) {
	intentID := sanitizeID(intentName)

	payload := map[string]any{
		"id":                   intentID,
		"name":                 intentName,
		"riskClassification":   "UNCLASSIFIED",
		"dataSensitivity":      "UNCLASSIFIED",
		"authorityRequirement": "NONE",
		"blastRadius":          "LOW",
		"reversible":           true,
		"category":             "",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("auto-register intent marshal error", "component", "engine", "error", err)
		return
	}

	url := fmt.Sprintf("%s/api/v1/products/%s/intents", b.cfg.ConsoleAPIURL, b.cfg.ProductID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		slog.Error("auto-register intent request error", "component", "engine", "error", sanitize.Error(err.Error()))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.cfg.APIKey)

	resp, err := b.client.Do(req)
	if err != nil {
		slog.Error("auto-register intent failed", "component", "engine", "error", sanitize.Error(err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		slog.Info("auto-registered intent", "component", "engine", "intent", intentName)
		b.registeredIntents.Add(1)
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Warn("auto-register intent returned non-2xx", "component", "engine", "status", resp.StatusCode, "body", string(respBody))
	}
}

// autoRegisterAgent calls console-api to register the proxy's agent with default settings.
func (b *Bridge) autoRegisterAgent(ctx context.Context) {
	payload := map[string]any{
		"id":         b.cfg.AgentID,
		"name":       b.cfg.AgentID,
		"trustLevel": "LOW",
		"agentType":  "FULLY_AUTONOMOUS",
		"rateLimit":  100,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		slog.Error("auto-register agent marshal error", "component", "engine", "error", err)
		return
	}

	url := fmt.Sprintf("%s/api/v1/products/%s/agents", b.cfg.ConsoleAPIURL, b.cfg.ProductID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		slog.Error("auto-register agent request error", "component", "engine", "error", sanitize.Error(err.Error()))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.cfg.APIKey)

	resp, err := b.client.Do(req)
	if err != nil {
		slog.Error("auto-register agent failed", "component", "engine", "error", sanitize.Error(err.Error()))
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		slog.Info("auto-registered agent", "component", "engine", "agent", b.cfg.AgentID)
	} else {
		respBody, _ := io.ReadAll(resp.Body)
		slog.Warn("auto-register agent returned non-2xx", "component", "engine", "status", resp.StatusCode, "body", string(respBody))
	}
}

// isValidEntityName checks that a name is within length limit and contains only allowed characters.
func isValidEntityName(name string, maxLen int) bool {
	if name == "" || len(name) > maxLen {
		return false
	}
	return validEntityNameRe.MatchString(name)
}

// sanitizeID converts a tool name into a valid entity ID.
func sanitizeID(name string) string {
	id := strings.ReplaceAll(name, " ", "-")
	id = strings.ToLower(id)
	if len(id) > 64 {
		id = id[:64]
	}
	return id
}

// fallback returns a decision based on the fail-open/fail-closed config.
func (b *Bridge) fallback(reason string) *DecideResponse {
	if b.cfg.BlockOnEngineUnavailable {
		return &DecideResponse{
			Proceed:  false,
			Decision: "BLOCK",
			Narrative: struct {
				Summary  string `json:"summary"`
				Guidance string `json:"guidance"`
			}{
				Summary:  "Governance engine unavailable — action blocked (fail-closed mode)",
				Guidance: reason,
			},
		}
	}
	return &DecideResponse{
		Proceed:  true,
		Decision: "EXECUTE",
		Narrative: struct {
			Summary  string `json:"summary"`
			Guidance string `json:"guidance"`
		}{
			Summary:  "Governance engine unavailable — action allowed (fail-open mode)",
			Guidance: reason,
		},
	}
}
