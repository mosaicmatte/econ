import React, { useMemo } from 'react';
import { Terminal } from 'lucide-react';

export default function TelemetryLogs({ simData }) {
  const logs = useMemo(() => {
     if (!simData || !simData.zones) return [];
     
     const entries = [];
     const timestamp = new Date().toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second:'2-digit' });
     
     Object.values(simData.zones).slice(0, 30).forEach(z => {
        // z.load is already kW (the engine streams BaseHeatGain/1000) — print it as-is.
        // It used to be multiplied by an arbitrary 20, which inflated every line of a
        // panel titled "RAW telemetry" by 20x.
        const load = z.load ? z.load.toFixed(1) : '0.0';
        const temp = z.temp ? z.temp.toFixed(1) : '0.0';
        entries.push(`[${timestamp}] NODE ${z.label.padEnd(20)} | T: ${temp}C | SP: ${z.setpoint}C | L: ${load}kW | OCC: ${z.occupancy}`);
     });
     return entries;
  }, [simData]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', gap: '1rem' }}>
       <div style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-secondary)', letterSpacing: '1px', marginBottom: '4px', display: 'flex', alignItems: 'center', gap: '6px' }}>
          <Terminal size={12} color="var(--accent-blue)" /> RAW TELEMETRY STREAM
       </div>
       <div style={{ flex: 1, background: 'rgba(0,0,0,0.6)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '12px', overflowY: 'auto', fontFamily: 'monospace', fontSize: '10px', color: 'var(--text-secondary)', display: 'flex', flexDirection: 'column', gap: '4px' }}>
          {logs.map((log, i) => (
             <div key={i} style={{ borderBottom: '1px solid rgba(255,255,255,0.05)', paddingBottom: '4px' }}>
                <span style={{ color: 'var(--accent-green)' }}>&gt; </span>{log}
             </div>
          ))}
       </div>
    </div>
  );
}
