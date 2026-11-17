package ring

import (
	"math"
	"time"
)

// RoutingReason indicates how a routing decision was resolved.
type RoutingReason string

const (
	ReasonDirectHit              RoutingReason = "direct_hit"
	ReasonRadixPrefixMatch       RoutingReason = "radix_prefix_match"
	ReasonBoundedLoadSpillover   RoutingReason = "bounded_load_spillover"
	ReasonVramSaturationSpillover RoutingReason = "vram_spillover"
	ReasonCircuitBreakerSpillover RoutingReason = "circuit_breaker_spillover"
	ReasonEmergencyFallback      RoutingReason = "emergency_fallback"
)

// RouteDecision contains the selected worker and diagnostic metadata.
type RouteDecision struct {
	TargetWorker    *WorkerNode
	PrimaryWorker   *WorkerNode
	Reason          RoutingReason
	SpilloverSteps  int
	CapacityCeiling int64
	TotalActiveLoad int64
	RoutingLatency  time.Duration
}

// Router executes Mirrokni consistent hashing with bounded loads and VRAM telemetry.
type Router struct {
	ring          *HashRing
	radixTree     *RadixTree
	admission     *AdmissionController
	epsilon       float64
	vramThreshold float64
}

// NewRouter creates a routing engine configured with load tolerance and VRAM bounds.
func NewRouter(ring *HashRing, epsilon, vramThreshold float64) *Router {
	if epsilon <= 0 {
		epsilon = 0.25
	}
	if vramThreshold <= 0 || vramThreshold > 1.0 {
		vramThreshold = 0.85
	}
	return &Router{
		ring:          ring,
		radixTree:     NewRadixTree(32, 1*time.Hour),
		admission:     NewAdmissionController(80, 80*1024, vramThreshold),
		epsilon:       epsilon,
		vramThreshold: vramThreshold,
	}
}

// AdmissionController returns the pre-flight admission controller.
func (r *Router) AdmissionController() *AdmissionController {
	return r.admission
}

// RadixTree returns the internal prefix tree for inspection or configuration.
func (r *Router) RadixTree() *RadixTree {
	return r.radixTree
}

// RouteWithPrompt inspects the prompt for radix prefix cache affinity before falling back to hash ring.
func (r *Router) RouteWithPrompt(affinityKey, prompt string) (*RouteDecision, error) {
	start := time.Now()

	// 1. Try Hierarchical Radix Prefix Matching first
	if len(prompt) >= 32 {
		matchedWorkerID, matchedLen := r.radixTree.LongestPrefixMatch(prompt)
		if matchedWorkerID != "" && matchedLen >= 32 {
			w, exists := r.ring.GetWorker(matchedWorkerID)
			if exists && w.Healthy.Load() && w.GetVramRatio() < r.vramThreshold {
				totalLoad := r.ring.ClusterTotalLoad()
				healthyCount := r.ring.HealthyWorkerCount()
				capacity := r.ComputeCapacity(totalLoad, healthyCount)

				if w.ActiveLoad.Load() < capacity {
					w.TotalRequests.Add(1)
					// Refresh prefix residence
					r.radixTree.Insert(prompt, w.ID)

					return &RouteDecision{
						TargetWorker:    w,
						PrimaryWorker:   w,
						Reason:          ReasonRadixPrefixMatch,
						SpilloverSteps:  0,
						CapacityCeiling: capacity,
						TotalActiveLoad: totalLoad,
						RoutingLatency:  time.Since(start),
					}, nil
				}
			}
		}
	}

	// 2. Fall back to standard Bounded-Load Consistent Hashing
	dec, err := r.Route(affinityKey)
	if err == nil && dec != nil && len(prompt) >= 32 {
		r.radixTree.Insert(prompt, dec.TargetWorker.ID)
	}
	return dec, err
}

// ComputeCapacity calculates dynamic ceiling C = ceil((1 + epsilon) * L_total / M).
func (r *Router) ComputeCapacity(totalLoad int64, healthyWorkers int) int64 {
	if healthyWorkers <= 0 {
		return 1
	}
	if totalLoad <= 0 {
		return 1
	}

	average := float64(totalLoad) / float64(healthyWorkers)
	bound := math.Ceil((1.0 + r.epsilon) * average)
	if bound < 1 {
		return 1
	}
	return int64(bound)
}

