package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexandrmotologa/kvring/internal/config"
	"github.com/alexandrmotologa/kvring/internal/ring"
	"github.com/alexandrmotologa/kvring/internal/telemetry"
)

func TestKubernetesProbes(t *testing.T) {
	cfg := config.DefaultConfig()
	hr := ring.NewHashRing(100)
	w1 := ring.NewWorkerNode("node-1", "http://127.0.0.1:8001", 1, 64)
	hr.AddWorker(w1)

	router := ring.NewRouter(hr, 0.25, 0.85)
	tm := telemetry.NewMetricsCollector(10)
	srv := NewServer(cfg, hr, router, tm, http.NotFoundHandler())

	// 1. /healthz
	rec := httptest.NewRecorder()
	srv.handleHealthz(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("expected 200 ok for healthz, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. /livez
	recLive := httptest.NewRecorder()
	srv.handleLivez(recLive, httptest.NewRequest("GET", "/livez", nil))
	if recLive.Code != http.StatusOK || !strings.Contains(recLive.Body.String(), `"status":"alive"`) {
		t.Fatalf("expected 200 alive for livez, got %d: %s", recLive.Code, recLive.Body.String())
	}

	// 3. /readyz with 1 healthy worker
	recReady := httptest.NewRecorder()
	srv.handleReadyz(recReady, httptest.NewRequest("GET", "/readyz", nil))
	if recReady.Code != http.StatusOK || !strings.Contains(recReady.Body.String(), `"healthy_nodes":1`) {
		t.Fatalf("expected 200 ready for readyz, got %d: %s", recReady.Code, recReady.Body.String())
	}

	// 4. /readyz when all workers degraded (0 healthy)
	hr.SetWorkerHealth("node-1", false)
	recDegraded := httptest.NewRecorder()
	srv.handleReadyz(recDegraded, httptest.NewRequest("GET", "/readyz", nil))
	if recDegraded.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 unavailable for readyz when degraded, got %d", recDegraded.Code)
	}
}
