import React, { useEffect, useState, useCallback } from 'react';
import { ClusterStatus, RouteEvent } from './types';
import { MetricsCards } from './components/MetricsCards';
import { ClusterTopology } from './components/ClusterTopology';
import { VramGauges } from './components/VramGauges';
import { SpilloverEventLog } from './components/SpilloverEventLog';
import { SimulationControls } from './components/SimulationControls';

export const App: React.FC = () => {
  const [status, setStatus] = useState<ClusterStatus | null>(null);
  const [events, setEvents] = useState<RouteEvent[]>([]);
  const [selectedWorker, setSelectedWorker] = useState<string | null>(null);
  const [connected, setConnected] = useState<boolean>(false);

  const fetchStatus = useCallback(async () => {
    try {
      const res = await fetch('/api/status');
      if (res.ok) {
        const data = await res.json();
        setStatus(data);
        setConnected(true);
      }
    } catch {
      setConnected(false);
    }
  }, []);

  useEffect(() => {
    fetchStatus();
    const interval = setInterval(fetchStatus, 1000);
    return () => clearInterval(interval);
  }, [fetchStatus]);

  // Connect to SSE event stream
  useEffect(() => {
    const es = new EventSource('/api/events');

    es.onopen = () => {
      setConnected(true);
    };

    es.onmessage = (e) => {
      try {
        const evt: RouteEvent = JSON.parse(e.data);
        setEvents((prev) => [...prev.slice(-100), evt]);
        fetchStatus();
      } catch (err) {
        console.error('Failed to parse SSE event:', err);
      }
    };

    es.onerror = () => {
      setConnected(false);
    };

    return () => {
      es.close();
    };
  }, [fetchStatus]);

  const handleToggleHealth = async (workerId: string, currentHealthy: boolean) => {
    try {
      await fetch('/api/nodes', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          worker_id: workerId,
          healthy: !currentHealthy,
        }),
      });
      fetchStatus();
    } catch (err) {
      console.error('Failed to toggle worker health:', err);
    }
  };

  const handleSimulateBurst = async (count: number, heavyContext: boolean) => {
    try {
      await fetch('/api/simulate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          count,
          heavy_context: heavyContext,
        }),
      });
      fetchStatus();
    } catch (err) {
      console.error('Failed to trigger simulation burst:', err);
    }
  };

  return (
    <div className="app-container">
      {/* Top Header */}
      <header
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          paddingBottom: '16px',
          borderBottom: '1px solid #272d3f',
          marginBottom: '20px',
          flexWrap: 'wrap',
          gap: '12px',
          width: '100%',
          boxSizing: 'border-box',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px', minWidth: 0, flex: '1 1 auto' }}>
          <div
            style={{
              width: '36px',
              height: '36px',
              borderRadius: '8px',
              backgroundColor: '#8b5cf6',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              fontWeight: 800,
              fontSize: '18px',
              color: '#ffffff',
              flexShrink: 0,
            }}
          >
            KV
          </div>
          <div>
            <h1 style={{ fontSize: '18px', fontWeight: 700, color: '#f0f3fa' }}>
              kvring <span style={{ fontSize: '12px', fontWeight: 500, color: '#8c96ac' }}>Cluster Cockpit</span>
            </h1>
            <p style={{ fontSize: '11px', color: '#8c96ac', wordBreak: 'break-word' }}>
              Mirrokni Bounded-Load Consistent Hashing with VRAM Telemetry
            </p>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '10px', fontSize: '12px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
            <span
              style={{
                width: '8px',
                height: '8px',
                borderRadius: '50%',
                backgroundColor: connected ? '#10b981' : '#f43f5e',
                boxShadow: connected ? '0 0 8px #10b981' : 'none',
              }}
            />
            <span style={{ color: connected ? '#10b981' : '#f43f5e', fontWeight: 600 }}>
              {connected ? 'LIVE TELEMETRY' : 'CONNECTING'}
            </span>
          </div>

          <a
            href="/metrics"
            target="_blank"
            rel="noreferrer"
            style={{
              color: '#06b6d4',
              textDecoration: 'none',
              border: '1px solid #06b6d4',
              padding: '3px 8px',
              borderRadius: '4px',
              fontWeight: 600,
              fontSize: '11px',
            }}
          >
            Prometheus /metrics
          </a>
        </div>
      </header>

      {/* Metric Cards Row */}
      <MetricsCards status={status} />

      {/* Traffic and Fault Injection Controls */}
      <SimulationControls onSimulateBurst={handleSimulateBurst} />

      {/* Cluster Ring Topology and VRAM Telemetry Grid */}
      <div className="dashboard-grid">
        <ClusterTopology
          workers={status?.workers || []}
          selectedWorker={selectedWorker}
          onSelectWorker={setSelectedWorker}
        />
        <VramGauges
          workers={status?.workers || []}
          selectedWorker={selectedWorker}
          onSelectWorker={setSelectedWorker}
          onToggleHealth={handleToggleHealth}
        />
      </div>

      {/* Live Routing Decision Log */}
      <SpilloverEventLog events={events} />
    </div>
  );
};
