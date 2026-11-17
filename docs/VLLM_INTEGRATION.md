# vLLM and SGLang Integration Guide

This guide describes how to configure physical or containerized vLLM and SGLang GPU instances to work with `kvring`.

## 1. Configuring vLLM Instances

To maximize KV-cache reuse with `kvring`, ensure that vLLM is launched with automatic prefix caching enabled.

### Example Launch Command

```bash
python3 -m vllm.entrypoints.openai.api_server \
  --model meta-llama/Llama-3.3-70B-Instruct \
  --tensor-parallel-size 4 \
  --enable-prefix-caching \
  --gpu-memory-utilization 0.90 \
  --max-num-seqs 64 \
  --port 8001
```

### Critical vLLM Arguments

- `--enable-prefix-caching`: Enables vLLM Automatic Prefix Caching (APC). Without this flag, vLLM recomputes KV-caches even when identical prefixes arrive on the same node.
- `--gpu-memory-utilization 0.90`: Allocates up to 90% of GPU VRAM for model weights and KV-cache blocks. `kvring` monitors `vllm:gpu_cache_usage_perc` and triggers spillover when this metric exceeds 85%.

## 2. Configuring SGLang Instances

SGLang uses RadixAttention for hierarchical KV-cache management.

### Example Launch Command

```bash
python3 -m sglang.launch_server \
  --model-path meta-llama/Llama-3.3-70B-Instruct \
  --tp 4 \
  --enable-metrics \
  --mem-fraction-static 0.85 \
  --port 8002
```

### Critical SGLang Arguments

- `--enable-metrics`: Exposes the Prometheus `/metrics` endpoint on the SGLang server.
- `--mem-fraction-static 0.85`: Caps memory allocation so that telemetry reliably reports saturation before out-of-memory errors occur.

## 3. Configuring `kvring.yaml`

Map the running instances into your `kvring.yaml` configuration file:

```yaml
server:
  listen_addr: ":8000"
  cockpit_addr: ":8484"

bounded_load:
  epsilon: 0.25
  virtual_replicas: 100
  vram_threshold: 0.85

affinity:
  header_key: "X-Session-ID"
  enable_prefix: true
  prefix_length: 128

health:
  poll_interval: 2s
  failure_threshold: 3
  vram_metric_name: "vllm:gpu_cache_usage_perc"

workers:
  - id: "gpu-node-vllm-1"
    url: "http://10.0.1.10:8001"
    weight: 1
    metrics_url: "http://10.0.1.10:8001/metrics"

  - id: "gpu-node-sglang-2"
    url: "http://10.0.1.11:8002"
    weight: 1
    metrics_url: "http://10.0.1.11:8002/metrics"

  - id: "gpu-node-vllm-3"
    url: "http://10.0.1.12:8003"
    weight: 1
    metrics_url: "http://10.0.1.12:8003/metrics"
```

## 4. Kubernetes Deployment Example

Deploy `kvring` as an ingress gateway in front of a StatefulSet of GPU inference workers:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kvring-gateway
  namespace: llm-platform
spec:
  replicas: 2
  selector:
    matchLabels:
      app: kvring
  template:
    metadata:
      labels:
        app: kvring
    spec:
      containers:
        - name: kvring
          image: ghcr.io/alexandrmotologa/kvring:latest
          args: ["serve", "--config", "/etc/kvring/kvring.yaml"]
          ports:
            - containerPort: 8000
              name: http-proxy
            - containerPort: 8484
              name: cockpit
          volumeMounts:
            - name: config-volume
              mountPath: /etc/kvring
          resources:
            requests:
              cpu: "500m"
              memory: "64Mi"
            limits:
              cpu: "2"
              memory: "256Mi"
      volumes:
        - name: config-volume
          configMap:
            name: kvring-config
```
