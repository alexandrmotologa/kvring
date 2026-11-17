package ring

import (
	"strconv"
	"testing"
)

func TestRouterDirectHit(t *testing.T) {
	ring := NewHashRing(100)
	w1 := NewWorkerNode("worker-1", "http://127.0.0.1:8001", 1, 64)
	w2 := NewWorkerNode("worker-2", "http://127.0.0.1:8002", 1, 64)
	ring.AddWorker(w1)
	ring.AddWorker(w2)

	router := NewRouter(ring, 0.25, 0.85)

	decision, err := router.Route("session-123")
	if err != nil {
		t.Fatalf("unexpected routing error: %v", err)
	}

	if decision.Reason != ReasonDirectHit {
		t.Errorf("expected direct hit, got %s", decision.Reason)
	}
	if decision.SpilloverSteps != 0 {
		t.Errorf("expected 0 spillover steps, got %d", decision.SpilloverSteps)
	}
}

func TestRouterSpilloverWhenVramSaturated(t *testing.T) {
	ring := NewHashRing(100)
	w1 := NewWorkerNode("worker-1", "http://127.0.0.1:8001", 1, 64)
	w2 := NewWorkerNode("worker-2", "http://127.0.0.1:8002", 1, 64)
	ring.AddWorker(w1)
	ring.AddWorker(w2)

	router := NewRouter(ring, 0.25, 0.85)

	// Determine primary worker for this test key
	testKey := "session-alpha"
	primaryIdx := ring.SearchIndex(HashKey(testKey))
	primaryID := ring.vNodes[primaryIdx].WorkerID

	// Saturate primary worker VRAM above threshold (e.g. 0.90 >= 0.85)
	primaryWorker, _ := ring.GetWorker(primaryID)
	primaryWorker.SetVramRatio(0.92)

	decision, err := router.Route(testKey)
	if err != nil {
		t.Fatalf("unexpected route error: %v", err)
	}

	if decision.Reason != ReasonVramSaturationSpillover {
		t.Errorf("expected reason vram_spillover, got %s", decision.Reason)
	}
	if decision.TargetWorker.ID == primaryID {
		t.Errorf("expected target to spill over away from %s, but stayed on it", primaryID)
	}
	if decision.SpilloverSteps != 1 {
		t.Errorf("expected 1 spillover step, got %d", decision.SpilloverSteps)
	}
}

func TestRouterSpilloverOnBoundedLoad(t *testing.T) {
	ring := NewHashRing(100)
	w1 := NewWorkerNode("worker-1", "http://127.0.0.1:8001", 1, 64)
	w2 := NewWorkerNode("worker-2", "http://127.0.0.1:8002", 1, 64)
	w3 := NewWorkerNode("worker-3", "http://127.0.0.1:8003", 1, 64)
	ring.AddWorker(w1)
	ring.AddWorker(w2)
	ring.AddWorker(w3)

	router := NewRouter(ring, 0.25, 0.85)

	// Simulate in-flight loads:
	// Total load = 10 + 2 + 0 = 12
	// M = 3
	// Average = 4
	// Capacity C = ceil(1.25 * 4) = 5
	w1.ActiveLoad.Store(10) // Over capacity (10 >= 5)
	w2.ActiveLoad.Store(2)  // Under capacity
	w3.ActiveLoad.Store(0)  // Under capacity

	// Find a key that naturally maps to worker-1
	var hotKey string
	for i := 0; i < 500; i++ {
		k := "key-" + strconv.Itoa(i)
		idx := ring.SearchIndex(HashKey(k))
		if ring.vNodes[idx].WorkerID == "worker-1" {
			hotKey = k
			break
		}
	}
	if hotKey == "" {
		t.Fatalf("could not find key mapped to worker-1")
	}

	decision, err := router.Route(hotKey)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decision.PrimaryWorker.ID != "worker-1" {
		t.Errorf("expected primary worker-1, got %s", decision.PrimaryWorker.ID)
	}
	if decision.TargetWorker.ID == "worker-1" {
		t.Errorf("expected target to spill over from worker-1, but got %s", decision.TargetWorker.ID)
	}
	if decision.Reason != ReasonBoundedLoadSpillover {
		t.Errorf("expected bounded_load_spillover, got %s", decision.Reason)
	}
}

func TestRouterEmergencyFallbackWhenAllSaturated(t *testing.T) {
	ring := NewHashRing(100)
	w1 := NewWorkerNode("worker-1", "http://127.0.0.1:8001", 1, 64)
	w2 := NewWorkerNode("worker-2", "http://127.0.0.1:8002", 1, 64)
	ring.AddWorker(w1)
	ring.AddWorker(w2)

	router := NewRouter(ring, 0.25, 0.85)

	// All workers exceed VRAM threshold (0.95 and 0.90)
	w1.SetVramRatio(0.95)
	w2.SetVramRatio(0.90)

	decision, err := router.Route("emergency-key")
	if err != nil {
		t.Fatalf("unexpected routing error: %v", err)
	}

	if decision.Reason != ReasonEmergencyFallback {
		t.Errorf("expected ReasonEmergencyFallback, got %s", decision.Reason)
	}
	// Should select worker-2 because 0.90 < 0.95
	if decision.TargetWorker.ID != "worker-2" {
		t.Errorf("expected fallback to worker-2 (lowest vram), got %s", decision.TargetWorker.ID)
	}
}

func BenchmarkRouterRoute(b *testing.B) {
	ring := NewHashRing(100)
	for i := 1; i <= 5; i++ {
		id := "worker-" + strconv.Itoa(i)
		ring.AddWorker(NewWorkerNode(id, "http://127.0.0.1:800"+strconv.Itoa(i), 1, 64))
	}
	router := NewRouter(ring, 0.25, 0.85)
	key := "benchmark-session-session-xyz-42"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = router.Route(key)
	}
}

func BenchmarkRouterRouteFast(b *testing.B) {
	ring := NewHashRing(100)
	for i := 1; i <= 5; i++ {
		id := "worker-" + strconv.Itoa(i)
		ring.AddWorker(NewWorkerNode(id, "http://127.0.0.1:800"+strconv.Itoa(i), 1, 64))
	}
	router := NewRouter(ring, 0.25, 0.85)
	key := "benchmark-session-session-xyz-42"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _, _ = router.RouteFast(key)
	}
}
