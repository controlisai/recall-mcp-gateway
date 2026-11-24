package approval

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Status represents the state of an approval request.
type Status string

const (
	StatusPending  Status = "pending"
	StatusApproved Status = "approved"
	StatusDenied   Status = "denied"
	StatusExpired  Status = "expired"
)

// Entry is a single pending or resolved approval request.
type Entry struct {
	ID            string         `json:"id"`
	AgentID       string         `json:"agentId"`
	Tool          string         `json:"tool"`
	Arguments     map[string]any `json:"arguments"`
	ArgsHash      string         `json:"argsHash"`
	Status        Status         `json:"status"`
	Reason        string         `json:"reason,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	ExpiresAt     time.Time      `json:"expiresAt"`
	ResolvedAt    time.Time      `json:"resolvedAt,omitempty"`
	ResolvedBy    string         `json:"resolvedBy,omitempty"`
	RetryMethod   string         `json:"retryMethod,omitempty"`   // "poll" or "callback"
	StatusWebhook string         `json:"statusWebhook,omitempty"` // URL to fire on resolution (callback mode)
	McpMethod     string         `json:"mcpMethod,omitempty"`     // MCP method (e.g. "tools/call")
	McpName       string         `json:"mcpName,omitempty"`       // MCP tool name
	ActingFor     string         `json:"actingFor,omitempty"`     // delegation: who the agent acts for
}

// Store manages in-memory pending approvals with TTL.
type Store struct {
	mu        sync.RWMutex
	entries   map[string]*Entry    // id -> entry
	waiters   map[string][]chan *Entry // id -> channels signaled on resolve (supports multiple waiters)
	ttl       time.Duration
	stop      chan struct{}
}

// NewStore creates an approval store with the given TTL for pending entries.
func NewStore(ttl time.Duration) *Store {
	if ttl == 0 {
		ttl = 5 * time.Minute
	}
	s := &Store{
		entries: make(map[string]*Entry),
		waiters: make(map[string][]chan *Entry),
		ttl:     ttl,
		stop:    make(chan struct{}),
	}
	go s.cleanupLoop()
	return s
}

// SetTTL updates the approval TTL at runtime.
func (s *Store) SetTTL(d time.Duration) { s.mu.Lock(); s.ttl = d; s.mu.Unlock() }

// Close stops the cleanup goroutine.
func (s *Store) Close() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
}

// Create creates a new pending approval and returns it.
func (s *Store) Create(agentID, tool string, args map[string]any) *Entry {
	entry := &Entry{
		ID:        generateApprovalID(),
		AgentID:   agentID,
		Tool:      tool,
		Arguments: args,
		ArgsHash:  HashArgs(agentID, tool, args),
		Status:    StatusPending,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(s.ttl),
	}

	s.mu.Lock()
	s.entries[entry.ID] = entry
	s.mu.Unlock()

	return entry
}

// Approve marks an approval as approved.
func (s *Store) Approve(id, resolvedBy string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[id]
	if !ok {
		return nil, fmt.Errorf("approval %s not found", id)
	}
	if entry.Status != StatusPending {
		return nil, fmt.Errorf("approval %s is %s, not pending", id, entry.Status)
	}
	if time.Now().After(entry.ExpiresAt) {
		entry.Status = StatusExpired
		return nil, fmt.Errorf("approval %s has expired", id)
	}

	entry.Status = StatusApproved
	entry.ResolvedAt = time.Now()
	entry.ResolvedBy = resolvedBy
	s.notifyWaiter(id, entry)
	return entry, nil
}

// Deny marks an approval as denied.
func (s *Store) Deny(id, resolvedBy, reason string) (*Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.entries[id]
	if !ok {
		return nil, fmt.Errorf("approval %s not found", id)
	}
	if entry.Status != StatusPending {
		return nil, fmt.Errorf("approval %s is %s, not pending", id, entry.Status)
	}

	entry.Status = StatusDenied
	entry.Reason = reason
	entry.ResolvedAt = time.Now()
	entry.ResolvedBy = resolvedBy
	s.notifyWaiter(id, entry)
	return entry, nil
}

// CheckApproved returns true if there's an approved entry matching the (agent, tool, args) combo.
// Consumes the approval (one-time use).
func (s *Store) CheckApproved(agentID, tool string, args map[string]any) bool {
	hash := HashArgs(agentID, tool, args)

	s.mu.Lock()
	defer s.mu.Unlock()

	for id, entry := range s.entries {
		if entry.ArgsHash == hash && entry.Status == StatusApproved {
			delete(s.entries, id) // consume
			return true
		}
	}
	return false
}

// Get returns a single approval by ID.
func (s *Store) Get(id string) *Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entries[id]
}

// CheckPendingOrDenied returns the status of an existing approval matching (agent, tool, args).
// Returns the entry if found (pending, denied, or expired), or nil if no matching approval exists.
// Does NOT consume the entry — use CheckApproved for consumption.
func (s *Store) CheckPendingOrDenied(agentID, tool string, args map[string]any) *Entry {
	hash := HashArgs(agentID, tool, args)

	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, entry := range s.entries {
		if entry.ArgsHash == hash {
			return entry
		}
	}
	return nil
}

// Pending returns all pending approvals.
func (s *Store) Pending() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []Entry
	now := time.Now()
	for _, e := range s.entries {
		if e.Status == StatusPending && now.Before(e.ExpiresAt) {
			result = append(result, *e)
		}
	}
	return result
}

// All returns all approvals (including resolved).
func (s *Store) All() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Entry, 0, len(s.entries))
	for _, e := range s.entries {
		result = append(result, *e)
	}
	return result
}

func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
		}
		s.mu.Lock()
		now := time.Now()
		for id, e := range s.entries {
			if e.Status == StatusPending && now.After(e.ExpiresAt) {
				e.Status = StatusExpired
				s.notifyWaiter(id, e)
			}
			// Remove resolved entries older than 1 hour
			if e.Status != StatusPending && !e.ResolvedAt.IsZero() && now.Sub(e.ResolvedAt) > time.Hour {
				delete(s.entries, id)
			}
		}
		s.mu.Unlock()
	}
}

func generateApprovalID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "apr-" + hex.EncodeToString(b)
}

// WaitForResolution blocks until the approval is approved, denied, or times out.
// Returns the resolved entry. The caller should check entry.Status.
func (s *Store) WaitForResolution(id string) *Entry {
	s.mu.Lock()
	entry, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	// Already resolved (race: approved between Create and WaitForResolution)
	if entry.Status != StatusPending {
		s.mu.Unlock()
		return entry
	}
	// Create waiter channel
	ch := make(chan *Entry, 1)
	s.waiters[id] = append(s.waiters[id], ch)
	s.mu.Unlock()

	// Wait for signal or TTL expiry
	select {
	case resolved := <-ch:
		return resolved
	case <-time.After(time.Until(entry.ExpiresAt)):
		// Expired — clean up waiter and mark expired
		s.mu.Lock()
		delete(s.waiters, id)
		if entry.Status == StatusPending {
			entry.Status = StatusExpired
		}
		s.mu.Unlock()
		return entry
	}
}

// WaitForResolutionCtx blocks until approved, denied, timed out, or context cancelled.
// On context cancellation (e.g., MCP task cancelled), marks the approval as denied.
// If both resolution and cancellation happen simultaneously, resolution wins.
func (s *Store) WaitForResolutionCtx(ctx context.Context, id string) *Entry {
	s.mu.Lock()
	entry, ok := s.entries[id]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	if entry.Status != StatusPending {
		s.mu.Unlock()
		return entry
	}
	ch := make(chan *Entry, 1)
	s.waiters[id] = append(s.waiters[id], ch)
	s.mu.Unlock()

	select {
	case resolved := <-ch:
		return resolved
	case <-time.After(time.Until(entry.ExpiresAt)):
		s.mu.Lock()
		delete(s.waiters, id)
		if entry.Status == StatusPending {
			entry.Status = StatusExpired
		}
		s.mu.Unlock()
		return entry
	case <-ctx.Done():
		// Check if approval was resolved concurrently (resolution wins over cancellation)
		select {
		case resolved := <-ch:
			return resolved
		default:
		}
		s.mu.Lock()
		delete(s.waiters, id)
		if entry.Status == StatusPending {
			entry.Status = StatusDenied
			entry.Reason = "task cancelled by client"
			entry.ResolvedAt = time.Now()
			entry.ResolvedBy = "system"
		}
		s.mu.Unlock()
		return entry
	}
}

// notifyWaiter signals ALL waiting goroutines that the approval has been resolved.
// Must be called while s.mu is held (Lock, not RLock).
func (s *Store) notifyWaiter(id string, entry *Entry) {
	if channels, ok := s.waiters[id]; ok {
		for _, ch := range channels {
			ch <- entry
		}
		delete(s.waiters, id)
	}
}

func HashArgs(agentID, tool string, args map[string]any) string {
	data, _ := json.Marshal(map[string]any{
		"agent": agentID,
		"tool":  tool,
		"args":  args,
	})
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:16])
}
