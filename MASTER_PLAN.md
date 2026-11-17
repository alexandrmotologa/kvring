# kvring: Architecture and Engineering Master Plan
> High-Performance LLM Inference Gateway with KV-Cache Affinity & Bounded-Load Consistent Hashing
> Target: Zero-CGO Single-Binary Go Gateway (< 25MB RAM, < 100ns routing) with Dual Bubble Tea TUI & Embedded React 19 Cluster Cockpit

---

## 1. Problem Statement and Motivation

### 1.1 The Unsustainable Economics of Distributed LLM Inference
Deploying self-hosted open-weight Large Language Models (such as Llama 3.3 70B, Qwen 2.5 72B, DeepSeek-V3, and Mistral Nemo) across multi-node GPU clusters (powered by engines such as vLLM, SGLang, TensorRT-LLM, or TGI) presents a fundamental architectural challenge in request routing:
1. **The Prefill Bottleneck and KV-Cache Memory**: In transformer inference, computing attention keys and values (the KV-cache) during the prompt prefill phase accounts for 70% to 80% of total GPU compute time and latency. Re-computing the KV-cache for repeated system prompts or multi-turn agent conversations wastes millions of FLOPs and inflates Time To First Token (TTFT) by 3x to 8x.
2. **The Failure of Generic Round-Robin / Random Load Balancers**: Standard Layer 7 reverse proxies (NGINX, HAProxy, Traefik, AWS ALB) distribute requests round-robin or least-connections. In multi-turn chat or agent loops, turn 1 lands on GPU Node A, turn 2 lands on GPU Node B, and turn 3 lands on GPU Node C. This completely destroys local prefix and KV-cache reuse, dropping cache hit rates to near 0%.
3. **The Catastrophic Cascading Failure of Naive Sticky Routing**: If a proxy employs naive sticky sessions (hashing on `session_id` or `user_id`), it creates severe **Hot Session Hotspots**. When a user submits a massive 100k context prompt or launches an autonomous multi-step agent loop, that single GPU worker suffers VRAM exhaustion, KV-cache thrashing, or out-of-memory (OOM) crashes. If that worker crashes, traditional consistent hashing dumps all of its heavy sessions onto the immediate clockwise neighbor, triggering a catastrophic cascading domino failure across the entire GPU cluster.

### 1.2 The kvring Solution
`kvring` is an autonomous LLM inference reverse proxy written in Go that solves the dual problem of **KV-Cache Affinity** and **Worker Overload Prevention**:
- **Consistent Hashing with Bounded Loads (Google Mirrokni Algorithm)**: Maps conversation sessions, tenant IDs, or prompt prefix hashes onto a virtual consistent hash ring. Unlike naive hashing, `kvring` enforces a dynamic capacity ceiling ($C$) per worker based on active in-flight tokens and real-time GPU VRAM saturation.
- **Active vLLM & SGLang Telemetry Ingestion**: Continuously polls GPU workers for exact KV-cache utilization (e.g. `gpu_cache_usage_perc` and `num_requests_waiting`), moving beyond crude TCP ping checks to true Layer 7 inference telemetry.
- **Dynamic Clockwise Spillover Walk**: If the primary affinity worker reaches capacity ($> 85\%$ VRAM or exceeding load bound $C$), `kvring` smoothly walks clockwise along the hash ring to the nearest underloaded worker. This preserves cache locality for normal sessions while gracefully distributing surge traffic without crushing adjacent nodes.
- **Zero-Buffering Streaming SSE & gRPC**: Proxies token streams chunk-by-chunk with zero allocation overhead and microsecond latency, preserving backpressure between GPU workers and downstream clients.
- **Dual Observability (Terminal TUI & Web Cockpit)**: Features an interactive terminal dashboard built with Charm's Bubble Tea and Lip Gloss for instant local monitoring and failure simulation, alongside an embedded React 19 web cockpit with Prometheus `/metrics` export for cloud-native production deployments.

---

## 2. High-Level System Architecture

