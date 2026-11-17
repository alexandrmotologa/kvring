export interface WorkerState {
  id: string;
  url: string;
  weight: number;
  max_concurrency: number;
  active_load: number;
  vram_ratio: number;
  healthy: boolean;
  total_requests: number;
  spillovers_in: number;
  spillovers_out: number;
}

export interface ClusterStatus {
  total_load: number;
  healthy_count: number;
  total_workers: number;
  capacity_ceiling: number;
  epsilon: number;
  vram_threshold: number;
  virtual_nodes: number;
  cache_hits: number;
  cache_misses: number;
  workers: WorkerState[];
}

export interface RouteEvent {
  timestamp: string;
  affinity_key: string;
  primary_worker: string;
  target_worker: string;
  reason: string;
  spillover: boolean;
  spillover_steps: number;
  ttft_ms: number;
  duration_ms: number;
  status_code: number;
}
