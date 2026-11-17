import React from 'react';
import { WorkerState } from '../types';

interface Props {
  workers: WorkerState[];
  selectedWorker: string | null;
  onSelectWorker: (id: string) => void;
  onToggleHealth: (id: string, currentHealthy: boolean) => void;
}

export const VramGauges: React.FC<Props> = ({
  workers,
  selectedWorker,
  onSelectWorker,
  onToggleHealth,
}) => {
  return (
    <div
      style={{
        backgroundColor: '#181c28',
        border: '1px solid #272d3f',
        borderRadius: '10px',
        padding: '16px',
        width: '100%',
        boxSizing: 'border-box',
        overflow: 'hidden',
      }}
    >
      <div style={{ marginBottom: '14px' }}>
        <h3 style={{ fontSize: '15px', fontWeight: 600, color: '#f0f3fa' }}>GPU Worker Fleet Telemetry</h3>
        <p style={{ fontSize: '12px', color: '#8c96ac' }}>Live VRAM saturation and active request load</p>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        {workers.map((worker) => {
          const isSelected = selectedWorker === worker.id;
          const vramPercent = Math.round(worker.vram_ratio * 100);

          let barColor = '#10b981';
          if (!worker.healthy) {
            barColor = '#f43f5e';
          } else if (vramPercent >= 85) {
            barColor = '#f43f5e';
          } else if (vramPercent >= 70) {
            barColor = '#f59e0b';
          }

          return (
            <div
              key={worker.id}
              onClick={() => onSelectWorker(worker.id)}
              style={{
                backgroundColor: isSelected ? '#1f2434' : '#12151e',
                border: `1px solid ${isSelected ? '#8b5cf6' : '#272d3f'}`,
                borderRadius: '8px',
                padding: '12px 14px',
                cursor: 'pointer',
                transition: 'all 0.15s ease',
                boxSizing: 'border-box',
                overflow: 'hidden',
              }}
            >
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '6px', marginBottom: '8px' }}>
                <div style={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: '6px' }}>
                  <span style={{ fontSize: '13px', fontWeight: 600, color: '#f0f3fa' }}>
                    {worker.id}
                  </span>
                  <span style={{ fontSize: '11px', color: '#8c96ac', fontFamily: 'monospace', wordBreak: 'break-all' }}>
                    {worker.url}
                  </span>
                </div>
                <div>
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      onToggleHealth(worker.id, worker.healthy);
                    }}
                    style={{
                      backgroundColor: worker.healthy ? 'rgba(16, 185, 129, 0.15)' : 'rgba(244, 63, 94, 0.15)',
                      color: worker.healthy ? '#10b981' : '#f43f5e',
                      border: `1px solid ${worker.healthy ? '#10b981' : '#f43f5e'}`,
                      borderRadius: '4px',
                      padding: '3px 8px',
                      fontSize: '11px',
                      fontWeight: 600,
                      cursor: 'pointer',
                    }}
                  >
                    {worker.healthy ? 'HEALTHY' : 'EJECTED'}
                  </button>
                </div>
              </div>

              {/* VRAM Progress Bar */}
              <div style={{ marginBottom: '10px' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', marginBottom: '4px' }}>
                  <span style={{ color: '#8c96ac' }}>VRAM / KV-Cache</span>
                  <span style={{ fontWeight: 600, color: barColor }}>{vramPercent}% / 85% limit</span>
                </div>
                <div style={{ height: '7px', backgroundColor: '#272d3f', borderRadius: '4px', overflow: 'hidden' }}>
                  <div
                    style={{
                      height: '100%',
                      width: `${Math.min(vramPercent, 100)}%`,
                      backgroundColor: barColor,
                      transition: 'width 0.3s ease',
                    }}
                  />
                </div>
              </div>

              {/* Responsive 2x2 or 4x1 Worker Counters */}
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(110px, 1fr))', gap: '6px', fontSize: '11px' }}>
                <div>
                  <span style={{ color: '#8c96ac' }}>In-Flight: </span>
                  <strong style={{ color: '#f0f3fa' }}>{worker.active_load}</strong>
                </div>
                <div>
                  <span style={{ color: '#8c96ac' }}>Spill In: </span>
                  <strong style={{ color: '#f59e0b' }}>{worker.spillovers_in}</strong>
                </div>
                <div>
                  <span style={{ color: '#8c96ac' }}>Spill Out: </span>
                  <strong style={{ color: '#f43f5e' }}>{worker.spillovers_out}</strong>
                </div>
                <div>
                  <span style={{ color: '#8c96ac' }}>Served: </span>
                  <strong style={{ color: '#06b6d4' }}>{worker.total_requests}</strong>
                </div>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};
