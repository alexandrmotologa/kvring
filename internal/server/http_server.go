package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alexandrmotologa/kvring/internal/config"
	"github.com/alexandrmotologa/kvring/internal/ring"
	"github.com/alexandrmotologa/kvring/internal/telemetry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server coordinates the proxy listener and the embedded web cockpit.
type Server struct {
	cfg       *config.Config
	ring      *ring.HashRing
	router    *ring.Router
	telemetry *telemetry.MetricsCollector
	proxy     http.Handler

	proxyServer   *http.Server
	cockpitServer *http.Server
}

// NewServer initializes the HTTP gateway and cockpit servers.
func NewServer(
	cfg *config.Config,
	hr *ring.HashRing,
	r *ring.Router,
	tm *telemetry.MetricsCollector,
	proxyHandler http.Handler,
) *Server {
	return &Server{
		cfg:       cfg,
		ring:      hr,
		router:    r,
		telemetry: tm,
		proxy:     proxyHandler,
	}
}

// Start launches both proxy and cockpit HTTP listeners.
func (s *Server) Start() error {
	// 1. Ingress Proxy Listener (Port :8000)
	promHandler := promhttp.HandlerFor(s.telemetry.Registry(), promhttp.HandlerOpts{})
	proxyMux := http.NewServeMux()
	proxyMux.HandleFunc("/healthz", s.handleHealthz)
	proxyMux.HandleFunc("/livez", s.handleLivez)
	proxyMux.HandleFunc("/readyz", s.handleReadyz)
	proxyMux.Handle("/metrics", promHandler)
	proxyMux.Handle("/", s.proxy)

	s.proxyServer = &http.Server{
		Addr:           s.cfg.Server.ListenAddr,
		Handler:        proxyMux,
		ReadTimeout:    s.cfg.Server.ReadTimeout,
		WriteTimeout:   s.cfg.Server.WriteTimeout,
		IdleTimeout:    s.cfg.Server.IdleTimeout,
		MaxHeaderBytes: s.cfg.Server.MaxHeaderBytes,
	}

	// 2. Web Cockpit & Telemetry API Listener (Port :8484)
	cockpitMux := http.NewServeMux()
	cockpitMux.Handle("/metrics", promHandler)
	cockpitMux.HandleFunc("/api/status", s.handleStatus)
	cockpitMux.HandleFunc("/api/events", s.handleEventsSSE)
	cockpitMux.HandleFunc("/api/nodes", s.handleToggleNode)
	cockpitMux.HandleFunc("/api/simulate", s.handleSimulate)

	// Embedded React SPA with client-side fallback
	distFS := DistFileSystem()
	fileServer := http.FileServer(distFS)
	cockpitMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") || r.URL.Path == "/metrics" {
			http.NotFound(w, r)
			return
		}
		// If requesting file that exists in dist, serve it
		f, err := distFS.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Fallback to index.html for SPA router
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})

	s.cockpitServer = &http.Server{
		Addr:         s.cfg.Server.CockpitAddr,
		Handler:      cockpitMux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	errChan := make(chan error, 2)

	go func() {
		if err := s.proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("proxy server error: %w", err)
		}
	}()

	go func() {
		if err := s.cockpitServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("cockpit server error: %w", err)
		}
	}()

	// Brief pause to catch immediate bind errors (e.g. port already in use)
	select {
	case err := <-errChan:
		return err
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

// Stop gracefully shuts down both HTTP servers.
func (s *Server) Stop(ctx context.Context) error {
	var errs []string
	if s.proxyServer != nil {
		if err := s.proxyServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("proxy shutdown: %v", err))
		}
	}
	if s.cockpitServer != nil {
		if err := s.cockpitServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("cockpit shutdown: %v", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	workers := s.ring.Workers()
	healthyCount := s.ring.HealthyWorkerCount()
	totalLoad := s.ring.ClusterTotalLoad()
	capacity := s.router.ComputeCapacity(totalLoad, healthyCount)

	workerStates := make([]map[string]any, 0, len(workers))
	for _, w := range workers {
		workerStates = append(workerStates, map[string]any{
			"id":              w.ID,
			"url":             w.URL,
			"weight":          w.Weight,
			"max_concurrency": w.MaxConcurrency,
			"active_load":     w.ActiveLoad.Load(),
			"vram_ratio":      w.GetVramRatio(),
			"healthy":         w.Healthy.Load(),
			"total_requests":  w.TotalRequests.Load(),
			"spillovers_in":   w.SpilloversIn.Load(),
			"spillovers_out":  w.SpilloversOut.Load(),
		})
	}

	hits, misses := s.telemetry.CacheStats()

	res := map[string]any{
		"total_load":       totalLoad,
		"healthy_count":    healthyCount,
		"total_workers":    len(workers),
		"capacity_ceiling": capacity,
		"epsilon":          s.cfg.BoundedLoad.Epsilon,
		"vram_threshold":   s.cfg.BoundedLoad.VramThreshold,
		"virtual_nodes":    s.ring.TotalVirtualNodes(),
		"cache_hits":       hits,
		"cache_misses":     misses,
		"workers":          workerStates,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *Server) handleEventsSSE(w http.ResponseWriter, req *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.telemetry.Subscribe()
	defer s.telemetry.Unsubscribe(ch)

	for {
		select {
		case <-req.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(evt)
			if err == nil {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
				flusher.Flush()
			}
		}
	}
}

type toggleNodeReq struct {
	WorkerID string `json:"worker_id"`
	Healthy  bool   `json:"healthy"`
}

func (s *Server) handleToggleNode(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload toggleNodeReq
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		http.Error(w, "bad request json", http.StatusBadRequest)
		return
	}

	s.ring.SetWorkerHealth(payload.WorkerID, payload.Healthy)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "worker_id": payload.WorkerID, "healthy": payload.Healthy})
}

type simulateReq struct {
	Count        int  `json:"count"`
	HeavyContext bool `json:"heavy_context"`
}

func (s *Server) handleSimulate(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload simulateReq
	if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
		payload.Count = 10
	}
	if payload.Count <= 0 {
		payload.Count = 10
	}
	if payload.Count > 100 {
		payload.Count = 100
	}

	go func() {
		for i := 0; i < payload.Count; i++ {
			sessionKey := "sim-session-" + strconv.Itoa(i%4)
			decision, err := s.router.Route(sessionKey)
			if err == nil && decision != nil {
				ttft := 18 * time.Millisecond
				dur := 85 * time.Millisecond
				if payload.HeavyContext {
					currentVram := decision.TargetWorker.GetVramRatio()
					if currentVram < 0.95 {
						decision.TargetWorker.SetVramRatio(currentVram + 0.05)
					}
					ttft = 120 * time.Millisecond
					dur = 450 * time.Millisecond
				}
				s.telemetry.RecordRoute(sessionKey, decision, http.StatusOK, ttft, dur, decision.Reason == ring.ReasonDirectHit)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "injected_requests": payload.Count})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleLivez(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"alive"}`))
}

func (s *Server) handleReadyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	healthy := s.ring.HealthyWorkerCount()
	if healthy <= 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"unavailable","error":"no healthy inference nodes in ring"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, `{"status":"ready","healthy_nodes":%d}`, healthy)
}
