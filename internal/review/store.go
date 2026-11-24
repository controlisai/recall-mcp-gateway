package review

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/store"
	_ "modernc.org/sqlite"
)

// reviewConfigNotifier broadcasts review mutations to other pods via PG LISTEN/NOTIFY.
type reviewConfigNotifier interface {
	NotifyChange(configType string, data []byte) error
}

// Status represents the review state of a tool.
type Status string

const (
	StatusUnreviewed Status = "unreviewed"
	StatusReviewed   Status = "reviewed"
)

// Entry records the review state of a single tool.
type Entry struct {
	Tool                string   `json:"tool"`
	Status              Status   `json:"status"`
	ReviewedAt          time.Time `json:"reviewedAt,omitempty"`
	ReviewedBy          string   `json:"reviewedBy,omitempty"`
	Note                string   `json:"note,omitempty"`
	AcknowledgedSignals []string `json:"acknowledgedSignals,omitempty"` // e.g. ["pii", "credential"] — expected signal types
}

// Store manages tool review statuses with JSON file persistence.
type Store struct {
	mu          sync.RWMutex
	entries     map[string]*Entry // toolName -> review entry
	path        string            // persistence file path (empty = no file)
	persistDB   *sql.DB           // shared DB for review persistence (nil = use file)
	configNotifier reviewConfigNotifier // PG LISTEN/NOTIFY for multi-pod (nil = standalone)
}

// NewStore creates a review store with optional persistence.
func NewStore(persistPath string) *Store {
	s := &Store{
		entries: make(map[string]*Entry),
		path:    persistPath,
	}
	if persistPath != "" {
		s.load()
	}
	return s
}

// NewDBStore creates a review store backed by the shared gateway DB.
// Existing reviews are loaded from the reviews table on startup.
func NewDBStore(db *sql.DB) *Store {
	s := &Store{
		entries:   make(map[string]*Entry),
		persistDB: db,
	}
	s.loadFromDB()
	return s
}

// SetPersistDB wires the shared gateway DB for review persistence.
// Existing reviews are loaded from the DB. Subsequent persist() calls write to the DB.
func (s *Store) SetPersistDB(db *sql.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.persistDB = db
	s.loadFromDBLocked()
}

func (s *Store) loadFromDB() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadFromDBLocked()
}

func (s *Store) loadFromDBLocked() {
	rows, err := s.persistDB.Query(
		"SELECT tool, status, reviewed_at, reviewed_by, note, acknowledged_signals FROM reviews",
	)
	if err != nil {
		slog.Warn("failed to load reviews from gateway.db", "component", "review", "error", err)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var (
			tool, status, reviewedAt, reviewedBy, note, acksJSON string
		)
		if rows.Scan(&tool, &status, &reviewedAt, &reviewedBy, &note, &acksJSON) != nil {
			continue
		}
		e := &Entry{
			Tool:       tool,
			Status:     Status(status),
			ReviewedBy: reviewedBy,
			Note:       note,
		}
		if reviewedAt != "" {
			e.ReviewedAt, _ = time.Parse(time.RFC3339, reviewedAt)
		}
		if acksJSON != "" {
			json.Unmarshal([]byte(acksJSON), &e.AcknowledgedSignals) //nolint:errcheck
		}
		s.entries[tool] = e
		count++
	}
	if count > 0 {
		slog.Info("loaded tool reviews", "component", "review", "count", count, "storage", "gateway.db")
	}
}

// SetConfigNotifier wires PG LISTEN/NOTIFY for cross-pod review change notification.
func (s *Store) SetConfigNotifier(cs reviewConfigNotifier) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.configNotifier = cs
}

// ReloadFromDB reloads all reviews from the database (for PG LISTEN/NOTIFY sync).
func (s *Store) ReloadFromDB() {
	if s.persistDB == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.persistDB.Query("SELECT tool, status, reviewed_at, reviewed_by, note, acknowledged_signals FROM reviews")
	if err != nil {
		slog.Warn("failed to reload reviews from DB", "component", "review", "error", err)
		return
	}
	defer rows.Close()
	s.entries = make(map[string]*Entry)
	for rows.Next() {
		var tool, status, reviewedAt, reviewedBy, note, signalsJSON string
		if rows.Scan(&tool, &status, &reviewedAt, &reviewedBy, &note, &signalsJSON) != nil {
			continue
		}
		entry := &Entry{Tool: tool, Status: Status(status), ReviewedBy: reviewedBy, Note: note}
		if reviewedAt != "" {
			entry.ReviewedAt, _ = time.Parse(time.RFC3339, reviewedAt)
		}
		if signalsJSON != "" {
			json.Unmarshal([]byte(signalsJSON), &entry.AcknowledgedSignals)
		}
		s.entries[tool] = entry
	}
}

