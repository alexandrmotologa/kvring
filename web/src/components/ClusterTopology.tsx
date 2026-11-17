import React from 'react';
import { WorkerState } from '../types';

interface Props {
  workers: WorkerState[];
  selectedWorker: string | null;
  onSelectWorker: (id: string) => void;
}

export const ClusterTopology: React.FC<Props> = ({ workers, selectedWorker, onSelectWorker }) => {
  const size = 380;
  const cx = size / 2;
  const cy = size / 2;
  const radius = 120;

  const count = workers.length;

  return (
    <div
      style={{
        backgroundColor: '#181c28',
        border: '1px solid #272d3f',
        borderRadius: '10px',
        padding: '16px',
        display: 'flex',
        flexDirection: 'column',
        alignItems: 'center',
        width: '100%',
        boxSizing: 'border-box',
        overflow: 'hidden',
      }}
    >
      <div style={{ width: '100%', marginBottom: '10px' }}>
        <h3 style={{ fontSize: '15px', fontWeight: 600, color: '#f0f3fa' }}>Consistent Hash Ring Topology</h3>
        <p style={{ fontSize: '12px', color: '#8c96ac' }}>Clockwise bounded load spillover walk</p>
      </div>

      <div style={{ width: '100%', maxWidth: '320px', margin: '0 auto', display: 'flex', justifyContent: 'center' }}>
        <svg
          viewBox={`0 0 ${size} ${size}`}
          style={{ width: '100%', height: 'auto', maxHeight: '320px', display: 'block' }}
        >
          {/* Outer Orbit Circle */}
          <circle
            cx={cx}
            cy={cy}
            r={radius}
            fill="none"
            stroke="#272d3f"
            strokeWidth="3"
            strokeDasharray="6 6"
          />

          {/* Dynamic Ring Perimeter Accent */}
          <circle
            cx={cx}
            cy={cy}
            r={radius}
            fill="none"
            stroke="#8b5cf6"
            strokeWidth="1.5"
            opacity="0.4"
          />

          {/* Center Cluster Core */}
          <circle cx={cx} cy={cy} r="40" fill="#12151e" stroke="#8b5cf6" strokeWidth="2" />
          <text
            x={cx}
            y={cy - 5}
            textAnchor="middle"
            fill="#8b5cf6"
            fontSize="11"
            fontWeight="700"
            letterSpacing="1px"
          >
            KVRING
          </text>
          <text
            x={cx}
            y={cy + 12}
            textAnchor="middle"
            fill="#8c96ac"
            fontSize="10"
          >
            {count > 0 ? `${count} NODES` : 'SCANNING'}
          </text>

          {/* Physical Worker Nodes on the Perimeter */}
          {count > 0 &&
            workers.map((worker, i) => {
              const angle = (i * 2 * Math.PI) / count - Math.PI / 2;
              const nodeX = cx + radius * Math.cos(angle);
              const nodeY = cy + radius * Math.sin(angle);
              const isSelected = selectedWorker === worker.id;

              const isHealthy = worker.healthy;
              const vramRatio = worker.vram_ratio;
              let nodeColor = '#10b981'; // Green < 70%
              if (!isHealthy) {
                nodeColor = '#f43f5e'; // Red (Ejected)
              } else if (vramRatio >= 0.85) {
                nodeColor = '#f43f5e'; // Red (Saturated > 85%)
              } else if (vramRatio >= 0.70) {
                nodeColor = '#f59e0b'; // Amber (70-85%)
              }

              return (
                <g
                  key={worker.id}
                  onClick={() => onSelectWorker(worker.id)}
                  style={{ cursor: 'pointer' }}
                >
                  {/* Outer Selection Halo */}
                  {isSelected && (
                    <circle
                      cx={nodeX}
                      cy={nodeY}
                      r="26"
                      fill="none"
                      stroke={nodeColor}
                      strokeWidth="2"
                      strokeDasharray="3 3"
                    />
                  )}

                  {/* Node Background */}
                  <circle
                    cx={nodeX}
                    cy={nodeY}
                    r="20"
                    fill="#12151e"
                    stroke={nodeColor}
                    strokeWidth={isSelected ? '3' : '2'}
                  />

                  {/* Node Initial */}
                  <text
                    x={nodeX}
                    y={nodeY - 2}
                    textAnchor="middle"
                    fill="#f0f3fa"
                    fontSize="10"
                    fontWeight="700"
                  >
                    {worker.id.replace('worker-', 'W')}
                  </text>

                  {/* VRAM Percentage Tag */}
                  <text
                    x={nodeX}
                    y={nodeY + 9}
                    textAnchor="middle"
                    fill={nodeColor}
                    fontSize="9"
                    fontWeight="600"
                  >
                    {isHealthy ? `${Math.round(vramRatio * 100)}%` : 'OFF'}
                  </text>

                  {/* Node Label Below */}
                  <text
                    x={nodeX}
                    y={nodeY + 28}
                    textAnchor="middle"
                    fill="#8c96ac"
                    fontSize="9"
                    fontWeight="500"
                  >
                    {worker.id}
                  </text>
                </g>
              );
            })}
        </svg>
      </div>
    </div>
  );
};
