package health

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// MetricSnapshot contains parsed telemetry from a vLLM or SGLang worker.
type MetricSnapshot struct {
	VramRatio      float64
	RequestsWaiting int
	RequestsRunning int
	Valid          bool
}

// ParseVllmMetrics extracts GPU cache usage and queue depths from Prometheus text output.
func ParseVllmMetrics(r io.Reader, targetMetric string) MetricSnapshot {
	if targetMetric == "" {
		targetMetric = "vllm:gpu_cache_usage_perc"
	}

	scanner := bufio.NewScanner(r)
	snapshot := MetricSnapshot{}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		metricNameAndLabels := parts[0]
		valStr := parts[len(parts)-1]

		// Extract metric name by stripping labels {...}
		metricName := metricNameAndLabels
		if braceIdx := strings.IndexByte(metricNameAndLabels, '{'); braceIdx != -1 {
			metricName = metricNameAndLabels[:braceIdx]
		}

		val, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}

		// Check for VRAM / KV cache ratio
		if metricName == targetMetric ||
			metricName == "gpu_cache_usage_perc" ||
			metricName == "sglang:gpu_cache_usage_perc" ||
			strings.HasSuffix(metricName, ":gpu_cache_usage_perc") {
			snapshot.VramRatio = val
			snapshot.Valid = true
		} else if strings.Contains(metricName, "num_requests_waiting") {
			snapshot.RequestsWaiting = int(val)
		} else if strings.Contains(metricName, "num_requests_running") {
			snapshot.RequestsRunning = int(val)
		}
	}

	return snapshot
}
