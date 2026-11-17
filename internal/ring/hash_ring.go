package ring

import (
	"fmt"
	"hash/crc32"
	"math"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
)

// WorkerNode represents an active backend inference engine (e.g. vLLM or SGLang instance).
type WorkerNode struct {
	ID             string
	URL            string
	Weight         int
	MaxConcurrency int

	// Runtime metrics updated by telemetry and in-flight tracking
	ActiveLoad     atomic.Int64
	VramRatioBits  atomic.Uint64 // stores math.Float64bits(ratio)
	Healthy        atomic.Bool
	TotalRequests  atomic.Uint64
	SpilloversIn   atomic.Uint64
	SpilloversOut  atomic.Uint64
}

// NewWorkerNode creates a healthy worker node initialized with zero active load.
func NewWorkerNode(id, url string, weight, maxConcurrency int) *WorkerNode {
	if weight <= 0 {
		weight = 1
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 64
	}
	wn := &WorkerNode{
		ID:             id,
		URL:            url,
		Weight:         weight,
		MaxConcurrency: maxConcurrency,
	}
	wn.Healthy.Store(true)
	return wn
}

// SetVramRatio updates the current VRAM utilization ratio (0.0 to 1.0) atomically.
func (w *WorkerNode) SetVramRatio(ratio float64) {
	if ratio < 0 {
		ratio = 0
	} else if ratio > 1.0 {
		ratio = 1.0
	}
	w.VramRatioBits.Store(math.Float64bits(ratio))
}

// GetVramRatio returns the current VRAM utilization ratio atomically.
func (w *WorkerNode) GetVramRatio() float64 {
	return math.Float64frombits(w.VramRatioBits.Load())
}

// VirtualNode maps a point on the consistent hash circle to a physical worker.
type VirtualNode struct {
	Hash     uint32
	WorkerID string
}

// HashRing maintains the consistent hash ring with virtual nodes.
type HashRing struct {
	mu               sync.RWMutex
	vNodes           []VirtualNode
	workers          map[string]*WorkerNode
	numReplicas      int
	clusterTotalLoad atomic.Int64
	healthyWorkers   atomic.Int32
}

// NewHashRing initializes an empty consistent hash ring.
func NewHashRing(replicas int) *HashRing {
	if replicas <= 0 {
		replicas = 100
	}
	return &HashRing{
		workers:     make(map[string]*WorkerNode),
		numReplicas: replicas,
	}
}

// AddWorker inserts a physical worker node and its virtual replicas into the ring.
func (r *HashRing) AddWorker(worker *WorkerNode) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.workers[worker.ID] = worker
	if worker.Healthy.Load() {
		r.healthyWorkers.Add(1)
	}
	totalVNodes := r.numReplicas * worker.Weight

	for i := 0; i < totalVNodes; i++ {
		key := worker.ID + "#" + strconv.Itoa(i)
		h := crc32.ChecksumIEEE([]byte(key))
		r.vNodes = append(r.vNodes, VirtualNode{
			Hash:     h,
			WorkerID: worker.ID,
		})
	}

	// Maintain sorted ring for O(log N) binary search
	sort.Slice(r.vNodes, func(i, j int) bool {
		return r.vNodes[i].Hash < r.vNodes[j].Hash
	})
}

// IncrLoad increments both worker-specific and cluster-wide in-flight load counters.
func (r *HashRing) IncrLoad(w *WorkerNode) {
	w.ActiveLoad.Add(1)
	r.clusterTotalLoad.Add(1)
}

// DecrLoad decrements both worker-specific and cluster-wide in-flight load counters.
func (r *HashRing) DecrLoad(w *WorkerNode) {
	w.ActiveLoad.Add(-1)
	r.clusterTotalLoad.Add(-1)
}

// ClusterTotalLoad returns the instantaneous sum of all in-flight requests.
func (r *HashRing) ClusterTotalLoad() int64 {
	val := r.clusterTotalLoad.Load()
	if val < 0 {
		return 0
	}
	return val
}

// HealthyWorkerCount returns the active number of healthy workers.
func (r *HashRing) HealthyWorkerCount() int {
	c := int(r.healthyWorkers.Load())
	if c < 0 {
		return 0
	}
	return c
}

// RemoveWorker deletes a physical worker and all its virtual replicas from the ring.
func (r *HashRing) RemoveWorker(workerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if w, ok := r.workers[workerID]; ok && w.Healthy.Load() {
		r.healthyWorkers.Add(-1)
	}
	delete(r.workers, workerID)

	filtered := make([]VirtualNode, 0, len(r.vNodes))
	for _, vn := range r.vNodes {
		if vn.WorkerID != workerID {
			filtered = append(filtered, vn)
		}
	}
	r.vNodes = filtered
}

// SetWorkerHealth updates a worker's health status and adjusts active counters.
func (r *HashRing) SetWorkerHealth(workerID string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	w, exists := r.workers[workerID]
	if !exists {
		return
	}
	prev := w.Healthy.Swap(healthy)
	if prev != healthy {
		if healthy {
			r.healthyWorkers.Add(1)
		} else {
			r.healthyWorkers.Add(-1)
		}
	}
}

// GetWorker returns a worker node by its ID.
func (r *HashRing) GetWorker(id string) (*WorkerNode, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	w, exists := r.workers[id]
	return w, exists
}

// Workers returns a snapshot slice of all registered workers.
func (r *HashRing) Workers() []*WorkerNode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]*WorkerNode, 0, len(r.workers))
	for _, w := range r.workers {
		res = append(res, w)
	}
	return res
}

// TotalVirtualNodes returns the count of points mapped on the ring.
func (r *HashRing) TotalVirtualNodes() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.vNodes)
}

// SearchIndex locates the closest virtual node index for a given hash value.
func (r *HashRing) SearchIndex(h uint32) int {
	n := len(r.vNodes)
	if n == 0 {
		return -1
	}

	idx := sort.Search(n, func(i int) bool {
		return r.vNodes[i].Hash >= h
	})

	if idx == n {
		return 0 // Wrap around the circle
	}
	return idx
}

// HashKey computes the 32-bit CRC32 IEEE hash of an affinity key.
func HashKey(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}

// VirtualNodesSnapshot returns an ordered copy of the virtual nodes on the ring.
func (r *HashRing) VirtualNodesSnapshot() []VirtualNode {
	r.mu.RLock()
	defer r.mu.RUnlock()
	copied := make([]VirtualNode, len(r.vNodes))
	copy(copied, r.vNodes)
	return copied
}

// Error definitions
var (
	ErrNoWorkersAvailable = fmt.Errorf("no workers available in hash ring")
	ErrAllWorkersDegraded = fmt.Errorf("all cluster workers degraded or saturated")
)
