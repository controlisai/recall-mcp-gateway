package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/platform"
)

func TestPlatformHandlers_Standalone(t *testing.T) {
	// No platform client — standalone mode
	handlers := NewPlatformHandlers(nil, "", "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/admin/v1/platform/summary", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}

	var body platform.PlatformSummary
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Connected {
		t.Error("should not be connected in standalone mode")
	}
}

func TestPlatformHandlers_Connected(t *testing.T) {
	// Mock console-api
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode([]any{})
	}))
	defer srv.Close()

	client := platform.NewClient(srv.URL, "token")
	handlers := NewPlatformHandlers(client, "product1", "http://localhost:3000")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/admin/v1/platform/summary", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}

	var body platform.PlatformSummary
	json.Unmarshal(w.Body.Bytes(), &body)
	if !body.Connected {
		t.Error("should be connected")
	}
}

func TestPlatformHandlers_AgentsEndpoint(t *testing.T) {
	handlers := NewPlatformHandlers(nil, "", "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/admin/v1/platform/agents", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
}

func TestPlatformHandlers_DriftEndpoint(t *testing.T) {
	handlers := NewPlatformHandlers(nil, "", "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/admin/v1/platform/drift", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
}
