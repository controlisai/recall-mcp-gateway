package upstream

import (
	"testing"
	"time"
)

func TestCircuitBreaker_StartsClose(t *testing.T) {
	cb := NewCircuitBreaker()
	if cb.State() != CircuitClosed {
		t.Errorf("initial state = %v, want closed", cb.State())
	}
	if !cb.AllowRequest() {
		t.Error("should allow requests when closed")
	}
}

func TestCircuitBreaker_TripsAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker()

	// 2 failures — still closed
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Errorf("after 2 failures: state = %v, want closed", cb.State())
	}

	// 3rd failure — trips to open
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("after 3 failures: state = %v, want open", cb.State())
	}
	if cb.AllowRequest() {
		t.Error("should reject requests when open")
	}
}

func TestCircuitBreaker_SuccessResets(t *testing.T) {
	cb := NewCircuitBreaker()

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // reset before threshold
	if cb.ConsecutiveErrors() != 0 {
		t.Errorf("errors = %d after success, want 0", cb.ConsecutiveErrors())
	}

	// Need 3 more failures to trip
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitClosed {
		t.Error("should still be closed after 2 failures post-reset")
	}
}

func TestCircuitBreaker_HalfOpenOnProbeInterval(t *testing.T) {
	cb := NewCircuitBreaker()

	// Trip circuit
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatal("expected open")
	}

	// Simulate time passing beyond probe interval
	cb.mu.Lock()
	cb.lastFailure = time.Now().Add(-circuitProbeInterval - time.Second)
	cb.mu.Unlock()

	if cb.State() != CircuitHalfOpen {
		t.Errorf("after probe interval: state = %v, want half_open", cb.State())
	}
	if !cb.AllowRequest() {
		t.Error("should allow request in half_open (probe)")
	}
}

func TestCircuitBreaker_HalfOpenSuccessCloses(t *testing.T) {
	cb := NewCircuitBreaker()

	// Trip to open
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	// Move to half-open
	cb.mu.Lock()
	cb.lastFailure = time.Now().Add(-circuitProbeInterval - time.Second)
	cb.mu.Unlock()
	_ = cb.State() // trigger transition

	// Success in half-open → closed
	cb.RecordSuccess()
	if cb.State() != CircuitClosed {
		t.Errorf("after half-open success: state = %v, want closed", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker()

	// Trip to open
	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordFailure()

	// Move to half-open
	cb.mu.Lock()
	cb.state = CircuitHalfOpen
	cb.mu.Unlock()

	// Failure in half-open → back to open
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Errorf("after half-open failure: state = %v, want open", cb.State())
	}
}

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		state CircuitState
		want  string
	}{
		{CircuitClosed, "closed"},
		{CircuitOpen, "open"},
		{CircuitHalfOpen, "half_open"},
		{CircuitState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("CircuitState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}
