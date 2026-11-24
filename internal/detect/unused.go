package detect

import (
	"sync"
	"time"
)

// UnusedToolTracker tracks which tools from tools/list are actually called.
// After a configurable duration, unused tools are flagged as attack surface.
type UnusedToolTracker struct {
	mu         sync.RWMutex
	registered map[string]time.Time // toolName -> registered at
	called     map[string]time.Time // toolName -> last called at
}

// NewUnusedToolTracker creates a tracker. Call RegisterTool for each tool at startup.
func NewUnusedToolTracker() *UnusedToolTracker {
	return &UnusedToolTracker{
		registered: make(map[string]time.Time),
		called:     make(map[string]time.Time),
	}
}

// RegisterTool records a tool as available from tools/list.
func (u *UnusedToolTracker) RegisterTool(name string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if _, exists := u.registered[name]; !exists {
		u.registered[name] = time.Now()
	}
}

// RecordCall marks a tool as having been called.
func (u *UnusedToolTracker) RecordCall(name string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.called[name] = time.Now()
}

// UnusedTool represents a tool that was registered but never called.
type UnusedTool struct {
	Name           string        `json:"name"`
	RegisteredAt   time.Time     `json:"registeredAt"`
	RegisteredFor  time.Duration `json:"registeredFor"`
}

// Unused returns tools registered for at least minAge that have never been called.
func (u *UnusedToolTracker) Unused(minAge time.Duration) []UnusedTool {
	u.mu.RLock()
	defer u.mu.RUnlock()

	now := time.Now()
	var unused []UnusedTool

	for name, regAt := range u.registered {
		if now.Sub(regAt) < minAge {
			continue // not old enough
		}
		if _, wasCalled := u.called[name]; !wasCalled {
			unused = append(unused, UnusedTool{
				Name:          name,
				RegisteredAt:  regAt,
				RegisteredFor: now.Sub(regAt),
			})
		}
	}
	return unused
}

// Stats returns total registered and total called counts.
func (u *UnusedToolTracker) Stats() (registered, called int) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return len(u.registered), len(u.called)
}
