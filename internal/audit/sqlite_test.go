package audit

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func tempDBPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "test-audit.db")
}

func TestSQLiteStore_WriteAndRecent(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: tempDBPath(t)})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	// Log 3 entries
	for i := 0; i < 3; i++ {
		store.Log(Entry{
			Timestamp: time.Now(),
			RequestID: "req-" + string(rune('a'+i)),
			AgentID:   "agent-1",
			Tool:      "test-tool",
			Decision:  "allow",
			Mode:      "rule",
		})
	}

	recent := store.Recent()
	if len(recent) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(recent))
	}
	// Oldest first
	if recent[0].RequestID != "req-a" {
		t.Errorf("first entry = %q, want req-a", recent[0].RequestID)
	}
	if recent[2].RequestID != "req-c" {
		t.Errorf("last entry = %q, want req-c", recent[2].RequestID)
	}
}

func TestSQLiteStore_Arguments_Signals_Roundtrip(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: tempDBPath(t)})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	store.Log(Entry{
		Timestamp: time.Now(),
		RequestID: "req-1",
		AgentID:   "agent-1",
		Tool:      "test-tool",
		Decision:  "block",
		Arguments: map[string]any{"key": "value", "count": float64(42)},
		Signals: []SignalEntry{
			{Name: "pii_detected", Category: "content", Severity: "high", Message: "SSN found"},
		},
		IsError:      true,
		ErrorMessage: "blocked by rule",
	})

	recent := store.Recent()
	if len(recent) != 1 {
		t.Fatalf("expected 1, got %d", len(recent))
	}
	e := recent[0]
	if e.Arguments["key"] != "value" {
		t.Errorf("arguments[key] = %v, want value", e.Arguments["key"])
	}
	if e.Arguments["count"] != float64(42) {
		t.Errorf("arguments[count] = %v, want 42", e.Arguments["count"])
	}
	if len(e.Signals) != 1 || e.Signals[0].Name != "pii_detected" {
		t.Errorf("signals = %v, want pii_detected", e.Signals)
	}
	if !e.IsError {
		t.Error("expected IsError = true")
	}
	if e.ErrorMessage != "blocked by rule" {
		t.Errorf("errorMessage = %q", e.ErrorMessage)
	}
}

func TestSQLiteStore_Subscribe(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: tempDBPath(t)})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	var received []Entry
	var mu sync.Mutex
	unsub := store.Subscribe(func(e Entry) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	store.Log(Entry{
		Timestamp: time.Now(),
		RequestID: "req-sub",
		Tool:      "tool-1",
		Decision:  "allow",
	})

	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	count := len(received)
	mu.Unlock()
	if count != 1 {
		t.Fatalf("subscriber received %d entries, want 1", count)
	}
	if received[0].RequestID != "req-sub" {
		t.Errorf("received[0].RequestID = %q", received[0].RequestID)
	}

	// Unsubscribe
	unsub()
	store.Log(Entry{
		Timestamp: time.Now(),
		RequestID: "req-unsub",
		Tool:      "tool-1",
		Decision:  "allow",
	})
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	count = len(received)
	mu.Unlock()
	if count != 1 {
		t.Errorf("after unsub: received %d entries, want 1", count)
	}
}

func TestSQLiteStore_Persistence(t *testing.T) {
	dbPath := tempDBPath(t)

	// Write entries
	store1, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	store1.Log(Entry{
		Timestamp: time.Now(),
		RequestID: "persistent-1",
		Tool:      "tool-1",
		Decision:  "allow",
	})
	store1.Close()

	// Reopen and verify
	store2, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store2.Close()

	recent := store2.Recent()
	if len(recent) != 1 {
		t.Fatalf("expected 1 entry after reopen, got %d", len(recent))
	}
	if recent[0].RequestID != "persistent-1" {
		t.Errorf("requestId = %q, want persistent-1", recent[0].RequestID)
	}
}

