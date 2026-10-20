package upstream

import "time"

type CircuitState string

const (
	CircuitClosed   CircuitState = "CLOSED"
	CircuitOpen     CircuitState = "OPEN"
	CircuitHalfOpen CircuitState = "HALF_OPEN"
)

type circuit struct {
	state       CircuitState
	failures    int
	openedAt    time.Time
	probeActive bool
}

func newCircuit() circuit { return circuit{state: CircuitClosed} }

func (breaker *circuit) eligible(now time.Time, openDuration time.Duration) bool {
	switch breaker.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		return !now.Before(breaker.openedAt.Add(openDuration))
	case CircuitHalfOpen:
		return !breaker.probeActive
	default:
		return false
	}
}

func (breaker *circuit) selected(now time.Time, openDuration time.Duration) {
	if breaker.state == CircuitOpen && !now.Before(breaker.openedAt.Add(openDuration)) {
		breaker.state = CircuitHalfOpen
	}
	if breaker.state == CircuitHalfOpen {
		breaker.probeActive = true
	}
}

func (breaker *circuit) success() {
	breaker.state = CircuitClosed
	breaker.failures = 0
	breaker.probeActive = false
}

func (breaker *circuit) failure(now time.Time, threshold int) {
	if breaker.state == CircuitHalfOpen {
		breaker.state = CircuitOpen
		breaker.openedAt = now
		breaker.probeActive = false
		return
	}
	if breaker.state != CircuitClosed {
		return
	}
	breaker.failures++
	if breaker.failures >= threshold {
		breaker.state = CircuitOpen
		breaker.openedAt = now
	}
}
