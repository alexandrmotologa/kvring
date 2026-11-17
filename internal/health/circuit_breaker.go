package health

import (
	"sync"
	"time"

	"github.com/alexandrmotologa/kvring/internal/ring"
)

// CircuitState represents the current isolation status of a worker.
type CircuitState int

const (
	StateClosed   CircuitState = iota // Healthy, passing all traffic
	StateOpen                         // Tripped, isolated from ring
	StateHalfOpen                     // Probing to verify recovery
)

// WorkerCircuit maintains passive failure stats for an individual worker.
type WorkerCircuit struct {
	mu                  sync.Mutex
	workerID            string
	state               CircuitState
	consecutiveFailures int
	failureThreshold    int
	recoveryDuration    time.Duration
	lastStateChange     time.Time
}

// CircuitBreaker manages outlier detection across all cluster workers.
type CircuitBreaker struct {
	mu               sync.RWMutex
	circuits         map[string]*WorkerCircuit
	ring             *ring.HashRing
	failureThreshold int
	recoveryDuration time.Duration
}

// NewCircuitBreaker creates a circuit breaker bound to a hash ring.
func NewCircuitBreaker(r *ring.HashRing, threshold int, recovery time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if recovery <= 0 {
		recovery = 15 * time.Second
	}
	return &CircuitBreaker{
		circuits:         make(map[string]*WorkerCircuit),
		ring:             r,
		failureThreshold: threshold,
		recoveryDuration: recovery,
	}
}

// getOrCreateCircuit returns the circuit tracker for a worker.
func (cb *CircuitBreaker) getOrCreateCircuit(workerID string) *WorkerCircuit {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.circuits[workerID]
	if !exists {
		c = &WorkerCircuit{
			workerID:         workerID,
			state:            StateClosed,
			failureThreshold: cb.failureThreshold,
			recoveryDuration: cb.recoveryDuration,
			lastStateChange:  time.Now(),
		}
		cb.circuits[workerID] = c
	}
	return c
}

// RecordSuccess notes a successful HTTP request, resetting failure counters.
func (cb *CircuitBreaker) RecordSuccess(workerID string) {
	c := cb.getOrCreateCircuit(workerID)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.consecutiveFailures = 0
	if c.state == StateHalfOpen || c.state == StateOpen {
		c.state = StateClosed
		c.lastStateChange = time.Now()
		cb.ring.SetWorkerHealth(workerID, true)
	}
}

// RecordFailure notes a 5xx error or connection drop.
func (cb *CircuitBreaker) RecordFailure(workerID string) {
	c := cb.getOrCreateCircuit(workerID)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.consecutiveFailures++
	if c.consecutiveFailures >= c.failureThreshold && c.state == StateClosed {
		c.state = StateOpen
		c.lastStateChange = time.Now()
		cb.ring.SetWorkerHealth(workerID, false)
	}
}

// CheckProbes transitions open circuits to half-open when recovery period elapses.
func (cb *CircuitBreaker) CheckProbes() {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	now := time.Now()
	for _, c := range cb.circuits {
		c.mu.Lock()
		if c.state == StateOpen && now.Sub(c.lastStateChange) >= c.recoveryDuration {
			c.state = StateHalfOpen
			c.lastStateChange = now
			cb.ring.SetWorkerHealth(c.workerID, true) // allow a probe request
		}
		c.mu.Unlock()
	}
}
