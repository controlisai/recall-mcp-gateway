package platform

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_IsConfigured(t *testing.T) {
	c1 := NewClient("", "")
	if c1 != nil {
		t.Error("should return nil for empty URL")
	}

	c2 := NewClient("http://localhost:8082", "token")
	if !c2.IsConfigured() {
		t.Error("should be configured")
	}
}

func TestClient_FetchAgentHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]AgentHealth{
			{AgentID: "agent1", TrustLevel: "MEDIUM", ComplianceScore: 850, ComplianceBand: "HEALTHY"},
			{AgentID: "agent2", TrustLevel: "LOW", ComplianceScore: 450, ComplianceBand: "AT_RISK"},
		})
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token")
	agents, err := client.FetchAgentHealth(context.Background(), "product1")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("agents = %d, want 2", len(agents))
	}
	if agents[0].ComplianceBand != "HEALTHY" {
		t.Errorf("band = %q, want HEALTHY", agents[0].ComplianceBand)
	}
}

func TestClient_FetchSummary(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		json.NewEncoder(w).Encode([]any{}) // empty arrays for all endpoints
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token")
	summary := client.FetchSummary(context.Background(), "product1", "http://localhost:3000")

	if !summary.Connected {
		t.Error("should be connected")
	}
	if summary.ConsoleURL != "http://localhost:3000" {
		t.Errorf("consoleUrl = %q", summary.ConsoleURL)
	}
}

func TestClient_FetchAgentHealth_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "token")
	_, err := client.FetchAgentHealth(context.Background(), "product1")
	if err == nil {
		t.Error("expected error for 500 response")
	}
}
