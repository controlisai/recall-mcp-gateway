package detect

import (
	"sync"
)

// DataFlow tracks where PII enters and exits through tool calls.
type DataFlow struct {
	DataType    string   `json:"dataType"`    // ssn, email, credit_card
	EntryTool   string   `json:"entryTool"`   // tool where PII first detected
	ExitTools   []string `json:"exitTools"`   // subsequent tools where same PII pattern appeared
	Occurrences int      `json:"occurrences"`
	Risk        string   `json:"risk"` // high if exit is EXTERNAL_COMM
}

// DataFlowTracker correlates PII signals across sequential calls per agent.
type DataFlowTracker struct {
	mu    sync.RWMutex
	flows map[string]*DataFlow // key: "dataType:entryTool"
}

// NewDataFlowTracker creates a PII data flow tracker.
func NewDataFlowTracker() *DataFlowTracker {
	return &DataFlowTracker{
		flows: make(map[string]*DataFlow),
	}
}

// Analyze checks recent history for PII flow patterns.
// When PII is detected in a call, look at subsequent calls for the same PII type.
func (dft *DataFlowTracker) Analyze(history []HistoryEntry, profiler *Profiler) {
	for i, entry := range history {
		for _, sig := range entry.Signals {
			if sig.Category != "pii" && sig.Category != "credential" {
				continue
			}

			// Look ahead 5 calls for same PII type
			for j := i + 1; j < len(history) && j <= i+5; j++ {
				nextEntry := history[j]
				for _, nextSig := range nextEntry.Signals {
					if nextSig.Name == sig.Name {
						dft.recordFlow(sig.Name, entry.Tool, nextEntry.Tool, profiler)
					}
				}
			}
		}
	}
}

func (dft *DataFlowTracker) recordFlow(dataType, entryTool, exitTool string, profiler *Profiler) {
	key := dataType + ":" + entryTool
	dft.mu.Lock()
	defer dft.mu.Unlock()

	flow, ok := dft.flows[key]
	if !ok {
		flow = &DataFlow{
			DataType:  dataType,
			EntryTool: entryTool,
			Risk:      "low",
		}
		dft.flows[key] = flow
	}

	flow.Occurrences++

	// Add exit tool if not already tracked
	found := false
	for _, et := range flow.ExitTools {
		if et == exitTool {
			found = true
			break
		}
	}
	if !found {
		flow.ExitTools = append(flow.ExitTools, exitTool)
	}

	// Check if exit tool is external comm
	if profiler != nil {
		profile := profiler.GetProfile(exitTool)
		if profile != nil && profile.Classification == "EXTERNAL_COMM" {
			flow.Risk = "high"
		}
	}
}

// Flows returns all detected PII data flows.
func (dft *DataFlowTracker) Flows() []DataFlow {
	dft.mu.RLock()
	defer dft.mu.RUnlock()
	result := make([]DataFlow, 0, len(dft.flows))
	for _, f := range dft.flows {
		result = append(result, *f)
	}
	return result
}
