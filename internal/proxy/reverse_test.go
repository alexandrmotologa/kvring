package proxy

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexandrmotologa/kvring/internal/config"
	"github.com/alexandrmotologa/kvring/internal/health"
	"github.com/alexandrmotologa/kvring/internal/ring"
	"github.com/alexandrmotologa/kvring/internal/telemetry"
)

func TestProxyStandardAndStreamingRouting(t *testing.T) {
	// Setup 2 mock backend workers
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			fmt.Fprintf(w, "data: {\"token\":\"Hello\"}\n\n")
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
			fmt.Fprintf(w, "data: {\"token\":\"World\"}\n\n")
			flusher.Flush()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"id":"ts1-response"}`)
	}))
	defer ts1.Close()

	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"id":"ts2-response"}`)
	}))
	defer ts2.Close()

	hr := ring.NewHashRing(100)
	w1 := ring.NewWorkerNode("worker-1", ts1.URL, 1, 64)
	w2 := ring.NewWorkerNode("worker-2", ts2.URL, 1, 64)
	hr.AddWorker(w1)
	hr.AddWorker(w2)

	router := ring.NewRouter(hr, 0.25, 0.85)
	extractor := ring.NewAffinityExtractor(config.AffinityConfig{
		HeaderKey: "X-Session-ID",
	})
	cb := health.NewCircuitBreaker(hr, 3, 5*time.Second)
	tm := telemetry.NewMetricsCollector(50)

	proxy := NewProxy(router, hr, extractor, cb, tm)

	// Test Case 1: Standard JSON Request
	req1 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"llama"}`))
	req1.Header.Set("X-Session-ID", "sess-1")
	rec1 := httptest.NewRecorder()

	proxy.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec1.Code)
	}
	workerHdr := rec1.Header().Get("X-KVRing-Worker")
	if workerHdr == "" {
		t.Fatalf("expected X-KVRing-Worker header to be populated")
	}

	// Test Case 2: Streaming SSE Request
	req2 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"stream":true}`))
	req2.Header.Set("X-Session-ID", "sess-1")
	req2.Header.Set("Accept", "text/event-stream")
	rec2 := httptest.NewRecorder()

	proxy.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("expected status 200 for SSE, got %d", rec2.Code)
	}
	bodyStr := rec2.Body.String()
	if !strings.Contains(bodyStr, "Hello") || !strings.Contains(bodyStr, "World") {
		t.Errorf("expected streamed SSE chunks in body, got: %s", bodyStr)
	}

	// Verify telemetry events recorded
	events := tm.RecentEvents()
	if len(events) < 2 {
		t.Errorf("expected at least 2 telemetry events, got %d", len(events))
	}
}