// MarkReviewed marks a tool as reviewed by the given user.
func (s *Store) MarkReviewed(tool, reviewedBy, note string, acknowledgedSignals []string) *Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := &Entry{
		Tool:                tool,
		Status:              StatusReviewed,
		ReviewedAt:          time.Now(),
		ReviewedBy:          reviewedBy,
		Note:                note,
		AcknowledgedSignals: acknowledgedSignals,
	}
	s.entries[tool] = entry
	s.persist()
	return entry
}

// IsReviewed returns true if the tool has been reviewed.
func (s *Store) IsReviewed(tool string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[tool]
	return ok && e.Status == StatusReviewed
}

// GetReview returns the review entry for a tool, or nil if not reviewed.
func (s *Store) GetReview(tool string) *Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[tool]
}

// AllReviews returns all review entries.
func (s *Store) AllReviews() map[string]*Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]*Entry, len(s.entries))
	for k, v := range s.entries {
		result[k] = v
	}
	return result
}

// IsSignalAcknowledged returns true if the given signal category is acknowledged
// for the tool (expected, should not count against signal safety score).
func (s *Store) IsSignalAcknowledged(tool, signalCategory string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[tool]
	if !ok || e.Status != StatusReviewed {
		return false
	}
	for _, ack := range e.AcknowledgedSignals {
		if ack == signalCategory {
			return true
		}
	}
	return false
}


func (s *Store) load() {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // no file yet — start empty
	}
	var entries map[string]*Entry
	if json.Unmarshal(data, &entries) == nil {
		s.entries = entries
		slog.Info("loaded tool reviews", "component", "review", "count", len(entries))
	}
}

func (s *Store) persist() {
	data, _ := json.MarshalIndent(s.entries, "", "  ")
	if s.persistDB != nil {
		s.saveReviewsToDB()
	} else if s.path != "" {
		dir := filepath.Dir(s.path)
		os.MkdirAll(dir, 0700)
		if err := os.WriteFile(s.path, data, 0600); err != nil {
			slog.Warn("failed to persist tool reviews", "component", "review", "error", err)
		}
	}
	if s.configNotifier != nil {
		if err := s.configNotifier.NotifyChange("reviews", data); err != nil {
			slog.Warn("failed to notify reviews change", "component", "review", "error", err)
		}
	}
}

func (s *Store) saveReviewsToDB() {
	tx, err := s.persistDB.Begin()
	if err != nil {
		slog.Warn("failed to begin review tx", "component", "review", "error", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck
	if _, err := tx.Exec("DELETE FROM reviews"); err != nil {
		slog.Warn("failed to clear reviews", "component", "review", "error", err)
		return
	}
	for tool, e := range s.entries {
		acksJSON, _ := json.Marshal(e.AcknowledgedSignals)
		reviewedAt := ""
		if !e.ReviewedAt.IsZero() {
			reviewedAt = e.ReviewedAt.Format(time.RFC3339)
		}
		_, err := store.ExecDialectTx(tx,
			`INSERT OR REPLACE INTO reviews (tool, status, reviewed_at, reviewed_by, note, acknowledged_signals)
			VALUES (?, ?, ?, ?, ?, ?)`,
			`INSERT INTO reviews (tool, status, reviewed_at, reviewed_by, note, acknowledged_signals)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (tool) DO UPDATE SET status = $2, reviewed_at = $3, reviewed_by = $4, note = $5, acknowledged_signals = $6`,
			store.IsPostgres(s.persistDB),
			tool, string(e.Status), reviewedAt, e.ReviewedBy, e.Note, string(acksJSON),
		)
		if err != nil {
			slog.Warn("failed to insert review", "component", "review", "tool", tool, "error", err)
		}
	}
	if err := tx.Commit(); err != nil {
		slog.Warn("failed to commit reviews", "component", "review", "error", err)
	}
}
