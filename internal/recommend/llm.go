package recommend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/config"
)

// LLMRecommender generates recommendations via LLM analysis.
type LLMRecommender struct {
	cfg    config.LLMConfig
	client *http.Client
}

// NewLLMRecommender creates an LLM recommender. Returns nil if not enabled.
// Validates provider name and warns about missing API key.
func NewLLMRecommender(cfg config.LLMConfig) *LLMRecommender {
	if !cfg.Enabled {
		return nil
	}

	// Validate provider
	switch cfg.Provider {
	case "openai", "anthropic", "ollama":
		// valid
	default:
		slog.Error("invalid LLM provider — must be openai, anthropic, or ollama", "component", "recommend", "provider", cfg.Provider)
		return nil
	}

	// Warn about missing API key (not required for ollama)
	if cfg.Provider != "ollama" && cfg.APIKeyEnv == "" {
		slog.Warn("LLM api_key_env not set — LLM calls will fail", "component", "recommend", "provider", cfg.Provider)
	}

	return &LLMRecommender{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// LLMInput is the context sent to the LLM for analysis.
type LLMInput struct {
	ToolProfiles     any `json:"tool_profiles"`
	BehaviorSummary  any `json:"behavior_summary"`
	SignalsSummary   any `json:"signals_summary"`
	CurrentRules     any `json:"current_rules"`
}

// LLMOutput is the expected response from the LLM.
type LLMOutput struct {
	Title          string `json:"title"`
	Description    string `json:"description"`
	Evidence       string `json:"evidence"`
	SuggestedTool  string `json:"suggested_tool"`
	SuggestedAction string `json:"suggested_action"`
	Severity       string `json:"severity"`
}

// Generate sends context to the LLM and parses recommendations.
func (r *LLMRecommender) Generate(ctx context.Context, input LLMInput) ([]Recommendation, error) {
	apiKey := os.Getenv(r.cfg.APIKeyEnv)
	if apiKey == "" {
		return nil, fmt.Errorf("LLM API key not set: env %s", r.cfg.APIKeyEnv)
	}

	prompt := buildPrompt(input)

	var recs []Recommendation
	var err error

	switch r.cfg.Provider {
	case "openai":
		recs, err = r.callOpenAI(ctx, apiKey, prompt)
	case "anthropic":
		recs, err = r.callAnthropic(ctx, apiKey, prompt)
	case "ollama":
		recs, err = r.callOllama(ctx, prompt)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", r.cfg.Provider)
	}

	if err != nil {
		slog.Warn("LLM provider error", "component", "recommend", "provider", r.cfg.Provider, "error", err)
		return nil, err
	}

	// Tag all as LLM-sourced
	for i := range recs {
		recs[i].Source = "llm"
	}

	return recs, nil
}

func buildPrompt(input LLMInput) string {
	toolsJSON, _ := json.Marshal(input.ToolProfiles)
	behaviorJSON, _ := json.Marshal(input.BehaviorSummary)
	signalsJSON, _ := json.Marshal(input.SignalsSummary)
	rulesJSON, _ := json.Marshal(input.CurrentRules)

	return fmt.Sprintf(`You are a security analyst reviewing AI agent tool usage through an MCP gateway.

The gateway enforces governance in 4 tiers:
- Tier 1 (Access Control): Which agents see which servers (allowed_servers)
- Tier 2 (Tool Visibility): Hide specific tools or classifications from agents (action: "hide")
- Tier 3 (Call Control): Approval, rate limits, argument-based blocking (actions: "require_approval", "rate_limited", "block")
- Tier 4 (Default Action): What happens when no rule matches ("allow" or "block")

When recommending rules, use the MOST APPROPRIATE tier:
- Use "hide" when a tool should NEVER be visible to an agent (e.g., destructive tools for untrusted agents)
- Use "block" ONLY for argument-based conditions (e.g., block when SQL contains DELETE)
- Use "require_approval" for sensitive operations that need human review
- Use "rate_limited" for tools that should be throttled (e.g., "100/hour")
- Use "allow" only in deny-by-default mode to whitelist specific tools

Here are the MCP tools available and their auto-classified risk levels:
%s

Here is a 24-hour behavioral summary:
%s

Here are the signals detected today:
%s

Current rules in place:
%s

Based on this data:
1. What HIGH/CRITICAL tools lack governance rules?
2. What rules would you recommend, using the appropriate tier?
3. Are there behavioral anomalies or suspicious patterns?

Return recommendations as a JSON array with fields: title, description, evidence, suggested_tool, suggested_action, severity.
Return ONLY the JSON array, no markdown or explanation.`, string(toolsJSON), string(behaviorJSON), string(signalsJSON), string(rulesJSON))
}

func (r *LLMRecommender) callOpenAI(ctx context.Context, apiKey, prompt string) ([]Recommendation, error) {
	body := map[string]any{
		"model": r.cfg.Model,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0.3,
	}
	data, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	return r.doRequest(req)
}

func (r *LLMRecommender) callAnthropic(ctx context.Context, apiKey, prompt string) ([]Recommendation, error) {
	body := map[string]any{
		"model":      r.cfg.Model,
		"max_tokens": 2048,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	data, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	return r.doRequest(req)
}

func (r *LLMRecommender) callOllama(ctx context.Context, prompt string) ([]Recommendation, error) {
	url := r.cfg.URL
	if url == "" {
		url = "http://localhost:11434"
	}

	body := map[string]any{
		"model":  r.cfg.Model,
		"prompt": prompt,
		"stream": false,
	}
	data, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, "POST", url+"/api/generate", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")

	return r.doRequest(req)
}

func (r *LLMRecommender) doRequest(req *http.Request) ([]Recommendation, error) {
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LLM request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("LLM returned %d: %s", resp.StatusCode, string(respBody))
	}

	// Extract content from various provider response formats
	content := extractContent(respBody)
	if content == "" {
		return nil, fmt.Errorf("no content in LLM response")
	}

	// Parse JSON array of recommendations
	var outputs []LLMOutput
	if err := json.Unmarshal([]byte(content), &outputs); err != nil {
		return nil, fmt.Errorf("parse LLM output: %w", err)
	}

	var recs []Recommendation
	for _, out := range outputs {
		recs = append(recs, Recommendation{
			ID:          generateRecID(),
			Type:        "llm_insight",
			Severity:    out.Severity,
			Title:       out.Title,
			Description: out.Description,
			Evidence:    out.Evidence,
			SuggestedRule: config.RuleConfig{
				Tool:   out.SuggestedTool,
				Action: out.SuggestedAction,
				Reason: out.Title,
			},
			ImpactScore: 50, // LLM recs get moderate default score
			Source:      "llm",
			Status:      "active",
			CreatedAt:   time.Now(),
		})
	}

	return recs, nil
}

// extractContent extracts the text content from provider-specific response formats.
func extractContent(body []byte) string {
	// Try OpenAI format: {choices: [{message: {content: "..."}}]}
	var openai struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &openai) == nil && len(openai.Choices) > 0 {
		return openai.Choices[0].Message.Content
	}

	// Try Anthropic format: {content: [{text: "..."}]}
	var anthropic struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if json.Unmarshal(body, &anthropic) == nil && len(anthropic.Content) > 0 {
		return anthropic.Content[0].Text
	}

	// Try Ollama format: {response: "..."}
	var ollama struct {
		Response string `json:"response"`
	}
	if json.Unmarshal(body, &ollama) == nil && ollama.Response != "" {
		return ollama.Response
	}

	return string(body)
}
