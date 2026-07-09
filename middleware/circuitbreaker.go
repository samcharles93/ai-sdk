package middleware

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker rejects a call because
// it is in the OPEN state and the open-timeout has not yet expired.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitState represents the three possible states of the circuit breaker
// state machine: CLOSED (normal operation), OPEN (fail-fast with timeout),
// and HALF_OPEN (limited probing to test recovery).
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitHalfOpen
	CircuitOpen
)

// CircuitBreakerConfig controls the circuit breaker state machine behaviour.
// All fields are required; zero values will cause the breaker to never trip
// (FailureThreshold=0) or never recover (SuccessThreshold=0, OpenTimeout=0).
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of consecutive failures in CLOSED state
	// required to trip the breaker to OPEN.
	FailureThreshold uint32

	// SuccessThreshold is the number of consecutive successes in HALF_OPEN
	// state required to transition back to CLOSED.
	SuccessThreshold uint32

	// OpenTimeout is the duration the breaker stays OPEN before
	// automatically transitioning to HALF_OPEN to probe recovery.
	OpenTimeout time.Duration
}

// circuitBreaker tracks per-circuit state, consecutive failure/success
// counters, and the time the circuit transitioned to OPEN. All access is
// protected by a sync.Mutex for goroutine safety.
type circuitBreaker struct {
	mu                   sync.Mutex
	state                CircuitState
	consecutiveSuccesses uint32
	consecutiveFailures  uint32
	openSince            time.Time
	cfg                  CircuitBreakerConfig
}

// beforeRequest checks the current state and potentially transitions from
// OPEN to HALF_OPEN if the open-timeout has expired. It returns
// ErrCircuitOpen when the breaker is OPEN and the timeout has not yet
// elapsed; otherwise it returns nil to allow the request.
func (cb *circuitBreaker) beforeRequest() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	switch cb.state {
	case CircuitOpen:
		if now.After(cb.openSince.Add(cb.cfg.OpenTimeout)) {
			cb.state = CircuitHalfOpen
			cb.consecutiveSuccesses = 0
			cb.consecutiveFailures = 0
			return nil
		}
		return ErrCircuitOpen
	case CircuitHalfOpen:
		return nil
	default: // CircuitClosed
		return nil
	}
}

// recordResult records the outcome of a provider call. If the call was
// successful, the consecutive-success counter increments and, when in
// HALF_OPEN and the SuccessThreshold is reached, the circuit transitions
// to CLOSED.
//
// If the call failed, the consecutive-failure counter increments. In
// CLOSED, reaching FailureThreshold trips the circuit to OPEN. In
// HALF_OPEN, any single failure immediately trips the circuit back to
// OPEN.
func (cb *circuitBreaker) recordResult(success bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if success {
		cb.consecutiveSuccesses++
		cb.consecutiveFailures = 0
		if cb.state == CircuitHalfOpen && cb.consecutiveSuccesses >= cb.cfg.SuccessThreshold {
			cb.state = CircuitClosed
		}
	} else {
		cb.consecutiveFailures++
		cb.consecutiveSuccesses = 0
		if cb.state == CircuitClosed && cb.consecutiveFailures >= cb.cfg.FailureThreshold {
			cb.state = CircuitOpen
			cb.openSince = time.Now()
		} else if cb.state == CircuitHalfOpen {
			// Any failure during half-open immediately trips back to open.
			cb.state = CircuitOpen
			cb.openSince = time.Now()
		}
	}
}
