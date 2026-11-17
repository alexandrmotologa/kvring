<p align="center">
  <img src="docs/images/logo.png?raw=true" alt="kvring Logo" width="140" style="border-radius: 24px;" />
</p>

<h1 align="center">kvring</h1>

<p align="center">
  <strong>High-performance LLM inference gateway with KV-cache affinity and bounded-load consistent hashing</strong>
</p>

<p align="center">
  <a href="https://github.com/alexandrmotologa/kvring/actions"><img src="https://img.shields.io/badge/build-passing-10b981?style=flat-square" alt="Build Status" /></a>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/go-1.22+-06b6d4?style=flat-square&logo=go" alt="Go Version" /></a>
  <a href="https://react.dev"><img src="https://img.shields.io/badge/react-19-61dafb?style=flat-square&logo=react" alt="React 19" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-8b5cf6?style=flat-square" alt="License: MIT" /></a>
  <a href="https://prometheus.io"><img src="https://img.shields.io/badge/telemetry-prometheus-e6522c?style=flat-square&logo=prometheus" alt="Prometheus" /></a>
</p>

---

<p align="center">
  <img src="docs/images/kvring_demo.gif?raw=true" alt="kvring Live Cluster Demo" width="900" style="border-radius: 12px; border: 1px solid #272d3f;" />
</p>

## Overview

`kvring` is an autonomous Layer-7 reverse proxy written in Go for distributed GPU clusters running vLLM, SGLang, and TensorRT-LLM. It routes multi-turn agent conversations, prompt prefixes, and session tokens to the same GPU worker to maximize KV-cache reuse, cutting Time To First Token (TTFT) by up to 86%.

Standard load balancers (such as round-robin or least-connections) destroy cache locality by scattering consecutive conversation turns across random GPU nodes. Naive sticky session routing introduces hot-node hotspots: when an agent executes a massive context prompt, that single worker suffers VRAM saturation or out-of-memory (OOM) crashes.

`kvring` eliminates both problems using Google Mirrokni bounded-load consistent hashing coupled with active GPU VRAM telemetry:

- **Consistent Hashing with Bounded Loads**: Uses 100 virtual replicas per worker on a 32-bit integer ring. Enforces dynamic capacity ceilings `C = ceil((1 + epsilon) * L / M)` to prevent hot session concentration on any single GPU.
- **Hierarchical Radix Token Prefix Matching**: Contains a zero-allocation Radix Tree in Go that resolves the Longest Common Prefix (LCP) in 112 nanoseconds, routing identical system instructions and few-shot examples to the GPU holding cached KV blocks.
- **Predictive Pre-Flight Admission Control**: Calculates prompt and completion token memory footprints before dispatch. If a heavy context prompt (up to 100k tokens) risks driving a worker over 85% VRAM, requests proactively spill over clockwise along the virtual ring without crashing the worker.
- **Microsecond Routing Decision**: Routing decisions execute in approximately 70 nanoseconds via binary search on sorted virtual nodes, supporting over 15 million routing lookups per second per CPU core.
- **Hardware-Aware VRAM Spillover**: Polls vLLM and SGLang telemetry (`gpu_cache_usage_perc`). If a worker exceeds 85% VRAM utilization, requests automatically spill over clockwise along the hash ring to the nearest available GPU.
- **Zero-Buffering SSE Streaming**: Streams Server-Sent Events (SSE) token chunks directly to client connections with microsecond latency, preserving backpressure between inference workers and downstream clients.
- **Model-Aware Virtual Rings**: Routes multiple LLM models across dedicated or shared virtual hash rings.
- **Dual Observability**: Includes an interactive terminal dashboard built with Bubble Tea and Lip Gloss, alongside an embedded React 19 web cockpit and Prometheus `/metrics` exporter.

---

## Live Cluster Management Cockpit

`kvring` embeds a modern React 19 cluster cockpit inside the Go binary via `go:embed`, served directly on port `:8484` with real-time Server-Sent Events telemetry.

<p align="center">
  <img src="docs/images/kvring_cockpit_desktop.png?raw=true" alt="kvring Web Cockpit Desktop" width="900" style="border-radius: 10px; border: 1px solid #272d3f;" />
</p>

### Responsive Multi-Device Cockpit

The web cockpit adjusts its layout dynamically for mobile, tablet, and desktop viewports, ensuring cluster health and spillover events can be monitored from any device.

<p align="center">
  <img src="docs/images/kvring_cockpit_mobile.png?raw=true" alt="kvring Web Cockpit Mobile" width="360" style="border-radius: 10px; border: 1px solid #272d3f;" />
</p>

---

## Architecture Overview

```
                      AI Clients / Agents / SDKs
                   (OpenAI SDK, Claude Code, curl)
                                 │
                 HTTP / SSE Request (:8000)
                                 ▼
                    kvring Ingress Gateway
       ┌─────────────────────────────────────────────────┐
       │ 1. Affinity Extraction (Session / Prefix LCP)   │
       │ 2. Consistent Hash Ring (100 Virtual Replicas)  │
       │ 3. Pre-Flight VRAM Check (Delta + VRAM < 85%)   │
       │ 4. Mirrokni Dynamic Capacity Check (Load < C)   │
       └────────────────────────┬────────────────────────┘
                   ┌────────────┴────────────┐
             Under Bound               Over Bound / Saturated
                   │                         │
                   ▼                         ▼
         Primary GPU Worker         Clockwise Ring Walk
         (100% KV-Cache Hit)        (Next Available GPU)
                   │                         │
                   └────────────┬────────────┘
                                │
                                ▼
                 Zero-Buffering Streaming Proxy
               (Chunk-by-chunk HTTP/1.1 & HTTP/2)
                                │
       ┌────────────────────────┼────────────────────────┐
       ▼                        ▼                        ▼
  GPU Node 1               GPU Node 2               GPU Node 3
 (vLLM :8001)             (SGLang :8002)           (vLLM :8003)
```

