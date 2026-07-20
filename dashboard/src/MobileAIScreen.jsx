import React, { useMemo, useState } from 'react';
import { Brain, Zap, AlertTriangle, TrendingDown, Activity, Radio, ThermometerSnowflake, Power, Plug, CloudOff, Wind, WifiOff } from 'lucide-react';
import { money, energyCostPerDay, peakShiftSavingPerMonth, rateStr, touPeriod, minutesToPeak, TARIFF } from './tariff';
import { useOpsStatus, untilLabel } from './useOpsStatus';
import { usePlugs } from './usePlugs';

// Mobile "AI & Automation" screen — the phone-sized twin of the desktop AI Insights panel.
// Gives the operator the three things the app is meant to offer on the go:
//   1. AUTOMATE  — a master Auto-Pilot switch (the autonomous optimizer intent).
//   2. RECOMMENDATIONS — the same live, data-driven insights the desktop shows, each with a
//      real action (open a pre-cool window, purge a stale-air room, fly to a faulting room).
//   3. MANUAL    — a reminder that tapping any room in the 3D view opens per-zone overrides.
// Every card is generated from real state: the telemetry stream, the edge-node registry,
// the EVN TOU clock, and the engine's own control loops (pre-cool, plug sweep, weather).
export default function MobileAIScreen({
  simData = {}, activeScenario, faultTarget, aiForecast, hardwareNodes = {},
  autoPilot, setAutoPilot, sendManualOverride, onFocusZone, onOpenEnergy, onClose,
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

  // Live control-loop state from the engine (shared hooks with the desktop panel).
  const { precool, weather } = useOpsStatus();
  const { status: plugStatus } = usePlugs();

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

    // 2. A bound edge node the broker has declared dead (MQTT Last Will) — a field
    // callout: its zone has fallen back to pure simulation.
    const dead = hwList.filter((n) => !n.online);
    if (dead.length) {
      const n = dead[0];
      out.push({
        id: 'offline', accent: '#FF3B30', icon: <WifiOff size={20} color="#FF3B30" />,
        title: `Edge Node${dead.length > 1 ? 's' : ''} Offline`,
        message: `${dead.map((d) => `${(d.source || 'edge').toUpperCase()} on ${(d.zoneId || '').replace('zone-', '')}`).join('; ')} — broker LWT reports offline. Sensing and socket control are gone until the node returns.`,
        actionLabel: 'SHOW ZONE →', onAction: () => onFocusZone && onFocusZone(n.zoneId),
      });
    }

    // 3. Physics-grounded AFDD divergence (real hardware residual).
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

    // 3b. Measured CO₂ over the comfort line — only a real NDIR can raise this, and the
    // action is a real purge override (lights off, max airflow flush, 15-min latch).
    const staleAir = zones.filter((z) => (z.co2 || 0) > 1000).sort((a, b) => b.co2 - a.co2);
    if (staleAir.length) {
      const z = staleAir[0];
      out.push({
        id: 'co2', accent: '#F5C242', icon: <Wind size={20} color="#F5C242" />,
        title: 'Measured CO₂ High',
        message: `${z.label} reads ${Math.round(z.co2)} ppm from its NDIR sensor (guideline ≤ 1000). Ventilation is not keeping up with occupancy.`,
        actionLabel: 'PURGE ZONE', onAction: () => sendManualOverride && sendManualOverride('purge', z.id),
      });
    }

    // 4. High grid demand — the EVN TOU CLOCK decides, not a demo toggle: in cao điểm,
    // or within 90 minutes of it. Reflects the engine's real pre-cool window state.
    const tou = touPeriod();
    const toPeak = minutesToPeak();
    if (tou === 'peak' || (toPeak !== null && toPeak <= 90)) {
      const windowOpen = !!precool?.active;
      out.push({
        id: 'precool', accent: '#FFD60A', icon: <TrendingDown size={20} color="#FFD60A" />,
        title: tou === 'peak' ? 'Peak Tariff Running Now' : `Peak Tariff in ${toPeak} min`,
        message: windowOpen
          ? `A pre-cool window is open until ${untilLabel(precool.until)} — thermal mass is charging so chillers coast through the ${rateStr('peak')}/kWh window.`
          : `${tou === 'peak' ? `Cao điểm is charging ${rateStr('peak')}/kWh right now.` : `Cao điểm (${rateStr('peak')}/kWh) begins at 17:30.`} Pre-cooling shifts ≈ ${shedKw.toFixed(0)} kW off peak ≈ ${money(peakShiftSavingPerMonth(shedKw))}/month.`,
        actionLabel: windowOpen ? `✓ OPEN UNTIL ${untilLabel(precool.until)}` : 'ACTIVATE PRE-COOLING',
        done: windowOpen,
        onAction: windowOpen ? undefined : () => { sendManualOverride && sendManualOverride('precool', 'GLOBAL'); },
      });
    }

    // 5. LSTM forecast (informational), with input provenance: whose weather it used
    // and whether the sampled window is still warming up after a boot.
    if (aiForecast && aiForecast.predicted_peak_load) {
      const src = aiForecast.weather_source === 'engine' ? '(engine’s live weather feed)'
        : aiForecast.weather_source === 'fallback' ? '(fallback weather)' : '(live weather)';
      const realN = aiForecast.window_real_samples;
      const winLen = aiForecast.window_len || 12;
      const warmup = realN != null && realN < winLen ? ` Window warming up: ${realN}/${winLen} real samples.` : '';
      out.push({
        id: 'forecast', accent: '#4A90E2', icon: <Activity size={20} color="#4A90E2" />,
        title: 'LSTM Load Forecast',
        message: `Model predicts an upcoming peak of ${aiForecast.predicted_peak_load.toFixed(2)} MW ${src}.${warmup}`,
      });
    }

    // 5b. Weather feed stale → envelope on the climatological fallback.
    if (weather && !weather.live) {
      out.push({
        id: 'weather', accent: '#F5C242', icon: <CloudOff size={20} color="#F5C242" />,
        title: 'Weather Feed Stale',
        message: `Open-Meteo has not refreshed${weather.ageSec > 0 ? ` in ${(weather.ageSec / 3600).toFixed(1)} h` : ''}. The envelope physics is running on the ${weather.outdoorC.toFixed(1)} °C fallback — loads and forecasts are less trustworthy until it recovers.`,
      });
    }

    // 5c. Plug sweep (APLC): the engine's own after-hours socket control, live.
    if (plugStatus) {
      const saved = simData.plugSavedKwh ?? plugStatus.savedKwh ?? 0;
      if (!plugStatus.config?.enabled) {
        out.push({
          id: 'plugs', accent: '#F5C242', icon: <Plug size={20} color="#F5C242" />,
          title: 'Plug Sweep Disabled',
          message: `${(simData.plugStandbyKw ?? plugStatus.standbyKw ?? 0).toFixed(1)} kW of always-on standby has no after-hours control — the case-study buildings lost 26.4% of their energy to exactly this.`,
          actionLabel: 'OPEN ENERGY →', onAction: onOpenEnergy,
        });
      } else if (plugStatus.armed) {
        out.push({
          id: 'plugs', accent: '#3DDC84', icon: <Plug size={20} color="#3DDC84" />,
          title: 'Plug Sweep Armed',
          message: `${plugStatus.shedZones} vacant zone${plugStatus.shedZones === 1 ? '' : 's'} swept — ${(simData.plugShedKw ?? plugStatus.shedKw ?? 0).toFixed(1)} kW off. Avoided so far: ${saved.toFixed(1)} kWh ≈ ${money(saved * TARIFF.normalPerKwh)}. Sockets restore on presence.`,
          actionLabel: 'OPEN ENERGY →', onAction: onOpenEnergy,
        });
      }
    }

    // 6. Physical edge nodes bound in.
    if (hwList.length) {
      const online = hwList.filter((n) => n.online).length;
      out.push({
        id: 'hw', accent: '#3DDC84', icon: <Radio size={20} color="#3DDC84" />,
        title: 'Hardware-in-the-Loop',
        message: `${online}/${hwList.length} physical edge node${hwList.length > 1 ? 's' : ''} online (${[...new Set(hwList.map((n) => (n.source || 'edge').toUpperCase()))].join(', ')}).`,
      });
    }

    // 7. Unoccupied zones still at occupied setpoints — priced through the live COP at
    // the live tariff, and attributed honestly: instrumented zones set back themselves.
    // 24/7-critical types excluded: an empty server room being cooled is correct, not waste.
    const wasting = zones.filter((z) =>
      z.occupancy === 0 && z.load > 0 && z.lightsOn !== false
      && z.type !== 'server-room' && z.type !== 'mechanical');
    if (wasting.length && plantCop > 0) {
      const wasteKw = wasting.reduce((acc, z) => acc + z.load, 0) / plantCop;
      out.push({
        id: 'eco', accent: '#4A90E2', icon: <Zap size={20} color="#4A90E2" />,
        title: 'Unoccupied, Still Cooled',
        message: `${wasting.length} unoccupied zone${wasting.length === 1 ? '' : 's'} still held at occupied setpoints — ≈ ${wasteKw.toFixed(1)} kW electrical (${money(energyCostPerDay(wasteKw))}/day). Zones with presence sensors set back automatically; the rest wait for edge nodes or the after-hours plug sweep.`,
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
  }, [simData, activeScenario, faultTarget, aiForecast, hwList, shedKw, savingsPct, savedMw, onFocusZone, sendManualOverride, setAutoPilot, precool, weather, plugStatus, onOpenEnergy]);

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
