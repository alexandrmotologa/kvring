package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexandrmotologa/kvring/internal/config"
	"github.com/alexandrmotologa/kvring/internal/ring"
	"github.com/alexandrmotologa/kvring/internal/telemetry"
)

func TestEmbeddedCockpitAndAPIServer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Server.ListenAddr = "127.0.0.1:0"
	cfg.Server.CockpitAddr = "127.0.0.1:0"

	hr := ring.NewHashRing(100)
	w1 := ring.NewWorkerNode("node-1", "http://127.0.0.1:8001", 1, 64)
	hr.AddWorker(w1)

	router := ring.NewRouter(hr, 0.25, 0.85)
	tm := telemetry.NewMetricsCollector(10)

	dummyProxy := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"proxy":true}`))
	})

	srv := NewServer(cfg, hr, router, tm, dummyProxy)

	// Test status handler
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/status", nil)
	srv.handleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Verify embedded file system opens index.html
	fs := DistFileSystem()
	f, err := fs.Open("index.html")
	if err != nil {
		t.Fatalf("failed to open embedded index.html: %v", err)
	}
	_ = f.Close()

	// Test server lifecycle
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	if err := srv.Stop(ctx); err != nil {
		t.Fatalf("failed to stop server: %v", err)
	}
}