```
       +-------------------------------------------------------------+
       |               AI Clients / Multi-Turn Agents                |
       |             (Claude Code, Chat Apps, LangChain)             |
       +-------------------------------------------------------------+
                                      |
                     HTTP / SSE Request (:8000) or gRPC
                                      v
       +-------------------------------------------------------------+
       |                      kvring Ingress Gateway                 |
       |                    (High-Performance Go CLI)                |
       |                                                             |
       |  +---------------------+        +------------------------+  |
       |  | Affinity Key Extr.  |------->| Consistent Hash Ring   |  |
       |  | (Session / Prefix)  |        | (100 Virtual Replicas) |  |
       |  +---------------------+        +------------------------+  |
       |                                              |              |
       |                                              v              |
       |  +-------------------------------------------------------+  |
       |  |          Bounded Load & VRAM Capacity Evaluator       |  |
       |  |  Query: Active Load <= C && Worker VRAM < 85%?        |  |
       |  +-------------------------------------------------------+  |
       |               /                               \             |
       |       Under Capacity                     Over Capacity      |
       |             /                                   \           |
       |            v                                     v          |
       |   +------------------+                 +-----------------+  |
       |   | Primary Worker   |                 | Clockwise Ring  |  |
       |   | (100% Cache Hit) |                 | Spillover Walk  |  |
       |   +------------------+                 +-----------------+  |
       |            \                                     /          |
       |             +-----------------+-----------------+           |
       |                               |                             |
       |                               v                             |
       |  +-------------------------------------------------------+  |
       |  |             Zero-Buffering Streaming Proxy            |  |
       |  |   Streaming SSE Chunk Passthrough (Zero Overhead)     |  |
       |  +-------------------------------------------------------+  |
       |                               |                             |
       +-------------------------------|-----------------------------+
                                       |
                   +-------------------+-------------------+
                   |                   |                   |
                   v                   v                   v
            +-------------+     +-------------+     +-------------+
            | GPU Node 1  |     | GPU Node 2  |     | GPU Node 3  |
            | vLLM :8001  |     | SGLang :8002|     | vLLM :8003  |
            +-------------+     +-------------+     +-------------+
                   ▲                   ▲                   ▲
                   └───────────────────┼───────────────────┘
                                       │
                        L7 KV-Cache Health Poller (:metrics)
                                       │
       +-------------------------------------------------------------+
       |                   Observability Surfaces                    |
       |  1. Bubble Tea Interactive Terminal TUI (Live Key Simulation)|
       |  2. Embedded React 19 Cluster Cockpit (Local HTTP :8484)    |
       |  3. Prometheus Metrics Exporter (/metrics)                  |
       +-------------------------------------------------------------+
```

---

## 3. Mathematical Formulation: Bounded-Load Consistent Hashing for KV-Caches

### 3.1 Consistent Hash Ring Placement
Let $\mathcal{W} = \{w_1, w_2, \dots, w_M\}$ be the set of active GPU inference workers. Each worker is assigned $R = 100$ virtual positions on the circle $[0, 2^{32} - 1]$ using IEEE CRC32 or Murmur3 hashing:

$$v_{i, r} = \text{Hash}(\text{worker\_id}_i \parallel r), \quad \forall r \in \{1, \dots, R\}$$

A request with affinity key $K$ (derived from session ID or prompt prefix hash) is initially mapped to primary worker $w^*$:

$$w^* = \arg\min_{v_{i,r}} \{ v_{i,r} \mid v_{i,r} \ge \text{Hash}(K) \}$$

### 3.2 Dynamic Capacity Bound ($C$) with VRAM Modulation
Let $L_{\text{total}}$ represent the total active in-flight requests (or active token load), $M$ the number of active healthy GPU workers, and $\epsilon$ the bounded load factor (default $\epsilon = 0.25$, or $125\%$ of mean):

$$C = \left\lceil (1 + \epsilon) \cdot \frac{L_{\text{total}}}{M} \right\rceil$$

To incorporate GPU hardware memory limits, the effective worker acceptance condition is:

$$\text{Accept}(w_i) = \Big( \text{Load}(w_i) < C \Big) \;\land\; \Big( \text{VRAM}_{\text{util}}(w_i) < \theta_{\text{vram\_max}} \Big)$$

*(where $\theta_{\text{vram\_max}}$ is configured to $0.85$ or $85\%$).*

### 3.3 Clockwise Spillover Traversal
If $\text{Accept}(w^*)$ evaluates to `false`, the routing engine advances clockwise along the virtual ring:

$$w_{\text{target}} = \text{WalkClockwise}(\text{Ring}, w^*, \text{condition}=\text{Accept})$$

If all nodes exceed the capacity bound $C$, the request is dispatched to the worker with the lowest instantaneous VRAM utilization $\min_{w \in \mathcal{W}} \text{VRAM}_{\text{util}}(w)$, preventing cluster-wide deadlock.

---

## 4. Repository Structure

