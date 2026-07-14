import React, { useMemo } from 'react';
import { X } from 'lucide-react';
import { PieChart, Pie, Cell, ResponsiveContainer, BarChart, Bar, XAxis, YAxis, Tooltip } from 'recharts';
import { money, energyCostPerDay } from './tariff';

// Mobile "Impact" screen — the phone-sized face of the twin. Served automatically on
// small viewports instead of the WebGL-heavy desktop stack, and fed entirely by the
// same live stream: engine savings, LSTM forecast, per-level occupancy, edge nodes.
export default function MobileImpactScreen({ simData = {}, aiForecast, hardwareNodes = {}, onClose }) {
  const loadMw = simData.buildingLoadMw || 0;
  const savedMw = simData.energySavedMw || 0;
  const savingsPct = savedMw + loadMw > 0 ? (100 * savedMw) / (savedMw + loadMw) : 0;
  const health = simData.systemHealth ?? 100;
  const occupants = simData.totalOccupants ?? 0;

  const savingsData = [
    { name: 'Setbacks', value: Math.max(0.001, savedMw), color: '#3DDC84' },
    { name: 'Grid draw', value: Math.max(0.001, loadMw), color: '#B8B8B8' },
  ];

  // Live occupancy per level (top 6), straight from the streamed zone state.
  const levelOccupancy = useMemo(() => {
    const byLevel = {};
    Object.values(simData.zones || {}).forEach((z) => {
      byLevel[z.level] = (byLevel[z.level] || 0) + (z.occupancy || 0);
    });
    return Object.entries(byLevel)
      .map(([lvl, pax]) => ({ level: `L${lvl}`, pax }))
      .sort((a, b) => b.pax - a.pax)
      .slice(0, 6);
  }, [simData.zones]);

  const peak = aiForecast?.predicted_peak_load;
  const peakPct = peak > 0 ? Math.max(0, Math.min(100, (loadMw / peak) * 100)) : 0;

  const hwList = Object.values(hardwareNodes || {});

  const Stat = ({ label, value, unit, color = '#ffffff' }) => (
    <div style={{ flex: 1, background: 'rgba(255,255,255,0.06)', borderRadius: '12px', padding: '10px 8px', textAlign: 'center', border: '1px solid rgba(255,255,255,0.08)' }}>
      <div style={{ fontSize: '18px', fontWeight: 'bold', color, fontFamily: 'ui-monospace, monospace' }}>{value}<span style={{ fontSize: '10px', color: 'rgba(255,255,255,0.5)' }}> {unit}</span></div>
      <div style={{ fontSize: '10px', color: 'rgba(255,255,255,0.6)', marginTop: '2px' }}>{label}</div>
    </div>
  );

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', color: '#ffffff', background: '#000000', fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", sans-serif', padding: '20px', minHeight: '100dvh', overflowY: 'auto' }}>
      <div style={{ display: 'flex', justifyContent: 'flex-start', alignItems: 'center', marginBottom: '20px' }}>
        <h2 style={{ margin: 0, fontSize: '24px', fontWeight: '600' }}>ECON · Live</h2>
      </div>

      {/* Live headline stats */}
      <div style={{ display: 'flex', gap: '10px', marginBottom: '20px' }}>
        <Stat label="Load" value={loadMw.toFixed(2)} unit="MW" color="#F5C242" />
        <Stat label="Occupants" value={occupants} unit="Pax" color="#4FC3F7" />
        <Stat label="Health" value={health.toFixed(0)} unit="%" color={health < 80 ? '#FF3B30' : '#3DDC84'} />
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>

        {/* Autonomous Savings Card */}
        <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
          <h3 style={{ margin: '0 0 8px 0', fontSize: '18px', fontWeight: '600' }}>Autonomous Savings</h3>
          <div style={{ fontSize: '14px', color: 'rgba(255,255,255,0.6)', marginBottom: '20px' }}>Share of plant load avoided right now by occupancy-driven setbacks.</div>

          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', position: 'relative', height: '200px' }}>
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie
                  data={savingsData}
                  innerRadius={65}
                  outerRadius={80}
                  paddingAngle={2}
                  dataKey="value"
                  stroke="none"
                  isAnimationActive={false}
                >
                  {savingsData.map((entry, index) => (
                    <Cell key={`cell-${index}`} fill={entry.color} />
                  ))}
                </Pie>
              </PieChart>
            </ResponsiveContainer>
            <div style={{ position: 'absolute', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
              <span style={{ fontSize: '32px', fontWeight: 'bold' }}>{savingsPct.toFixed(1)}%</span>
              <span style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)' }}>{(savedMw * 1000).toFixed(0)} kW saved</span>
              <span style={{ fontSize: '11px', color: '#3DDC84', marginTop: '4px', maxWidth: '118px', textAlign: 'center', lineHeight: 1.2 }}>≈ {money(energyCostPerDay((simData.energySavedMw||0)*1000))}/day</span>
            </div>
          </div>
        </div>

        {/* Peak Shaving Card — live load against the LSTM's predicted peak */}
        <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
          <h3 style={{ margin: '0 0 8px 0', fontSize: '18px', fontWeight: '600' }}>Peak Shaving</h3>
          <div style={{ fontSize: '14px', color: 'rgba(255,255,255,0.6)', marginBottom: '20px' }}>Current load against the LSTM-predicted peak.</div>
          {peak ? (
            <>
              <div style={{ fontSize: '32px', fontWeight: 'bold', marginBottom: '16px' }}>{peakPct.toFixed(0)}%</div>
              <div style={{ height: '24px', background: 'rgba(255,255,255,0.06)', borderRadius: '12px', overflow: 'hidden', display: 'flex' }}>
                <div style={{ width: `${peakPct}%`, background: peakPct > 90 ? '#FF3B30' : '#3DDC84', transition: 'width 0.5s' }} />
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: '8px', fontSize: '12px', color: 'rgba(255,255,255,0.6)' }}>
                <span>Now: {loadMw.toFixed(2)} MW</span>
                <span>Predicted peak: {peak.toFixed(2)} MW</span>
              </div>
            </>
          ) : (
            <div style={{ fontSize: '13px', color: 'rgba(255,255,255,0.5)' }}>Forecaster offline — start the stack's forecasting service to see the predicted peak.</div>
          )}
        </div>

        {/* Occupancy by Level Card */}
        <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
          <h3 style={{ margin: '0 0 8px 0', fontSize: '18px', fontWeight: '600' }}>Occupancy by Level</h3>
          <div style={{ fontSize: '14px', color: 'rgba(255,255,255,0.6)', marginBottom: '20px' }}>Busiest floors right now, from the live zone stream.</div>

          <div style={{ height: '170px' }}>
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={levelOccupancy} layout="vertical" margin={{ top: 0, right: 0, left: -20, bottom: 0 }}>
                <XAxis type="number" hide />
                <YAxis dataKey="level" type="category" stroke="rgba(255,255,255,0.6)" tick={{ fill: 'rgba(255,255,255,0.6)', fontSize: 12 }} axisLine={false} tickLine={false} />
                <Tooltip cursor={{ fill: 'rgba(255,255,255,0.05)' }} contentStyle={{ background: 'rgba(30,30,30,0.95)', border: 'none', borderRadius: '8px', color: '#ffffff' }} formatter={(v) => [`${v} pax`, 'occupancy']} />
                <Bar dataKey="pax" radius={[0, 4, 4, 0]} barSize={20} fill="#3DDC84" isAnimationActive={false} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Edge hardware (only when physical nodes have reported) */}
        {hwList.length > 0 && (
          <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
            <h3 style={{ margin: '0 0 8px 0', fontSize: '18px', fontWeight: '600' }}>Edge Hardware</h3>
            <div style={{ fontSize: '14px', color: 'rgba(255,255,255,0.6)', marginBottom: '12px' }}>Physical boards bound into the twin.</div>
            {hwList.map((n) => (
              <div key={n.zoneId} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '8px 0', borderTop: '1px solid rgba(255,255,255,0.06)', fontSize: '13px', fontFamily: 'ui-monospace, monospace' }}>
                <span style={{ width: 8, height: 8, borderRadius: '50%', flexShrink: 0, background: n.online ? '#3DDC84' : 'rgba(255,255,255,0.3)' }} />
                <span style={{ fontWeight: 'bold' }}>{(n.source || 'edge').toUpperCase()}</span>
                <span style={{ color: 'rgba(255,255,255,0.5)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{(n.zoneId || '').replace('zone-', '')}</span>
                {n.tempPinned && <span style={{ color: '#4FC3F7' }}>{(n.hwTemp || 0).toFixed(1)}°C</span>}
                <span style={{ color: 'rgba(255,255,255,0.6)' }}>{n.occupancy ?? 0}P</span>
              </div>
            ))}
          </div>
        )}

      </div>
      <div style={{ height: '40px' }} />
    </div>
  );
}
