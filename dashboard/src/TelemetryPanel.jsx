import React, { useMemo, useState } from 'react';
import { Activity, AlertTriangle, Zap, CheckCircle } from 'lucide-react';
import { ScatterChart, Scatter, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { money, energyCostPerDay, peakShiftSavingPerDay, peakShiftSavingPerMonth, touPeriod, touPeriodLabel } from './tariff';

export default function TelemetryPanel({ simData, loadHistory, activeScenario, faultTarget, onOpenMaintenance, autoPilot, setAutoPilot }) {

  const [activeChartMode, setActiveChartMode] = useState('office_dcv');
  // Manual "apply" actions engage the autonomous controller, which performs the suggested
  // setback / load-shed / flow change — so the button does the real thing instead of nothing.
  const applySuggestion = () => setAutoPilot && setAutoPilot(true);

  const historicalData = useMemo(() => {
    const data = [];
    if (activeChartMode === 'office_dcv') {
      for (let i = 0; i < 300; i++) {
        const co2 = 400 + Math.random() * 3000;
        // Physically correct saturating curve: P_cool = P_base + alpha * N(CO2) + beta * Q_vent(CO2)
        const co2_out = 400;
        const n_est = Math.min(50, Math.max(0, (co2 - co2_out) / 20)); // N(CO2) saturates around 50 people
        const q_vent = Math.max(0, co2 - co2_out) * 0.05;
        const basePower = 200 + (3.5 * n_est) + (1.2 * q_vent);
        const actualPower = basePower + (Math.random() * 20 - 10);
        data.push({ x: Math.round(co2), y: Number(actualPower.toFixed(1)) });
      }
    } else if (activeChartMode === 'server_room') {
      for (let i = 0; i < 300; i++) {
        // Servers: IT Load (kW) vs Cooling Power (kW)
        const itLoad = 100 + Math.random() * 900;
        const coolingPower = itLoad * 0.35; // Approx PUE
        const actualPower = coolingPower + (Math.random() * 10 - 5);
        data.push({ x: Math.round(itLoad), y: Number(actualPower.toFixed(1)) });
      }
    }
    return data;
  }, [activeChartMode]);

  const currentData = useMemo(() => {
    // Make sure z.archetype is handled (fallback to 'office_dcv' if missing)
    const filteredZones = Object.values(simData.zones).filter(z => (z.archetype || 'office_dcv') === activeChartMode);
    
    return filteredZones.map((z, idx) => {
      const isFaulting = (activeScenario === 'fault' && z.id === faultTarget);
      
      if (activeChartMode === 'office_dcv') {
        let yPower = z.load * 20; 
        let co2 = 400 + (z.occupancy * 40);
        // Fault introduces high co2 and power deviation
        if (isFaulting) { co2 += 1000; yPower += 150; } 
        return { name: z.label, x: Math.round(co2), y: Number(yPower.toFixed(1)), isFaulting };
      } else {
        let itLoad = z.baseHeatGain * 10 + (z.load * 10);
        let yPower = z.load * 20; 
        // Fault introduces high cooling power despite constant IT load
        if (isFaulting) { yPower += 300; } 
        return { name: z.label, x: Math.round(itLoad), y: Number(yPower.toFixed(1)), isFaulting };
      }
    });
  }, [simData, activeChartMode, activeScenario, faultTarget]);

  // Insights Data logic
  const isFault = activeScenario === 'fault';
  const faultZoneId = isFault ? faultTarget : null;
  const faultZone = faultZoneId ? simData.zones[faultZoneId] : null;
  
  const unoccupiedWasting = Object.values(simData.zones).filter(z => z.occupancy === 0).slice(0, 1);
  const outOfBand = Object.values(simData.zones).filter(z => z.temp > z.setpoint + z.deadband && activeScenario !== 'fault');

  // --- Cost model (see tariff.js) ---
  const cop = simData.plantCop || 3.0;
  // Cooling electrical draw = building load minus the fixed ~2 MW lighting/plug/fan
  // baseline the engine bakes in; this is the load a shave/pre-cool actually addresses.
  const coolingElectricalKw = Math.max(0, (simData.buildingLoadMw || 0) - 2.0) * 1000;
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
            {activeChartMode === 'office_dcv' ? 'OFFICE DCV (CO₂ vs COOLING kW)' : 'SERVER ROOM (IT kW vs COOLING kW)'}
          </div>
          <div style={{ display: 'flex', gap: '4px' }}>
            <button 
              onClick={() => setActiveChartMode('office_dcv')}
              style={{ background: activeChartMode === 'office_dcv' ? 'var(--accent-blue)' : 'var(--bg-obsidian)', color: activeChartMode === 'office_dcv' ? '#000' : 'var(--text-secondary)', border: '1px solid var(--border-glass)', fontSize: '9px', padding: '2px 6px', borderRadius: '4px', cursor: 'pointer', fontWeight: 'bold' }}
            >
              OFFICE
            </button>
            <button 
              onClick={() => setActiveChartMode('server_room')}
              style={{ background: activeChartMode === 'server_room' ? 'var(--accent-blue)' : 'var(--bg-obsidian)', color: activeChartMode === 'server_room' ? '#000' : 'var(--text-secondary)', border: '1px solid var(--border-glass)', fontSize: '9px', padding: '2px 6px', borderRadius: '4px', cursor: 'pointer', fontWeight: 'bold' }}
            >
              SERVER
            </button>
          </div>
        </div>
        
        <div style={{ height: '260px', background: 'rgba(0,0,0,0.4)', borderRadius: '8px', padding: '12px', border: '1px solid var(--border-glass)' }}>
          <ResponsiveContainer width="100%" height="100%">
            <ScatterChart margin={{ top: 10, right: 10, bottom: 0, left: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" />
              <XAxis type="number" dataKey="x" name={activeChartMode === 'office_dcv' ? 'CO₂' : 'IT Load'} unit={activeChartMode === 'office_dcv' ? ' ppm' : ' kW'} domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} tickMargin={8} />
              <YAxis type="number" dataKey="y" name="Cooling" unit=" kW" domain={['auto', 'auto']} stroke="var(--text-secondary)" fontSize={10} width={45} />
              <Tooltip cursor={{ strokeDasharray: '3 3' }} contentStyle={{ background: '#111', border: '1px solid var(--border-glass)' }} />
              <Scatter name="Historical Baseline" data={historicalData} fill="rgba(255,255,255,0.35)" isAnimationActive={false} />
              <Scatter name="Live Telemetry Node" data={currentData} fill="var(--accent-yellow)" isAnimationActive={false} />
            </ScatterChart>
          </ResponsiveContainer>
        </div>
        <div style={{ display: 'flex', gap: '1rem', marginTop: '8px', paddingLeft: '40px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
            <div style={{ width: '8px', height: '8px', borderRadius: '50%', background: 'rgba(255,255,255,0.3)' }} />
            <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>Historical Baseline</span>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: '4px' }}>
            <div style={{ width: '8px', height: '8px', borderRadius: '50%', background: 'var(--accent-yellow)' }} />
            <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>Live Telemetry Node</span>
          </div>
        </div>
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
