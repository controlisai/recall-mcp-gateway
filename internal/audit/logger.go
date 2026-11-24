package audit

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Subscriber receives audit entries in real time (e.g., WebSocket connections).
type Subscriber func(Entry)

// Logger writes audit entries to a JSONL output and broadcasts to live subscribers.
type Logger struct {
	mu          sync.RWMutex
	writer      io.Writer
	subscribers map[int]Subscriber
	nextID      int
	recent      []Entry // ring buffer of recent entries
	maxRecent   int
	storageName string // "memory" or "jsonl"
	filePath    string // non-empty for file logger
}

// NewLogger creates an audit logger that writes JSONL to the given writer.
// If writer is nil, defaults to os.Stdout.
func NewLogger(writer io.Writer) *Logger {
	if writer == nil {
		writer = os.Stdout
	}
	return &Logger{
		writer:      writer,
		subscribers: make(map[int]Subscriber),
		maxRecent:   200,
		storageName: "memory",
	}
}

// NewFileLogger creates an audit logger that appends to a file.
func NewFileLogger(path string) (*Logger, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	l := NewLogger(f)
	l.storageName = "jsonl"
	l.filePath = path
	return l, nil
}

// Log records an audit entry: writes to output and broadcasts to subscribers.
func (l *Logger) Log(entry Entry) {
	data, err := json.Marshal(entry)
	if err != nil {
		slog.Error("marshal error", "component", "audit", "error", err)
		return
	}

	// Write JSONL
	l.mu.Lock()
	l.writer.Write(append(data, '\n'))

	// Add to recent buffer
	if len(l.recent) >= l.maxRecent {
		l.recent = l.recent[1:]
	}
	l.recent = append(l.recent, entry)

	// Snapshot subscribers for broadcast outside lock
	subs := make(map[int]Subscriber, len(l.subscribers))
	for id, sub := range l.subscribers {
		subs[id] = sub
	}
	l.mu.Unlock()

	// Broadcast to subscribers (non-blocking)
	for _, sub := range subs {
		func() {
			defer func() { recover() }() // protect against panicking subscriber
			sub(entry)
		}()
	}
}

// Subscribe registers a live listener. Returns an unsubscribe function.
func (l *Logger) Subscribe(sub Subscriber) func() {
	l.mu.Lock()
	id := l.nextID
	l.nextID++
	l.subscribers[id] = sub
	l.mu.Unlock()

	return func() {
		l.mu.Lock()
		delete(l.subscribers, id)
		l.mu.Unlock()
	}
}

// Recent returns the most recent audit entries (up to maxRecent).
func (l *Logger) Recent() []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	result := make([]Entry, len(l.recent))
	copy(result, l.recent)
	return result
}

// RecentN returns at most limit recent entries (oldest first).
func (l *Logger) RecentN(limit int) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if limit <= 0 {
		limit = l.maxRecent
	}
	src := l.recent
	if limit < len(src) {
		src = src[len(src)-limit:]
	}
	result := make([]Entry, len(src))
	copy(result, src)
	return result
}

// Search returns entries from the in-memory buffer matching the filter.
func (l *Logger) Search(filter SearchFilter) []Entry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	limit := filter.Limit
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	results := make([]Entry, 0)
	// Iterate newest-first to respect limit efficiently
	for i := len(l.recent) - 1; i >= 0 && len(results) < limit; i-- {
		e := l.recent[i]
		if filter.Tool != "" {
			if matched, _ := filepath.Match(filter.Tool, e.Tool); !matched {
				continue
			}
		}
		if filter.AgentID != "" && e.AgentID != filter.AgentID {
			continue
		}
		if filter.Decision != "" && e.Decision != filter.Decision {
			continue
		}
		if filter.Server != "" && e.Server != filter.Server {
			continue
		}
		if !filter.From.IsZero() && e.Timestamp.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && e.Timestamp.After(filter.To) {
			continue
		}
		results = append(results, e)
	}

	// Reverse to oldest-first
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}
	return results
}

