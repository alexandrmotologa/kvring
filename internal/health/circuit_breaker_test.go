package health

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexandrmotologa/kvring/internal/config"
	"github.com/alexandrmotologa/kvring/internal/ring"
)

func TestCircuitBreakerTrippingAndRecovery(t *testing.T) {
	r := ring.NewHashRing(100)
	w1 := ring.NewWorkerNode("node-1", "http://127.0.0.1:8001", 1, 64)
	r.AddWorker(w1)

	cb := NewCircuitBreaker(r, 3, 50*time.Millisecond)

	// Record 2 failures - should still be healthy
	cb.RecordFailure("node-1")
	cb.RecordFailure("node-1")
	if !w1.Healthy.Load() {
		t.Fatalf("expected node-1 to remain healthy after 2 failures")
	}

	// 3rd failure trips the breaker
	cb.RecordFailure("node-1")
	if w1.Healthy.Load() {
		t.Fatalf("expected node-1 to be marked unhealthy after 3 failures")
	}

	// Wait for recovery period to elapse
	time.Sleep(60 * time.Millisecond)
	cb.CheckProbes()

	// Should transition to half-open, restoring health for probe
	if !w1.Healthy.Load() {
		t.Fatalf("expected node-1 to be restored for half-open probe")
	}

	// Success resets circuit to closed
	cb.RecordSuccess("node-1")
	if !w1.Healthy.Load() {
		t.Fatalf("expected node-1 to remain healthy after success")
	}
}

func TestPollerScrapesSuccessfully(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/metrics" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, "vllm:gpu_cache_usage_perc 0.485")
			fmt.Fprintln(w, "vllm:num_requests_running 3")
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	r := ring.NewHashRing(100)
	wNode := ring.NewWorkerNode("test-worker", ts.URL, 1, 64)
	r.AddWorker(wNode)

	cb := NewCircuitBreaker(r, 3, 10*time.Second)
	poller := NewPoller(
		r,
		cb,
		[]config.WorkerConfig{
			{
				ID:         "test-worker",
				URL:        ts.URL,
				MetricsURL: ts.URL + "/metrics",
			},
		},
		config.HealthCheckConfig{
			PollInterval:   20 * time.Millisecond,
			VramMetricName: "vllm:gpu_cache_usage_perc",
		},
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	poller.Stop()

	ratio := wNode.GetVramRatio()
	if ratio < 0.48 || ratio > 0.49 {
		t.Fatalf("expected VRAM ratio around 0.485, got %.4f", ratio)
	}
}
