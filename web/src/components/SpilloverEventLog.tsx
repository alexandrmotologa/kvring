import React, { useState } from 'react';
import { RouteEvent } from '../types';

interface Props {
  events: RouteEvent[];
}

export const SpilloverEventLog: React.FC<Props> = ({ events }) => {
  const [filterSpilloverOnly, setFilterSpilloverOnly] = useState(false);

  const displayedEvents = filterSpilloverOnly
    ? events.filter((e) => e.spillover)
    : events;

  return (
    <div
      style={{
        backgroundColor: '#181c28',
        border: '1px solid #272d3f',
        borderRadius: '10px',
        padding: '16px',
        marginTop: '20px',
        width: '100%',
        boxSizing: 'border-box',
        overflow: 'hidden',
      }}
    >
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px', marginBottom: '14px' }}>
        <div>
          <h3 style={{ fontSize: '15px', fontWeight: 600, color: '#f0f3fa' }}>Live Routing & Spillover Event Stream</h3>
          <p style={{ fontSize: '12px', color: '#8c96ac' }}>Microsecond telemetry stream over Server-Sent Events</p>
        </div>

        <label style={{ display: 'flex', alignItems: 'center', gap: '8px', fontSize: '12px', color: '#8c96ac', cursor: 'pointer' }}>
          <input
            type="checkbox"
            checked={filterSpilloverOnly}
            onChange={(e) => setFilterSpilloverOnly(e.target.checked)}
            style={{ accentColor: '#8b5cf6' }}
          />
          Show Spillovers Only
        </label>
      </div>

      <div style={{ overflowX: 'auto', WebkitOverflowScrolling: 'touch', width: '100%', display: 'block' }}>
        <table style={{ width: '100%', minWidth: '600px', borderCollapse: 'collapse', textAlign: 'left', fontSize: '12px' }}>
          <thead>
            <tr style={{ borderBottom: '1px solid #272d3f', color: '#8c96ac' }}>
              <th style={{ padding: '8px 10px' }}>TIMESTAMP</th>
              <th style={{ padding: '8px 10px' }}>AFFINITY KEY</th>
              <th style={{ padding: '8px 10px' }}>PRIMARY</th>
              <th style={{ padding: '8px 10px' }}>ROUTED TARGET</th>
              <th style={{ padding: '8px 10px' }}>DECISION REASON</th>
              <th style={{ padding: '8px 10px' }}>TTFT</th>
              <th style={{ padding: '8px 10px' }}>TOTAL</th>
              <th style={{ padding: '8px 10px' }}>STATUS</th>
            </tr>
          </thead>
          <tbody>
            {displayedEvents.length === 0 ? (
              <tr>
                <td colSpan={8} style={{ padding: '20px', textAlign: 'center', color: '#8c96ac' }}>
                  No inference events recorded yet. Send requests to :8000 or click "Inject Traffic Burst".
                </td>
              </tr>
            ) : (
              displayedEvents.slice(-15).reverse().map((evt, idx) => {
                const isSpill = evt.spillover;
                let reasonColor = '#10b981';
                if (evt.reason.includes('vram')) {
                  reasonColor = '#f43f5e';
                } else if (evt.reason.includes('bounded')) {
                  reasonColor = '#f59e0b';
                } else if (evt.reason.includes('fallback')) {
                  reasonColor = '#06b6d4';
                }

                return (
                  <tr
                    key={idx}
                    style={{
                      borderBottom: '1px solid #1f2434',
                      backgroundColor: isSpill ? 'rgba(245, 158, 11, 0.04)' : 'transparent',
                    }}
                  >
                    <td style={{ padding: '8px 10px', fontFamily: 'monospace', color: '#8c96ac' }}>
                      {new Date(evt.timestamp).toLocaleTimeString()}
                    </td>
                    <td style={{ padding: '8px 10px', fontFamily: 'monospace', color: '#f0f3fa' }}>
                      {evt.affinity_key.length > 16 ? evt.affinity_key.slice(0, 14) + '...' : evt.affinity_key}
                    </td>
                    <td style={{ padding: '8px 10px', color: '#8c96ac' }}>
                      {evt.primary_worker}
                    </td>
                    <td style={{ padding: '8px 10px', fontWeight: 600, color: '#f0f3fa' }}>
                      {evt.target_worker}
                    </td>
                    <td style={{ padding: '8px 10px' }}>
                      <span
                        style={{
                          backgroundColor: `${reasonColor}22`,
                          color: reasonColor,
                          border: `1px solid ${reasonColor}55`,
                          padding: '2px 6px',
                          borderRadius: '4px',
                          fontSize: '11px',
                          fontWeight: 600,
                        }}
                      >
                        {evt.reason}
                      </span>
                    </td>
                    <td style={{ padding: '8px 10px', fontFamily: 'monospace', color: '#10b981' }}>
                      {evt.ttft_ms.toFixed(1)}ms
                    </td>
                    <td style={{ padding: '8px 10px', fontFamily: 'monospace', color: '#8c96ac' }}>
                      {evt.duration_ms.toFixed(1)}ms
                    </td>
                    <td style={{ padding: '8px 10px' }}>
                      <span
                        style={{
                          color: evt.status_code === 200 ? '#10b981' : '#f43f5e',
                          fontWeight: 600,
                        }}
                      >
                        {evt.status_code}
                      </span>
                    </td>
                  </tr>
                );
              })
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};
