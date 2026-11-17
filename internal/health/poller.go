package health

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/alexandrmotologa/kvring/internal/config"
	"github.com/alexandrmotologa/kvring/internal/ring"
)

// Poller continuously scrapes backend inference workers for real-time VRAM telemetry.
type Poller struct {
	client         *http.Client
	ring           *ring.HashRing
	circuitBreaker *CircuitBreaker
	interval       time.Duration
	vramMetric     string
	workers        []config.WorkerConfig
	cancel         context.CancelFunc
	wg             sync.WaitGroup
}

// NewPoller initializes the background telemetry scraper.
func NewPoller(
	r *ring.HashRing,
	cb *CircuitBreaker,
	workers []config.WorkerConfig,
	cfg config.HealthCheckConfig,
) *Poller {
	interval := cfg.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	metricName := cfg.VramMetricName
	if metricName == "" {
		metricName = "vllm:gpu_cache_usage_perc"
	}

	return &Poller{
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
		ring:           r,
		circuitBreaker: cb,
		interval:       interval,
		vramMetric:     metricName,
		workers:        workers,
	}
}

// Start begins periodic polling in background goroutines.
func (p *Poller) Start(parentCtx context.Context) {
	ctx, cancel := context.WithCancel(parentCtx)
	p.cancel = cancel

	ticker := time.NewTicker(p.interval)
	p.wg.Add(1)

	go func() {
		defer p.wg.Done()
		defer ticker.Stop()

		// Run immediate first poll
		p.pollAll(ctx)

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				p.pollAll(ctx)
				p.circuitBreaker.CheckProbes()
			}
		}
	}()
}

// Stop shuts down the background telemetry poller.
func (p *Poller) Stop() {
	if p.cancel != nil {
		p.cancel()
		p.wg.Wait()
	}
}

func (p *Poller) pollAll(ctx context.Context) {
	for _, w := range p.workers {
		workerNode, exists := p.ring.GetWorker(w.ID)
		if !exists {
			continue
		}

		go func(wc config.WorkerConfig, wn *ring.WorkerNode) {
			p.pollWorker(ctx, wc, wn)
		}(w, workerNode)
	}
}

func (p *Poller) pollWorker(ctx context.Context, wc config.WorkerConfig, wn *ring.WorkerNode) {
	metricsURL := wc.MetricsURL
	if metricsURL == "" {
		metricsURL = wc.URL + "/metrics"
	}

	req, err := http.NewRequestWithContext(ctx, "GET", metricsURL, nil)
	if err != nil {
		p.circuitBreaker.RecordFailure(wn.ID)
		return
	}

	resp, err := p.client.Do(req)
	if err != nil {
		p.circuitBreaker.RecordFailure(wn.ID)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		p.circuitBreaker.RecordFailure(wn.ID)
		return
	}

	// Limit body read to 512KB for metrics
	lr := io.LimitReader(resp.Body, 512*1024)
	snapshot := ParseVllmMetrics(lr, p.vramMetric)
	if snapshot.Valid {
		wn.SetVramRatio(snapshot.VramRatio)
		p.circuitBreaker.RecordSuccess(wn.ID)
	} else {
		// Even if custom metric was not parsed, 200 OK confirms worker process is alive
		p.circuitBreaker.RecordSuccess(wn.ID)
	}
}

// ScrapeOnce performs an immediate synchronous poll of a given worker for diagnostics.
func (p *Poller) ScrapeOnce(ctx context.Context, workerID string) (MetricSnapshot, error) {
	for _, w := range p.workers {
		if w.ID == workerID {
			metricsURL := w.MetricsURL
			if metricsURL == "" {
				metricsURL = w.URL + "/metrics"
			}
			req, err := http.NewRequestWithContext(ctx, "GET", metricsURL, nil)
			if err != nil {
				return MetricSnapshot{}, err
			}
			resp, err := p.client.Do(req)
			if err != nil {
				return MetricSnapshot{}, err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return MetricSnapshot{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
			}
			snapshot := ParseVllmMetrics(resp.Body, p.vramMetric)
			return snapshot, nil
		}
	}
	return MetricSnapshot{}, fmt.Errorf("worker %s not found", workerID)
}
