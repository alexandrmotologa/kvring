package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfigValidates(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected default config to be valid, got: %v", err)
	}
	if len(cfg.Workers) != 3 {
		t.Fatalf("expected 3 default workers, got %d", len(cfg.Workers))
	}
}

func TestLoadConfigFile(t *testing.T) {
	yamlContent := `
server:
  listen_addr: ":9000"
  cockpit_addr: ":9090"
bounded_load:
  epsilon: 0.3
  virtual_replicas: 150
  vram_threshold: 0.80
affinity:
  header_key: "Custom-Session"
workers:
  - id: "gpu-node-a"
    url: "http://10.0.0.1:8000"
    weight: 2
  - id: "gpu-node-b"
    url: "http://10.0.0.2:8000"
    weight: 1
`
	tmpDir := t.TempDir()
	confPath := filepath.Join(tmpDir, "kvring.yaml")
	if err := os.WriteFile(confPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(confPath)
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}

	if cfg.Server.ListenAddr != ":9000" {
		t.Errorf("expected listen_addr :9000, got %s", cfg.Server.ListenAddr)
	}
	if cfg.BoundedLoad.VirtualReplicas != 150 {
		t.Errorf("expected 150 replicas, got %d", cfg.BoundedLoad.VirtualReplicas)
	}
	if len(cfg.Workers) != 2 {
		t.Errorf("expected 2 workers, got %d", len(cfg.Workers))
	}
	if cfg.Workers[0].ID != "gpu-node-a" {
		t.Errorf("expected first worker id gpu-node-a, got %s", cfg.Workers[0].ID)
	}
}

func TestConfigValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr bool
	}{
		{
			name: "no workers",
			mutate: func(c *Config) {
				c.Workers = nil
			},
			wantErr: true,
		},
		{
			name: "invalid epsilon",
			mutate: func(c *Config) {
				c.BoundedLoad.Epsilon = 0
			},
			wantErr: true,
		},
		{
			name: "invalid vram threshold",
			mutate: func(c *Config) {
				c.BoundedLoad.VramThreshold = 1.5
			},
			wantErr: true,
		},
		{
			name: "duplicate worker id",
			mutate: func(c *Config) {
				c.Workers = []WorkerConfig{
					{ID: "node-1", URL: "http://localhost:8001"},
					{ID: "node-1", URL: "http://localhost:8002"},
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}
		})
	}
}
