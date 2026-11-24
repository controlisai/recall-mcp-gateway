package alerts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestWebhookSender_Send(t *testing.T) {
	var received WebhookPayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	sender := NewWebhookSender(srv.URL, "http://localhost:8090/dashboard")

	alert := Alert{
		Type:     "rate_spike",
		Severity: "critical",
		AgentID:  "agent1",
		Tool:     "query",
		Message:  "Agent calling query 50x (baseline: 8x)",
	}

	err := sender.Send(alert, []RecentCall{
		{Tool: "query", ArgsPreview: "SELECT *", At: "10:30"},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if received.Type != "rate_spike" {
		t.Errorf("type = %q, want rate_spike", received.Type)
	}
	if received.DashboardURL != "http://localhost:8090/dashboard" {
		t.Errorf("dashboardUrl = %q", received.DashboardURL)
	}
	if len(received.RecentCalls) != 1 {
		t.Errorf("recentCalls = %d, want 1", len(received.RecentCalls))
	}
}

func TestWebhookSender_NoURL(t *testing.T) {
	sender := NewWebhookSender("", "")
	err := sender.Send(Alert{Message: "test"}, nil)
	if err != nil {
		t.Errorf("expected no error with empty URL: %v", err)
	}
}

func TestWebhookSender_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	sender := NewWebhookSender(srv.URL, "")

	err := sender.Send(Alert{
		Type: "test", Timestamp: time.Now(),
	}, nil)
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestWebhookSender_IsConfigured(t *testing.T) {
	s1 := NewWebhookSender("", "")
	if s1.IsConfigured() {
		t.Error("should not be configured with empty URL")
	}

	s2 := NewWebhookSender("https://hooks.slack.com/xxx", "")
	if !s2.IsConfigured() {
		t.Error("should be configured")
	}
}
