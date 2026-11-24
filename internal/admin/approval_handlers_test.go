package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/approval"
)

func TestApprovalHandlers_List(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	store.Create("agent1", "tool1", nil)
	store.Create("agent2", "tool2", nil)

	handlers := NewApprovalHandlers(store, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("GET", "/admin/v1/approvals", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	count := int(body["count"].(float64))
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestApprovalHandlers_Approve(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("agent1", "tool1", nil)

	handlers := NewApprovalHandlers(store, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/approve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "approved" {
		t.Errorf("status = %v, want approved", body["status"])
	}
}

func TestApprovalHandlers_Deny(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("agent1", "tool1", nil)

	handlers := NewApprovalHandlers(store, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	body := `{"reason":"not authorized"}`
	req := httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/deny", strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d, want 200", w.Code)
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "denied" {
		t.Errorf("status = %v, want denied", resp["status"])
	}
}

func TestApprovalHandlers_NotFound(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	handlers := NewApprovalHandlers(store, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/admin/v1/approvals/nonexistent/approve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 400 {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestApprovalHandlers_MethodNotAllowed(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	handlers := NewApprovalHandlers(store, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("DELETE", "/admin/v1/approvals", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

func TestApprovalHandlers_AuthRequired(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("agent1", "tool1", nil)
	handlers := NewApprovalHandlers(store, "my-admin-key")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	// No auth header → 401
	req := httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/approve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("no auth: status = %d, want 401", w.Code)
	}

	// Wrong key → 403
	req = httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("wrong key: status = %d, want 403", w.Code)
	}

	// Correct key → 200
	req = httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/approve", nil)
	req.Header.Set("Authorization", "Bearer my-admin-key")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("correct key: status = %d, want 200", w.Code)
	}
}

func TestApprovalHandlers_AuthOnList(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	handlers := NewApprovalHandlers(store, "my-admin-key")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	// List without auth → 401
	req := httptest.NewRequest("GET", "/admin/v1/approvals", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("list no auth: status = %d, want 401", w.Code)
	}

	// List with auth → 200
	req = httptest.NewRequest("GET", "/admin/v1/approvals", nil)
	req.Header.Set("Authorization", "Bearer my-admin-key")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("list with auth: status = %d, want 200", w.Code)
	}
}

func TestApprovalHandlers_NoAuthWhenKeyEmpty(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("agent1", "tool1", nil)
	handlers := NewApprovalHandlers(store, "") // no admin key
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	// Should work without any auth header
	req := httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/approve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("no key configured: status = %d, want 200", w.Code)
	}
}

func TestApprovalHandlers_ReviewerIdentity(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("agent1", "tool1", nil)
	handlers := NewApprovalHandlers(store, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	req := httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/approve", nil)
	req.Header.Set("X-Reviewer", "alice@corp.com")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Approval approval.Entry `json:"approval"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Approval.ResolvedBy != "alice@corp.com" {
		t.Errorf("resolvedBy = %q, want alice@corp.com", resp.Approval.ResolvedBy)
	}
}

func TestApprovalHandlers_DefaultReviewer(t *testing.T) {
	store := approval.NewStore(5 * time.Minute)
	entry := store.Create("agent1", "tool1", nil)
	handlers := NewApprovalHandlers(store, "")
	mux := http.NewServeMux()
	handlers.RegisterRoutes(mux)

	// No X-Reviewer header → defaults to "admin-api"
	req := httptest.NewRequest("POST", "/admin/v1/approvals/"+entry.ID+"/approve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var resp struct {
		Approval approval.Entry `json:"approval"`
	}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Approval.ResolvedBy != "admin-api" {
		t.Errorf("resolvedBy = %q, want admin-api", resp.Approval.ResolvedBy)
	}
}