// Stats returns aggregated statistics from the in-memory buffer.
func (l *Logger) Stats(filter StatsFilter) AuditStats {
	l.mu.RLock()
	defer l.mu.RUnlock()

	agentCounts := make(map[string]int64)
	toolCounts := make(map[string]int64)
	decisionCounts := make(map[string]int64)
	serverCounts := make(map[string]int64)
	modeCounts := make(map[string]int64)
	var total, errors, totalLatency int64

	// Dashboard-specific tracking
	pseudoAgents := map[string]bool{"system": true, "anonymous": true, "unauthorized": true}
	var blocked int64
	signalCounts := map[string]int64{"pii": 0, "credential": 0, "destructive": 0}
	type agentAcc struct {
		count  int64
		errors int64
	}
	type toolAcc struct {
		count  int64
		server string
	}
	dashAgents := make(map[string]*agentAcc)
	dashTools := make(map[string]*toolAcc)
	dashUniqueTools := make(map[string]bool)
	dashUniqueAgents := make(map[string]bool)

	for _, e := range l.recent {
		if !filter.From.IsZero() && e.Timestamp.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && e.Timestamp.After(filter.To) {
			continue
		}
		total++
		agentCounts[e.AgentID]++
		toolCounts[e.Tool]++
		decisionCounts[e.Decision]++
		serverCounts[e.Server]++
		modeCounts[e.Mode]++
		totalLatency += e.LatencyMs
		if e.IsError {
			errors++
		}

		// Dashboard aggregations (exclude system/pseudo)
		isDash := !pseudoAgents[e.AgentID] && e.Tool != "(server-lifecycle)"
		if !isDash {
			continue
		}
		if e.Decision == "block" || e.Decision == "rate_limited" {
			blocked++
		}
		for _, sig := range e.Signals {
			if sig.Category == "pii" || sig.Category == "credential" || sig.Category == "destructive" {
				signalCounts[sig.Category]++
			}
		}
		// Top agents
		acc := dashAgents[e.AgentID]
		if acc == nil {
			acc = &agentAcc{}
			dashAgents[e.AgentID] = acc
		}
		acc.count++
		if e.IsError || e.Decision == "block" || e.Decision == "rate_limited" {
			acc.errors++
		}
		dashUniqueAgents[e.AgentID] = true
		// Top tools
		if e.Tool != "tools/list" {
			ta := dashTools[e.Tool]
			if ta == nil {
				ta = &toolAcc{server: e.Server}
				dashTools[e.Tool] = ta
			}
			ta.count++
			dashUniqueTools[e.Tool] = true
		}
	}

	var avgLatency float64
	if total > 0 {
		avgLatency = float64(totalLatency) / float64(total)
	}

	// Build top agents sorted
	topAgents := make([]TopAgentStat, 0, len(dashAgents))
	for id, a := range dashAgents {
		topAgents = append(topAgents, TopAgentStat{ID: id, Count: a.count, Errors: a.errors})
	}
	for i := 0; i < len(topAgents); i++ {
		for j := i + 1; j < len(topAgents); j++ {
			if topAgents[j].Count > topAgents[i].Count {
				topAgents[i], topAgents[j] = topAgents[j], topAgents[i]
			}
		}
	}
	if len(topAgents) > 10 {
		topAgents = topAgents[:10]
	}

	// Build top tools sorted
	topTools := make([]TopToolStat, 0, len(dashTools))
	for name, t := range dashTools {
		topTools = append(topTools, TopToolStat{Name: name, Count: t.count, Server: t.server})
	}
	for i := 0; i < len(topTools); i++ {
		for j := i + 1; j < len(topTools); j++ {
			if topTools[j].Count > topTools[i].Count {
				topTools[i], topTools[j] = topTools[j], topTools[i]
			}
		}
	}
	if len(topTools) > 10 {
		topTools = topTools[:10]
	}

	// Hourly activity buckets
	hourlyMap := make(map[string]*HourlyBucket)
	for _, e := range l.recent {
		if !filter.From.IsZero() && e.Timestamp.Before(filter.From) {
			continue
		}
		if !filter.To.IsZero() && e.Timestamp.After(filter.To) {
			continue
		}
		if pseudoAgents[e.AgentID] || e.Tool == "(server-lifecycle)" || e.Tool == "tools/list" {
			continue
		}
		hr := e.Timestamp.UTC().Truncate(time.Hour).Format(time.RFC3339)
		b := hourlyMap[hr]
		if b == nil {
			b = &HourlyBucket{Hour: hr}
			hourlyMap[hr] = b
		}
		b.Total++
		if e.IsError || e.Decision == "block" || e.Decision == "rate_limited" {
			b.Errors++
		}
	}
	hourly := make([]HourlyBucket, 0, len(hourlyMap))
	for _, b := range hourlyMap {
		hourly = append(hourly, *b)
	}
	for i := 0; i < len(hourly); i++ {
		for j := i + 1; j < len(hourly); j++ {
			if hourly[j].Hour < hourly[i].Hour {
				hourly[i], hourly[j] = hourly[j], hourly[i]
			}
		}
	}

	return AuditStats{
		TotalCount:     total,
		ByAgent:        mapToBuckets(agentCounts),
		ByTool:         mapToBuckets(toolCounts),
		ByDecision:     mapToBuckets(decisionCounts),
		ByServer:       mapToBuckets(serverCounts),
		ByMode:         mapToBuckets(modeCounts),
		ErrorCount:     errors,
		AvgLatencyMs:   avgLatency,
		BlockedCount:   blocked,
		SignalCounts:   signalCounts,
		TopAgents:      topAgents,
		TopTools:       topTools,
		UniqueTools:    int64(len(dashUniqueTools)),
		UniqueAgents:   int64(len(dashUniqueAgents)),
		HourlyActivity: hourly,
	}
}

func mapToBuckets(m map[string]int64) []CountBucket {
	buckets := make([]CountBucket, 0, len(m))
	for label, count := range m {
		buckets = append(buckets, CountBucket{Label: label, Count: count})
	}
	// Sort descending by count
	for i := 0; i < len(buckets); i++ {
		for j := i + 1; j < len(buckets); j++ {
			if buckets[j].Count > buckets[i].Count {
				buckets[i], buckets[j] = buckets[j], buckets[i]
			}
		}
	}
	if len(buckets) > 50 {
		buckets = buckets[:50]
	}
	return buckets
}

// SubscriberCount returns the number of active subscribers.
func (l *Logger) SubscriberCount() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.subscribers)
}

// Info returns store metadata. Logger is in-memory so size is unknown.
func (l *Logger) Info() StoreInfo {
	l.mu.RLock()
	defer l.mu.RUnlock()
	info := StoreInfo{
		Storage:    l.storageName,
		Path:       l.filePath,
		EntryCount: int64(len(l.recent)),
		SizeBytes:  -1,
	}
	if len(l.recent) > 0 {
		info.OldestEntry = l.recent[0].Timestamp.Format("2006-01-02T15:04:05Z07:00")
	}
	return info
}

// SetMaxEntries is a no-op for the in-memory logger (buffer is fixed at maxRecent).
func (l *Logger) SetMaxEntries(n int) {}

// Purge clears the in-memory buffer.
func (l *Logger) Purge() error {
	l.mu.Lock()
	l.recent = nil
	l.mu.Unlock()
	return nil
}

// Close closes the underlying writer if it implements io.Closer.
func (l *Logger) Close() error {
	if closer, ok := l.writer.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}
