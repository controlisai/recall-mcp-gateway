package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOriginMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("empty allow list passes all", func(t *testing.T) {
		h := originMiddleware(nil, ok)
		req := httptest.NewRequest("POST", "/mcp", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("matching origin allowed", func(t *testing.T) {
		h := originMiddleware([]string{"https://app.example.com"}, ok)
		req := httptest.NewRequest("POST", "/mcp", nil)
		req.Header.Set("Origin", "https://app.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
			t.Fatalf("expected ACAO header")
		}
	})

	t.Run("non-matching origin blocked", func(t *testing.T) {
		h := originMiddleware([]string{"https://app.example.com"}, ok)
		req := httptest.NewRequest("POST", "/mcp", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("no origin header passes", func(t *testing.T) {
		h := originMiddleware([]string{"https://app.example.com"}, ok)
		req := httptest.NewRequest("POST", "/mcp", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("OPTIONS preflight with matching origin", func(t *testing.T) {
		h := originMiddleware([]string{"https://app.example.com"}, ok)
		req := httptest.NewRequest("OPTIONS", "/mcp", nil)
		req.Header.Set("Origin", "https://app.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "https://app.example.com" {
			t.Fatalf("expected ACAO header on preflight")
		}
	})

	t.Run("OPTIONS preflight with non-matching origin", func(t *testing.T) {
		h := originMiddleware([]string{"https://app.example.com"}, ok)
		req := httptest.NewRequest("OPTIONS", "/mcp", nil)
		req.Header.Set("Origin", "https://evil.example.com")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("expected 204, got %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("should not set ACAO for non-matching origin")
		}
	})

	t.Run("trailing slash normalization", func(t *testing.T) {
		h := originMiddleware([]string{"https://app.example.com"}, ok)
		req := httptest.NewRequest("POST", "/mcp", nil)
		req.Header.Set("Origin", "https://app.example.com/")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 with trailing slash, got %d", rec.Code)
		}
	})
}
