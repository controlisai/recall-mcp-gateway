package approval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// WebhookPayload is sent to notify reviewers of a pending approval.
type WebhookPayload struct {
	ApprovalID  string         `json:"approvalId"`
	AgentID     string         `json:"agentId"`
	Tool        string         `json:"tool"`
	Arguments   map[string]any `json:"arguments"`
	ApproveURL  string         `json:"approveUrl"`
	DenyURL     string         `json:"denyUrl"`
	ExpiresAt   time.Time      `json:"expiresAt"`
	McpMethod   string         `json:"mcpMethod,omitempty"`
	McpName     string         `json:"mcpName,omitempty"`
	Text        string         `json:"text,omitempty"`        // human-readable description for Slack/notifications
	RequestedAt time.Time      `json:"requestedAt,omitempty"` // when the approval was created
	ActingFor   string         `json:"actingFor,omitempty"`   // delegation: who the agent acts for
}

// WebhookNotifier sends approval notifications to a webhook URL.
type WebhookNotifier struct {
	webhookURL string
	baseURL    string // proxy base URL for approve/deny links
	reviewer   string // identity embedded in signed URLs (e.g., "admin", "alice@example.com")
	signer     *SignedURLGenerator // if non-nil, generates signed approve/deny URLs
	client     *http.Client
}

// NewWebhookNotifier creates a notifier for approval webhooks.
func NewWebhookNotifier(webhookURL, baseURL string) *WebhookNotifier {
	return &WebhookNotifier{
		webhookURL: webhookURL,
		baseURL:    baseURL,
		reviewer:   "admin",
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// NewSignedWebhookNotifier creates a notifier that embeds signed approve/deny URLs.
func NewSignedWebhookNotifier(webhookURL, baseURL, reviewer string, signer *SignedURLGenerator) *WebhookNotifier {
	return &WebhookNotifier{
		webhookURL: webhookURL,
		baseURL:    baseURL,
		reviewer:   reviewer,
		signer:     signer,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify sends a webhook notification for a pending approval.
func (n *WebhookNotifier) Notify(entry *Entry) error {
	if n.webhookURL == "" {
		slog.Info("approval pending (no webhook configured)", "component", "approval", "approval_id", entry.ID, "agent_id", entry.AgentID, "tool", entry.Tool)
		return nil
	}

	text := fmt.Sprintf("Agent %s wants to call %s", entry.AgentID, entry.Tool)
	if entry.ActingFor != "" {
		text = fmt.Sprintf("Agent %s (acting for %s) wants to call %s", entry.AgentID, entry.ActingFor, entry.Tool)
	}
	if len(entry.Arguments) > 0 {
		argSummary, _ := json.Marshal(entry.Arguments)
		if len(argSummary) > 200 {
			argSummary = append(argSummary[:197], '.', '.', '.')
		}
		text += fmt.Sprintf(" with args: %s", string(argSummary))
	}

	// Build approve/deny URLs — signed if signer is available, plain otherwise
	var approveURL, denyURL string
	if n.signer != nil {
		approveURL, denyURL = n.signer.GeneratePair(entry.ID, n.reviewer, entry.ExpiresAt)
	} else {
		approveURL = fmt.Sprintf("%s/admin/v1/approvals/%s/approve", n.baseURL, entry.ID)
		denyURL = fmt.Sprintf("%s/admin/v1/approvals/%s/deny", n.baseURL, entry.ID)
	}

	payload := WebhookPayload{
		ApprovalID:  entry.ID,
		AgentID:     entry.AgentID,
		Tool:        entry.Tool,
		Arguments:   entry.Arguments,
		ApproveURL:  approveURL,
		DenyURL:     denyURL,
		ExpiresAt:   entry.ExpiresAt,
		McpMethod:   entry.McpMethod,
		McpName:     entry.McpName,
		Text:        text,
		RequestedAt: entry.CreatedAt,
		ActingFor:   entry.ActingFor,
	}

	// Use Slack Block Kit format for Slack webhook URLs
	var data []byte
	var marshalErr error
	if strings.Contains(n.webhookURL, "hooks.slack.com") {
		data, marshalErr = json.Marshal(buildSlackPayload(entry, text, approveURL, denyURL))
	} else {
		data, marshalErr = json.Marshal(payload)
	}
	if marshalErr != nil {
		return fmt.Errorf("marshal approval webhook: %w", marshalErr)
	}

	resp, err := n.client.Post(n.webhookURL, "application/json", bytes.NewReader(data))
	if err != nil {
		slog.Error("approval webhook delivery failed", "component", "approval", "error", err)
		return fmt.Errorf("approval webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("approval webhook returned %d", resp.StatusCode)
	}

	slog.Info("approval notified", "component", "approval", "approval_id", entry.ID, "agent_id", entry.AgentID, "tool", entry.Tool)
	return nil
}

// SetURL updates the webhook URL at runtime.
func (n *WebhookNotifier) SetURL(url string) {
	n.webhookURL = url
}

// URL returns the current webhook URL.
func (n *WebhookNotifier) URL() string {
	return n.webhookURL
}

// IsConfigured returns true if a webhook URL is set.
func (n *WebhookNotifier) IsConfigured() bool {
	return n.webhookURL != ""
}

// buildSlackPayload creates a Slack Block Kit message with interactive approve/deny buttons.
func buildSlackPayload(entry *Entry, text, approveURL, denyURL string) map[string]any {
	remaining := time.Until(entry.ExpiresAt).Round(time.Second)
	contextText := fmt.Sprintf("Expires in %s | <%s|Approve in browser> | <%s|Deny in browser>",
		remaining, approveURL, denyURL)

	return map[string]any{
		"blocks": []map[string]any{
			{
				"type": "section",
				"text": map[string]any{
					"type": "mrkdwn",
					"text": fmt.Sprintf("*Approval Required*\n%s", text),
				},
			},
			{
				"type":     "actions",
				"block_id": "approval_" + entry.ID,
				"elements": []map[string]any{
					{
						"type":      "button",
						"text":      map[string]any{"type": "plain_text", "text": "Approve"},
						"style":     "primary",
						"action_id": "approve_" + entry.ID,
						"value":     "approve",
					},
					{
						"type":      "button",
						"text":      map[string]any{"type": "plain_text", "text": "Deny"},
						"style":     "danger",
						"action_id": "deny_" + entry.ID,
						"value":     "deny",
					},
				},
			},
			{
				"type": "context",
				"elements": []map[string]any{
					{"type": "mrkdwn", "text": contextText},
				},
			},
		},
	}
}
