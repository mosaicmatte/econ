import React, { useMemo, useState } from 'react';
import { Brain, Zap, AlertTriangle, TrendingDown, Activity, Radio, ThermometerSnowflake, Power } from 'lucide-react';
import { money, energyCostPerDay, peakShiftSavingPerMonth } from './tariff';

// Mobile "AI & Automation" screen — the phone-sized twin of the desktop AI Insights panel.
// Gives the operator the three things the app is meant to offer on the go:
//   1. AUTOMATE  — a master Auto-Pilot switch (the autonomous optimizer intent).
//   2. RECOMMENDATIONS — the same live, data-driven insights the desktop shows, each with a
//      real action (open a pre-cool window, fly to a faulting room, engage auto-pilot).
//   3. MANUAL    — a reminder that tapping any room in the 3D view opens per-zone overrides.
// All figures are live (engine stream + EVN tariff); nothing here is hard-coded.
export default function MobileAIScreen({
  simData = {}, activeScenario, faultTarget, aiForecast, hardwareNodes = {},
  autoPilot, setAutoPilot, sendManualOverride, onFocusZone, onClose,
}) {
  const [engaged, setEngaged] = useState({});
  const mark = (id) => setEngaged((e) => ({ ...e, [id]: true }));

  const loadMw = simData.buildingLoadMw || 0;
  const savedMw = simData.energySavedMw || 0;
  const savingsPct = savedMw + loadMw > 0 ? (100 * savedMw) / (savedMw + loadMw) : 0;
  // The plant's electrical draw is the live thermal cooling divided by the live COP, so the
  // shed estimate tracks the real plant instead of subtracting a hard-coded baseline.
  const plantCop = simData.plantCop || 0;
  const hvacMw = plantCop > 0 ? Math.min(loadMw, (simData.coolingOutputMw || 0) / plantCop) : 0;
  const shedKw = 0.05 * hvacMw * 1000; // ~5%-per-°C deadband/pre-cool shave
  const hwList = Object.values(hardwareNodes || {});

  const recs = useMemo(() => {
    const out = [];
    const zones = Object.values(simData.zones || {});

    // 1. Critical thermal runaway (worst alerting zone first).
    const alerting = zones.filter((z) => z.alert === true || z.alert === 'REMEDIATING')
      .sort((a, b) => (b.temp - b.setpoint) - (a.temp - a.setpoint));
    if (alerting.length) {
      const z = alerting[0];
      out.push({
        id: 'fault', accent: '#FF3B30', icon: <AlertTriangle size={20} color="#FF3B30" />,
        title: 'Thermal Runaway Detected',
        message: `${z.label} is at ${z.temp.toFixed(1)}°C — cooling capacity degraded. Fly the camera to the room to inspect and override.`,
        actionLabel: 'ZOOM TO ROOM →', onAction: () => onFocusZone && onFocusZone(z.id),
      });
    }

    // 2. Physics-grounded AFDD divergence (real hardware residual).
    const afdd = hwList.filter((n) => n.afddAlert);
    if (afdd.length) {
      const n = afdd[0];
      out.push({
        id: 'afdd', accent: '#F5C242', icon: <Activity size={20} color="#F5C242" />,
        title: 'AFDD: Physics Divergence',
        message: `${(n.zoneId || '').replace('zone-', '')} is ${(n.residual || 0).toFixed(1)}°C off its calibrated 2R1C model — possible coil/damper fault or open window.`,
        actionLabel: 'INSPECT ZONE →', onAction: () => onFocusZone && onFocusZone(n.zoneId),
      });
    }

    // 3. High grid demand → forecast-driven pre-cooling (a REAL global action).
    if (activeScenario === 'peak') {
      out.push({
        id: 'precool', accent: '#FFD60A', icon: <TrendingDown size={20} color="#FFD60A" />,
        title: 'High Grid Demand',
        message: `Pre-cool the thermal mass now so chillers coast through the 17:30–22:30 peak hours — shifting ≈ ${shedKw.toFixed(0)} kW off peak saves ≈ ${money(peakShiftSavingPerMonth(shedKw))}/month.`,
        actionLabel: 'ACTIVATE PRE-COOLING',
        onAction: () => { sendManualOverride && sendManualOverride('precool', 'GLOBAL'); setAutoPilot && setAutoPilot(true); },
      });
    }

    // 4. LSTM forecast (informational).
    if (aiForecast && aiForecast.predicted_peak_load) {
      out.push({
        id: 'forecast', accent: '#4A90E2', icon: <Activity size={20} color="#4A90E2" />,
        title: 'LSTM Load Forecast',
        message: `Model predicts an upcoming peak of ${aiForecast.predicted_peak_load.toFixed(2)} MW ${aiForecast.weather_source === 'fallback' ? '(fallback weather).' : '(live weather).'}`,
      });
    }

    // 5. Physical edge nodes bound in.
    if (hwList.length) {
      const online = hwList.filter((n) => n.online).length;
      out.push({
        id: 'hw', accent: '#3DDC84', icon: <Radio size={20} color="#3DDC84" />,
        title: 'Hardware-in-the-Loop',
        message: `${online}/${hwList.length} physical edge node${hwList.length > 1 ? 's' : ''} online (${[...new Set(hwList.map((n) => (n.source || 'edge').toUpperCase()))].join(', ')}).`,
      });
    }

    // 6. Unoccupied waste → engage auto-pilot.
    const wasting = zones.filter((z) => z.occupancy === 0 && z.load > 0);
    if (wasting.length) {
      out.push({
        id: 'eco', accent: '#4A90E2', icon: <Zap size={20} color="#4A90E2" />,
        title: 'Energy Optimization',
        message: `${wasting.length} zones are unoccupied but still drawing cooling. Auto-Pilot will set them back automatically.`,
        actionLabel: 'ENGAGE AUTO-PILOT', onAction: () => setAutoPilot && setAutoPilot(true),
      });
    }

    // 7. Thermal drift (informational).
    const hot = zones.filter((z) => z.temp > z.setpoint + (z.deadband || 1));
    if (hot.length && activeScenario !== 'fault') {
      out.push({
        id: 'drift', accent: '#FFD60A', icon: <ThermometerSnowflake size={20} color="#FFD60A" />,
        title: 'Thermal Drift Detected',
        message: `${hot.length} zone${hot.length > 1 ? 's have' : ' has'} drifted above the cooling deadband. Auto-Pilot is rebalancing supply.`,
      });
    }

    // 8. Always-present nominal/savings summary.
    out.push({
      id: 'nominal', accent: '#3DDC84', icon: <Brain size={20} color="#3DDC84" />,
      title: 'Autonomous Operations',
      message: `Occupancy-driven setback is balancing comfort and energy. Live saving: ${savingsPct.toFixed(1)}% of plant load (≈ ${money(energyCostPerDay(savedMw * 1000))}/day).`,
    });

    return out;
  }, [simData, activeScenario, faultTarget, aiForecast, hwList, shedKw, savingsPct, savedMw, onFocusZone, sendManualOverride, setAutoPilot]);

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', color: '#fff', background: '#000', fontFamily: 'system-ui, -apple-system, "SF Pro Display", sans-serif', padding: '20px', minHeight: '100dvh', overflowY: 'auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
        <h2 style={{ margin: 0, fontSize: '24px', fontWeight: 600, display: 'flex', alignItems: 'center', gap: '10px' }}>
          <Brain size={22} color="#4A90E2" /> AI & Automation
        </h2>
      </div>

      {/* AUTO-PILOT master switch */}
      <div
        onClick={() => setAutoPilot && setAutoPilot(!autoPilot)}
        style={{
          display: 'flex', alignItems: 'center', gap: '14px', cursor: 'pointer',
          background: autoPilot ? 'rgba(52,199,89,0.10)' : 'rgba(255,255,255,0.05)',
          border: `1px solid ${autoPilot ? 'rgba(52,199,89,0.4)' : 'rgba(255,255,255,0.12)'}`,
          borderRadius: '16px', padding: '18px', marginBottom: '22px',
        }}
      >
        <Power size={26} color={autoPilot ? '#34C759' : 'rgba(255,255,255,0.5)'} />
        <div style={{ flex: 1 }}>
          <div style={{ fontSize: '17px', fontWeight: 700, color: autoPilot ? '#34C759' : '#fff' }}>
            Auto-Pilot {autoPilot ? 'ON' : 'OFF'}
          </div>
          <div style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)', marginTop: '3px', lineHeight: 1.35 }}>
            {autoPilot
              ? 'Autonomous optimizer is managing setbacks, lighting & pre-cooling.'
              : 'Manual mode — you are in control. Recommendations below are suggestions.'}
          </div>
        </div>
        {/* iOS-style track */}
        <div style={{ width: '48px', height: '28px', borderRadius: '999px', background: autoPilot ? '#34C759' : 'rgba(255,255,255,0.2)', position: 'relative', transition: 'background 0.2s', flexShrink: 0 }}>
          <div style={{ position: 'absolute', top: '3px', left: autoPilot ? '23px' : '3px', width: '22px', height: '22px', borderRadius: '50%', background: '#fff', transition: 'left 0.2s' }} />
        </div>
      </div>

      <div style={{ fontSize: '12px', fontWeight: 600, letterSpacing: '0.1em', color: 'rgba(255,255,255,0.45)', textTransform: 'uppercase', marginBottom: '12px' }}>
        Live Recommendations
      </div>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        {recs.map((r) => (
          <RecCard key={r.id} rec={r} done={!!engaged[r.id]} onEngage={() => mark(r.id)} />
        ))}
      </div>

      {/* Manual-control hint */}
      <div style={{ marginTop: '22px', padding: '16px', borderRadius: '14px', background: 'rgba(255,255,255,0.04)', border: '1px dashed rgba(255,255,255,0.12)', fontSize: '13px', color: 'rgba(255,255,255,0.6)', lineHeight: 1.5 }}>
        <b style={{ color: '#fff' }}>Manual control:</b> tap any room in the 3D view to open it and force lights off, max-cool, or reset that zone — your override latches for 15 minutes over the optimizer.
      </div>
      <div style={{ height: '40px' }} />
    </div>
  );
}

