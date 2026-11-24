package alerts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// WebhookSender delivers alerts to configured webhook URLs.
type WebhookSender struct {
	url          string
	dashboardURL string
	client       *http.Client
}

// NewWebhookSender creates a webhook sender.
func NewWebhookSender(url, dashboardURL string) *WebhookSender {
	return &WebhookSender{
		url:          url,
		dashboardURL: dashboardURL,
		client:       &http.Client{Timeout: 10 * time.Second},
	}
}

// Send delivers an alert to the webhook URL.
func (ws *WebhookSender) Send(alert Alert, recentCalls []RecentCall) error {
	if ws.url == "" {
		slog.Info("alert suppressed (no webhook configured)", "component", "alert", "severity", alert.Severity, "message", alert.Message)
		return nil
	}

	payload := WebhookPayload{
		Type:         alert.Type,
		Severity:     alert.Severity,
		AgentID:      alert.AgentID,
		Tool:         alert.Tool,
		Message:      alert.Message,
		Current:      alert.Current,
		Baseline:     alert.Baseline,
		Deviation:    alert.Deviation,
		DashboardURL: ws.dashboardURL,
		Timestamp:    alert.Timestamp,
		RecentCalls:  recentCalls,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook: %w", err)
	}

	resp, err := ws.client.Post(ws.url, "application/json", bytes.NewReader(data))
	if err != nil {
		slog.Error("webhook delivery failed", "component", "alert", "error", err)
		return fmt.Errorf("webhook POST: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		slog.Warn("webhook returned non-2xx status", "component", "alert", "status", resp.StatusCode)
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}

	return nil
}

// SetURL updates the webhook URL at runtime.
func (ws *WebhookSender) SetURL(url string) {
	ws.url = url
}

// URL returns the current webhook URL.
func (ws *WebhookSender) URL() string {
	return ws.url
}

// IsConfigured returns true if a webhook URL is set.
func (ws *WebhookSender) IsConfigured() bool {
	return ws.url != ""
}
