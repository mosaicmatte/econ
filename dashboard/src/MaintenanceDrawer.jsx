import React, { useMemo } from 'react';
import { ResponsiveContainer, LineChart, Line, XAxis, YAxis, Tooltip, CartesianGrid, Area, AreaChart } from 'recharts';
import { AlertTriangle, TrendingDown, Wrench, DollarSign } from 'lucide-react';

export default function MaintenanceDrawer({ zoneId, simData, onClose }) {
  const zone = simData.zones[zoneId];
  if (!zone) return null;

  // Generate a decay curve showing the asset health over the last 30 days
  const decayData = useMemo(() => {
    const data = [];
    let health = 100;
    for (let i = 0; i < 30; i++) {
      // Small random walk downwards
      health = health - (Math.random() * 1.5 + 0.5);
      data.push({ day: `- ${30 - i}d`, health: Math.max(0, Number(health.toFixed(1))) });
    }
    // Add projections
    for (let i = 1; i <= 14; i++) {
      health = health - (Math.random() * 2 + 1);
      data.push({ day: `+ ${i}d`, projected: Math.max(0, Number(health.toFixed(1))) });
    }
    return data;
  }, []);

  const currentHealth = decayData[29].health;
  const daysToFailure = decayData.findIndex(d => (d.projected !== undefined && d.projected < 30)) - 29;

  return (
    <div style={{ position: 'absolute', top: '1.5rem', right: '1.5rem', width: '400px', background: 'rgba(20,0,0,0.95)', border: '1px solid var(--accent-red)', borderRadius: '12px', padding: '1.5rem', zIndex: 100, boxShadow: '0 0 40px rgba(255, 0, 0, 0.15)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', borderBottom: '1px solid rgba(255,0,0,0.3)', paddingBottom: '0.75rem' }}>
        <h3 style={{ margin: 0, fontSize: '14px', color: 'var(--accent-red)', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <AlertTriangle size={16} />
          PREDICTIVE MAINTENANCE
        </h3>
        <button onClick={onClose} style={{ background: 'transparent', border: '1px solid var(--text-secondary)', borderRadius: '4px', padding: '4px 8px', color: 'var(--text-secondary)', cursor: 'pointer', fontSize: '10px', fontWeight: 'bold' }}>CLOSE</button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
        
        {/* Header Stats */}
        <div style={{ display: 'flex', gap: '8px' }}>
          <div style={{ flex: 1, background: 'rgba(255,0,0,0.1)', padding: '12px', borderRadius: '8px', border: '1px solid rgba(255,0,0,0.3)' }}>
            <div style={{ fontSize: '10px', color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: '4px' }}><Wrench size={12}/> Asset</div>
            <div style={{ fontSize: '14px', fontWeight: 'bold', color: 'var(--text-primary)', marginTop: '4px' }}>VAV-Damper</div>
            <div style={{ fontSize: '10px', color: 'var(--text-secondary)', marginTop: '2px' }}>Loc: {zone.label}</div>
          </div>
          <div style={{ flex: 1, background: 'rgba(255,0,0,0.1)', padding: '12px', borderRadius: '8px', border: '1px solid rgba(255,0,0,0.3)' }}>
            <div style={{ fontSize: '10px', color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: '4px' }}><TrendingDown size={12}/> REMAINING USEFUL LIFE (RUL)</div>
            <div style={{ fontSize: '18px', fontWeight: 'bold', color: 'var(--accent-red)', marginTop: '4px' }}>{daysToFailure} Days</div>
            <div style={{ fontSize: '10px', color: 'var(--accent-red)', marginTop: '2px' }}>Asset Health: {currentHealth}%</div>
            
            <div style={{ marginTop: '8px', background: 'rgba(255,0,0,0.2)', height: '4px', borderRadius: '2px', position: 'relative' }}>
               <div style={{ position: 'absolute', left: '0', top: '0', bottom: '0', width: '88%', background: 'var(--accent-red)', borderRadius: '2px' }} />
               <div style={{ position: 'absolute', right: '0', top: '-12px', fontSize: '9px', color: 'var(--accent-red)' }}>88% Cert.</div>
            </div>
            <div style={{ fontSize: '9px', fontWeight: 'bold', color: 'var(--text-primary)', marginTop: '8px', padding: '4px', background: 'rgba(255,0,0,0.2)', borderRadius: '2px', textAlign: 'center', letterSpacing: '0.5px' }}>
              URGENCY: HIGH IMPACT
            </div>
          </div>
        </div>

        {/* Decay Curve */}
        <div>
          <div style={{ fontSize: '10px', color: 'var(--text-secondary)', marginBottom: '8px' }}>HEALTH DECAY PROJECTION (30D HISTORICAL + 14D FORECAST)</div>
          <div style={{ width: '100%', height: '160px', background: 'rgba(0,0,0,0.5)', borderRadius: '8px', padding: '12px 12px 12px 0' }}>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={decayData}>
                <defs>
                  <linearGradient id="colorHealth" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="var(--accent-red)" stopOpacity={0.3}/>
                    <stop offset="95%" stopColor="var(--accent-red)" stopOpacity={0}/>
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" vertical={false} />
                <XAxis dataKey="day" stroke="var(--text-secondary)" fontSize={10} tickMargin={8} />
                <YAxis domain={[0, 100]} stroke="var(--text-secondary)" fontSize={10} width={30} />
                <Tooltip contentStyle={{ background: '#111', border: '1px solid var(--border-glass)' }} />
                <Area type="monotone" dataKey="health" stroke="var(--text-primary)" fillOpacity={1} fill="url(#colorHealth)" isAnimationActive={false} />
                <Area type="monotone" dataKey="projected" stroke="var(--accent-red)" strokeDasharray="3 3" fillOpacity={1} fill="url(#colorHealth)" isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Financial & Anomaly Evidence */}
        <div style={{ background: 'rgba(255,255,255,0.03)', padding: '12px', borderRadius: '8px', border: '1px solid var(--border-glass)' }}>
          <div style={{ fontSize: '10px', color: 'var(--accent-yellow)', fontWeight: 'bold', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '6px' }}>
            <DollarSign size={12}/> FINANCIAL IMPACT
          </div>
          <p style={{ margin: 0, fontSize: '12px', color: 'var(--text-primary)', lineHeight: 1.5 }}>
            <span style={{ color: 'var(--text-secondary)' }}>Evidence:</span> Actuator drawing +23% current. Damper stuck at 80% open.<br/>
            <span style={{ color: 'var(--text-secondary)' }}>Current Waste:</span> ~4.2 kWh/day ($0.50/day)<br/>
            <span style={{ color: 'var(--text-secondary)' }}>Projected Waste at failure:</span> ~18.0 kWh/day ($2.16/day)
          </p>
        </div>

        <button 
          style={{ width: '100%', background: 'var(--accent-red)', color: '#fff', border: 'none', borderRadius: '6px', padding: '12px', fontSize: '12px', fontWeight: 'bold', cursor: 'pointer', letterSpacing: '1px' }}
          onClick={onClose}
        >
          DISPATCH WORK ORDER
        </button>

      </div>
    </div>
  );
}
