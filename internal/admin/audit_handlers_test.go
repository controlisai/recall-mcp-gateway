package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
)

func TestAuditRecent_Empty(t *testing.T) {
	store := audit.NewLogger(nil)
	h := NewAuditHandlers(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/audit/recent", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		Entries []json.RawMessage `json:"entries"`
		Count   int               `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&body)
	if body.Count != 0 {
		t.Errorf("count = %d, want 0", body.Count)
	}
}

func TestAuditRecent_WithLimit(t *testing.T) {
	store := audit.NewLogger(nil)
	for i := 0; i < 10; i++ {
		store.Log(audit.Entry{Tool: "t", Decision: "allow"})
	}
	h := NewAuditHandlers(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/audit/recent?limit=3", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var body struct {
		Count int `json:"count"`
	}
	json.NewDecoder(w.Body).Decode(&body)
	if body.Count != 3 {
		t.Errorf("count = %d, want 3", body.Count)
	}
}

func TestAuditInfo(t *testing.T) {
	store := audit.NewLogger(nil)
	store.Log(audit.Entry{Tool: "t", Decision: "allow"})

	h := NewAuditHandlers(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/audit/info", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var info audit.StoreInfo
	json.NewDecoder(w.Body).Decode(&info)
	if info.Storage != "memory" {
		t.Errorf("storage = %q, want memory", info.Storage)
	}
	if info.EntryCount != 1 {
		t.Errorf("entryCount = %d, want 1", info.EntryCount)
	}
}

func TestAuditPurge(t *testing.T) {
	store := audit.NewLogger(nil)
	store.Log(audit.Entry{Tool: "t", Decision: "allow"})

	h := NewAuditHandlers(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/admin/v1/audit/purge", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	// Verify purged
	if entries := store.Recent(); len(entries) != 0 {
		t.Errorf("after purge: %d entries remain", len(entries))
	}
}

func TestAuditSearch(t *testing.T) {
	store := audit.NewLogger(nil)
	store.Log(audit.Entry{Tool: "read_file", AgentID: "a1", Decision: "allow"})
	store.Log(audit.Entry{Tool: "write_file", AgentID: "a2", Decision: "block"})
	store.Log(audit.Entry{Tool: "read_dir", AgentID: "a1", Decision: "allow"})

	h := NewAuditHandlers(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/audit/search?agent=a1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct{ Count int `json:"count"` }
	json.NewDecoder(w.Body).Decode(&body)
	if body.Count != 2 {
		t.Errorf("count = %d, want 2", body.Count)
	}
}

func TestAuditStats(t *testing.T) {
	store := audit.NewLogger(nil)
	store.Log(audit.Entry{Tool: "t1", Decision: "allow", LatencyMs: 10})
	store.Log(audit.Entry{Tool: "t2", Decision: "block", LatencyMs: 30, IsError: true})

	h := NewAuditHandlers(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/audit/stats", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var stats audit.AuditStats
	json.NewDecoder(w.Body).Decode(&stats)
	if stats.TotalCount != 2 {
		t.Errorf("totalCount = %d, want 2", stats.TotalCount)
	}
	if stats.ErrorCount != 1 {
		t.Errorf("errorCount = %d, want 1", stats.ErrorCount)
	}
}

func TestAuditPurge_WrongMethod(t *testing.T) {
	store := audit.NewLogger(nil)
	h := NewAuditHandlers(store)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/v1/audit/purge", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
}
