package approval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWebhookNotifier_Notify(t *testing.T) {
	var received WebhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	notifier := NewWebhookNotifier(srv.URL, "http://localhost:8090")

	entry := &Entry{
		ID:        "apr-test123",
		AgentID:   "agent1",
		Tool:      "send_email",
		Arguments: map[string]any{"to": "user@example.com"},
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}

	err := notifier.Notify(entry)
	if err != nil {
		t.Fatalf("notify: %v", err)
	}

	if received.ApprovalID != "apr-test123" {
		t.Errorf("approvalId = %q", received.ApprovalID)
	}
	if received.AgentID != "agent1" {
		t.Errorf("agentId = %q", received.AgentID)
	}
	if !strings.Contains(received.ApproveURL, "apr-test123/approve") {
		t.Errorf("approveUrl = %q", received.ApproveURL)
	}
	if !strings.Contains(received.DenyURL, "apr-test123/deny") {
		t.Errorf("denyUrl = %q", received.DenyURL)
	}
}

func TestWebhookNotifier_NoURL(t *testing.T) {
	notifier := NewWebhookNotifier("", "")

	err := notifier.Notify(&Entry{ID: "test", AgentID: "a", Tool: "t"})
	if err != nil {
		t.Errorf("expected no error with empty URL: %v", err)
	}
}

func TestWebhookNotifier_IsConfigured(t *testing.T) {
	n1 := NewWebhookNotifier("", "")
	if n1.IsConfigured() {
		t.Error("should not be configured")
	}

	n2 := NewWebhookNotifier("https://hooks.slack.com/xxx", "")
	if !n2.IsConfigured() {
		t.Error("should be configured")
	}
}

func TestWebhookPayload_TextAndRequestedAt(t *testing.T) {
	var received WebhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, "http://localhost:8090")
	entry := &Entry{
		ID:        "apr-test",
		AgentID:   "bot-1",
		Tool:      "db_write",
		Arguments: map[string]any{"table": "users", "action": "delete"},
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	n.Notify(entry)

	if received.Text == "" {
		t.Error("expected non-empty Text in webhook payload")
	}
	if !strings.Contains(received.Text, "bot-1") {
		t.Errorf("Text should mention agent, got %q", received.Text)
	}
	if !strings.Contains(received.Text, "db_write") {
		t.Errorf("Text should mention tool, got %q", received.Text)
	}
	if received.RequestedAt.IsZero() {
		t.Error("expected non-zero RequestedAt")
	}
}
