package ring

import (
	"testing"
)

func TestAdmissionControllerPredictiveSpillover(t *testing.T) {
	ac := NewAdmissionController(80, 80*1024, 0.85)

	w := NewWorkerNode("worker-1", "http://127.0.0.1:8001", 1, 64)
	w.SetVramRatio(0.80) // 80% VRAM currently

	// Small request: 1,000 tokens -> ~80MB on 80GB -> +0.1% VRAM -> total 80.1% < 85% -> Accepted
	if !ac.CanAccept(w, 1000) {
		t.Errorf("expected small request of 1,000 tokens to be accepted")
	}

	// Massive context request: 80,000 tokens -> ~6.4GB on 80GB -> +8.0% VRAM -> total 88.0% > 85% -> Rejected!
	if ac.CanAccept(w, 80000) {
		t.Errorf("expected massive request of 80,000 tokens to be rejected by pre-flight admission")
	}
}

func TestRouterPreFlightSpillover(t *testing.T) {
	ring := NewHashRing(100)
	w1 := NewWorkerNode("worker-1", "http://127.0.0.1:8001", 1, 64)
	w2 := NewWorkerNode("worker-2", "http://127.0.0.1:8002", 1, 64)
	w1.SetVramRatio(0.82) // 82% VRAM
	w2.SetVramRatio(0.40) // 40% VRAM
	ring.AddWorker(w1)
	ring.AddWorker(w2)

	router := NewRouter(ring, 0.25, 0.85)

	// Find key mapping to worker-1
	var keyWorker1 string
	for i := 0; i < 500; i++ {
		k := "test-session-" + string(rune('a'+i%26)) + string(rune('0'+i%10))
		idx := ring.SearchIndex(HashKey(k))
		if ring.vNodes[idx].WorkerID == "worker-1" {
			keyWorker1 = k
			break
		}
	}
	if keyWorker1 == "" {
		t.Fatalf("could not find key for worker-1")
	}

	// Route a small request (no pre-flight saturation)
	decSmall, err := router.RouteWithAdmission(keyWorker1, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decSmall.TargetWorker.ID != "worker-1" {
		t.Errorf("expected small request to hit worker-1, got %s", decSmall.TargetWorker.ID)
	}

	// Route a massive 60k token prompt: w1 (82% + 6% = 88% > 85%) must proactively spill over to w2!
	decLarge, err := router.RouteWithAdmission(keyWorker1, 60000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if decLarge.TargetWorker.ID != "worker-2" {
		t.Errorf("expected massive request to spill over to worker-2, got %s", decLarge.TargetWorker.ID)
	}
	if decLarge.Reason != ReasonVramSaturationSpillover {
		t.Errorf("expected vram_spillover reason, got %s", decLarge.Reason)
	}
}
