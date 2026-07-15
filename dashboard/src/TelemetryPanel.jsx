import React, { useMemo } from 'react';
import { Activity, AlertTriangle, Zap, CheckCircle } from 'lucide-react';
import { ScatterChart, Scatter, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer, ReferenceArea, ReferenceLine } from 'recharts';
import { money, energyCostPerDay, peakShiftSavingPerDay, peakShiftSavingPerMonth, touPeriod, touPeriodLabel } from './tariff';

export default function TelemetryPanel({ simData, loadHistory, activeScenario, faultTarget, onOpenMaintenance, autoPilot, setAutoPilot }) {

  // Manual "apply" actions engage the autonomous controller, which performs the suggested
  // setback / load-shed / flow change — so the button does the real thing instead of nothing.
  const applySuggestion = () => setAutoPilot && setAutoPilot(true);

  // --- Cost model (see tariff.js) ---
  const cop = simData.plantCop || 3.0;
  // The plant's electrical draw is the thermal cooling it delivers over its live COP — read
  // off the stream rather than subtracting a hard-coded non-HVAC baseline.
  const coolingElectricalKw = Math.min(
    (simData.buildingLoadMw || 0),
    (simData.plantCop || 0) > 0 ? (simData.coolingOutputMw || 0) / simData.plantCop : 0,
  ) * 1000;

  // Zone performance: every zone plotted as (how far it has drifted from its setpoint) vs
  // (the cooling it is drawing). Every value is streamed — no synthetic baseline. The
  // quadrants are the whole point, and each one is an action:
  //   right + high kW -> pouring cooling in and still hot  = fault / undersized
  //   right + low kW  -> hot but barely cooling            = starved: stuck damper, shut valve
  //   left            -> cooled well past setpoint         = wasted money, raise the setpoint
  //   centre          -> inside the deadband               = healthy
  const perf = useMemo(() => {
    const g = { healthy: [], overcooled: [], starved: [], struggling: [], alarm: [] };
    let wasteKw = 0;
    let maxDb = 2;
    Object.values(simData.zones || {}).forEach((z) => {
      const sp = z.setpoint ?? 24;
      const db = z.deadband ?? 2;
      maxDb = Math.max(maxDb, db);
      const dev = (z.temp ?? sp) - sp;
      const kw = z.load || 0;
      const pt = {
        x: Number(dev.toFixed(2)), y: Number(kw.toFixed(1)), name: z.label || z.id,
        sensor: (z.co2 || 0) > 0 || (z.humidity || 0) > 0,
      };
      if (z.alert === true || z.alert === 'REMEDIATING') g.alarm.push(pt);
      else if (dev > db) (kw > 1 ? g.struggling : g.starved).push(pt);
      else if (dev < -db) {
        g.overcooled.push(pt);
        // Every °C below the deadband's lower edge is roughly 5% of extra plant electrical.
        wasteKw += (Math.abs(dev) - db) * 0.05 * (kw / cop);
      } else g.healthy.push(pt);
    });
    return { ...g, wasteKw, maxDb };
  }, [simData, cop]);

  const sensorCount = useMemo(
    () => Object.values(simData.zones || {}).filter(z => (z.co2 || 0) > 0 || (z.humidity || 0) > 0).length,
    [simData],
  );

  // Insights Data logic
  const isFault = activeScenario === 'fault';
  const faultZoneId = isFault ? faultTarget : null;
  const faultZone = faultZoneId ? simData.zones[faultZoneId] : null;
  
  const unoccupiedWasting = Object.values(simData.zones).filter(z => z.occupancy === 0).slice(0, 1);
  const outOfBand = Object.values(simData.zones).filter(z => z.temp > z.setpoint + z.deadband && activeScenario !== 'fault');

  // Wasted electrical draw when a scheduled-occupied zone sits empty: mirrors the engine's
  // own setback-savings formula (≈2 kW lighting + 25% of internal-gain cooling, at plant COP).
  const zoneWasteKw = (z) => 2 + (0.25 * (z.load || 0)) / cop;
  const rateLabel = touPeriodLabel(touPeriod()); // current EVN TOU band, e.g. "normal hours"
  // Cooling load a 1°C deadband widen / pre-cool shifts OFF the daily peak window: ~5% of
  // cooling electrical (a standard ~3–5%-per-°C HVAC rule of thumb). The value is the peak-vs-
  // normal rate gap on that shifted energy, not a (non-existent in Vietnam) demand charge.
  const shedKw = 0.05 * coolingElectricalKw;

  const getRCA = (zone) => {
    if (!zone) return { cause: 'Unknown error', blastRadius: 1, confidence: 0 };
    if (zone.type === 'server-room') {
      return { cause: 'CRAC unit compressor failure', blastRadius: 4, confidence: 96 };
    }
    if (zone.type === 'open-office') {
      return { cause: 'VAV box damper stuck closed', blastRadius: 2, confidence: 88 };
    }
    if (zone.type === 'perimeter') {
      return { cause: 'perimeter radiant heater stuck ON', blastRadius: 1, confidence: 85 };
    }
    return { cause: 'upstream chilled water valve failure', blastRadius: 3, confidence: 92 };
  };
  
  const rcaData = getRCA(faultZone);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
      
      {/* PRIMARY ANALYTICS: Characteristic Curve */}
      <div style={{ display: 'flex', flexDirection: 'column', minHeight: '350px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
          <div style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-secondary)', letterSpacing: '1px', display: 'flex', alignItems: 'center', gap: '6px' }}>
            <Activity size={12} color="var(--accent-blue)" />
            ZONE PERFORMANCE — COOLING kW vs °C FROM SETPOINT
          </div>
          <span style={{ fontSize: '9px', color: 'var(--text-muted)' }}>
            {Object.keys(simData.zones || {}).length} zones{sensorCount > 0 ? ` · ${sensorCount} sensor-backed` : ''}
          </span>
        </div>

        <div style={{ height: '260px', background: 'rgba(0,0,0,0.4)', borderRadius: '8px', padding: '12px', border: '1px solid var(--border-glass)' }}>
          <ResponsiveContainer width="100%" height="100%">
            <ScatterChart margin={{ top: 10, right: 10, bottom: 0, left: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" />
              {/* the comfort deadband: anything inside it is doing its job */}
              <ReferenceArea x1={-perf.maxDb} x2={perf.maxDb} fill="rgba(46,204,113,0.07)" stroke="rgba(46,204,113,0.25)" strokeDasharray="2 2" />
              <ReferenceLine x={0} stroke="rgba(255,255,255,0.25)" strokeDasharray="3 3" />
              <XAxis type="number" dataKey="x" name="Δ from setpoint" unit="°C" domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} tickMargin={8} />
              <YAxis type="number" dataKey="y" name="Cooling" unit=" kW" domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} width={45} />
              <Tooltip
                cursor={{ strokeDasharray: '3 3' }}
                contentStyle={{ background: '#111', border: '1px solid var(--border-glass)', fontSize: '11px' }}
                formatter={(v, n) => [n === 'Cooling' ? `${v} kW` : `${v > 0 ? '+' : ''}${v} °C`, n === 'Cooling' ? 'Cooling' : 'Δ setpoint']}
                labelFormatter={() => ''}
                itemSorter={() => 0}
              />
              <Scatter name="In band" data={perf.healthy} fill="rgba(46,204,113,0.55)" isAnimationActive={false} />
              <Scatter name="Overcooled" data={perf.overcooled} fill="var(--accent-blue)" isAnimationActive={false} />
              <Scatter name="Starved" data={perf.starved} fill="#b06bd8" isAnimationActive={false} />
              <Scatter name="Struggling" data={perf.struggling} fill="var(--accent-yellow)" isAnimationActive={false} />
              <Scatter name="Alarm" data={perf.alarm} fill="var(--accent-red)" isAnimationActive={false} />
            </ScatterChart>
          </ResponsiveContainer>
        </div>

        {/* Legend doubles as the read-out: each colour is a count and a decision. */}
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: '10px', marginTop: '8px' }}>
          {[
            ['In band', 'rgba(46,204,113,0.55)', perf.healthy.length],
            ['Overcooled', 'var(--accent-blue)', perf.overcooled.length],
            ['Starved', '#b06bd8', perf.starved.length],
            ['Struggling', 'var(--accent-yellow)', perf.struggling.length],
            ['Alarm', 'var(--accent-red)', perf.alarm.length],
          ].map(([label, colour, n]) => (
            <div key={label} style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
              <div style={{ width: '8px', height: '8px', borderRadius: '50%', background: colour }} />
              <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>{label} <b style={{ color: 'var(--text-primary)' }}>{n}</b></span>
            </div>
          ))}
        </div>
        {perf.overcooled.length > 0 && perf.wasteKw > 0.5 && (
          <div style={{ marginTop: '8px', fontSize: '10px', color: 'var(--accent-blue)', lineHeight: 1.4 }}>
            {perf.overcooled.length} zones are cooled past their deadband — ≈ {perf.wasteKw.toFixed(0)} kW of avoidable
            plant draw, about {money(energyCostPerDay(perf.wasteKw))}/day at {rateLabel}. Raising those setpoints is free money.
          </div>
        )}
        {perf.starved.length > 0 && (
          <div style={{ marginTop: '6px', fontSize: '10px', color: '#b06bd8', lineHeight: 1.4 }}>
            {perf.starved.length} zones are above the deadband while barely drawing cooling — the classic stuck-damper /
            closed-valve signature. Worth a look before they alarm.
          </div>
        )}
      </div>

      <hr style={{ border: 'none', borderTop: '1px solid var(--border-glass)' }} />

      {/* AI OPERATIONAL INSIGHTS (The "Why" and "What to do") */}
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
        <div style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-secondary)', letterSpacing: '1px', marginBottom: '8px', display: 'flex', alignItems: 'center', gap: '6px' }}>
          <Zap size={12} color="var(--accent-blue)" /> AI OPERATIONAL INSIGHTS
        </div>
        
        <div style={{ display: 'flex', flexDirection: 'column', gap: '12px', flex: 1, overflowY: 'auto', paddingRight: '4px' }}>
          
          {/* Critical Fault Insight (Root Cause Analysis) */}
          {isFault && faultZone && (
            <div style={{ background: 'rgba(255,0,0,0.05)', border: '1px solid rgba(255,0,0,0.3)', borderRadius: '6px', padding: '12px' }}>
              <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
                <AlertTriangle size={16} color="var(--accent-red)" style={{ marginTop: '2px', flexShrink: 0 }} />
                <div>
                  <div style={{ fontSize: '12px', fontWeight: 'bold', color: 'var(--accent-red)', letterSpacing: '0.5px' }}>ROOT CAUSE ANALYSIS</div>
                  <div style={{ fontSize: '11px', color: 'var(--text-primary)', marginTop: '4px', lineHeight: 1.4 }}>
                    Alerts in {faultZone.label} are likely caused by a single failure in the {rcaData.cause}. Blast radius spans {rcaData.blastRadius} dependent zones.
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px', marginTop: '6px' }}>
                    <div style={{ height: '4px', width: `${rcaData.confidence}%`, background: 'var(--accent-green)', borderRadius: '2px' }} />
                    <span style={{ fontSize: '10px', color: 'var(--accent-green)' }}>{rcaData.confidence}% Confidence</span>
                  </div>
                </div>
              </div>
              <button 
                onClick={() => onOpenMaintenance(faultZoneId)}
                style={{ width: '100%', background: 'rgba(255,0,0,0.15)', border: '1px solid var(--accent-red)', color: 'var(--accent-red)', padding: '8px', fontSize: '10px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold', letterSpacing: '1px', transition: '0.2s', marginTop: '4px' }}
                onMouseOver={e => e.target.style.background = 'var(--accent-red)'}
                onMouseOut={e => e.target.style.background = 'rgba(255,0,0,0.15)'}
              >
                VIEW PREDICTIVE MAINTENANCE
              </button>
            </div>
          )}

          {/* Optimization Insight (Auto-Pilot Aware) */}
          {unoccupiedWasting.map((z, i) => (
            <div key={i} style={{ background: autoPilot ? 'rgba(0,255,0,0.05)' : 'rgba(255,255,255,0.02)', border: autoPilot ? '1px solid rgba(0,255,0,0.2)' : '1px solid var(--border-glass)', borderRadius: '6px', padding: '12px' }}>
               <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
                <Zap size={16} color="var(--accent-green)" style={{ marginTop: '2px', flexShrink: 0 }} />
                <div>
                  <div style={{ fontSize: '12px', fontWeight: 'bold', color: autoPilot ? 'var(--accent-green)' : 'var(--text-primary)', letterSpacing: '0.5px' }}>
                    {autoPilot ? 'AUTONOMOUS ACTION' : 'SETPOINT OPTIMIZATION'}
                  </div>
                  <div style={{ fontSize: '11px', color: 'var(--text-secondary)', marginTop: '4px', lineHeight: 1.4 }}>
                    {z.label} is unoccupied but still drawing an estimated {zoneWasteKw(z).toFixed(1)} kW (lighting + cooling of internal gains).
                    <div style={{ marginTop: '4px', color: 'var(--accent-green)' }}>
                      {autoPilot
                        ? `Setback engaged — saving ≈ ${money(energyCostPerDay(zoneWasteKw(z)))}/day at the current ${rateLabel} rate.`
                        : `Financial impact ≈ ${money(energyCostPerDay(zoneWasteKw(z)))}/day at the current ${rateLabel} rate.`}
                    </div>
                  </div>
                </div>
              </div>
              {!autoPilot && (
                <button
                  onClick={applySuggestion}
                  style={{ width: '100%', background: 'transparent', border: '1px solid var(--accent-green)', color: 'var(--accent-green)', padding: '6px', fontSize: '10px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold', letterSpacing: '1px', marginTop: '4px' }}
                >
                  PREVIEW & APPLY SETBACK
                </button>
              )}
            </div>
          ))}

          {/* 1. Peak Load Shaving Insight */}
          {simData.buildingLoadMw > 3.0 && (
            <div style={{ background: autoPilot ? 'rgba(0,255,0,0.05)' : 'rgba(255,165,0,0.05)', border: autoPilot ? '1px solid rgba(0,255,0,0.2)' : '1px solid rgba(255,165,0,0.3)', borderRadius: '6px', padding: '12px' }}>
              <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
                <Zap size={16} color={autoPilot ? "var(--accent-green)" : "orange"} style={{ marginTop: '2px', flexShrink: 0 }} />
                <div>
                  <div style={{ fontSize: '12px', fontWeight: 'bold', color: autoPilot ? 'var(--accent-green)' : 'orange', letterSpacing: '0.5px' }}>
                    {autoPilot ? 'AUTONOMOUS LOAD SHEDDING' : 'PEAK SHAVING OPPORTUNITY'}
                  </div>
                  <div style={{ fontSize: '11px', color: 'var(--text-primary)', marginTop: '4px', lineHeight: 1.4 }}>
                    Total building load ({simData.buildingLoadMw.toFixed(2)} MW) — the 17:30–22:30 peak hours are the costly window.
                    <div style={{ marginTop: '4px', color: autoPilot ? 'var(--accent-green)' : 'var(--text-secondary)' }}>
                      {autoPilot
                        ? `Pre-cooling shifts ≈ ${shedKw.toFixed(0)} kW of cooling out of peak hours into normal-rate hours — saving ≈ ${money(peakShiftSavingPerDay(shedKw))}/day (${money(peakShiftSavingPerMonth(shedKw))}/month) on peak-hour energy.`
                        : `Widen deadbands 1°C / pre-cool before 17:30 → shift ≈ ${shedKw.toFixed(0)} kW off peak hours, saving ≈ ${money(peakShiftSavingPerDay(shedKw))}/day (${money(peakShiftSavingPerMonth(shedKw))}/month).`}
                    </div>
                  </div>
                </div>
              </div>
              {!autoPilot && (
                <button
                  onClick={applySuggestion}
                  style={{ width: '100%', background: 'transparent', border: '1px solid orange', color: 'orange', padding: '6px', fontSize: '10px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold', letterSpacing: '1px', marginTop: '4px' }}
                >
                  PREVIEW & APPLY LOAD SHEDDING
                </button>
              )}
            </div>
          )}

          {/* 2. Thermal Comfort Anomaly Insight */}
          {outOfBand.map((z, i) => (
            <div key={`comfort-${i}`} style={{ background: autoPilot ? 'rgba(0,255,0,0.05)' : 'rgba(255,255,255,0.02)', border: autoPilot ? '1px solid rgba(0,255,0,0.2)' : '1px solid var(--border-glass)', borderRadius: '6px', padding: '12px' }}>
              <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
                <Activity size={16} color={autoPilot ? "var(--accent-green)" : "var(--accent-yellow)"} style={{ marginTop: '2px', flexShrink: 0 }} />
                <div>
                  <div style={{ fontSize: '12px', fontWeight: 'bold', color: autoPilot ? 'var(--accent-green)' : 'var(--accent-yellow)', letterSpacing: '0.5px' }}>
                    {autoPilot ? 'AUTONOMOUS COMFORT CORRECTION' : 'THERMAL COMFORT ANOMALY'}
                  </div>
                  <div style={{ fontSize: '11px', color: 'var(--text-secondary)', marginTop: '4px', lineHeight: 1.4 }}>
                    {z.label} is currently {z.temp.toFixed(1)}°C (Limit: {(z.setpoint + z.deadband).toFixed(1)}°C). Occupant discomfort predicted.
                    <div style={{ marginTop: '4px', color: 'var(--accent-green)' }}>
                      {autoPilot ? 'Auto-Adjusted VAV flow +15% to restore comfort parameters.' : 'Suggested: Increase VAV flow +15%.'}
                    </div>
                  </div>
                </div>
              </div>
              {!autoPilot && (
                <button
                  onClick={applySuggestion}
                  style={{ width: '100%', background: 'transparent', border: '1px solid var(--accent-yellow)', color: 'var(--accent-yellow)', padding: '6px', fontSize: '10px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold', letterSpacing: '1px', marginTop: '4px' }}
                >
                  PREVIEW & INCREASE FLOW
                </button>
              )}
            </div>
          ))}

          {/* Nominal Status Fallback */}
          {!isFault && unoccupiedWasting.length === 0 && outOfBand.length === 0 && simData.buildingLoadMw <= 3.0 && (
            <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '6px', padding: '12px', display: 'flex', gap: '8px', alignItems: 'center' }}>
               <CheckCircle size={16} color="var(--text-secondary)" />
               <span style={{ fontSize: '11px', color: 'var(--text-secondary)' }}>All thermodynamic characteristics are within optimal baseline variance.</span>
            </div>
          )}

        </div>
      </div>
    </div>
  );
}
