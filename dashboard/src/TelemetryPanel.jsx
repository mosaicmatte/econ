import React, { useMemo, useState, useEffect } from 'react';
import { Activity, AlertTriangle, Zap, CheckCircle, Plug, X } from 'lucide-react';
import { ScatterChart, Scatter, XAxis, YAxis, ZAxis, CartesianGrid, Tooltip, ResponsiveContainer, ReferenceArea, ReferenceLine, LineChart, Line } from 'recharts';
import { money, energyCostPerDay, peakShiftSavingPerDay, peakShiftSavingPerMonth, touPeriod, touPeriodLabel, minutesToPeak, rateStr } from './tariff';
import { usePlugs } from './usePlugs';
import { API_BASE } from './api';

// Supply-air temperature the engine cools toward (simulation/engine.go uses 12.0 °C in
// the cooling law). Per-zone cooling POWER is the physical Q = ρ·V̇·cp·ΔT from the zone's
// live VAV airflow — a real delivered-cooling figure, not the internal heat-gain constant
// the scatter used to plot under a "cooling" label.
const SUPPLY_C = 12.0;
const AIR_RHO = 1.2;    // kg/m³
const AIR_CP = 1.005;   // kJ/kg·K  → Q in kW when V̇ is m³/s

export default function TelemetryPanel({ simData, loadHistory, activeScenario, faultTarget, onOpenMaintenance, autoPilot, setAutoPilot, setSelectedZone, isMobile = false }) {

  // Manual "apply" actions engage the autonomous controller, which performs the suggested
  // setback / load-shed / flow change — so the button does the real thing instead of nothing.
  const applySuggestion = () => setAutoPilot && setAutoPilot(true);

  const { status: plugStatus } = usePlugs();
  const [drill, setDrill] = useState(null); // {id, name, sensor} of the zone being inspected

  // Chart-level click resolves to the NEAREST point via the active tooltip payload — so a
  // tap anywhere near a dot drills in. A per-point onClick would demand a pixel-perfect hit
  // on a 4 px circle, which is fine with a mouse but unusable on a phone (the whole reason
  // the mobile scatter felt dead). Both are wired; whichever fires first opens the zone.
  const handleChartClick = (state) => {
    const p = state?.activePayload?.[0]?.payload;
    if (p && p.id) setDrill(p);
  };

  // --- Cost model (see tariff.js) ---
  const cop = simData.plantCop || 3.0;
  // The plant's electrical draw is the thermal cooling it delivers over its live COP — read
  // off the stream rather than subtracting a hard-coded non-HVAC baseline.
  const coolingElectricalKw = Math.min(
    (simData.buildingLoadMw || 0),
    (simData.plantCop || 0) > 0 ? (simData.coolingOutputMw || 0) / simData.plantCop : 0,
  ) * 1000;

  // Real per-zone cooling POWER (kW thermal), from each zone's live VAV airflow. The VAV
  // stream carries targetZone, so this maps flow → zone directly. Q = ρ·V̇·cp·(Troom−Tsupply):
  // the actual cooling being delivered, which is what the scatter's Y axis claims to show.
  const coolingByZone = useMemo(() => {
    const m = {};
    Object.values(simData.vavs || {}).forEach((v) => {
      const z = simData.zones?.[v.targetZone];
      if (!z) return;
      const dT = Math.max(0, (z.temp ?? 24) - SUPPLY_C);
      m[v.targetZone] = AIR_RHO * (v.flow || 0) * AIR_CP * dT;
    });
    return m;
  }, [simData]);

  // Zone performance: every zone plotted as (how far it has drifted from its setpoint) vs
  // (the cooling power it is actually drawing). Every value is streamed — no synthetic
  // baseline. The quadrants are the whole point, and each one is an action:
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
      const coolKw = coolingByZone[z.id] || 0;
      const pt = {
        x: Number(dev.toFixed(2)), y: Number(coolKw.toFixed(1)), id: z.id, name: z.label || z.id,
        sensor: (z.co2 || 0) > 0 || (z.humidity || 0) > 0,
      };
      if (z.alert === true || z.alert === 'REMEDIATING') g.alarm.push(pt);
      else if (dev > db) (coolKw > 1 ? g.struggling : g.starved).push(pt);
      else if (dev < -db) {
        g.overcooled.push(pt);
        // Electrical waste of overcooling: ~5% of the zone's cooling electrical per °C
        // below the deadband's lower edge.
        wasteKw += (Math.abs(dev) - db) * 0.05 * (coolKw / cop);
      } else g.healthy.push(pt);
    });
    return { ...g, wasteKw, maxDb };
  }, [simData, cop, coolingByZone]);

  const sensorCount = useMemo(
    () => Object.values(simData.zones || {}).filter(z => (z.co2 || 0) > 0 || (z.humidity || 0) > 0).length,
    [simData],
  );

  // Insights Data logic
  const isFault = activeScenario === 'fault';
  const faultZoneId = isFault ? faultTarget : null;
  const faultZone = faultZoneId ? simData.zones[faultZoneId] : null;

  const unoccupiedWasting = Object.values(simData.zones || {})
    .filter(z => z.occupancy === 0 && z.type !== 'server-room' && z.type !== 'mechanical' && (coolingByZone[z.id] || 0) > 0.5)
    .slice(0, 1);
  const outOfBand = Object.values(simData.zones || {}).filter(z => z.temp > z.setpoint + z.deadband && activeScenario !== 'fault');

  // Wasted electrical draw when a scheduled-occupied zone sits empty: the REAL cooling
  // electrical it is drawing (thermal cooling / plant COP), no fabricated lighting constant.
  const zoneWasteKw = (z) => (coolingByZone[z.id] || 0) / cop;
  const rateLabel = touPeriodLabel(touPeriod()); // current EVN TOU band, e.g. "normal hours"
  // Cooling load a 1°C deadband widen / pre-cool shifts OFF the daily peak window: ~5% of
  // cooling electrical (a standard ~3–5%-per-°C HVAC rule of thumb). The value is the peak-vs-
  // normal rate gap on that shifted energy, not a (non-existent in Vietnam) demand charge.
  const shedKw = 0.05 * coolingElectricalKw;
  // Peak-shaving advice is driven by the EVN TOU clock, not a hardcoded load threshold:
  // relevant while cao điểm is running or within 90 minutes of it.
  const tou = touPeriod();
  const toPeak = minutesToPeak();
  const peakRelevant = tou === 'peak' || (toPeak !== null && toPeak <= 90);

  // Likely cause is a RULE, not a model: the most common failure mode for the flagged
  // zone's equipment class. It used to ship with an invented "96% Confidence" (a literal,
  // per zone type) and a fixed "blast radius" no computation produced — the bar and the
  // percentage taught operators to trust precision that did not exist. The evidence shown
  // now is the zone's real overtemp and the real count of zones currently in alarm.
  const getRCA = (zone) => {
    if (!zone) return { cause: 'unknown equipment' };
    if (zone.type === 'server-room') return { cause: 'CRAC unit compressor failure' };
    if (zone.type === 'open-office') return { cause: 'VAV box damper stuck closed' };
    if (zone.type === 'perimeter') return { cause: 'perimeter radiant heater stuck ON' };
    return { cause: 'upstream chilled water valve failure' };
  };

  const rcaData = getRCA(faultZone);
  const alarmingCount = Object.values(simData.zones || {}).filter((z) => z.alert).length;
  const faultOverC = faultZone ? faultZone.temp - (faultZone.setpoint + (faultZone.deadband ?? 1)) : 0;

  const chartH = isMobile ? 300 : 260;
  const fontBig = isMobile ? '13px' : '11px';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>

      {/* Real-inputs strip: what this analysis is actually made of right now. */}
      <div style={{ display: 'flex', flexWrap: 'wrap', gap: '10px', fontSize: isMobile ? '11px' : '9px', color: 'var(--text-muted)' }}>
        <span>{Object.keys(simData.zones || {}).length} zones</span>
        <span style={{ color: sensorCount > 0 ? 'var(--accent-blue)' : 'var(--text-muted)' }}>
          {sensorCount} sensor-backed
        </span>
        {plugStatus && (
          <span style={{ display: 'flex', alignItems: 'center', gap: '4px', color: plugStatus.armed ? 'var(--accent-green)' : 'var(--text-muted)' }}>
            <Plug size={isMobile ? 12 : 10} /> plug sweep {plugStatus.config?.enabled ? (plugStatus.armed ? `armed · ${plugStatus.shedZones} swept` : 'disarmed (work hrs)') : 'off'}
          </span>
        )}
        <span>{rateStr(tou)}/kWh · {rateLabel}</span>
      </div>

      {/* PRIMARY ANALYTICS: Characteristic Curve */}
      <div style={{ display: 'flex', flexDirection: 'column', minHeight: isMobile ? '400px' : '350px' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
          <div style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-secondary)', letterSpacing: '1px', display: 'flex', alignItems: 'center', gap: '6px' }}>
            <Activity size={12} color="var(--accent-blue)" />
            ZONE PERFORMANCE — COOLING kW vs °C FROM SETPOINT
          </div>
          <span style={{ fontSize: '9px', color: 'var(--text-muted)' }}>tap a point →</span>
        </div>

        <div style={{ height: chartH, background: 'rgba(0,0,0,0.4)', borderRadius: '8px', padding: '12px', border: '1px solid var(--border-glass)' }}>
          <ResponsiveContainer width="100%" height="100%">
            <ScatterChart margin={{ top: 10, right: 10, bottom: 0, left: 0 }} onClick={handleChartClick} style={{ cursor: 'pointer' }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" />
              {/* the comfort deadband: anything inside it is doing its job */}
              <ReferenceArea x1={-perf.maxDb} x2={perf.maxDb} fill="rgba(46,204,113,0.07)" stroke="rgba(46,204,113,0.25)" strokeDasharray="2 2" />
              <ReferenceLine x={0} stroke="rgba(255,255,255,0.25)" strokeDasharray="3 3" />
              <XAxis type="number" dataKey="x" name="Δ from setpoint" unit="°C" domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} tickMargin={8} />
              <YAxis type="number" dataKey="y" name="Cooling" unit=" kW" domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} width={45} />
              {/* Bigger symbols on a phone so a fingertip can actually land on one — the
                  dot IS the tap target for the drill-down. */}
              <ZAxis range={isMobile ? [130, 130] : [60, 60]} />
              <Tooltip
                cursor={{ strokeDasharray: '3 3' }}
                contentStyle={{ background: '#111', border: '1px solid var(--border-glass)', fontSize: '11px' }}
                formatter={(v, n) => [n === 'Cooling' ? `${v} kW` : `${v > 0 ? '+' : ''}${v} °C`, n === 'Cooling' ? 'Cooling' : 'Δ setpoint']}
                labelFormatter={() => ''}
                itemSorter={() => 0}
              />
              <Scatter name="In band" data={perf.healthy} fill="rgba(46,204,113,0.55)" isAnimationActive={false} onClick={setDrill} />
              <Scatter name="Overcooled" data={perf.overcooled} fill="var(--accent-blue)" isAnimationActive={false} onClick={setDrill} />
              <Scatter name="Starved" data={perf.starved} fill="#b06bd8" isAnimationActive={false} onClick={setDrill} />
              <Scatter name="Struggling" data={perf.struggling} fill="var(--accent-yellow)" isAnimationActive={false} onClick={setDrill} />
              <Scatter name="Alarm" data={perf.alarm} fill="var(--accent-red)" isAnimationActive={false} onClick={setDrill} />
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

        {/* Per-zone drill-down: real persisted history from TimescaleDB (/api/series). */}
        {drill && (
          <ZoneDrillHistory
            zone={drill}
            liveZone={simData.zones?.[drill.id]}
            onClose={() => setDrill(null)}
            onZoom={setSelectedZone}
            isMobile={isMobile}
          />
        )}

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
                  <div style={{ fontSize: fontBig, color: 'var(--text-primary)', marginTop: '4px', lineHeight: 1.4 }}>
                    Likely cause for a {faultZone.type || 'zone'} (rule-based): {rcaData.cause}.
                  </div>
                  <div style={{ fontSize: '10px', color: 'var(--accent-green)', marginTop: '6px' }}>
                    Evidence: {faultZone.temp.toFixed(1)}°C, {faultOverC > 0 ? `+${faultOverC.toFixed(1)}°C over` : 'inside'} the comfort limit · {alarmingCount} zone{alarmingCount === 1 ? '' : 's'} currently in alarm
                  </div>
                </div>
              </div>
              <button
                onClick={() => onOpenMaintenance(faultZoneId)}
                style={{ width: '100%', background: 'rgba(255,0,0,0.15)', border: '1px solid var(--accent-red)', color: 'var(--accent-red)', padding: '8px', fontSize: '10px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold', letterSpacing: '1px', transition: '0.2s', marginTop: '4px' }}
                onMouseOver={e => e.target.style.background = 'var(--accent-red)'}
                onMouseOut={e => e.target.style.background = 'rgba(255,0,0,0.15)'}
              >
                VIEW FAULT DIAGNOSTICS
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
                  <div style={{ fontSize: fontBig, color: 'var(--text-secondary)', marginTop: '4px', lineHeight: 1.4 }}>
                    {z.label} is unoccupied but its VAV is still delivering ≈ {zoneWasteKw(z).toFixed(1)} kW of cooling electrical.
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
                  style={{ width: '100%', background: 'transparent', border: '1px solid var(--accent-green)', color: 'var(--accent-green)', padding: isMobile ? '10px' : '6px', fontSize: '10px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold', letterSpacing: '1px', marginTop: '4px' }}
                >
                  PREVIEW & APPLY SETBACK
                </button>
              )}
            </div>
          ))}

          {/* 1. Peak Load Shaving Insight — TOU-clock driven, not a hardcoded MW threshold */}
          {peakRelevant && (
            <div style={{ background: autoPilot ? 'rgba(0,255,0,0.05)' : 'rgba(255,165,0,0.05)', border: autoPilot ? '1px solid rgba(0,255,0,0.2)' : '1px solid rgba(255,165,0,0.3)', borderRadius: '6px', padding: '12px' }}>
              <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
                <Zap size={16} color={autoPilot ? "var(--accent-green)" : "orange"} style={{ marginTop: '2px', flexShrink: 0 }} />
                <div>
                  <div style={{ fontSize: '12px', fontWeight: 'bold', color: autoPilot ? 'var(--accent-green)' : 'orange', letterSpacing: '0.5px' }}>
                    {autoPilot ? 'AUTONOMOUS LOAD SHEDDING' : (tou === 'peak' ? 'PEAK TARIFF RUNNING' : `PEAK IN ${toPeak} MIN`)}
                  </div>
                  <div style={{ fontSize: fontBig, color: 'var(--text-primary)', marginTop: '4px', lineHeight: 1.4 }}>
                    Building load {(simData.buildingLoadMw || 0).toFixed(2)} MW — the 17:30–22:30 cao điểm ({rateStr('peak')}/kWh) is the costly window.
                    <div style={{ marginTop: '4px', color: autoPilot ? 'var(--accent-green)' : 'var(--text-secondary)' }}>
                      {autoPilot
                        ? `Pre-cooling shifts ≈ ${shedKw.toFixed(0)} kW of cooling out of peak into normal-rate hours — saving ≈ ${money(peakShiftSavingPerDay(shedKw))}/day (${money(peakShiftSavingPerMonth(shedKw))}/month).`
                        : `Widen deadbands 1°C / pre-cool before 17:30 → shift ≈ ${shedKw.toFixed(0)} kW off peak, saving ≈ ${money(peakShiftSavingPerDay(shedKw))}/day (${money(peakShiftSavingPerMonth(shedKw))}/month).`}
                    </div>
                  </div>
                </div>
              </div>
              {!autoPilot && (
                <button
                  onClick={applySuggestion}
                  style={{ width: '100%', background: 'transparent', border: '1px solid orange', color: 'orange', padding: isMobile ? '10px' : '6px', fontSize: '10px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold', letterSpacing: '1px', marginTop: '4px' }}
                >
                  PREVIEW & APPLY LOAD SHEDDING
                </button>
              )}
            </div>
          )}

          {/* 2. Thermal Comfort Anomaly Insight */}
          {outOfBand.slice(0, isMobile ? 3 : 8).map((z, i) => (
            <div key={`comfort-${i}`} style={{ background: autoPilot ? 'rgba(0,255,0,0.05)' : 'rgba(255,255,255,0.02)', border: autoPilot ? '1px solid rgba(0,255,0,0.2)' : '1px solid var(--border-glass)', borderRadius: '6px', padding: '12px' }}>
              <div style={{ display: 'flex', gap: '8px', marginBottom: '8px' }}>
                <Activity size={16} color={autoPilot ? "var(--accent-green)" : "var(--accent-yellow)"} style={{ marginTop: '2px', flexShrink: 0 }} />
                <div>
                  <div style={{ fontSize: '12px', fontWeight: 'bold', color: autoPilot ? 'var(--accent-green)' : 'var(--accent-yellow)', letterSpacing: '0.5px' }}>
                    {autoPilot ? 'AUTONOMOUS COMFORT CORRECTION' : 'THERMAL COMFORT ANOMALY'}
                  </div>
                  <div style={{ fontSize: fontBig, color: 'var(--text-secondary)', marginTop: '4px', lineHeight: 1.4 }}>
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
                  style={{ width: '100%', background: 'transparent', border: '1px solid var(--accent-yellow)', color: 'var(--accent-yellow)', padding: isMobile ? '10px' : '6px', fontSize: '10px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold', letterSpacing: '1px', marginTop: '4px' }}
                >
                  PREVIEW & INCREASE FLOW
                </button>
              )}
            </div>
          ))}

          {/* Nominal Status Fallback */}
          {!isFault && unoccupiedWasting.length === 0 && outOfBand.length === 0 && !peakRelevant && (
            <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '6px', padding: '12px', display: 'flex', gap: '8px', alignItems: 'center' }}>
               <CheckCircle size={16} color="var(--text-secondary)" />
               <span style={{ fontSize: fontBig, color: 'var(--text-secondary)' }}>All thermodynamic characteristics are within optimal baseline variance.</span>
            </div>
          )}

        </div>
      </div>
    </div>
  );
}

