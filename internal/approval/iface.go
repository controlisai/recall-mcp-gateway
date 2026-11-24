package approval

import (
	"context"
	"time"
)

// StoreInterface is the contract for approval persistence.
// Implementations: Store (in-memory), SQLiteStore (hybrid SQLite + memory).
type StoreInterface interface {
	Create(agentID, tool string, args map[string]any) *Entry
	Approve(id, resolvedBy string) (*Entry, error)
	Deny(id, resolvedBy, reason string) (*Entry, error)
	CheckApproved(agentID, tool string, args map[string]any) bool
	CheckPendingOrDenied(agentID, tool string, args map[string]any) *Entry
	Get(id string) *Entry
	Pending() []Entry
	All() []Entry
	WaitForResolution(id string) *Entry
	WaitForResolutionCtx(ctx context.Context, id string) *Entry
	SetTTL(d time.Duration)
	Close()
}
