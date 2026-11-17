import React, { useState } from 'react';

interface Props {
  onSimulateBurst: (count: number, heavyContext: boolean) => Promise<void>;
}

export const SimulationControls: React.FC<Props> = ({ onSimulateBurst }) => {
  const [loading, setLoading] = useState(false);

  const handleBurst = async (count: number, heavy: boolean) => {
    setLoading(true);
    try {
      await onSimulateBurst(count, heavy);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div
      style={{
        backgroundColor: '#181c28',
        border: '1px solid #272d3f',
        borderRadius: '10px',
        padding: '16px 20px',
        marginBottom: '20px',
        display: 'flex',
        justifyContent: 'space-between',
        alignItems: 'center',
        flexWrap: 'wrap',
        gap: '14px',
        width: '100%',
        boxSizing: 'border-box',
      }}
    >
      <div style={{ flex: '1 1 220px', minWidth: 0, width: '100%' }}>
        <h4 style={{ fontSize: '14px', fontWeight: 600, color: '#f0f3fa' }}>Cluster Fault & Traffic Injection Controls</h4>
        <p style={{ fontSize: '12px', color: '#8c96ac' }}>Trigger synthetic load or simulate VRAM saturation</p>
      </div>

      <div className="sim-controls-buttons" style={{ flex: '1 1 auto' }}>
        <button
          disabled={loading}
          onClick={() => handleBurst(25, false)}
          style={{
            backgroundColor: '#8b5cf6',
            color: '#ffffff',
            border: 'none',
            borderRadius: '6px',
            padding: '10px 14px',
            fontSize: '12px',
            fontWeight: 600,
            cursor: loading ? 'not-allowed' : 'pointer',
            opacity: loading ? 0.6 : 1,
            whiteSpace: 'nowrap',
            textAlign: 'center',
          }}
        >
          ⚡ Inject Traffic Burst (25 reqs)
        </button>

        <button
          disabled={loading}
          onClick={() => handleBurst(10, true)}
          style={{
            backgroundColor: '#f59e0b',
            color: '#12151e',
            border: 'none',
            borderRadius: '6px',
            padding: '10px 14px',
            fontSize: '12px',
            fontWeight: 700,
            cursor: loading ? 'not-allowed' : 'pointer',
            opacity: loading ? 0.6 : 1,
            whiteSpace: 'nowrap',
            textAlign: 'center',
          }}
        >
          🔥 Heavy Context Spike (100k)
        </button>
      </div>
    </div>
  );
};
