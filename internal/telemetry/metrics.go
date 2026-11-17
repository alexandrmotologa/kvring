package telemetry

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alexandrmotologa/kvring/internal/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// RouteEvent records metadata for an individual routed request for live dashboards.
type RouteEvent struct {
	Timestamp      time.Time `json:"timestamp"`
	AffinityKey    string    `json:"affinity_key"`
	PrimaryWorker  string    `json:"primary_worker"`
	TargetWorker   string    `json:"target_worker"`
	Reason         string    `json:"reason"`
	Spillover      bool      `json:"spillover"`
	SpilloverSteps int       `json:"spillover_steps"`
	TTFT           float64   `json:"ttft_ms"`
	TotalDuration  float64   `json:"duration_ms"`
	StatusCode     int       `json:"status_code"`
}

// MetricsCollector maintains Prometheus metrics and a live event stream.
type MetricsCollector struct {
	mu           sync.RWMutex
	registry     *prometheus.Registry
	recentEvents []RouteEvent
	maxEvents    int

	// Prometheus Metrics
	RequestsTotal    *prometheus.CounterVec
	RequestDuration  *prometheus.HistogramVec
	TTFTHistogram    prometheus.Histogram
	RoutingDuration  prometheus.Histogram
	WorkerVramGauge  *prometheus.GaugeVec
	WorkerLoadGauge  *prometheus.GaugeVec
	CacheHitsTotal   prometheus.Counter
	CacheMissesTotal prometheus.Counter
	SpilloverTotal   *prometheus.CounterVec

	// Atomic totals for live JSON APIs
	liveCacheHits   atomic.Uint64
	liveCacheMisses atomic.Uint64

	// Subscribers for real-time SSE streaming to web cockpit / TUI
	subscribersMu sync.RWMutex
	subscribers   map[chan RouteEvent]struct{}
}

// NewMetricsCollector registers and initializes Prometheus instrumentation with an isolated registry.
func NewMetricsCollector(maxRecentEvents int) *MetricsCollector {
	if maxRecentEvents <= 0 {
		maxRecentEvents = 200
	}

	reg := prometheus.NewRegistry()
	factory := promauto.With(reg)

	mc := &MetricsCollector{
		registry:     reg,
		recentEvents: make([]RouteEvent, 0, maxRecentEvents),
		maxEvents:    maxRecentEvents,
		subscribers:  make(map[chan RouteEvent]struct{}),

		RequestsTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kvring_requests_total",
				Help: "Total count of inference requests processed by kvring",
			},
			[]string{"worker", "status", "reason", "spillover"},
		),

		RequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "kvring_request_duration_seconds",
				Help:    "End-to-end request duration through the kvring gateway",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0},
			},
			[]string{"worker"},
		),

		TTFTHistogram: factory.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "kvring_ttft_seconds",
				Help:    "Time to first token (TTFT) for streaming inference requests",
				Buckets: []float64{0.005, 0.015, 0.030, 0.060, 0.120, 0.250, 0.500, 1.0, 2.0},
			},
		),

		RoutingDuration: factory.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "kvring_routing_duration_nanoseconds",
				Help:    "Nanoseconds taken by the Mirrokni bounded load algorithm to determine target worker",
				Buckets: []float64{50, 100, 250, 500, 1000, 5000, 10000},
			},
		),

		WorkerVramGauge: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kvring_worker_vram_ratio",
				Help: "Instantaneous GPU VRAM / KV-cache utilization ratio (0.0 - 1.0)",
			},
			[]string{"worker"},
		),

		WorkerLoadGauge: factory.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "kvring_worker_active_load",
				Help: "Current active in-flight requests on this worker",
			},
			[]string{"worker"},
		),

		CacheHitsTotal: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "kvring_cache_hits_total",
				Help: "Requests that hit existing KV cache on affinity worker",
			},
		),

		CacheMissesTotal: factory.NewCounter(
			prometheus.CounterOpts{
				Name: "kvring_cache_misses_total",
				Help: "Requests that missed KV cache or had to recompute prefix",
			},
		),

		SpilloverTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "kvring_spillover_total",
				Help: "Total count of requests diverted to a clockwise neighbor worker",
			},
			[]string{"primary_worker", "target_worker", "reason"},
		),
	}

	return mc
}

