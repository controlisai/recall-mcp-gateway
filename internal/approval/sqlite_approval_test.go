package approval

import (
	"path/filepath"
	"testing"
	"time"
)

// Compile-time interface compliance checks
var _ StoreInterface = (*Store)(nil)
var _ StoreInterface = (*SQLiteStore)(nil)

func newTestSQLiteStore(t *testing.T, ttl time.Duration) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test-approvals.db")
	s, err := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: ttl})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSQLiteStore_Create(t *testing.T) {
	s := newTestSQLiteStore(t, 5*time.Minute)
	e := s.Create("agent1", "db_write", map[string]any{"table": "users"})
	if e.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if e.Status != StatusPending {
		t.Errorf("status = %q, want pending", e.Status)
	}
	if e.AgentID != "agent1" {
		t.Errorf("agentID = %q, want agent1", e.AgentID)
	}
}

func TestSQLiteStore_Approve(t *testing.T) {
	s := newTestSQLiteStore(t, 5*time.Minute)
	e := s.Create("agent1", "db_write", nil)

	approved, err := s.Approve(e.ID, "reviewer-alice")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if approved.Status != StatusApproved {
		t.Errorf("status = %q, want approved", approved.Status)
	}
	if approved.ResolvedBy != "reviewer-alice" {
		t.Errorf("resolvedBy = %q, want reviewer-alice", approved.ResolvedBy)
	}
}

func TestSQLiteStore_Deny(t *testing.T) {
	s := newTestSQLiteStore(t, 5*time.Minute)
	e := s.Create("agent1", "delete_all", nil)

	denied, err := s.Deny(e.ID, "reviewer-bob", "too risky")
	if err != nil {
		t.Fatalf("Deny: %v", err)
	}
	if denied.Status != StatusDenied {
		t.Errorf("status = %q, want denied", denied.Status)
	}
	if denied.Reason != "too risky" {
		t.Errorf("reason = %q, want 'too risky'", denied.Reason)
	}
}

func TestSQLiteStore_CheckApprovedConsumes(t *testing.T) {
	s := newTestSQLiteStore(t, 5*time.Minute)
	args := map[string]any{"query": "SELECT 1"}
	e := s.Create("agent1", "db_read", args)
	s.Approve(e.ID, "admin")

	// First check: should find and consume
	if !s.CheckApproved("agent1", "db_read", args) {
		t.Fatal("expected approved entry on first check")
	}
	// Second check: consumed, should not find
	if s.CheckApproved("agent1", "db_read", args) {
		t.Fatal("expected consumed entry on second check")
	}
}

func TestSQLiteStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "persist-test.db")

	// Create and populate
	s1, err := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	e := s1.Create("agent1", "tool1", map[string]any{"key": "value"})
	id := e.ID
	s1.Close()

	// Reopen and verify
	s2, err := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	got := s2.Get(id)
	if got == nil {
		t.Fatal("expected entry to survive restart")
	}
	if got.AgentID != "agent1" {
		t.Errorf("agentID = %q, want agent1", got.AgentID)
	}
	if got.Tool != "tool1" {
		t.Errorf("tool = %q, want tool1", got.Tool)
	}
}

func TestSQLiteStore_PendingRehydration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rehydrate-test.db")

	s1, _ := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: 5 * time.Minute})
	s1.Create("agent1", "tool1", nil)
	s1.Create("agent2", "tool2", nil)
	s1.Close()

	s2, _ := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: 5 * time.Minute})
	defer s2.Close()

	pending := s2.Pending()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending after rehydration, got %d", len(pending))
	}
}

func TestSQLiteStore_ApproveAfterRestart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restart-approve-test.db")

	s1, _ := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: 5 * time.Minute})
	e := s1.Create("agent1", "tool1", nil)
	id := e.ID
	s1.Close()

	s2, _ := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: 5 * time.Minute})
	defer s2.Close()

	approved, err := s2.Approve(id, "admin-after-restart")
	if err != nil {
		t.Fatalf("Approve after restart: %v", err)
	}
	if approved.Status != StatusApproved {
		t.Errorf("status = %q, want approved", approved.Status)
	}
}

func TestSQLiteStore_WaitForResolution(t *testing.T) {
	s := newTestSQLiteStore(t, 5*time.Minute)
	e := s.Create("agent1", "tool1", nil)

	done := make(chan *Entry, 1)
	go func() {
		done <- s.WaitForResolution(e.ID)
	}()

	time.Sleep(50 * time.Millisecond) // let waiter start
	s.Approve(e.ID, "admin")

	select {
	case resolved := <-done:
		if resolved == nil {
			t.Fatal("expected resolved entry")
		}
		if resolved.Status != StatusApproved {
			t.Errorf("status = %q, want approved", resolved.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForResolution timed out")
	}
}

func TestSQLiteStore_All(t *testing.T) {
	s := newTestSQLiteStore(t, 5*time.Minute)
	s.Create("a1", "t1", nil)
	e2 := s.Create("a2", "t2", nil)
	s.Approve(e2.ID, "admin")

	all := s.All()
	if len(all) != 2 {
		t.Errorf("expected 2 entries from All(), got %d", len(all))
	}
}

func TestSQLiteStore_McpFieldsPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp-fields-test.db")

	s1, _ := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: 5 * time.Minute})
	e := s1.Create("agent1", "db_write", nil)
	e.McpMethod = "tools/call"
	e.McpName = "db_write"
	e.RetryMethod = "poll"
	s1.UpdateFields(e.ID)
	id := e.ID
	s1.Close()

	// Reopen and verify MCP fields survived
	s2, _ := NewSQLiteStore(SQLiteStoreConfig{Path: path, TTL: 5 * time.Minute})
	defer s2.Close()

	got := s2.Get(id)
	if got == nil {
		t.Fatal("expected entry after restart")
	}
	if got.McpMethod != "tools/call" {
		t.Errorf("McpMethod = %q, want tools/call", got.McpMethod)
	}
	if got.McpName != "db_write" {
		t.Errorf("McpName = %q, want db_write", got.McpName)
	}
}

func TestSQLiteStore_Expired(t *testing.T) {
	s := newTestSQLiteStore(t, 100*time.Millisecond) // very short TTL
	e := s.Create("agent1", "tool1", nil)

	time.Sleep(200 * time.Millisecond) // wait for expiry

	// Approve should fail — entry is expired
	_, err := s.Approve(e.ID, "admin")
	if err == nil {
		t.Fatal("expected error approving expired entry")
	}
}
