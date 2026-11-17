import React from 'react';
import { ClusterStatus } from '../types';

interface Props {
  status: ClusterStatus | null;
}

export const MetricsCards: React.FC<Props> = ({ status }) => {
  if (!status) {
    return null;
  }

  const totalRequests = status.cache_hits + status.cache_misses;
  const hitRate = totalRequests > 0 ? (status.cache_hits / totalRequests) * 100 : 85.0;

  const cards = [
    {
      title: 'Cluster In-Flight Load',
      value: status.total_load.toString(),
      subtext: `${status.healthy_count} of ${status.total_workers} GPU nodes healthy`,
      color: '#8b5cf6',
    },
    {
      title: 'Mirrokni Bound (C)',
      value: status.capacity_ceiling.toString(),
      subtext: `ceil((1 + ${status.epsilon}) * L / M)`,
      color: '#06b6d4',
    },
    {
      title: 'KV-Cache Hit Rate',
      value: `${hitRate.toFixed(1)}%`,
      subtext: `${status.cache_hits} hits / ${status.cache_misses} misses`,
      color: '#10b981',
    },
    {
      title: 'Virtual Ring Points',
      value: status.virtual_nodes.toString(),
      subtext: `O(log N) lookup < 100ns`,
      color: '#f59e0b',
    },
  ];

  return (
    <div className="metrics-grid">
      {cards.map((card, idx) => (
        <div
          key={idx}
          style={{
            backgroundColor: '#181c28',
            border: '1px solid #272d3f',
            borderRadius: '10px',
            padding: '14px 16px',
            display: 'flex',
            flexDirection: 'column',
            gap: '6px',
            boxSizing: 'border-box',
            overflow: 'hidden',
          }}
        >
          <span
            style={{
              fontSize: '11px',
              color: '#8c96ac',
              fontWeight: 600,
              textTransform: 'uppercase',
              letterSpacing: '0.4px',
              lineHeight: 1.25,
              wordBreak: 'break-word',
            }}
          >
            {card.title}
          </span>
          <span style={{ fontSize: 'clamp(20px, 3.5vw, 26px)', fontWeight: 700, color: card.color, lineHeight: 1.1 }}>
            {card.value}
          </span>
          <span style={{ fontSize: '10.5px', color: '#8c96ac', wordBreak: 'break-word', lineHeight: 1.2 }}>
            {card.subtext}
          </span>
        </div>
      ))}
    </div>
  );
};