// Registry returns the isolated Prometheus registry.
func (mc *MetricsCollector) Registry() *prometheus.Registry {
	return mc.registry
}

// CacheStats returns instantaneous counts of cache hits and misses.
func (mc *MetricsCollector) CacheStats() (hits, misses uint64) {
	return mc.liveCacheHits.Load(), mc.liveCacheMisses.Load()
}

// RecordRoute tracks the completion of a request across all metrics and active subscribers.
func (mc *MetricsCollector) RecordRoute(
	affinityKey string,
	decision *ring.RouteDecision,
	statusCode int,
	ttft time.Duration,
	totalDuration time.Duration,
	isCacheHit bool,
) {
	primaryID := "unknown"
	targetID := "unknown"
	if decision.PrimaryWorker != nil {
		primaryID = decision.PrimaryWorker.ID
	}
	if decision.TargetWorker != nil {
		targetID = decision.TargetWorker.ID
	}

	spillover := decision.Reason != ring.ReasonDirectHit
	spilloverStr := "false"
	if spillover {
		spilloverStr = "true"
		mc.SpilloverTotal.WithLabelValues(primaryID, targetID, string(decision.Reason)).Inc()
	}

	statusStr := "200"
	if statusCode != 0 {
		statusStr = strconv.Itoa(statusCode)
	}

	mc.RequestsTotal.WithLabelValues(targetID, statusStr, string(decision.Reason), spilloverStr).Inc()
	mc.RequestDuration.WithLabelValues(targetID).Observe(totalDuration.Seconds())

	if ttft > 0 {
		mc.TTFTHistogram.Observe(ttft.Seconds())
	}
	if decision.RoutingLatency > 0 {
		mc.RoutingDuration.Observe(float64(decision.RoutingLatency.Nanoseconds()))
	}

	if isCacheHit {
		mc.CacheHitsTotal.Inc()
		mc.liveCacheHits.Add(1)
	} else {
		mc.CacheMissesTotal.Inc()
		mc.liveCacheMisses.Add(1)
	}

	// Update gauges
	if decision.TargetWorker != nil {
		mc.WorkerVramGauge.WithLabelValues(targetID).Set(decision.TargetWorker.GetVramRatio())
		mc.WorkerLoadGauge.WithLabelValues(targetID).Set(float64(decision.TargetWorker.ActiveLoad.Load()))
	}

	// Add to rolling event buffer
	evt := RouteEvent{
		Timestamp:      time.Now(),
		AffinityKey:    affinityKey,
		PrimaryWorker:  primaryID,
		TargetWorker:   targetID,
		Reason:         string(decision.Reason),
		Spillover:      spillover,
		SpilloverSteps: decision.SpilloverSteps,
		TTFT:           float64(ttft.Microseconds()) / 1000.0,
		TotalDuration:  float64(totalDuration.Microseconds()) / 1000.0,
		StatusCode:     statusCode,
	}

	mc.mu.Lock()
	if len(mc.recentEvents) >= mc.maxEvents {
		mc.recentEvents = mc.recentEvents[1:]
	}
	mc.recentEvents = append(mc.recentEvents, evt)
	mc.mu.Unlock()

	// Broadcast to active subscribers (non-blocking)
	mc.subscribersMu.RLock()
	for ch := range mc.subscribers {
		select {
		case ch <- evt:
		default:
		}
	}
	mc.subscribersMu.RUnlock()
}

// RecentEvents returns a snapshot of recent routing events.
func (mc *MetricsCollector) RecentEvents() []RouteEvent {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	cp := make([]RouteEvent, len(mc.recentEvents))
	copy(cp, mc.recentEvents)
	return cp
}

// Subscribe returns a channel that receives live RouteEvents.
func (mc *MetricsCollector) Subscribe() chan RouteEvent {
	ch := make(chan RouteEvent, 64)
	mc.subscribersMu.Lock()
	mc.subscribers[ch] = struct{}{}
	mc.subscribersMu.Unlock()
	return ch
}

// Unsubscribe removes an event subscription channel.
func (mc *MetricsCollector) Unsubscribe(ch chan RouteEvent) {
	mc.subscribersMu.Lock()
	delete(mc.subscribers, ch)
	mc.subscribersMu.Unlock()
	close(ch)
}
