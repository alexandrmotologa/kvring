package health

import (
	"strings"
	"testing"
)

func TestParseVllmMetrics(t *testing.T) {
	sampleOutput := `
# HELP vllm:num_requests_running Number of requests currently running on GPU.
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{model_name="meta-llama/Llama-3-70B"} 4.0
# HELP vllm:num_requests_waiting Number of requests waiting to be processed.
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting{model_name="meta-llama/Llama-3-70B"} 2.0
# HELP vllm:gpu_cache_usage_perc GPU KV-cache usage percentage (0.0 to 1.0).
# TYPE vllm:gpu_cache_usage_perc gauge
vllm:gpu_cache_usage_perc{model_name="meta-llama/Llama-3-70B"} 0.742
`

	snapshot := ParseVllmMetrics(strings.NewReader(sampleOutput), "vllm:gpu_cache_usage_perc")
	if !snapshot.Valid {
		t.Fatalf("expected valid metric snapshot")
	}

	if snapshot.VramRatio < 0.74 || snapshot.VramRatio > 0.75 {
		t.Errorf("expected VRAM ratio ~0.742, got %.4f", snapshot.VramRatio)
	}

	if snapshot.RequestsRunning != 4 {
		t.Errorf("expected 4 running requests, got %d", snapshot.RequestsRunning)
	}

	if snapshot.RequestsWaiting != 2 {
		t.Errorf("expected 2 waiting requests, got %d", snapshot.RequestsWaiting)
	}
}

func TestParseSglangMetrics(t *testing.T) {
	sampleOutput := `
sglang:num_requests_running 1.0
sglang:gpu_cache_usage_perc 0.618
`
	snapshot := ParseVllmMetrics(strings.NewReader(sampleOutput), "sglang:gpu_cache_usage_perc")
	if !snapshot.Valid {
		t.Fatalf("expected valid snapshot for sglang")
	}
	if snapshot.VramRatio < 0.61 || snapshot.VramRatio > 0.62 {
		t.Errorf("expected ~0.618, got %.4f", snapshot.VramRatio)
	}
}
