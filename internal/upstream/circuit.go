package upstream

import (
	"log/slog"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // normal — traffic flows
	CircuitOpen                         // tripped — reject calls, probe periodically
	CircuitHalfOpen                     // probing — allow one call to test recovery
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half_open"
	default:
		return "unknown"
	}
}

const (
	circuitFailureThreshold = 3                // consecutive failures to trip
	circuitProbeInterval    = 30 * time.Second // how often to probe in OPEN state
)

// CircuitBreaker implements a per-server circuit breaker.
// Thread-safe for concurrent access from multiple goroutines.
type CircuitBreaker struct {
	mu                sync.Mutex
	name              string // server name, for logging
	state             CircuitState
	consecutiveErrors int
	lastFailure       time.Time
	lastStateChange   time.Time
	halfOpenInFlight  bool // true when a probe call is in progress
}

// NewCircuitBreaker creates a circuit breaker in CLOSED state.
func NewCircuitBreaker(name ...string) *CircuitBreaker {
	n := ""
	if len(name) > 0 {
		n = name[0]
	}
	return &CircuitBreaker{
		name:            n,
		state:           CircuitClosed,
		lastStateChange: time.Now(),
	}
}

// State returns the current circuit state.
// Also performs auto-transition from OPEN to HALF_OPEN after probe interval.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= circuitProbeInterval {
		cb.state = CircuitHalfOpen
		cb.lastStateChange = time.Now()
		cb.halfOpenInFlight = false
	}
	return cb.state
}

// AllowRequest returns true if a request should be allowed through.
// In CLOSED state, always allows.
// In OPEN state, rejects.
// In HALF_OPEN state, allows exactly one probe call.
func (cb *CircuitBreaker) AllowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	// Auto-transition OPEN → HALF_OPEN
	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= circuitProbeInterval {
		cb.state = CircuitHalfOpen
		cb.lastStateChange = time.Now()
		cb.halfOpenInFlight = false
	}

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitHalfOpen:
		if cb.halfOpenInFlight {
			return false // another probe is already in progress
		}
		cb.halfOpenInFlight = true
		return true
	default: // CircuitOpen
		return false
	}
}

// RecordSuccess records a successful call. Resets errors and closes the circuit.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveErrors = 0
	cb.halfOpenInFlight = false
	if cb.state != CircuitClosed {
		prev := cb.state
		cb.state = CircuitClosed
		cb.lastStateChange = time.Now()
		if cb.name != "" {
			slog.Info("circuit closed", "component", "circuit", "server", cb.name, "previous_state", prev)
		}
	}
}

// RecordFailure records a failed call. May trip the circuit to OPEN.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.consecutiveErrors++
	cb.lastFailure = time.Now()

	if cb.consecutiveErrors >= circuitFailureThreshold && cb.state == CircuitClosed {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
		if cb.name != "" {
			slog.Warn("circuit opened", "component", "circuit", "server", cb.name, "consecutive_failures", cb.consecutiveErrors)
		}
	}
	// HALF_OPEN probe failed → back to OPEN
	if cb.state == CircuitHalfOpen {
		cb.state = CircuitOpen
		cb.lastStateChange = time.Now()
		cb.halfOpenInFlight = false
		if cb.name != "" {
			slog.Warn("circuit half_open probe failed, reopened", "component", "circuit", "server", cb.name)
		}
	}
}

// ConsecutiveErrors returns the current consecutive error count.
func (cb *CircuitBreaker) ConsecutiveErrors() int {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.consecutiveErrors
}

// LastStateChange returns when the circuit last changed state.
func (cb *CircuitBreaker) LastStateChange() time.Time {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.lastStateChange
}
