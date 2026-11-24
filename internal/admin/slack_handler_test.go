package admin

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/approval"
)

func TestSlackHandler_ApproveAction(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("test-agent", "delete_record", map[string]any{"table": "users"})

	handler := NewSlackHandler(store, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	payload := fmt.Sprintf(`{"type":"block_actions","user":{"id":"U123","username":"alice"},"actions":[{"action_id":"approve_%s","value":"approve"}]}`, entry.ID)
	body := "payload=" + url.QueryEscape(payload)

	req := httptest.NewRequest("POST", "/slack/callback", nil)
	req.Body = io.NopCloser(strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	resolved := store.Get(entry.ID)
	if resolved.Status != approval.StatusApproved {
		t.Errorf("status = %s, want approved", resolved.Status)
	}
	if resolved.ResolvedBy != "alice" {
		t.Errorf("resolvedBy = %s, want alice", resolved.ResolvedBy)
	}
}

func TestSlackHandler_DenyAction(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("test-agent", "send_email", map[string]any{"to": "x@x.com"})

	handler := NewSlackHandler(store, "")
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	payload := fmt.Sprintf(`{"type":"block_actions","user":{"id":"U456","username":"bob"},"actions":[{"action_id":"deny_%s","value":"deny"}]}`, entry.ID)
	body := "payload=" + url.QueryEscape(payload)

	req := httptest.NewRequest("POST", "/slack/callback", nil)
	req.Body = io.NopCloser(strings.NewReader(body))

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	resolved := store.Get(entry.ID)
	if resolved.Status != approval.StatusDenied {
		t.Errorf("status = %s, want denied", resolved.Status)
	}
}

func TestSlackHandler_SignatureVerification(t *testing.T) {
	secret := "test-slack-secret"
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("test-agent", "delete_record", nil)

	handler := NewSlackHandler(store, secret)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	payload := fmt.Sprintf(`{"type":"block_actions","user":{"id":"U1","username":"admin"},"actions":[{"action_id":"approve_%s","value":"approve"}]}`, entry.ID)
	body := "payload=" + url.QueryEscape(payload)

	// Test with valid signature
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	baseString := fmt.Sprintf("v0:%s:%s", timestamp, body)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(baseString))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/slack/callback", nil)
	req.Body = io.NopCloser(strings.NewReader(body))
	req.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req.Header.Set("X-Slack-Signature", sig)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("valid signature: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Test with invalid signature
	entry2 := store.Create("agent2", "tool2", nil)
	payload2 := fmt.Sprintf(`{"type":"block_actions","user":{"id":"U1","username":"admin"},"actions":[{"action_id":"approve_%s","value":"approve"}]}`, entry2.ID)
	body2 := "payload=" + url.QueryEscape(payload2)

	req2 := httptest.NewRequest("POST", "/slack/callback", nil)
	req2.Body = io.NopCloser(strings.NewReader(body2))
	req2.Header.Set("X-Slack-Request-Timestamp", timestamp)
	req2.Header.Set("X-Slack-Signature", "v0=invalid")

	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusForbidden {
		t.Errorf("invalid signature: expected 403, got %d", rec2.Code)
	}
}

// unused import guard
var _ = fmt.Sprintf
