package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLogger_Log_WritesJSONL(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(&buf)

	entry := Entry{
		Timestamp: time.Date(2026, 8, 21, 10, 30, 0, 0, time.UTC),
		RequestID: "req-abc123",
		AgentID:   "test-agent",
		Tool:      "query",
		Server:    "database",
		Decision:  "allow",
		LatencyMs: 45,
		Mode:      "passthrough",
	}

	logger.Log(entry)

	lines := strings.TrimSpace(buf.String())
	if lines == "" {
		t.Fatal("expected output")
	}

	var parsed Entry
	if err := json.Unmarshal([]byte(lines), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if parsed.Tool != "query" {
		t.Errorf("tool = %q, want query", parsed.Tool)
	}
	if parsed.AgentID != "test-agent" {
		t.Errorf("agent = %q, want test-agent", parsed.AgentID)
	}
}

func TestLogger_Subscribe_ReceivesEntries(t *testing.T) {
	logger := NewLogger(&bytes.Buffer{})

	var received []Entry
	var mu sync.Mutex
	unsub := logger.Subscribe(func(e Entry) {
		mu.Lock()
		received = append(received, e)
		mu.Unlock()
	})

	logger.Log(Entry{Tool: "tool1"})
	logger.Log(Entry{Tool: "tool2"})

	time.Sleep(10 * time.Millisecond) // let broadcast complete

	mu.Lock()
	count := len(received)
	mu.Unlock()

	if count != 2 {
		t.Errorf("received = %d, want 2", count)
	}

	unsub()

	logger.Log(Entry{Tool: "tool3"})

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	count = len(received)
	mu.Unlock()

	if count != 2 {
		t.Errorf("after unsub: received = %d, want 2", count)
	}
}

func TestLogger_Recent(t *testing.T) {
	logger := NewLogger(&bytes.Buffer{})

	for i := 0; i < 5; i++ {
		logger.Log(Entry{Tool: "tool"})
	}

	recent := logger.Recent()
	if len(recent) != 5 {
		t.Errorf("recent = %d, want 5", len(recent))
	}
}

func TestLogger_Recent_RingBuffer(t *testing.T) {
	logger := NewLogger(&bytes.Buffer{})
	logger.maxRecent = 3

	for i := 0; i < 10; i++ {
		logger.Log(Entry{RequestID: string(rune('a' + i))})
	}

	recent := logger.Recent()
	if len(recent) != 3 {
		t.Errorf("recent = %d, want 3", len(recent))
	}
}

func TestLogger_ConcurrentWrites(t *testing.T) {
	logger := NewLogger(&bytes.Buffer{})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			logger.Log(Entry{Tool: "concurrent", LatencyMs: int64(i)})
		}(i)
	}
	wg.Wait()

	recent := logger.Recent()
	if len(recent) != 100 {
		t.Errorf("recent = %d, want 100", len(recent))
	}
}

func TestLogger_SubscriberCount(t *testing.T) {
	logger := NewLogger(&bytes.Buffer{})

	if logger.SubscriberCount() != 0 {
		t.Error("expected 0 subscribers")
	}

	unsub1 := logger.Subscribe(func(e Entry) {})
	unsub2 := logger.Subscribe(func(e Entry) {})

	if logger.SubscriberCount() != 2 {
		t.Errorf("subscribers = %d, want 2", logger.SubscriberCount())
	}

	unsub1()
	if logger.SubscriberCount() != 1 {
		t.Errorf("after unsub: subscribers = %d, want 1", logger.SubscriberCount())
	}

	unsub2()
	if logger.SubscriberCount() != 0 {
		t.Errorf("after both unsub: subscribers = %d, want 0", logger.SubscriberCount())
	}
}