// ZoneDrillHistory pulls a clicked zone's REAL persisted history from TimescaleDB
// (/api/series): temperature always, plus measured CO₂ when a sensor is backing the zone.
// This turns the scatter from a snapshot into a drill-down — an operator can see whether a
// hot point has been hot for an hour or just blipped. Falls back gracefully when the DB has
// no history for the zone yet.
function ZoneDrillHistory({ zone, liveZone, onClose, onZoom, isMobile }) {
  const [temp, setTemp] = useState(null);
  const [co2, setCo2] = useState(null);

  useEffect(() => {
    let alive = true;
    setTemp(null); setCo2(null);
    fetch(`${API_BASE}/api/series?zone=${encodeURIComponent(zone.id)}&metric=temp&minutes=60`)
      .then((r) => (r.ok ? r.json() : []))
      .then((d) => { if (alive) setTemp(Array.isArray(d) ? d : []); })
      .catch(() => { if (alive) setTemp([]); });
    if (zone.sensor) {
      fetch(`${API_BASE}/api/series?zone=${encodeURIComponent(zone.id)}&metric=co2&minutes=60`)
        .then((r) => (r.ok ? r.json() : []))
        .then((d) => { if (alive) setCo2(Array.isArray(d) ? d : []); })
        .catch(() => {});
    }
    return () => { alive = false; };
  }, [zone.id, zone.sensor]);

  const tempData = (temp || []).map((p) => ({ t: p.t.slice(11, 16), v: +p.v.toFixed(2) }));
  const co2Data = (co2 || []).map((p) => ({ t: p.t.slice(11, 16), v: Math.round(p.v) }));
  const sp = liveZone?.setpoint;

  return (
    <div style={{ marginTop: '10px', background: 'rgba(0,0,0,0.35)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '10px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
        <span style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-primary)', letterSpacing: '0.04em' }}>
          {(zone.name || zone.id).toUpperCase()} · LAST HOUR
        </span>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          {onZoom && (
            <button onClick={() => onZoom(zone.id)} style={{ background: 'transparent', border: '1px solid var(--accent-blue)', color: 'var(--accent-blue)', borderRadius: '4px', padding: '3px 8px', fontSize: '9px', fontWeight: 'bold', cursor: 'pointer' }}>
              ZOOM TO ROOM
            </button>
          )}
          <button onClick={onClose} style={{ background: 'transparent', border: 'none', color: 'var(--text-secondary)', cursor: 'pointer', display: 'flex' }}><X size={14} /></button>
        </div>
      </div>

      {temp === null ? (
        <div style={{ fontSize: '10px', color: 'var(--text-muted)' }}>Loading history…</div>
      ) : tempData.length === 0 ? (
        <div style={{ fontSize: '10px', color: 'var(--text-muted)' }}>
          No persisted history yet — the live point is {(liveZone?.temp ?? 0).toFixed(1)}°C{sp != null ? ` vs ${sp.toFixed(1)}°C setpoint` : ''}. History fills once TimescaleDB has logged this zone.
        </div>
      ) : (
        <>
          <div style={{ fontSize: '9px', color: 'var(--text-muted)', marginBottom: '2px' }}>TEMPERATURE °C{sp != null ? ` · setpoint ${sp.toFixed(1)}` : ''}</div>
          <div style={{ width: '100%', height: isMobile ? 130 : 100 }}>
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={tempData} margin={{ top: 4, right: 8, bottom: 0, left: -24 }}>
                <XAxis dataKey="t" tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={{ stroke: 'rgba(255,255,255,0.1)' }} interval="preserveStartEnd" minTickGap={40} />
                <YAxis tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={false} domain={['auto', 'auto']} />
                <Tooltip contentStyle={{ background: 'rgba(10,10,10,0.95)', border: '1px solid var(--border-glass)', borderRadius: 6, fontSize: 10 }} labelStyle={{ color: 'var(--text-secondary)' }} formatter={(v) => [`${v}°C`, 'temp']} />
                {sp != null && <ReferenceLine y={sp} stroke="rgba(46,204,113,0.5)" strokeDasharray="4 4" />}
                <Line type="monotone" dataKey="v" stroke="var(--accent-blue)" strokeWidth={2} dot={false} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
          {zone.sensor && co2Data.length > 0 && (
            <>
              <div style={{ fontSize: '9px', color: 'var(--text-muted)', margin: '6px 0 2px' }}>MEASURED CO₂ ppm (NDIR)</div>
              <div style={{ width: '100%', height: isMobile ? 110 : 80 }}>
                <ResponsiveContainer width="100%" height="100%">
                  <LineChart data={co2Data} margin={{ top: 4, right: 8, bottom: 0, left: -24 }}>
                    <XAxis dataKey="t" tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={{ stroke: 'rgba(255,255,255,0.1)' }} interval="preserveStartEnd" minTickGap={40} />
                    <YAxis tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={false} domain={['auto', 'auto']} />
                    <Tooltip contentStyle={{ background: 'rgba(10,10,10,0.95)', border: '1px solid var(--border-glass)', borderRadius: 6, fontSize: 10 }} labelStyle={{ color: 'var(--text-secondary)' }} formatter={(v) => [`${v} ppm`, 'CO₂']} />
                    <ReferenceLine y={1000} stroke="var(--accent-yellow)" strokeDasharray="4 4" />
                    <Line type="monotone" dataKey="v" stroke="var(--accent-yellow)" strokeWidth={2} dot={false} isAnimationActive={false} />
                  </LineChart>
                </ResponsiveContainer>
              </div>
            </>
          )}
        </>
      )}
    </div>
  );
}
