package baseline

import (
	"sync"
	"time"
)

// LatencyTracker monitors upstream MCP server response times using EWMA.
type LatencyTracker struct {
	mu      sync.RWMutex
	servers map[string]*ServerHealth
}

// NewLatencyTracker creates a new server latency tracker.
func NewLatencyTracker() *LatencyTracker {
	return &LatencyTracker{
		servers: make(map[string]*ServerHealth),
	}
}

// Record adds a latency observation for a server.
func (lt *LatencyTracker) Record(serverName string, latencyMs float64, isError bool) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	sh, ok := lt.servers[serverName]
	if !ok {
		sh = &ServerHealth{Name: serverName, Status: "healthy"}
		lt.servers[serverName] = sh
	}

	sh.SampleCount++

	alpha := AlphaResponsive
	if sh.SampleCount > MinSamplesForBaseline {
		alpha = AlphaStable
	}

	sh.MeanLatencyMs = ewma(sh.MeanLatencyMs, latencyMs, alpha)
	sh.CurrentLatencyMs = latencyMs

	errorVal := 0.0
	if isError {
		errorVal = 1.0
	}
	sh.ErrorRate = ewma(sh.ErrorRate, errorVal, alpha)

	// Determine status
	sh.Status = lt.computeStatus(sh)
}

// Get returns the health status for a server.
func (lt *LatencyTracker) Get(serverName string) *ServerHealth {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	sh := lt.servers[serverName]
	if sh == nil {
		return nil
	}
	copy := *sh
	return &copy
}

// All returns all server health records.
func (lt *LatencyTracker) All() map[string]ServerHealth {
	lt.mu.RLock()
	defer lt.mu.RUnlock()
	result := make(map[string]ServerHealth, len(lt.servers))
	for k, v := range lt.servers {
		result[k] = *v
	}
	return result
}

// SetServers loads server health records (for persistence restore).
func (lt *LatencyTracker) SetServers(servers map[string]ServerHealth) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	for k, v := range servers {
		copy := v
		lt.servers[k] = &copy
	}
}

func (lt *LatencyTracker) computeStatus(sh *ServerHealth) string {
	if sh.SampleCount < 10 {
		return "healthy" // not enough data
	}
	if sh.ErrorRate > 0.5 {
		return "down"
	}
	if sh.MeanLatencyMs > 0 && sh.CurrentLatencyMs > sh.MeanLatencyMs*DeviationThreshold {
		return "degraded"
	}
	if sh.ErrorRate > 0.1 {
		return "degraded"
	}
	return "healthy"
}

// StartPeriodicSave starts a goroutine that saves state every interval.
func StartPeriodicSave(path string, tracker *Tracker, latency *LatencyTracker, interval time.Duration, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := SaveState(path, tracker, latency.All()); err != nil {
					// Log but don't crash
					_ = err
				}
			case <-stop:
				// Final save on shutdown
				SaveState(path, tracker, latency.All())
				return
			}
		}
	}()
}
