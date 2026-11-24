package approval

import (
	"testing"
	"time"
)

func newTestStore(t *testing.T, ttl time.Duration) *Store {
	t.Helper()
	s := NewStore(ttl)
	t.Cleanup(s.Close)
	return s
}

func TestStore_Create(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)

	entry := store.Create("agent1", "send_email", map[string]any{"to": "user@example.com"})
	if entry.ID == "" {
		t.Error("expected non-empty ID")
	}
	if entry.Status != StatusPending {
		t.Errorf("status = %q, want pending", entry.Status)
	}
	if entry.AgentID != "agent1" {
		t.Errorf("agent = %q, want agent1", entry.AgentID)
	}
	if entry.Tool != "send_email" {
		t.Errorf("tool = %q, want send_email", entry.Tool)
	}
}

func TestStore_Approve(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)
	entry := store.Create("agent1", "send_email", map[string]any{"to": "test"})

	approved, err := store.Approve(entry.ID, "admin")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != StatusApproved {
		t.Errorf("status = %q, want approved", approved.Status)
	}
	if approved.ResolvedBy != "admin" {
		t.Errorf("resolvedBy = %q, want admin", approved.ResolvedBy)
	}
}

func TestStore_Deny(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)
	entry := store.Create("agent1", "send_email", map[string]any{"to": "test"})

	denied, err := store.Deny(entry.ID, "admin", "not authorized")
	if err != nil {
		t.Fatalf("deny: %v", err)
	}
	if denied.Status != StatusDenied {
		t.Errorf("status = %q, want denied", denied.Status)
	}
	if denied.Reason != "not authorized" {
		t.Errorf("reason = %q", denied.Reason)
	}
}

func TestStore_Approve_NotFound(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)
	_, err := store.Approve("nonexistent", "admin")
	if err == nil {
		t.Error("expected error for nonexistent approval")
	}
}

func TestStore_Approve_AlreadyApproved(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)
	entry := store.Create("agent1", "tool", nil)
	store.Approve(entry.ID, "admin")

	_, err := store.Approve(entry.ID, "admin")
	if err == nil {
		t.Error("expected error for already approved")
	}
}

func TestStore_CheckApproved(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)
	args := map[string]any{"to": "test@example.com"}

	entry := store.Create("agent1", "send_email", args)
	store.Approve(entry.ID, "admin")

	// First check: should find and consume
	if !store.CheckApproved("agent1", "send_email", args) {
		t.Error("expected approved match")
	}

	// Second check: consumed, should not find
	if store.CheckApproved("agent1", "send_email", args) {
		t.Error("approval should be consumed after first check")
	}
}

func TestStore_CheckApproved_DifferentArgs(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)

	entry := store.Create("agent1", "send_email", map[string]any{"to": "a@b.com"})
	store.Approve(entry.ID, "admin")

	// Different args should not match
	if store.CheckApproved("agent1", "send_email", map[string]any{"to": "c@d.com"}) {
		t.Error("should not match with different args")
	}
}

func TestStore_Expired(t *testing.T) {
	store := newTestStore(t, 50*time.Millisecond) // very short TTL

	entry := store.Create("agent1", "tool", nil)

	time.Sleep(100 * time.Millisecond)

	_, err := store.Approve(entry.ID, "admin")
	if err == nil {
		t.Error("expected error for expired approval")
	}
}

func TestStore_Pending(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)

	store.Create("agent1", "tool1", nil)
	store.Create("agent2", "tool2", nil)

	pending := store.Pending()
	if len(pending) != 2 {
		t.Errorf("pending = %d, want 2", len(pending))
	}
}

func TestStore_Pending_ExcludesResolved(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)

	entry := store.Create("agent1", "tool1", nil)
	store.Create("agent2", "tool2", nil)
	store.Approve(entry.ID, "admin")

	pending := store.Pending()
	if len(pending) != 1 {
		t.Errorf("pending = %d, want 1", len(pending))
	}
}

func TestStore_Get(t *testing.T) {
	store := newTestStore(t, 5*time.Minute)
	entry := store.Create("agent1", "tool", nil)

	got := store.Get(entry.ID)
	if got == nil {
		t.Fatal("expected entry")
	}
	if got.ID != entry.ID {
		t.Errorf("id = %q, want %q", got.ID, entry.ID)
	}
}