function RecCard({ rec, done, onEngage }) {
  const actionable = !!rec.onAction;
  return (
    <div style={{ position: 'relative', background: 'rgba(255,255,255,0.04)', border: '1px solid rgba(255,255,255,0.08)', borderRadius: '14px', padding: '16px 16px 16px 20px', overflow: 'hidden' }}>
      <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: '4px', background: rec.accent }} />
      <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '8px' }}>
        <div style={{ padding: '6px', background: 'rgba(255,255,255,0.05)', borderRadius: '8px', display: 'flex' }}>{rec.icon}</div>
        <span style={{ fontSize: '15px', fontWeight: 700, color: rec.accent }}>{rec.title}</span>
      </div>
      <p style={{ margin: '0 0 6px 0', fontSize: '13px', color: 'rgba(255,255,255,0.72)', lineHeight: 1.45 }}>{rec.message}</p>
      {actionable && (
        <button
          onClick={() => { rec.onAction(); onEngage(); }}
          disabled={done}
          style={{
            marginTop: '8px', width: '100%', padding: '12px', borderRadius: '10px', cursor: done ? 'default' : 'pointer',
            background: done ? 'rgba(52,199,89,0.15)' : rec.accent, color: done ? '#34C759' : '#000',
            border: 'none', fontSize: '14px', fontWeight: 700, letterSpacing: '0.02em',
          }}
        >
          {done ? '✓ ENGAGED' : rec.actionLabel}
        </button>
      )}
    </div>
  );
}