// RouteFast is the zero-allocation fast-path router returning just target worker and reason.
func (r *Router) RouteFast(affinityKey string) (*WorkerNode, RoutingReason, error) {
	r.ring.mu.RLock()
	nNodes := len(r.ring.vNodes)
	if nNodes == 0 {
		r.ring.mu.RUnlock()
		return nil, "", ErrNoWorkersAvailable
	}

	healthyCount := r.ring.HealthyWorkerCount()
	if healthyCount == 0 {
		r.ring.mu.RUnlock()
		return nil, "", ErrAllWorkersDegraded
	}

	totalLoad := r.ring.ClusterTotalLoad()
	capacity := r.ComputeCapacity(totalLoad, healthyCount)

	h := HashKey(affinityKey)
	idx := r.ring.SearchIndex(h)
	if idx < 0 {
		r.ring.mu.RUnlock()
		return nil, "", ErrNoWorkersAvailable
	}

	primaryID := r.ring.vNodes[idx].WorkerID
	primaryWorker := r.ring.workers[primaryID]

	// Hot path direct hit check
	if primaryWorker.Healthy.Load() && primaryWorker.GetVramRatio() < r.vramThreshold && primaryWorker.ActiveLoad.Load() < capacity {
		r.ring.mu.RUnlock()
		primaryWorker.TotalRequests.Add(1)
		return primaryWorker, ReasonDirectHit, nil
	}

	var rejectReason RoutingReason
	if !primaryWorker.Healthy.Load() {
		rejectReason = ReasonCircuitBreakerSpillover
	} else if primaryWorker.GetVramRatio() >= r.vramThreshold {
		rejectReason = ReasonVramSaturationSpillover
	} else {
		rejectReason = ReasonBoundedLoadSpillover
	}

	// Clockwise spillover walk
	visited := make(map[string]bool, len(r.ring.workers))
	visited[primaryID] = true

	for i := 1; i < nNodes; i++ {
		nextIdx := (idx + i) % nNodes
		candID := r.ring.vNodes[nextIdx].WorkerID

		if visited[candID] {
			continue
		}
		visited[candID] = true

		candWorker := r.ring.workers[candID]
		if candWorker.Healthy.Load() && candWorker.GetVramRatio() < r.vramThreshold && candWorker.ActiveLoad.Load() < capacity {
			r.ring.mu.RUnlock()
			candWorker.TotalRequests.Add(1)
			candWorker.SpilloversIn.Add(1)
			primaryWorker.SpilloversOut.Add(1)
			return candWorker, rejectReason, nil
		}
	}

	// Fallback
	var lowestVramWorker *WorkerNode
	var lowestVramVal float64 = 2.0
	for _, w := range r.ring.workers {
		if w.Healthy.Load() {
			v := w.GetVramRatio()
			if v < lowestVramVal {
				lowestVramVal = v
				lowestVramWorker = w
			}
		}
	}
	r.ring.mu.RUnlock()

	if lowestVramWorker != nil {
		lowestVramWorker.TotalRequests.Add(1)
		lowestVramWorker.SpilloversIn.Add(1)
		primaryWorker.SpilloversOut.Add(1)
		return lowestVramWorker, ReasonEmergencyFallback, nil
	}

	return nil, "", ErrAllWorkersDegraded
}
// Route maps an affinity key to an underloaded, healthy worker node.
func (r *Router) Route(affinityKey string) (*RouteDecision, error) {
	return r.RouteWithAdmission(affinityKey, 0)
}

