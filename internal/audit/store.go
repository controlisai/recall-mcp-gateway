package audit

import "time"

// AuditStore is the interface for audit entry persistence and live broadcasting.
// Implementations: Logger (JSONL/memory), SQLiteStore (persistent).
type AuditStore interface {
	// Log records an audit entry and broadcasts to subscribers.
	Log(entry Entry)

	// Recent returns the most recent audit entries (up to implementation limit).
	Recent() []Entry

	// RecentN returns at most limit recent entries (oldest first).
	RecentN(limit int) []Entry

	// Search returns entries matching the filter criteria.
	Search(filter SearchFilter) []Entry

	// Stats returns aggregated statistics, optionally filtered by time range.
	Stats(filter StatsFilter) AuditStats

	// Subscribe registers a live listener. Returns an unsubscribe function.
	Subscribe(sub Subscriber) func()

	// SubscriberCount returns the number of active subscribers.
	SubscriberCount() int

	// Info returns storage metadata (entry count, size, oldest entry).
	Info() StoreInfo

	// SetMaxEntries updates the hard cap on entry count (0 = unlimited).
	SetMaxEntries(n int)

	// Purge deletes all entries and reclaims space.
	Purge() error

	// Close releases resources (file handles, DB connections).
	Close() error
}

// SearchFilter defines criteria for searching audit entries.
type SearchFilter struct {
	Tool     string    // glob match on tool name
	AgentID  string    // exact match
	Decision string    // exact match
	Server   string    // exact match
	From     time.Time // entries at or after this time
	To       time.Time // entries at or before this time
	Limit    int       // max results (0 = default 200)
}

// StatsFilter defines the time range for aggregation queries.
type StatsFilter struct {
	From time.Time
	To   time.Time
}

// AuditStats holds aggregated audit statistics.
type AuditStats struct {
	TotalCount   int64          `json:"totalCount"`
	ByAgent      []CountBucket  `json:"byAgent"`
	ByTool       []CountBucket  `json:"byTool"`
	ByDecision   []CountBucket  `json:"byDecision"`
	ByServer     []CountBucket  `json:"byServer"`
	ByMode       []CountBucket  `json:"byMode"`
	ErrorCount   int64          `json:"errorCount"`
	AvgLatencyMs float64        `json:"avgLatencyMs"`

	// Dashboard-specific aggregations (excludes system/pseudo entries)
	BlockedCount int64            `json:"blockedCount"`
	SignalCounts map[string]int64 `json:"signalCounts"`
	TopAgents    []TopAgentStat   `json:"topAgents"`
	TopTools     []TopToolStat    `json:"topTools"`
	UniqueTools    int64            `json:"uniqueTools"`
	UniqueAgents   int64            `json:"uniqueAgents"`
	HourlyActivity []HourlyBucket   `json:"hourlyActivity,omitempty"`
}

// HourlyBucket is an hour's call and error count for the activity timeline.
type HourlyBucket struct {
	Hour   string `json:"hour"`   // RFC3339 of the hour start
	Total  int64  `json:"total"`
	Errors int64  `json:"errors"`
}

// TopAgentStat is an agent with call count and error count for the dashboard.
type TopAgentStat struct {
	ID     string `json:"id"`
	Count  int64  `json:"count"`
	Errors int64  `json:"errors"`
}

// TopToolStat is a tool with call count and server for the dashboard.
type TopToolStat struct {
	Name   string `json:"name"`
	Count  int64  `json:"count"`
	Server string `json:"server"`
}

// CountBucket is a label + count pair for aggregation.
type CountBucket struct {
	Label string `json:"label"`
	Count int64  `json:"count"`
}

// StoreInfo holds audit store metadata.
type StoreInfo struct {
	Storage     string `json:"storage"`               // "sqlite", "jsonl", "memory"
	Path        string `json:"path,omitempty"`         // file path
	EntryCount  int64  `json:"entryCount"`
	SizeBytes   int64  `json:"sizeBytes"`              // -1 if unknown
	OldestEntry string `json:"oldestEntry,omitempty"`  // RFC3339, empty if no entries
	Retention   string `json:"retention,omitempty"`
	MaxEntries  int    `json:"maxEntries,omitempty"`
}
