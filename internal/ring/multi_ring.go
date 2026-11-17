package ring

import (
	"sync"
)

// RingManager coordinates multiple model-specific consistent hash rings and routers.
type RingManager struct {
	mu            sync.RWMutex
	defaultRouter *Router
	defaultRing   *HashRing
	modelRouters  map[string]*Router
	modelRings    map[string]*HashRing
	epsilon       float64
	vramThreshold float64
	replicas      int
}

// NewRingManager creates an engine capable of managing multi-model clusters.
func NewRingManager(replicas int, epsilon, vramThreshold float64) *RingManager {
	defRing := NewHashRing(replicas)
	defRouter := NewRouter(defRing, epsilon, vramThreshold)

	return &RingManager{
		defaultRouter: defRouter,
		defaultRing:   defRing,
		modelRouters:  make(map[string]*Router),
		modelRings:    make(map[string]*HashRing),
		epsilon:       epsilon,
		vramThreshold: vramThreshold,
		replicas:      replicas,
	}
}

// AddWorkerToModels associates a worker with its supported LLM models.
func (rm *RingManager) AddWorkerToModels(worker *WorkerNode, models []string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Always register in default ring
	rm.defaultRing.AddWorker(worker)

	for _, m := range models {
		if m == "" || m == "*" {
			continue
		}
		ring, exists := rm.modelRings[m]
		if !exists {
			ring = NewHashRing(rm.replicas)
			router := NewRouter(ring, rm.epsilon, rm.vramThreshold)
			rm.modelRings[m] = ring
			rm.modelRouters[m] = router
		}
		ring.AddWorker(worker)
	}
}

// GetRouter returns the model-specific router if configured, or the default router.
func (rm *RingManager) GetRouter(model string) (*Router, *HashRing) {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	if model != "" {
		if router, exists := rm.modelRouters[model]; exists {
			return router, rm.modelRings[model]
		}
	}
	return rm.defaultRouter, rm.defaultRing
}

// DefaultRouter returns the primary fallback router.
func (rm *RingManager) DefaultRouter() *Router {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.defaultRouter
}

// DefaultRing returns the primary fallback ring.
func (rm *RingManager) DefaultRing() *HashRing {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.defaultRing
}
