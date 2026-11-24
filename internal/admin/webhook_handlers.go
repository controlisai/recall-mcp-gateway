package admin

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/alerts"
	"github.com/controlisai/recall-mcp-gateway/internal/approval"
)

// WebhookHandlers serves webhook configuration endpoints.
type WebhookHandlers struct {
	approvalNotif *approval.WebhookNotifier
	alertWebhook  *alerts.WebhookSender
}

// NewWebhookHandlers creates webhook config handlers.
func NewWebhookHandlers(approvalNotif *approval.WebhookNotifier, alertWebhook *alerts.WebhookSender) *WebhookHandlers {
	return &WebhookHandlers{approvalNotif: approvalNotif, alertWebhook: alertWebhook}
}

// RegisterRoutes mounts webhook config endpoints.
func (h *WebhookHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/config/webhook", h.handleWebhookConfig)
	mux.HandleFunc("/admin/v1/config/webhook-test", h.handleWebhookTest)
}

func (h *WebhookHandlers) handleWebhookConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(map[string]any{
			"approvalWebhookUrl": h.approvalNotif.URL(),
			"alertWebhookUrl":    h.alertWebhook.URL(),
		})

	case http.MethodPut:
		var body struct {
			ApprovalWebhookURL string `json:"approvalWebhookUrl"`
			AlertWebhookURL    string `json:"alertWebhookUrl"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
			return
		}

		if body.ApprovalWebhookURL != "" {
			if u, err := url.Parse(body.ApprovalWebhookURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "approvalWebhookUrl must be http or https"})
				return
			}
			h.approvalNotif.SetURL(body.ApprovalWebhookURL)
			slog.Info("approval webhook URL updated", "component", "webhook")
		}
		if body.AlertWebhookURL != "" {
			if u, err := url.Parse(body.AlertWebhookURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				w.WriteHeader(http.StatusBadRequest)
				json.NewEncoder(w).Encode(map[string]string{"error": "alertWebhookUrl must be http or https"})
				return
			}
			h.alertWebhook.SetURL(body.AlertWebhookURL)
			slog.Info("alert webhook URL updated", "component", "webhook")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"status":             "saved",
			"approvalWebhookUrl": h.approvalNotif.URL(),
			"alertWebhookUrl":    h.alertWebhook.URL(),
			"note":               "Runtime only. Add to YAML config for persistence across restarts.",
		})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (h *WebhookHandlers) handleWebhookTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	var body struct {
		URL  string `json:"url"`
		Type string `json:"type"` // "approval" or "alert"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid request body"})
		return
	}

	if body.URL == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "url is required"})
		return
	}

	if u, err := url.Parse(body.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "webhook URL must be http or https"})
		return
	}

	// Send a test payload
	testPayload := map[string]any{
		"type":    "test",
		"message": "Test notification from Recall MCP Gateway",
		"time":    time.Now().UTC().Format(time.RFC3339),
	}
	data, _ := json.Marshal(testPayload)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(body.URL, "application/json", bytes.NewReader(data))
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{
			"status": "error",
			"error":  "delivery failed: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		json.NewEncoder(w).Encode(map[string]any{
			"status":     "error",
			"error":      "webhook returned " + resp.Status,
			"statusCode": resp.StatusCode,
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]any{
		"status":     "delivered",
		"statusCode": resp.StatusCode,
	})
}
