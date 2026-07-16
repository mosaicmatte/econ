import React, { useEffect, useMemo, useState } from 'react';
import { ResponsiveContainer, XAxis, YAxis, Tooltip, CartesianGrid, Area, AreaChart, ReferenceLine } from 'recharts';
import { AlertTriangle, TrendingUp, Wrench, DollarSign } from 'lucide-react';
import { API_BASE } from './api';
import { energyCostPerDay, money } from './tariff';

// Fault diagnostics for the zone the engine has flagged. Every figure on this card is
// either read from live state or derived from it with the assumption stated inline.
// The previous version fabricated all of it — a Math.random() decay curve, a "days to
// failure" computed FROM that randomness, an invented "88% Cert." and "+23% current",
// and waste priced in dollars — which is exactly the demo-ware this dashboard has been
// systematically stripped of.
export default function MaintenanceDrawer({ zoneId, simData, onClose }) {
  const zone = simData.zones[zoneId];
  const [history, setHistory] = useState(null); // null = loading, [] = none available

  // Real recent trajectory: the engine persists every zone's temperature to TimescaleDB
  // once a second; /api/history replays it. No DB just means no chart, never a fake one.
  useEffect(() => {
    if (!zoneId) return;
    let dead = false;
    fetch(`${API_BASE}/api/history?zone=${encodeURIComponent(zoneId)}&minutes=10`)
      .then((r) => (r.ok ? r.json() : []))
      .then((rows) => {
        if (dead) return;
        // Rows arrive newest-first as {time, pwr: temp, co2: occupancy}; flip and rename.
        const series = (rows || [])
          .map((r) => ({ time: r.time, temp: r.pwr }))
          .filter((r) => r.temp > 0)
          .reverse();
        setHistory(series);
      })
      .catch(() => !dead && setHistory([]));
    return () => { dead = true; };
  }, [zoneId]);

  const facts = useMemo(() => {
    if (!zone) return null;
    const limit = zone.setpoint + (zone.deadband ?? 1);
    const overC = zone.temp - limit;

    // Climb rate from the real series: least-squares slope over the fetched window.
    let climbPerMin = null;
    if (history && history.length >= 10) {
      const n = history.length;
      let sx = 0, sy = 0, sxy = 0, sxx = 0;
      history.forEach((p, i) => { sx += i; sy += p.temp; sxy += i * p.temp; sxx += i * i; });
      const perSample = (n * sxy - sx * sy) / (n * sxx - sx * sx || 1);
      climbPerMin = perSample * 60; // samples are 1 s buckets
    }

    // The engine's own fault model multiplies the zone's internal gain by 5 during a
    // thermal-runaway scenario (engine.go: qInternal *= 5.0 on the fault target), so the
    // excess heat the plant must eventually remove is 4x the design gain. Cost prices
    // that excess as electrical input at the current plant COP and live EVN tariff.
    const cop = simData.plantCop || 3.0;
    const faultThermalKw = 4 * (zone.load || 0);
    const faultElecKw = faultThermalKw / cop;

    return { limit, overC, climbPerMin, faultThermalKw, faultElecKw, cop };
  }, [zone, history, simData.plantCop]);

  if (!zone || !facts) return null;

  const inAlarm = facts.overC > 0;

  return (
    <div style={{ position: 'absolute', top: '1.5rem', right: '1.5rem', width: '400px', background: 'rgba(20,0,0,0.95)', border: '1px solid var(--accent-red)', borderRadius: '12px', padding: '1.5rem', zIndex: 100, boxShadow: '0 0 40px rgba(255, 0, 0, 0.15)' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', borderBottom: '1px solid rgba(255,0,0,0.3)', paddingBottom: '0.75rem' }}>
        <h3 style={{ margin: 0, fontSize: '14px', color: 'var(--accent-red)', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <AlertTriangle size={16} />
          FAULT DIAGNOSTICS
        </h3>
        <button onClick={onClose} style={{ background: 'transparent', border: '1px solid var(--text-secondary)', borderRadius: '4px', padding: '4px 8px', color: 'var(--text-secondary)', cursor: 'pointer', fontSize: '10px', fontWeight: 'bold' }}>CLOSE</button>
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>

        {/* Header Stats — live zone state */}
        <div style={{ display: 'flex', gap: '8px' }}>
          <div style={{ flex: 1, background: 'rgba(255,0,0,0.1)', padding: '12px', borderRadius: '8px', border: '1px solid rgba(255,0,0,0.3)' }}>
            <div style={{ fontSize: '10px', color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: '4px' }}><Wrench size={12}/> Zone</div>
            <div style={{ fontSize: '14px', fontWeight: 'bold', color: 'var(--text-primary)', marginTop: '4px' }}>{zone.label || zoneId}</div>
            <div style={{ fontSize: '10px', color: 'var(--text-secondary)', marginTop: '2px' }}>Setpoint {zone.setpoint.toFixed(1)} °C · limit {facts.limit.toFixed(1)} °C</div>
          </div>
          <div style={{ flex: 1, background: 'rgba(255,0,0,0.1)', padding: '12px', borderRadius: '8px', border: '1px solid rgba(255,0,0,0.3)' }}>
            <div style={{ fontSize: '10px', color: 'var(--text-secondary)', display: 'flex', alignItems: 'center', gap: '4px' }}><TrendingUp size={12}/> LIVE TEMPERATURE</div>
            <div style={{ fontSize: '18px', fontWeight: 'bold', color: 'var(--accent-red)', marginTop: '4px' }}>{zone.temp.toFixed(1)} °C</div>
            <div style={{ fontSize: '10px', color: inAlarm ? 'var(--accent-red)' : 'var(--text-secondary)', marginTop: '2px' }}>
              {inAlarm ? `+${facts.overC.toFixed(1)} °C over the comfort limit` : 'inside the comfort band'}
            </div>
            {facts.climbPerMin !== null && (
              <div style={{ fontSize: '9px', fontWeight: 'bold', color: 'var(--text-primary)', marginTop: '8px', padding: '4px', background: 'rgba(255,0,0,0.2)', borderRadius: '2px', textAlign: 'center', letterSpacing: '0.5px' }}>
                {facts.climbPerMin >= 0.05
                  ? `CLIMBING ${facts.climbPerMin.toFixed(1)} °C/MIN`
                  : facts.climbPerMin <= -0.05
                    ? `RECOVERING ${Math.abs(facts.climbPerMin).toFixed(1)} °C/MIN`
                    : 'HOLDING STEADY'}
              </div>
            )}
          </div>
        </div>

        {/* Real temperature trajectory vs the alarm limit */}
        <div>
          <div style={{ fontSize: '10px', color: 'var(--text-secondary)', marginBottom: '8px' }}>ZONE TEMPERATURE — LAST 10 MIN (RECORDED) VS ALARM LIMIT</div>
          <div style={{ width: '100%', height: '160px', background: 'rgba(0,0,0,0.5)', borderRadius: '8px', padding: '12px 12px 12px 0' }}>
            {history === null ? (
              <div style={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '11px', color: 'var(--text-secondary)' }}>loading recorded history…</div>
            ) : history.length < 5 ? (
              <div style={{ height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '11px', color: 'var(--text-secondary)', textAlign: 'center', padding: '0 16px' }}>
                No recorded history for this zone yet (TimescaleDB offline or freshly started) — showing live value only, not an invented curve.
              </div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={history}>
                  <defs>
                    <linearGradient id="colorTemp" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--accent-red)" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="var(--accent-red)" stopOpacity={0}/>
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" vertical={false} />
                  <XAxis dataKey="time" stroke="var(--text-secondary)" fontSize={9} tickMargin={8} minTickGap={40} />
                  <YAxis domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} width={34} tickFormatter={(v) => v.toFixed(0)} />
                  <Tooltip contentStyle={{ background: '#111', border: '1px solid var(--border-glass)' }} formatter={(v) => [`${Number(v).toFixed(1)} °C`, 'temp']} />
                  <ReferenceLine y={facts.limit} stroke="var(--accent-red)" strokeDasharray="4 4" label={{ value: `limit ${facts.limit.toFixed(1)}`, fill: 'var(--accent-red)', fontSize: 9, position: 'insideTopRight' }} />
                  <Area type="monotone" dataKey="temp" stroke="var(--text-primary)" fillOpacity={1} fill="url(#colorTemp)" isAnimationActive={false} />
                </AreaChart>
              </ResponsiveContainer>
            )}
          </div>
        </div>

        {/* Financial impact — derived, assumptions stated */}
        <div style={{ background: 'rgba(255,255,255,0.03)', padding: '12px', borderRadius: '8px', border: '1px solid var(--border-glass)' }}>
          <div style={{ fontSize: '10px', color: 'var(--accent-yellow)', fontWeight: 'bold', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '6px' }}>
            <DollarSign size={12}/> FINANCIAL IMPACT (DERIVED)
          </div>
          <p style={{ margin: 0, fontSize: '12px', color: 'var(--text-primary)', lineHeight: 1.5 }}>
            <span style={{ color: 'var(--text-secondary)' }}>Fault heat load:</span> +{facts.faultThermalKw.toFixed(1)} kW thermal (fault model: 5× the zone's {(zone.load || 0).toFixed(1)} kW design gain)<br/>
            <span style={{ color: 'var(--text-secondary)' }}>Extra plant draw:</span> ≈ {facts.faultElecKw.toFixed(1)} kW electrical at COP {facts.cop.toFixed(1)}<br/>
            <span style={{ color: 'var(--text-secondary)' }}>Cost while faulted:</span> ≈ {money(energyCostPerDay(facts.faultElecKw))}/day at the live EVN tariff
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
