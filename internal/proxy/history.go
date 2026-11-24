package proxy

import (
	"sync"

	"github.com/controlisai/recall-mcp-gateway/internal/detect"
)

// HistoryBuffer is a per-agent circular buffer of recent tool calls.
// Accessible to multiple consumers: sequence detector, arg diff, replay, workflow.
type HistoryBuffer struct {
	mu      sync.RWMutex
	entries map[string][]detect.HistoryEntry // agentID -> entries
	maxSize int
}

// NewHistoryBuffer creates a per-agent history buffer.
func NewHistoryBuffer(maxSize int) *HistoryBuffer {
	if maxSize <= 0 {
		maxSize = 50
	}
	return &HistoryBuffer{
		entries: make(map[string][]detect.HistoryEntry),
		maxSize: maxSize,
	}
}

// Add records a call in the agent's history.
func (h *HistoryBuffer) Add(agentID string, entry detect.HistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	history := h.entries[agentID]
	history = append(history, entry)
	if len(history) > h.maxSize {
		history = history[len(history)-h.maxSize:]
	}
	h.entries[agentID] = history
}

// Get returns the recent history for an agent.
func (h *HistoryBuffer) Get(agentID string) []detect.HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	src := h.entries[agentID]
	result := make([]detect.HistoryEntry, len(src))
	copy(result, src)
	return result
}

// LastSuccessful returns the most recent successful call for (agent, tool).
func (h *HistoryBuffer) LastSuccessful(agentID, tool string) *detect.HistoryEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()
	history := h.entries[agentID]
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Tool == tool && !history[i].IsError {
			e := history[i]
			return &e
		}
	}
	return nil
}

// RecentTools returns just the tool names from the agent's history.
func (h *HistoryBuffer) RecentTools(agentID string) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	history := h.entries[agentID]
	tools := make([]string, len(history))
	for i, e := range history {
		tools[i] = e.Tool
	}
	return tools
}

// AllAgents returns the list of agents with history.
func (h *HistoryBuffer) AllAgents() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	agents := make([]string, 0, len(h.entries))
	for a := range h.entries {
		agents = append(agents, a)
	}
	return agents
}
