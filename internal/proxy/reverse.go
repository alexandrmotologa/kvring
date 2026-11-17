package proxy

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alexandrmotologa/kvring/internal/health"
	"github.com/alexandrmotologa/kvring/internal/ring"
	"github.com/alexandrmotologa/kvring/internal/telemetry"
)

// Proxy handles Layer-7 reverse proxying with KV-cache affinity and bounded-load spillover.
type Proxy struct {
	router         *ring.Router
	ring           *ring.HashRing
	ringManager    *ring.RingManager
	extractor      *ring.AffinityExtractor
	circuitBreaker *health.CircuitBreaker
	telemetry      *telemetry.MetricsCollector
	transport      *http.Transport
}

// NewProxy constructs an optimized reverse proxy engine.
func NewProxy(
	r *ring.Router,
	hr *ring.HashRing,
	ext *ring.AffinityExtractor,
	cb *health.CircuitBreaker,
	tm *telemetry.MetricsCollector,
) *Proxy {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:          512,
		MaxIdleConnsPerHost:   128,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		DisableCompression:    true, // Preserve raw SSE chunks
		ForceAttemptHTTP2:     true,
	}

	return &Proxy{
		router:         r,
		ring:           hr,
		extractor:      ext,
		circuitBreaker: cb,
		telemetry:      tm,
		transport:      transport,
	}
}

// SetRingManager binds a multi-model ring manager to the proxy.
func (p *Proxy) SetRingManager(rm *ring.RingManager) {
	p.ringManager = rm
}

// ServeHTTP routes and forwards an inference request to an optimal backend worker.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	reqStart := time.Now()

	// 1. Extract affinity key, model, prompt prefix, and estimated token context
	affinityKey, model, prompt, estimatedTokens := p.extractor.ExtractMetadata(req)

	activeRouter := p.router
	activeRing := p.ring
	if p.ringManager != nil {
		activeRouter, activeRing = p.ringManager.GetRouter(model)
	}

	// 2. Compute bounded-load routing decision with Radix prefix & pre-flight checks
	var decision *ring.RouteDecision
	var err error

	if len(prompt) >= 32 {
		decision, err = activeRouter.RouteWithPrompt(affinityKey, prompt)
	} else if estimatedTokens > 0 {
		decision, err = activeRouter.RouteWithAdmission(affinityKey, estimatedTokens)
	} else {
		decision, err = activeRouter.Route(affinityKey)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"routing failure: %v"}`, err), http.StatusServiceUnavailable)
		return
	}

	targetWorker := decision.TargetWorker
	activeRing.IncrLoad(targetWorker)
	defer activeRing.DecrLoad(targetWorker)

	// 3. Resolve target URL
	targetURL, err := url.Parse(targetWorker.URL)
	if err != nil {
		http.Error(w, `{"error":"invalid backend target URL"}`, http.StatusBadGateway)
		return
	}

	outReq := req.Clone(req.Context())
	outReq.URL.Scheme = targetURL.Scheme
	outReq.URL.Host = targetURL.Host
	outReq.Host = targetURL.Host
	outReq.RequestURI = "" // RequestURI must be empty for client requests

	// Pass original client IP
	if clientIP, _, err := net.SplitHostPort(req.RemoteAddr); err == nil {
		prior := req.Header.Get("X-Forwarded-For")
		if prior != "" {
			clientIP = prior + ", " + clientIP
		}
		outReq.Header.Set("X-Forwarded-For", clientIP)
	}

	// 4. Forward to backend
	resp, err := p.transport.RoundTrip(outReq)
	if err != nil {
		p.circuitBreaker.RecordFailure(targetWorker.ID)
		p.telemetry.RecordRoute(affinityKey, decision, http.StatusBadGateway, 0, time.Since(reqStart), false)
		http.Error(w, fmt.Sprintf(`{"error":"upstream connection error: %v"}`, err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 5. Track circuit breaker status
	if resp.StatusCode >= 500 {
		p.circuitBreaker.RecordFailure(targetWorker.ID)
	} else {
		p.circuitBreaker.RecordSuccess(targetWorker.ID)
	}

	// 6. Set response telemetry headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}

	spillover := decision.Reason != ring.ReasonDirectHit
	w.Header().Set("X-KVRing-Worker", targetWorker.ID)
	w.Header().Set("X-KVRing-Primary", decision.PrimaryWorker.ID)
	w.Header().Set("X-KVRing-Spillover", strconv.FormatBool(spillover))
	w.Header().Set("X-KVRing-Reason", string(decision.Reason))
	w.Header().Set("X-KVRing-Routing-Ns", strconv.FormatInt(decision.RoutingLatency.Nanoseconds(), 10))

	w.WriteHeader(resp.StatusCode)

	// 7. Stream response with zero buffering
	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	var ttft time.Duration

	if isSSE {
		var streamErr error
		ttft, _, streamErr = StreamSSE(w, resp.Body)
		if streamErr != nil && !strings.Contains(streamErr.Error(), "context canceled") {
			// Client disconnected or pipe broke
		}
	} else {
		_, _ = io.Copy(w, resp.Body)
		ttft = time.Since(reqStart)
	}

	totalDuration := time.Since(reqStart)
	// Heuristic: if TTFT is under 35ms, it hit prefill KV cache
	isCacheHit := !spillover && ttft > 0 && ttft < 35*time.Millisecond

	p.telemetry.RecordRoute(affinityKey, decision, resp.StatusCode, ttft, totalDuration, isCacheHit)
}
