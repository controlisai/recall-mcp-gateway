package engine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBridge_IsConfigured(t *testing.T) {
	b := NewBridge(BridgeConfig{})
	if b.IsConfigured() {
		t.Error("should not be configured with empty URL")
	}

	b2 := NewBridge(BridgeConfig{EngineURL: "http://localhost:8084"})
	if !b2.IsConfigured() {
		t.Error("should be configured")
	}
}

func TestBridge_DecideForTool_Allowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(DecideResponse{
			Proceed:  true,
			Decision: "EXECUTE",
		})
	}))
	defer srv.Close()

	bridge := NewBridge(BridgeConfig{
		EngineURL: srv.URL,
		APIKey:    "test-key",
		OrgID:     "default",
	})

	resp, err := bridge.DecideForTool(context.Background(), "agent1", "query", nil, "", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !resp.Proceed {
		t.Error("expected proceed=true")
	}
}

func TestBridge_DecideForTool_Blocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(DecideResponse{
			Proceed:  false,
			Decision: "BLOCK",
			Narrative: struct {
				Summary  string `json:"summary"`
				Guidance string `json:"guidance"`
			}{
				Summary: "Policy violation",
			},
		})
	}))
	defer srv.Close()

	bridge := NewBridge(BridgeConfig{
		EngineURL: srv.URL,
		APIKey:    "test-key",
	})

	resp, err := bridge.DecideForTool(context.Background(), "agent1", "delete_user", nil, "", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if resp.Proceed {
		t.Error("expected proceed=false")
	}
	if resp.Decision != "BLOCK" {
		t.Errorf("decision = %q, want BLOCK", resp.Decision)
	}
}

func TestBridge_DecideForTool_EngineDown_FailOpen(t *testing.T) {
	bridge := NewBridge(BridgeConfig{
		EngineURL:                "http://localhost:1", // unreachable
		APIKey:                   "test-key",
		BlockOnEngineUnavailable: false,
	})

	resp, err := bridge.DecideForTool(context.Background(), "agent1", "query", nil, "", "")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !resp.Proceed {
		t.Error("fail-open: expected proceed=true when engine down")
	}
}

func TestBridge_DecideForTool_UsesProductAPIKey(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(DecideResponse{Proceed: true, Decision: "EXECUTE"})
	}))
	defer srv.Close()

	bridge := NewBridge(BridgeConfig{
		EngineURL: srv.URL,
		APIKey:    "default-key",
	})

	// Call with product-specific key
	bridge.DecideForTool(context.Background(), "agent1", "query", nil, "product-key", "")

	if receivedAuth != "Bearer product-key" {
		t.Errorf("auth = %q, want 'Bearer product-key'", receivedAuth)
	}
}

func TestBridge_DecideForTool_FallsBackToDefaultKey(t *testing.T) {
	var receivedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(DecideResponse{Proceed: true, Decision: "EXECUTE"})
	}))
	defer srv.Close()

	bridge := NewBridge(BridgeConfig{
		EngineURL: srv.URL,
		APIKey:    "default-key",
	})

	bridge.DecideForTool(context.Background(), "agent1", "query", nil, "", "")

	if receivedAuth != "Bearer default-key" {
		t.Errorf("auth = %q, want 'Bearer default-key'", receivedAuth)
	}
}

func TestBridge_DecideForTool_4xx_TreatedAsBlock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(DecideResponse{
			Proceed:  false,
			Decision: "BLOCK",
		})
	}))
	defer srv.Close()

	bridge := NewBridge(BridgeConfig{
		EngineURL: srv.URL,
		APIKey:    "key",
	})

	resp, _ := bridge.DecideForTool(context.Background(), "agent1", "tool", nil, "", "")
	if resp.Proceed {
		t.Error("4xx should be treated as block, not fallback")
	}
}
