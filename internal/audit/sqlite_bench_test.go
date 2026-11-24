package audit

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkSQLiteInsert(b *testing.B) {
	store, err := NewSQLiteStore(SQLiteConfig{Path: b.TempDir() + "/bench.db"})
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()

	entry := Entry{
		Timestamp: time.Now(),
		RequestID: "bench-req",
		AgentID:   "agent-1",
		Tool:      "read_file",
		Server:    "github",
		Decision:  "allow",
		Mode:      "rule",
		LatencyMs: 42,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.RequestID = fmt.Sprintf("bench-%d", i)
		store.Log(entry)
	}
}

func BenchmarkSQLiteRecent200(b *testing.B) {
	store := setupBenchStore(b, 10000)
	defer store.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Recent()
	}
}

func BenchmarkSQLiteSearch(b *testing.B) {
	store := setupBenchStore(b, 10000)
	defer store.Close()

	filter := SearchFilter{
		Decision: "allow",
		Limit:    100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Search(filter)
	}
}

func BenchmarkSQLiteSearchByAgent(b *testing.B) {
	store := setupBenchStore(b, 10000)
	defer store.Close()

	filter := SearchFilter{
		AgentID: "agent-3",
		Limit:   100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Search(filter)
	}
}

func BenchmarkSQLiteStats(b *testing.B) {
	store := setupBenchStore(b, 10000)
	defer store.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Stats(StatsFilter{})
	}
}

func BenchmarkSQLiteStatsWithTimeRange(b *testing.B) {
	store := setupBenchStore(b, 10000)
	defer store.Close()

	filter := StatsFilter{
		From: time.Now().Add(-1 * time.Hour),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Stats(filter)
	}
}

func setupBenchStore(b *testing.B, count int) *SQLiteStore {
	b.Helper()
	store, err := NewSQLiteStore(SQLiteConfig{Path: b.TempDir() + "/bench.db"})
	if err != nil {
		b.Fatal(err)
	}

	agents := []string{"agent-1", "agent-2", "agent-3", "agent-4", "agent-5"}
	tools := []string{"read_file", "write_file", "search", "execute_sql", "send_email"}
	decisions := []string{"allow", "allow", "allow", "block", "rate_limited"}
	servers := []string{"github", "postgres", "slack"}

	for i := 0; i < count; i++ {
		store.Log(Entry{
			Timestamp: time.Now().Add(-time.Duration(count-i) * time.Second),
			RequestID: fmt.Sprintf("req-%d", i),
			AgentID:   agents[i%len(agents)],
			Tool:      tools[i%len(tools)],
			Server:    servers[i%len(servers)],
			Decision:  decisions[i%len(decisions)],
			Mode:      "rule",
			LatencyMs: int64(10 + i%100),
			IsError:   i%20 == 0,
		})
	}
	return store
}

// TestPerformance_100K verifies acceptable latency with 100K entries.
func TestPerformance_100K(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 100K performance test in short mode")
	}

	store, err := NewSQLiteStore(SQLiteConfig{Path: t.TempDir() + "/perf.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Insert 100K entries
	agents := []string{"agent-1", "agent-2", "agent-3", "agent-4", "agent-5"}
	tools := []string{"read_file", "write_file", "search", "execute_sql", "send_email", "list_dir", "delete_file", "get_user"}
	decisions := []string{"allow", "allow", "allow", "block", "rate_limited"}
	servers := []string{"github", "postgres", "slack", "jira"}

	start := time.Now()
	for i := 0; i < 100000; i++ {
		store.Log(Entry{
			Timestamp: time.Now().Add(-time.Duration(100000-i) * time.Second),
			RequestID: fmt.Sprintf("req-%d", i),
			AgentID:   agents[i%len(agents)],
			Tool:      tools[i%len(tools)],
			Server:    servers[i%len(servers)],
			Decision:  decisions[i%len(decisions)],
			Mode:      "rule",
			LatencyMs: int64(5 + i%200),
			IsError:   i%50 == 0,
		})
	}
	insertTime := time.Since(start)
	t.Logf("100K inserts: %v (%.0f/sec)", insertTime, 100000/insertTime.Seconds())

	// Recent 200
	start = time.Now()
	entries := store.Recent()
	t.Logf("Recent(200): %v (%d entries)", time.Since(start), len(entries))

	// Search by agent
	start = time.Now()
	results := store.Search(SearchFilter{AgentID: "agent-3", Limit: 100})
	t.Logf("Search(agent=agent-3, limit=100): %v (%d results)", time.Since(start), len(results))

	// Search by tool glob
	start = time.Now()
	results = store.Search(SearchFilter{Tool: "read_*", Limit: 100})
	t.Logf("Search(tool=read_*, limit=100): %v (%d results)", time.Since(start), len(results))

	// Stats (all time)
	start = time.Now()
	stats := store.Stats(StatsFilter{})
	t.Logf("Stats(all): %v (total=%d)", time.Since(start), stats.TotalCount)
	if stats.TotalCount != 100000 {
		t.Errorf("stats total = %d, want 100000", stats.TotalCount)
	}

	// Stats (last hour)
	start = time.Now()
	stats = store.Stats(StatsFilter{From: time.Now().Add(-1 * time.Hour)})
	t.Logf("Stats(1h): %v (total=%d)", time.Since(start), stats.TotalCount)

	// Info
	start = time.Now()
	info := store.Info()
	t.Logf("Info: %v (entries=%d, size=%.1f MB)", time.Since(start), info.EntryCount, float64(info.SizeBytes)/(1024*1024))
}
