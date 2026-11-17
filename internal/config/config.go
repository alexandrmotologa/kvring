package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// WorkerConfig defines a backend inference worker (vLLM, SGLang, or TensorRT-LLM).
type WorkerConfig struct {
	ID             string        `yaml:"id"`
	URL            string        `yaml:"url"`
	Weight         int           `yaml:"weight"`
	MaxConcurrency int           `yaml:"max_concurrency"`
	MetricsURL     string        `yaml:"metrics_url"`
	HealthTimeout  time.Duration `yaml:"health_timeout"`
	Models         []string      `yaml:"models"`
}

// BoundedLoadConfig controls the Mirrokni bounded-load consistent hashing parameters.
type BoundedLoadConfig struct {
	Epsilon            float64 `yaml:"epsilon"`              // Relative load tolerance factor (e.g. 0.25 = 125% of average load)
	VirtualReplicas    int     `yaml:"virtual_replicas"`     // Number of points per worker on hash ring (default 100)
	VramThreshold      float64 `yaml:"vram_threshold"`       // Upper limit for VRAM utilization before spillover (default 0.85)
	MinTokensForPrefix int     `yaml:"min_tokens_for_prefix"`// Minimum prefix length to hash for KV reuse
}

// AffinityConfig controls session and prefix key extraction rules.
type AffinityConfig struct {
	HeaderKey      string   `yaml:"header_key"`      // Custom header such as X-Session-ID
	CookieKey      string   `yaml:"cookie_key"`      // Cookie name if session passed via cookies
	EnablePrefix   bool     `yaml:"enable_prefix"`   // Compute hash from prompt prefix if no explicit session
	PrefixLength   int      `yaml:"prefix_length"`   // Number of characters or tokens for prefix hashing
	FallbackHeaders []string `yaml:"fallback_headers"`// Secondary headers (e.g. Authorization, X-User-ID)
}

// HealthCheckConfig defines polling and circuit breaker behavior.
type HealthCheckConfig struct {
	PollInterval      time.Duration `yaml:"poll_interval"`
	FailureThreshold  int           `yaml:"failure_threshold"`
	RecoveryDuration  time.Duration `yaml:"recovery_duration"`
	VramMetricName    string        `yaml:"vram_metric_name"`
}

// TelemetryConfig defines metrics and tracing configuration.
type TelemetryConfig struct {
	PrometheusPath string `yaml:"prometheus_path"`
	EnableTracing  bool   `yaml:"enable_tracing"`
}

// ServerConfig defines listeners and timeout policies for kvring.
type ServerConfig struct {
	ListenAddr      string        `yaml:"listen_addr"`       // Proxy ingress port, e.g. ":8000"
	CockpitAddr     string        `yaml:"cockpit_addr"`      // Web dashboard port, e.g. ":8484"
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	IdleTimeout     time.Duration `yaml:"idle_timeout"`
	MaxHeaderBytes  int           `yaml:"max_header_bytes"`
}

// Config represents the complete kvring configuration.
type Config struct {
	Server      ServerConfig      `yaml:"server"`
	BoundedLoad BoundedLoadConfig `yaml:"bounded_load"`
	Affinity    AffinityConfig    `yaml:"affinity"`
	Health      HealthCheckConfig `yaml:"health"`
	Telemetry   TelemetryConfig   `yaml:"telemetry"`
	Workers     []WorkerConfig    `yaml:"workers"`
}

// DefaultConfig returns safe production defaults for kvring.
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			ListenAddr:     ":8000",
			CockpitAddr:    ":8484",
			ReadTimeout:    120 * time.Second,
			WriteTimeout:   120 * time.Second,
			IdleTimeout:    60 * time.Second,
			MaxHeaderBytes: 1 << 20, // 1 MB
		},
		BoundedLoad: BoundedLoadConfig{
			Epsilon:            0.25,
			VirtualReplicas:    100,
			VramThreshold:      0.85,
			MinTokensForPrefix: 16,
		},
		Affinity: AffinityConfig{
			HeaderKey:       "X-Session-ID",
			CookieKey:       "session_id",
			EnablePrefix:    true,
			PrefixLength:    128,
			FallbackHeaders: []string{"X-User-ID", "Authorization"},
		},
		Health: HealthCheckConfig{
			PollInterval:     2 * time.Second,
			FailureThreshold: 3,
			RecoveryDuration: 15 * time.Second,
			VramMetricName:   "vllm:gpu_cache_usage_perc",
		},
		Telemetry: TelemetryConfig{
			PrometheusPath: "/metrics",
			EnableTracing:  false,
		},
		Workers: []WorkerConfig{
			{
				ID:             "worker-1",
				URL:            "http://127.0.0.1:8001",
				Weight:         1,
				MaxConcurrency: 64,
				MetricsURL:     "http://127.0.0.1:8001/metrics",
				HealthTimeout:  2 * time.Second,
			},
			{
				ID:             "worker-2",
				URL:            "http://127.0.0.1:8002",
				Weight:         1,
				MaxConcurrency: 64,
				MetricsURL:     "http://127.0.0.1:8002/metrics",
				HealthTimeout:  2 * time.Second,
			},
			{
				ID:             "worker-3",
				URL:            "http://127.0.0.1:8003",
				Weight:         1,
				MaxConcurrency: 64,
				MetricsURL:     "http://127.0.0.1:8003/metrics",
				HealthTimeout:  2 * time.Second,
			},
		},
	}
}

// Load reads and parses a YAML configuration file.
func Load(path string) (*Config, error) {
	cfg := DefaultConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config yaml: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return cfg, nil
}

// Validate verifies logical invariants across configuration fields.
func (c *Config) Validate() error {
	if len(c.Workers) == 0 {
		return fmt.Errorf("at least one worker must be specified")
	}

	if c.BoundedLoad.Epsilon <= 0 {
		return fmt.Errorf("bounded load epsilon must be positive")
	}

	if c.BoundedLoad.VirtualReplicas <= 0 {
		return fmt.Errorf("virtual replicas must be greater than zero")
	}

	if c.BoundedLoad.VramThreshold <= 0 || c.BoundedLoad.VramThreshold > 1.0 {
		return fmt.Errorf("vram threshold must be between 0.0 and 1.0")
	}

	seenIDs := make(map[string]bool)
	for i, w := range c.Workers {
		if w.ID == "" {
			return fmt.Errorf("worker [%d] has an empty ID", i)
		}
		if seenIDs[w.ID] {
			return fmt.Errorf("duplicate worker ID: %s", w.ID)
		}
		seenIDs[w.ID] = true

		if w.URL == "" {
			return fmt.Errorf("worker %s has an empty URL", w.ID)
		}
		if w.Weight <= 0 {
			c.Workers[i].Weight = 1
		}
	}

	return nil
}
