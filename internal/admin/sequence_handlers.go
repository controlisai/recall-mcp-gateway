package admin

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/controlisai/recall-mcp-gateway/internal/audit"
)

// SequenceHandlers serves detected sequence data.
type SequenceHandlers struct {
	auditor audit.AuditStore
}

// NewSequenceHandlers creates sequence API handlers.
func NewSequenceHandlers(auditor audit.AuditStore) *SequenceHandlers {
	return &SequenceHandlers{auditor: auditor}
}

// RegisterRoutes registers GET /admin/v1/sequences.
func (h *SequenceHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/v1/sequences", h.handleList)
}

// detectedSequence represents a sequence chain extracted from audit entries.
type detectedSequence struct {
	Pattern  string    `json:"pattern"`  // e.g., "data_exfiltration"
	Tools    []string  `json:"tools"`    // ordered tool names in the chain
	Agent    string    `json:"agent"`    // agent that triggered it
	Count    int       `json:"count"`    // how many times this pattern was seen
	Message  string    `json:"message"`  // signal message
	LastSeen time.Time `json:"lastSeen"` // timestamp of most recent detection
}

func (h *SequenceHandlers) handleList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	recent := h.auditor.Recent()

	// Extract sequence signals from recent entries and group by pattern+agent
	type seqKey struct{ pattern, agent string }
	grouped := make(map[seqKey]*detectedSequence)
	// Track tool chains: for each agent, collect the tool call order
	agentTools := make(map[string][]string)

	for _, entry := range recent {
		agentTools[entry.AgentID] = append(agentTools[entry.AgentID], entry.Tool)
		for _, sig := range entry.Signals {
			if sig.Category != "sequence" {
				continue
			}
			key := seqKey{pattern: sig.Name, agent: entry.AgentID}
			if seq, ok := grouped[key]; ok {
				seq.Count++
				if entry.Timestamp.After(seq.LastSeen) {
					seq.LastSeen = entry.Timestamp
				}
				// Add tool if not already the last one
				if len(seq.Tools) == 0 || seq.Tools[len(seq.Tools)-1] != entry.Tool {
					seq.Tools = append(seq.Tools, entry.Tool)
				}
			} else {
				grouped[key] = &detectedSequence{
					Pattern:  sig.Name,
					Agent:    entry.AgentID,
					Count:    1,
					Message:  sig.Message,
					Tools:    []string{entry.Tool},
					LastSeen: entry.Timestamp,
				}
			}
		}
	}

	// Enrich tool chains from the agent's recent call history.
	// Show the last few unique tools leading up to the sequence trigger.
	for key, seq := range grouped {
		if tools, ok := agentTools[key.agent]; ok && len(seq.Tools) <= 1 {
			// Take the last N unique consecutive tools (capped at 8)
			var chain []string
			for _, t := range tools {
				if len(chain) == 0 || chain[len(chain)-1] != t {
					chain = append(chain, t)
				}
			}
			if len(chain) > 8 {
				chain = chain[len(chain)-8:]
			}
			seq.Tools = chain
		}
		// Cap existing chains too
		if len(seq.Tools) > 10 {
			seq.Tools = seq.Tools[len(seq.Tools)-10:]
		}
	}

	sequences := make([]detectedSequence, 0, len(grouped))
	for _, seq := range grouped {
		sequences = append(sequences, *seq)
	}
	sort.Slice(sequences, func(i, j int) bool {
		return sequences[i].Count > sequences[j].Count
	})

	json.NewEncoder(w).Encode(map[string]any{
		"sequences": sequences,
		"count":     len(sequences),
	})
}
