# Architecture and Technical Specification

This document details the routing algorithms, telemetry ingestion, and concurrency models implemented in `kvring`.

## 1. Problem Formulation

In transformer inference, prompt prefill computes attention keys and values (the KV-cache). This phase accounts for 70% to 80% of total inference computation. When subsequent turns in a multi-turn chat or agent workflow are routed to the same GPU worker, existing KV-cache tensors can be reused without recomputation, reducing Time To First Token (TTFT) by up to 80%.

Standard load balancers (such as round-robin or least-connections) destroy cache affinity by distributing consecutive turns across random nodes. Conversely, naive sticky session routing causes hot-worker hotspots: when an agent executes a long multi-step workflow with a large prompt context, a single GPU worker suffers VRAM saturation or out-of-memory (OOM) crashes. If that node crashes, naive consistent hashing dumps all of its workload onto the immediate neighbor, causing cascading failures.

`kvring` addresses both problems using Consistent Hashing with Bounded Loads coupled with real-time VRAM telemetry.

## 2. Mirrokni Bounded-Load Algorithm

`kvring` implements the bounded-load consistent hashing formulation originally developed by Google Research (Mirrokni, Thorup, et al.).

### 2.1 Consistent Hash Ring Placement

Let $W = \{w_1, w_2, \dots, w_M\}$ represent the set of active GPU workers. Each worker is mapped to $R$ virtual points on the circle $[0, 2^{32} - 1]$ using IEEE CRC32:

$$v_{i, r} = \text{CRC32}(w_i \parallel r), \quad \forall r \in \{1, \dots, R\}$$

The virtual points are sorted in ascending order. When an affinity key $K$ (session identifier or prompt prefix hash) arrives, its primary worker $w^*$ is located via binary search in $O(\log(M \cdot R))$ time:

$$w^* = \arg\min_{v_{i,r}} \{ v_{i,r} \mid v_{i,r} \ge \text{CRC32}(K) \}$$

If no virtual point is greater than $\text{CRC32}(K)$, the lookup wraps around to index 0.

### 2.2 Dynamic Capacity Bound

Let $L_{\text{total}}$ be the instantaneous sum of active in-flight requests across the cluster, $M_{\text{healthy}}$ the count of healthy workers, and $\epsilon$ the load tolerance parameter (default $\epsilon = 0.25$):

$$C = \left\lceil (1 + \epsilon) \cdot \frac{L_{\text{total}}}{M_{\text{healthy}}} \right\rceil$$

When $L_{\text{total}} = 0$, $C = 1$. The parameter $\epsilon = 0.25$ ensures that no worker receives more than 125% of the average cluster load.

### 2.3 Hardware-Aware Acceptance Condition

A candidate worker $w_i$ is accepted for request dispatch if and only if all three conditions are satisfied:

$$\text{Accept}(w_i) = \Big( \text{Healthy}(w_i) \Big) \land \Big( \text{ActiveLoad}(w_i) < C \Big) \land \Big( \text{VRAM}_{\text{util}}(w_i) < \theta_{\text{vram}} \Big)$$

where $\theta_{\text{vram}}$ is configured to $0.85$ (85% of GPU memory).

### 2.4 Clockwise Spillover Traversal

If the primary worker $w^*$ violates any acceptance condition, the router advances clockwise along the virtual ring:

1. Traverse virtual points $(idx + 1) \pmod N, (idx + 2) \pmod N, \dots$
2. For each unique worker encountered, evaluate $\text{Accept}(w_k)$.
3. Dispatch to the first acceptable worker found.
4. Record the spillover reason (`bounded_load_spillover` or `vram_spillover`).

If all workers in the cluster exceed the capacity bound or VRAM threshold during a traffic surge, `kvring` falls back to the worker with the lowest instantaneous VRAM utilization:

$$w_{\text{fallback}} = \arg\min_{w \in W_{\text{healthy}}} \text{VRAM}_{\text{util}}(w)$$

This prevents request drops and avoids cascading cluster deadlock.

## 3. High-Speed Routing Fast Path

To achieve routing decisions under 100 nanoseconds:

1. **Hardware-Accelerated Hashing**: `crc32.ChecksumIEEE` compiles directly to the `CRC32` instruction on x86_64 CPUs, executing in under 3 nanoseconds.
2. **Lock-Free Atomic Counters**: Total cluster load and per-worker loads are maintained with `sync/atomic` 64-bit integers.
3. **Sorted Virtual Slice**: Virtual nodes are stored in a contiguous slice. Binary search takes $\lceil \log_2(300) \rceil \approx 9$ comparisons for a 3-worker cluster with 100 replicas each.
4. **Zero-Allocation Direct Hit**: On the direct-hit path (which handles the majority of requests), no heap allocations are performed.

## 4. Telemetry and Health Engine

### 4.1 Prometheus Metric Scraping

The telemetry poller queries `/metrics` on each backend worker at configured intervals (default 2 seconds). It reads:

- `vllm:gpu_cache_usage_perc` or `sglang:gpu_cache_usage_perc`: float value representing current KV-cache memory usage.
- `vllm:num_requests_running`: count of active generation requests on the GPU.
- `vllm:num_requests_waiting`: count of requests queued in the engine scheduler.

Values are stored atomically in worker state using `math.Float64bits`.

### 4.2 Outlier Detection and Circuit Breaking

`kvring` maintains a passive circuit breaker per worker:

- Consecutive 5xx HTTP responses or TCP connection drops increment a failure counter.
- When the failure counter reaches `failure_threshold` (default 3), the worker is ejected from the active hash ring.
- After `recovery_duration` (default 15 seconds), the circuit transitions to half-open, allowing a single probe request to verify recovery before restoring full traffic.

## 5. Streaming Reverse Proxy

The reverse proxy uses `http.Transport` with connection pooling and disabled compression to preserve streaming token chunks:

- SSE responses (`text/event-stream`) are forwarded chunk-by-chunk via `http.Flusher`.
- Backpressure is preserved: if the downstream client reads slowly, the TCP window throttles the upstream GPU response.
- Diagnostic telemetry headers are injected into the response: `X-KVRing-Worker`, `X-KVRing-Primary`, `X-KVRing-Spillover`, `X-KVRing-Reason`, and `X-KVRing-Routing-Ns`.