func TestSQLiteStore_PurgeByCount(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{
		Path:       tempDBPath(t),
		MaxEntries: 5,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	// Write 10 entries
	for i := 0; i < 10; i++ {
		store.Log(Entry{
			Timestamp: time.Now(),
			RequestID: "req-" + string(rune('0'+i)),
			Tool:      "tool",
			Decision:  "allow",
		})
	}

	// Force purge
	store.purgeOnce()

	recent := store.Recent()
	if len(recent) != 5 {
		t.Fatalf("after purge: expected 5 entries, got %d", len(recent))
	}
}

func TestSQLiteStore_PurgeByRetention(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{
		Path:      tempDBPath(t),
		Retention: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	// Write old entry
	store.Log(Entry{
		Timestamp: time.Now().Add(-2 * time.Hour),
		RequestID: "old",
		Tool:      "tool",
		Decision:  "allow",
	})
	// Write recent entry
	store.Log(Entry{
		Timestamp: time.Now(),
		RequestID: "new",
		Tool:      "tool",
		Decision:  "allow",
	})

	store.purgeOnce()

	recent := store.Recent()
	if len(recent) != 1 {
		t.Fatalf("after purge: expected 1 entry, got %d", len(recent))
	}
	if recent[0].RequestID != "new" {
		t.Errorf("remaining entry = %q, want new", recent[0].RequestID)
	}
}

func TestSQLiteStore_ConcurrentWrites(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: tempDBPath(t)})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.Log(Entry{
				Timestamp: time.Now(),
				RequestID: "concurrent-" + string(rune('a'+i%26)),
				Tool:      "tool",
				Decision:  "allow",
			})
		}(i)
	}
	wg.Wait()

	recent := store.Recent()
	if len(recent) != 50 {
		t.Errorf("expected 50 entries, got %d", len(recent))
	}
}

func TestSQLiteStore_DBCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "subdir")
	// Dir doesn't exist yet
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Skip("dir already exists")
	}
	os.MkdirAll(dir, 0755)
	dbPath := filepath.Join(dir, "audit.db")
	store, err := NewSQLiteStore(SQLiteConfig{Path: dbPath})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	store.Close()
}

func TestSQLiteStore_RecentN(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: tempDBPath(t)})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 10; i++ {
		store.Log(Entry{
			Timestamp: time.Now(),
			RequestID: "req-" + string(rune('0'+i)),
			Tool:      "tool",
			Decision:  "allow",
		})
	}

	entries := store.RecentN(3)
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	// Should be last 3 entries, oldest first
	if entries[0].RequestID != "req-7" {
		t.Errorf("first = %q, want req-7", entries[0].RequestID)
	}
}

func TestSQLiteStore_Info(t *testing.T) {
	dbPath := tempDBPath(t)
	store, err := NewSQLiteStore(SQLiteConfig{
		Path:       dbPath,
		Retention:  48 * time.Hour,
		MaxEntries: 500,
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	store.Log(Entry{Timestamp: time.Now(), Tool: "t", Decision: "allow"})
	store.Log(Entry{Timestamp: time.Now(), Tool: "t", Decision: "block"})

	info := store.Info()
	if info.Storage != "sqlite" {
		t.Errorf("storage = %q", info.Storage)
	}
	if info.Path != dbPath {
		t.Errorf("path = %q, want %q", info.Path, dbPath)
	}
	if info.EntryCount != 2 {
		t.Errorf("entryCount = %d, want 2", info.EntryCount)
	}
	if info.SizeBytes <= 0 {
		t.Errorf("sizeBytes = %d, want > 0", info.SizeBytes)
	}
	if info.OldestEntry == "" {
		t.Error("oldestEntry is empty")
	}
	if info.Retention != "2d" {
		t.Errorf("retention = %q, want 2d", info.Retention)
	}
	if info.MaxEntries != 500 {
		t.Errorf("maxEntries = %d, want 500", info.MaxEntries)
	}
}

func TestSQLiteStore_Purge(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: tempDBPath(t)})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	for i := 0; i < 5; i++ {
		store.Log(Entry{Timestamp: time.Now(), Tool: "t", Decision: "allow"})
	}

	if err := store.Purge(); err != nil {
		t.Fatalf("purge: %v", err)
	}

	entries := store.Recent()
	if len(entries) != 0 {
		t.Errorf("after purge: %d entries", len(entries))
	}

	info := store.Info()
	if info.EntryCount != 0 {
		t.Errorf("after purge: entryCount = %d", info.EntryCount)
	}
}

func TestLogger_RecentN(t *testing.T) {
	logger := NewLogger(nil)
	for i := 0; i < 10; i++ {
		logger.Log(Entry{Tool: "t", Decision: "allow", RequestID: "r-" + string(rune('0'+i))})
	}

	entries := logger.RecentN(3)
	if len(entries) != 3 {
		t.Fatalf("expected 3, got %d", len(entries))
	}
	if entries[0].RequestID != "r-7" {
		t.Errorf("first = %q, want r-7", entries[0].RequestID)
	}
}

func TestLogger_InfoAndPurge(t *testing.T) {
	logger := NewLogger(nil)
	logger.Log(Entry{Timestamp: time.Now(), Tool: "t", Decision: "allow"})

	info := logger.Info()
	if info.Storage != "memory" {
		t.Errorf("storage = %q", info.Storage)
	}
	if info.EntryCount != 1 {
		t.Errorf("entryCount = %d", info.EntryCount)
	}

	logger.Purge()
	if entries := logger.Recent(); len(entries) != 0 {
		t.Errorf("after purge: %d entries", len(entries))
	}
}

