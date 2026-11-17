package ring

import (
	"strconv"
	"testing"
)

func TestHashRingAddAndRemove(t *testing.T) {
	ring := NewHashRing(100)
	w1 := NewWorkerNode("node-1", "http://127.0.0.1:8001", 1, 64)
	w2 := NewWorkerNode("node-2", "http://127.0.0.1:8002", 1, 64)

	ring.AddWorker(w1)
	ring.AddWorker(w2)

	if ring.TotalVirtualNodes() != 200 {
		t.Fatalf("expected 200 virtual nodes, got %d", ring.TotalVirtualNodes())
	}

	// Verify binary search index lookup
	idx := ring.SearchIndex(123456)
	if idx < 0 || idx >= 200 {
		t.Fatalf("search index out of bounds: %d", idx)
	}

	// Remove node-1
	ring.RemoveWorker("node-1")
	if ring.TotalVirtualNodes() != 100 {
		t.Fatalf("expected 100 virtual nodes after removal, got %d", ring.TotalVirtualNodes())
	}

	if _, ok := ring.GetWorker("node-1"); ok {
		t.Fatalf("expected node-1 to be deleted")
	}
	if _, ok := ring.GetWorker("node-2"); !ok {
		t.Fatalf("expected node-2 to remain present")
	}
}

func TestConsistentHashingDistribution(t *testing.T) {
	ring := NewHashRing(100)
	for i := 1; i <= 3; i++ {
		id := "worker-" + strconv.Itoa(i)
		ring.AddWorker(NewWorkerNode(id, "http://127.0.0.1:800"+strconv.Itoa(i), 1, 64))
	}

	counts := make(map[string]int)
	totalKeys := 10000

	for i := 0; i < totalKeys; i++ {
		key := "session-token-" + strconv.Itoa(i)
		h := HashKey(key)
		idx := ring.SearchIndex(h)
		vnodes := ring.VirtualNodesSnapshot()
		assignedID := vnodes[idx].WorkerID
		counts[assignedID]++
	}

	// Each of the 3 workers should receive roughly 33% of the keys (+- 10%)
	for id, count := range counts {
		share := float64(count) / float64(totalKeys)
		if share < 0.20 || share > 0.45 {
			t.Errorf("worker %s received unexpected key share: %.2f (%d keys)", id, share, count)
		}
	}
}