// RouteWithAdmission performs pre-flight predictive VRAM checks before routing.
func (r *Router) RouteWithAdmission(affinityKey string, estimatedTokens int) (*RouteDecision, error) {
	start := time.Now()

	r.ring.mu.RLock()
	nNodes := len(r.ring.vNodes)
	if nNodes == 0 {
		r.ring.mu.RUnlock()
		return nil, ErrNoWorkersAvailable
	}

	healthyCount := r.ring.HealthyWorkerCount()
	if healthyCount == 0 {
		r.ring.mu.RUnlock()
		return nil, ErrAllWorkersDegraded
	}

	totalLoad := r.ring.ClusterTotalLoad()
	capacity := r.ComputeCapacity(totalLoad, healthyCount)

	// Step 1: Find primary affinity worker on the ring
	h := HashKey(affinityKey)
	idx := r.ring.SearchIndex(h)
	if idx < 0 {
		r.ring.mu.RUnlock()
		return nil, ErrNoWorkersAvailable
	}

	primaryID := r.ring.vNodes[idx].WorkerID
	primaryWorker := r.ring.workers[primaryID]

	// Check direct acceptance (Hot path with pre-flight check)
	isVramAcceptable := primaryWorker.GetVramRatio() < r.vramThreshold
	if estimatedTokens > 0 && r.admission != nil {
		isVramAcceptable = r.admission.CanAccept(primaryWorker, estimatedTokens)
	}

	if primaryWorker.Healthy.Load() && isVramAcceptable && primaryWorker.ActiveLoad.Load() < capacity {
		r.ring.mu.RUnlock()
		primaryWorker.TotalRequests.Add(1)
		return &RouteDecision{
			TargetWorker:    primaryWorker,
			PrimaryWorker:   primaryWorker,
			Reason:          ReasonDirectHit,
			SpilloverSteps:  0,
			CapacityCeiling: capacity,
			TotalActiveLoad: totalLoad,
			RoutingLatency:  time.Since(start),
		}, nil
	}

	var rejectReason RoutingReason
	if !primaryWorker.Healthy.Load() {
		rejectReason = ReasonCircuitBreakerSpillover
	} else if !isVramAcceptable {
		rejectReason = ReasonVramSaturationSpillover
	} else {
		rejectReason = ReasonBoundedLoadSpillover
	}

	// Step 2: Clockwise spillover walk
	visited := make(map[string]bool, len(r.ring.workers))
	visited[primaryID] = true
	spilloverSteps := 0

	for i := 1; i < nNodes; i++ {
		nextIdx := (idx + i) % nNodes
		candID := r.ring.vNodes[nextIdx].WorkerID

		if visited[candID] {
			continue
		}
		visited[candID] = true
		spilloverSteps++

		candWorker := r.ring.workers[candID]
		candVramAcceptable := candWorker.GetVramRatio() < r.vramThreshold
		if estimatedTokens > 0 && r.admission != nil {
			candVramAcceptable = r.admission.CanAccept(candWorker, estimatedTokens)
		}

		if candWorker.Healthy.Load() && candVramAcceptable && candWorker.ActiveLoad.Load() < capacity {
			r.ring.mu.RUnlock()
			candWorker.TotalRequests.Add(1)
			candWorker.SpilloversIn.Add(1)
			primaryWorker.SpilloversOut.Add(1)

			return &RouteDecision{
				TargetWorker:    candWorker,
				PrimaryWorker:   primaryWorker,
				Reason:          rejectReason,
				SpilloverSteps:  spilloverSteps,
				CapacityCeiling: capacity,
				TotalActiveLoad: totalLoad,
				RoutingLatency:  time.Since(start),
			}, nil
		}
	}

	// Step 3: Emergency fallback when entire cluster exceeds load bounds or VRAM limits.
	var lowestVramWorker *WorkerNode
	var lowestVramVal float64 = 2.0
	for _, w := range r.ring.workers {
		if w.Healthy.Load() {
			v := w.GetVramRatio()
			if v < lowestVramVal {
				lowestVramVal = v
				lowestVramWorker = w
			}
		}
	}
	r.ring.mu.RUnlock()

	if lowestVramWorker != nil {
		lowestVramWorker.TotalRequests.Add(1)
		lowestVramWorker.SpilloversIn.Add(1)
		primaryWorker.SpilloversOut.Add(1)

		return &RouteDecision{
			TargetWorker:    lowestVramWorker,
			PrimaryWorker:   primaryWorker,
			Reason:          ReasonEmergencyFallback,
			SpilloverSteps:  spilloverSteps,
			CapacityCeiling: capacity,
			TotalActiveLoad: totalLoad,
			RoutingLatency:  time.Since(start),
		}, nil
	}

	return nil, ErrAllWorkersDegraded
}