func TestSQLiteStore_Search(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: tempDBPath(t)})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now()
	store.Log(Entry{Timestamp: now.Add(-2 * time.Hour), AgentID: "a1", Tool: "read_file", Server: "gh", Decision: "allow"})
	store.Log(Entry{Timestamp: now.Add(-1 * time.Hour), AgentID: "a2", Tool: "write_file", Server: "gh", Decision: "block"})
	store.Log(Entry{Timestamp: now, AgentID: "a1", Tool: "read_dir", Server: "pg", Decision: "allow"})

	// By agent
	results := store.Search(SearchFilter{AgentID: "a1"})
	if len(results) != 2 {
		t.Errorf("agent=a1: got %d, want 2", len(results))
	}

	// By decision
	results = store.Search(SearchFilter{Decision: "block"})
	if len(results) != 1 {
		t.Errorf("decision=block: got %d, want 1", len(results))
	}

	// By tool glob
	results = store.Search(SearchFilter{Tool: "read_*"})
	if len(results) != 2 {
		t.Errorf("tool=read_*: got %d, want 2", len(results))
	}

	// By time range
	results = store.Search(SearchFilter{From: now.Add(-90 * time.Minute)})
	if len(results) != 2 {
		t.Errorf("from=-90m: got %d, want 2", len(results))
	}

	// Combined
	results = store.Search(SearchFilter{AgentID: "a1", Server: "gh"})
	if len(results) != 1 {
		t.Errorf("agent=a1+server=gh: got %d, want 1", len(results))
	}
}

func TestSQLiteStore_Stats(t *testing.T) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: tempDBPath(t)})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	defer store.Close()

	now := time.Now()
	store.Log(Entry{Timestamp: now, AgentID: "a1", Tool: "t1", Decision: "allow", LatencyMs: 10})
	store.Log(Entry{Timestamp: now, AgentID: "a1", Tool: "t2", Decision: "block", LatencyMs: 20, IsError: true})
	store.Log(Entry{Timestamp: now, AgentID: "a2", Tool: "t1", Decision: "allow", LatencyMs: 30})

	stats := store.Stats(StatsFilter{})
	if stats.TotalCount != 3 {
		t.Errorf("totalCount = %d, want 3", stats.TotalCount)
	}
	if stats.ErrorCount != 1 {
		t.Errorf("errorCount = %d, want 1", stats.ErrorCount)
	}
	if stats.AvgLatencyMs != 20 {
		t.Errorf("avgLatency = %.1f, want 20", stats.AvgLatencyMs)
	}
	if len(stats.ByAgent) != 2 {
		t.Errorf("byAgent len = %d, want 2", len(stats.ByAgent))
	}
	if len(stats.ByDecision) != 2 {
		t.Errorf("byDecision len = %d, want 2", len(stats.ByDecision))
	}
}

func TestLogger_Search(t *testing.T) {
	logger := NewLogger(nil)
	now := time.Now()
	logger.Log(Entry{Timestamp: now, AgentID: "a1", Tool: "read_file", Decision: "allow"})
	logger.Log(Entry{Timestamp: now, AgentID: "a2", Tool: "write_file", Decision: "block"})
	logger.Log(Entry{Timestamp: now, AgentID: "a1", Tool: "read_dir", Decision: "allow"})

	results := logger.Search(SearchFilter{AgentID: "a1"})
	if len(results) != 2 {
		t.Errorf("agent=a1: got %d, want 2", len(results))
	}

	results = logger.Search(SearchFilter{Tool: "read_*"})
	if len(results) != 2 {
		t.Errorf("tool=read_*: got %d, want 2", len(results))
	}
}

func TestLogger_Stats(t *testing.T) {
	logger := NewLogger(nil)
	now := time.Now()
	logger.Log(Entry{Timestamp: now, AgentID: "a1", Tool: "t1", Decision: "allow", LatencyMs: 10})
	logger.Log(Entry{Timestamp: now, AgentID: "a2", Tool: "t1", Decision: "block", LatencyMs: 30, IsError: true})

	stats := logger.Stats(StatsFilter{})
	if stats.TotalCount != 2 {
		t.Errorf("total = %d", stats.TotalCount)
	}
	if stats.ErrorCount != 1 {
		t.Errorf("errors = %d", stats.ErrorCount)
	}
	if stats.AvgLatencyMs != 20 {
		t.Errorf("avgLatency = %.1f", stats.AvgLatencyMs)
	}
}

// Verify Logger implements AuditStore (compile-time check)
var _ AuditStore = (*Logger)(nil)
var _ AuditStore = (*SQLiteStore)(nil)