```
kvring/
├── cmd/
│   └── kvring/
│       └── main.go                 # CLI entry point, subcommands: serve, proxy, bench, tui
├── internal/
│   ├── config/
│   │   ├── config.go               # YAML configuration loader for workers, timeouts, weights
│   │   └── schema.go               # Worker definitions, bounded load factors, affinity rules
│   ├── ring/
│   │   ├── hash_ring.go            # Consistent hash ring with virtual nodes and binary search
│   │   ├── bounded_load.go         # Mirrokni capacity calculator and clockwise spillover logic
│   │   └── affinity.go             # Session ID, cookie, header, and prefix hash extractors
│   ├── proxy/
│   │   ├── reverse.go              # Zero-allocation streaming HTTP/1.1 and HTTP/2 reverse proxy
│   │   ├── sse_stream.go           # Streaming chunk forwarder maintaining backpressure
│   │   └── grpc_proxy.go           # High-speed gRPC inference proxy handler
│   ├── health/
│   │   ├── poller.go               # Background health checker polling vLLM/SGLang /metrics
│   │   ├── vram_monitor.go         # Parses gpu_cache_usage_perc and memory saturation
│   │   └── circuit_breaker.go      # Passive outlier detection ejecting failing workers
│   ├── telemetry/
│   │   ├── metrics.go              # Prometheus /metrics exporter (KV hit rate, TTFT, RPS)
│   │   └── atomic_counters.go      # Zero-lock sync/atomic counters for microsecond fast path
│   ├── tui/
│   │   ├── model.go                # Bubble Tea state model (active backends, traffic bars, events)
│   │   ├── view.go                 # Lip Gloss terminal layout and live visual gauge rendering
│   │   └── update.go               # Key bindings (K: kill node, R: restart, T: traffic spike)
│   └── server/
│       ├── http_server.go          # Main proxy listener on port :8000
│       ├── embed.go                # Go 1.16+ embed filesystem for React 19 cluster studio
│       └── sse_hub.go              # Real-time event stream for web studio
├── web/
│   ├── src/
│   │   ├── App.tsx                 # React 19 root application
│   │   ├── components/
│   │   │   ├── ClusterTopology.tsx # Interactive ring topology visualizer with active nodes
│   │   │   ├── VramGauges.tsx      # Real-time per-GPU VRAM and KV-cache utilization bars
│   │   │   ├── HitRateWatermark.tsx# Prefix cache hit rate percentage chart (0-100%)
│   │   │   ├── SpilloverEventLog.tsx # Log of requests redirected due to bounded load
│   │   │   └── TtftDistribution.tsx# Latency comparison (Cache Hit vs. Cache Miss)
│   │   └── pages/
│   │       └── Dashboard.tsx       # Main Cluster Management Cockpit
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts
├── examples/
│   ├── kvring.yaml                 # Standard multi-worker production configuration
│   ├── mock_vllm_cluster.go        # Simulates 3 vLLM workers with realistic KV metrics
│   └── traffic_benchmark.go        # High-concurrency traffic generator testing spillover
├── Makefile
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

---

## 5. Implementation Roadmap

### Phase 1: Go Engine Setup and Consistent Hash Ring
- Initialize Go module with minimal dependencies.
- Build virtual-node consistent hash ring in `internal/ring/hash_ring.go` with 100 virtual replicas per worker and binary search lookup in $< 100\text{ns}$.
- Implement affinity key extractors supporting `session_id` in JSON body, `X-Session-ID` header, or URL query parameters.

### Phase 2: Bounded-Load Engine & Mirrokni Spillover
- Implement dynamic capacity calculator in `internal/ring/bounded_load.go` computing $C = \lceil (1 + \epsilon) \cdot L_{\text{total}} / M \rceil$.
- Build clockwise spillover walk traversing the ring when candidate worker is saturated.

### Phase 3: L7 Inference Health & VRAM Metrics Poller
- Implement `vram_monitor.go` polling `/metrics` endpoints of vLLM (`vllm:gpu_cache_usage_perc`) and SGLang.
- Automatically mark workers as degraded or saturated when VRAM usage exceeds $85\%$.
- Implement passive outlier detection tripping on consecutive HTTP 5xx responses.

### Phase 4: Zero-Buffering Streaming Reverse Proxy
- Build high-speed HTTP/1.1 and HTTP/2 reverse proxy in `internal/proxy/reverse.go`.
- Stream Server-Sent Events (SSE) token chunks directly to client connections with zero memory buffering.
- Implement gRPC inference proxying for enterprise cluster deployments.

### Phase 5: Interactive Terminal TUI (Bubble Tea)
- Develop interactive TUI in `internal/tui` using `charmbracelet/bubbletea` and `lipgloss`.
- Display live cluster status, measured requests per second (RPS), per-worker KV cache hit rates, and recent routing logs.
- Add keyboard hotkeys (`K` to simulate node failure, `R` to recover, `T` to trigger traffic surge).

### Phase 6: Embedded React 19 Cluster Cockpit & Prometheus Metrics
- Develop modern web UI in `web/` featuring circular ring topology diagram, VRAM gauges, and spillover event log.
- Embed frontend build artifacts into the Go binary via `//go:embed` and expose on port `:8484`.
- Export standard Prometheus metrics on `/metrics`.

### Phase 7: Verification, Benchmarking, and Documentation
- Run `examples/mock_vllm_cluster.go` and `traffic_benchmark.go` verifying that hot sessions spill over cleanly without dropping requests.
- Verify proxy routing latency remains strictly under 100ns and binary idle RAM stays below 25MB.
- Author humanized README with setup guides, vLLM/SGLang integration examples, and architectural comparison diagrams.
