package ring

import (
	"testing"
)

func TestRingManagerModelIsolation(t *testing.T) {
	rm := NewRingManager(100, 0.25, 0.85)

	wLlama := NewWorkerNode("worker-llama", "http://127.0.0.1:8001", 1, 64)
	wQwen := NewWorkerNode("worker-qwen", "http://127.0.0.1:8002", 1, 64)

	rm.AddWorkerToModels(wLlama, []string{"meta-llama/Llama-3.3-70B"})
	rm.AddWorkerToModels(wQwen, []string{"Qwen/Qwen2.5-72B"})

	// Verify Llama router only directs to Llama worker
	routerLlama, _ := rm.GetRouter("meta-llama/Llama-3.3-70B")
	decLlama, err := routerLlama.Route("session-1")
	if err != nil {
		t.Fatalf("unexpected routing error: %v", err)
	}
	if decLlama.TargetWorker.ID != "worker-llama" {
		t.Errorf("expected worker-llama, got %s", decLlama.TargetWorker.ID)
	}

	// Verify Qwen router only directs to Qwen worker
	routerQwen, _ := rm.GetRouter("Qwen/Qwen2.5-72B")
	decQwen, err := routerQwen.Route("session-1")
	if err != nil {
		t.Fatalf("unexpected routing error: %v", err)
	}
	if decQwen.TargetWorker.ID != "worker-qwen" {
		t.Errorf("expected worker-qwen, got %s", decQwen.TargetWorker.ID)
	}

	// Unknown model falls back to default router with both workers
	routerDefault, ringDefault := rm.GetRouter("unknown-model")
	if ringDefault.HealthyWorkerCount() != 2 {
		t.Errorf("expected 2 workers in default ring, got %d", ringDefault.HealthyWorkerCount())
	}
	_ = routerDefault
}