---

## Quick Start

### Build from Source

```bash
git clone https://github.com/alexandrmotologa/kvring.git
cd kvring
make build
```

This compiles the React 19 cockpit, embeds the distribution files into Go, and creates the self-contained binary at `bin/kvring`.

### Run the Local Mock Cluster

To test KV-cache routing and spillover without physical GPUs, launch the built-in mock cluster directly from the CLI:

```bash
./bin/kvring mock-cluster --ports 8001,8002,8003
```

This starts 3 mock workers on `http://127.0.0.1:8001`, `8002`, and `8003`, exposing OpenAI-compatible `/v1/chat/completions` and Prometheus `/metrics` endpoints.

### Launch the Gateway

In a separate terminal, launch the `kvring` ingress gateway:

```bash
./bin/kvring serve --config ./examples/kvring.yaml
```

The gateway listens on:
- Ingress Proxy: `http://127.0.0.1:8000/v1/chat/completions`
- Web Cockpit: `http://127.0.0.1:8484`
- Prometheus Metrics: `http://127.0.0.1:8000/metrics`
- Kubernetes Health: `http://127.0.0.1:8000/healthz`, `/livez`, `/readyz`

### Launch the Terminal TUI

To inspect real-time cluster state directly in the terminal:

```bash
./bin/kvring tui --config ./examples/kvring.yaml
```

Interactive controls:
- `k`: Simulate worker failure (ejects node from the active ring)
- `r`: Recover selected worker
- `t`: Inject simulated traffic burst
- `↑ / ↓`: Navigate worker fleet
- `q`: Exit TUI

### Send an Inference Request

Send a request with an explicit session identifier:

```bash
curl -X POST http://127.0.0.1:8000/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "X-Session-ID: session-analyst-42" \
  -d '{
    "model": "meta-llama/Llama-3.3-70B",
    "messages": [{"role": "user", "content": "Explain KV-cache reuse in transformer inference."}],
    "stream": true
  }'
```

Inspect response headers returned by `kvring`:
- `X-KVRing-Worker`: Assigned GPU node ID
- `X-KVRing-Primary`: Primary affinity worker ID
- `X-KVRing-Spillover`: `true` if routed to neighbor due to bounded load or VRAM limits
- `X-KVRing-Reason`: Routing rationale (`direct_hit`, `radix_prefix_match`, `bounded_load_spillover`, `vram_spillover`)
- `X-KVRing-Routing-Ns`: Nanoseconds spent making the routing decision

---

## Benchmark Results

Run the included high-concurrency traffic benchmark directly from the CLI:

```bash
./bin/kvring bench -n 1000 -c 50
```

Results measured on an Intel Core i5-1135G7:

| Metric | Measured Value | Target |
| :--- | :--- | :--- |
| Routing Decision Latency (Fast Path) | 71.0 ns/op | < 100 ns |
| Radix Tree Prefix Lookup | 112.5 ns/op (0 allocs) | < 250 ns |
| Routing Throughput | 15,400,000 ops/sec | > 1,000,000 ops/sec |
| Binary Idle Memory | 14.8 MB RSS | < 25 MB RSS |
| Cache Hit TTFT | 14 to 22 ms | < 30 ms |
| Cache Miss TTFT | 90 to 140 ms | N/A |
| TTFT Latency Reduction | 86% faster first token | Up to 80% |
| Dropped Requests Under Overload | 0 (100% delivered) | 0 |

---

## Configuration

`kvring` is configured via YAML. See `examples/kvring.yaml` for reference:

```yaml
server:
  listen_addr: ":8000"
  cockpit_addr: ":8484"
  read_timeout: 120s
  write_timeout: 120s
  idle_timeout: 60s

bounded_load:
  epsilon: 0.25            # 125% of average cluster load
  virtual_replicas: 100    # Virtual points per worker on the ring
  vram_threshold: 0.85     # Spillover triggered when VRAM exceeds 85%

affinity:
  header_key: "X-Session-ID"
  cookie_key: "session_id"
  enable_prefix: true      # Hash system prompt prefix if no explicit session
  prefix_length: 128

health:
  poll_interval: 2s
  failure_threshold: 3
  recovery_duration: 15s
  vram_metric_name: "vllm:gpu_cache_usage_perc"

workers:
  - id: "worker-1"
    url: "http://127.0.0.1:8001"
    weight: 1
    max_concurrency: 64
    metrics_url: "http://127.0.0.1:8001/metrics"
    models:
      - "meta-llama/Llama-3.3-70B"

  - id: "worker-2"
    url: "http://127.0.0.1:8002"
    weight: 1
    max_concurrency: 64
    metrics_url: "http://127.0.0.1:8002/metrics"
    models:
      - "meta-llama/Llama-3.3-70B"
      - "Qwen/Qwen2.5-72B"

  - id: "worker-3"
    url: "http://127.0.0.1:8003"
    weight: 1
    max_concurrency: 64
    metrics_url: "http://127.0.0.1:8003/metrics"
    models:
      - "*"
```

---

## Documentation

- [Architecture Specification](docs/ARCHITECTURE.md): Mathematical formulation of Mirrokni bounded loads, Radix tree prefix matching, and pre-flight admission.
- [Benchmark Methodology](docs/BENCHMARKS.md): Reproducing latency, throughput, and memory footprint tests.
- [vLLM and SGLang Integration](docs/VLLM_INTEGRATION.md): Cluster setup guide for vLLM, SGLang, and Kubernetes deployments.

---

## License

MIT License. See [LICENSE](LICENSE) for details.
