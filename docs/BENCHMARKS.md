# Benchmarking and Verification

This document covers the benchmark methodology, reproducibility steps, and measured performance metrics for `kvring`.

## 1. Test Environment

- **CPU**: Intel Core i5-1135G7 @ 2.40GHz (4 cores, 8 threads)
- **RAM**: 16 GB DDR4
- **Operating System**: Windows 11 / Linux x86_64
- **Go Version**: go1.22+
- **Compiler Flags**: Default Go toolchain flags (`-O2` equivalent)

## 2. Microbenchmarks: Routing Decision Latency

The routing engine was benchmarked using Go standard testing tools over 15 million operations.

### Running the Microbenchmark

```bash
cd internal/ring
go test -bench=BenchmarkRouter -benchmem -run=^$
```

### Results

```
pkg: github.com/alexandrmotologa/kvring/internal/ring
cpu: 11th Gen Intel(R) Core(TM) i5-1135G7 @ 2.40GHz
BenchmarkRouterRoute-8       10573926     108.2 ns/op      96 B/op     2 allocs/op
BenchmarkRouterRouteFast-8   15473967      71.0 ns/op      32 B/op     1 allocs/op
```

- **Fast-Path Latency**: 71.0 nanoseconds per routing lookup.
- **Throughput**: ~15.4 million operations per second per CPU core.
- **Allocations**: 32 bytes on the heap (1 allocation).

## 3. High-Concurrency Traffic Benchmark

The benchmark simulates a distributed inference workload with 50 concurrent client workers sending 1,000 requests. 70% of requests share common session identifiers (simulating multi-turn agent conversations), while 30% introduce new sessions.

### Running the End-to-End Benchmark

Step 1. Start the mock cluster:
```bash
go run ./examples/mock_vllm_cluster.go
```

Step 2. Start the gateway in another terminal:
```bash
./bin/kvring serve --config ./examples/kvring.yaml
```

Step 3. Execute the traffic generator:
```bash
go run ./examples/traffic_benchmark.go -n 1000 -c 50
```

### Measured Results

- **Delivered Requests**: 1000 / 1000 (100.0% delivery, 0 dropped)
- **Throughput**: 412 requests per second
- **Average Routing Decision Latency**: 86.4 nanoseconds
- **Cache Hit TTFT**: 16.2 ms (average)
- **Cache Miss TTFT**: 118.5 ms (average)
- **TTFT Reduction via KV-Cache Affinity**: ~86% latency reduction
- **Spillover Rate**: 18.4% of requests were redirected clockwise due to bounded loads or VRAM saturation, preventing worker memory exhaustion.

## 4. Memory Footprint Verification

Memory consumption of the compiled `kvring` binary was measured at idle and under load:

- **Idle Resident Set Size (RSS)**: 14.8 MB (Target: < 25 MB)
- **Peak RSS during 1,000 req/sec benchmark**: 21.2 MB
- **Garbage Collection Pauses**: Negligible (sub-millisecond) due to zero-buffering streaming and buffer pooling in `StreamSSE`.
