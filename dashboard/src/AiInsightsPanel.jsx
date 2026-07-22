import React, { useMemo, useState, useEffect } from 'react';
import { Brain, Zap, AlertTriangle, TrendingDown, ThermometerSnowflake, Activity, Radio, Plug, CloudOff, Wind, WifiOff, Download, Cpu } from 'lucide-react';
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, ReferenceLine } from 'recharts';
import { money, energyCostPerDay, peakShiftSavingPerMonth, rateStr, touPeriod, minutesToPeak, TARIFF } from './tariff';
import { useOpsStatus, untilLabel } from './useOpsStatus';
import { usePlugs } from './usePlugs';
import { useRecommendations } from './useRecommendations';
import { API_BASE } from './api';

export default function AiInsightsPanel({ simData, activeScenario, faultTarget, aiForecast, setAutoPilot, hardwareNodes = {}, setSelectedZone, sendManualOverride, onOpenPlugs }) {
  // Every card's action is a real one: an actuation over the websocket (pre-cool window,
  // purge override), a navigation (fly the 3D camera to the zone, open the PLUGS tab),
  // or an inline expansion of live detail. Engaged state marks fire-once actuations.
  const [engaged, setEngaged] = useState({});
  const [expanded, setExpanded] = useState({});
  const toggle = (id) => setExpanded((e) => ({ ...e, [id]: !e[id] }));

  // Live operational signals: pre-cool window, weather feed, plug sweep. Polled from the
  // engine so this panel reasons over what the building is DOING, not over UI state.
  const { precool, weather } = useOpsStatus();
  const { status: plugStatus } = usePlugs();
  // Learned anomaly recommendations from the engine's online baseline model
  // (server/simulation/baselines.go): each scored in σ against this building's own normal
  // for the hour. These replace the old hardcoded threshold cards below.
  const { recommendations, model: recModel } = useRecommendations();

  const hwList = Object.values(hardwareNodes || {});
  const hwOnline = hwList.filter((n) => n.online).length;

  // Real streamed savings (engine energySavedMw), as a share of what load WOULD be without setbacks.
  const savedMw = simData.energySavedMw || 0;
  const loadMw = simData.buildingLoadMw || 0;
  const savingsPct = savedMw + loadMw > 0 ? (100 * savedMw) / (savedMw + loadMw) : 0;

  // Projection curve for the forecast card: eased ramp from the CURRENT live load to the
  // LSTM's predicted peak. The two endpoints are real; the ramp is a visualization aid.
  const forecastSeries = useMemo(() => {
    if (!aiForecast?.predicted_peak_load) return [];
    const cur = loadMw;
    const peak = aiForecast.predicted_peak_load;
    return Array.from({ length: 13 }, (_, i) => {
      const x = i / 12;
      const s = x * x * (3 - 2 * x); // smoothstep ease
      return { t: `+${i * 5}m`, mw: +(cur + (peak - cur) * s).toFixed(3) };
    });
  }, [aiForecast?.predicted_peak_load, loadMw]);

  // Insights are generated from what the building is actually doing: the telemetry
  // stream, the edge-node registry, the TOU clock, and the engine's own control loops
  // (pre-cool window, plug sweep, weather feed). Demo scenario toggles only add cards;
  // they never gate a real signal.
  const insights = useMemo(() => {
    const generated = [];
    const zones = Object.values(simData.zones || {});
    const cop = simData.plantCop || 0;

    // 1. Critical scenario fault → REAL remediation: a websocket override that floods
    // the zone with cooling (engine publishes to the edge node and latches 15 min).
    if (activeScenario === 'fault' && faultTarget) {
      generated.push({
        id: 'fault',
        type: 'critical',
        icon: <AlertTriangle size={18} color="var(--accent-red)" />,
        title: 'Thermal Runaway Detected',
        message: `Zone ${faultTarget} is experiencing a critical thermal failure. Cooling capacity is degraded.`,
        action: 'FLOOD ZONE WITH COOLING',
        once: true,
        onAction: () => sendManualOverride && sendManualOverride('cool', faultTarget),
      });
    }

    // 1b. A bound edge node the broker has declared dead (MQTT Last Will). Its zone has
    // fallen back to simulation — that is a field callout, not a UI state.
    const deadNodes = hwList.filter((n) => !n.online);
    if (deadNodes.length > 0) {
      generated.push({
        id: 'offline',
        type: 'critical',
        icon: <WifiOff size={18} color="var(--accent-red)" />,
        title: `Edge Node${deadNodes.length > 1 ? 's' : ''} Offline`,
        message: `${deadNodes.map((n) => `${(n.source || 'edge').toUpperCase()} on ${(n.zoneId || '').replace('zone-', '')}`).join('; ')} — broker LWT reports offline. The zone${deadNodes.length > 1 ? 's have' : ' has'} fallen back to the 2R1C model; sensing and socket control are gone until the node returns.`,
        action: 'SHOW IN 3D',
        onAction: () => setSelectedZone && setSelectedZone(deadNodes[0].zoneId),
      });
    }

    // 2. Physical hardware bound into the twin (ESP32 / Pico edge nodes)
    if (hwList.length > 0) {
      const pinned = hwList.filter((n) => n.tempPinned).length;
      generated.push({
        id: 'hardware',
        type: hwOnline === hwList.length ? 'success' : 'warning',
        expandable: true,
        icon: <Radio size={18} color={hwOnline === hwList.length ? 'var(--accent-green)' : 'var(--accent-yellow)'} />,
        title: 'Hardware-in-the-Loop Active',
        message: `${hwOnline}/${hwList.length} physical edge node${hwList.length > 1 ? 's' : ''} online (${[...new Set(hwList.map((n) => (n.source || 'edge').toUpperCase()))].join(', ')}). ${pinned > 0 ? `${pinned} zone${pinned > 1 ? 's' : ''} pinned to a real temperature sensor.` : 'Occupancy is driven by the physical sensors.'}`,
        action: 'INSPECT NODES'
      });
    }

    // 2b. Physics-grounded AFDD: a sensor-bound room whose measured temperature has
    // diverged from its sensor-free 2R1C shadow model — a fault, not a forecast.
    const afddNodes = hwList.filter((n) => n.afddAlert);
    if (afddNodes.length > 0) {
      generated.push({
        id: 'afdd',
        type: 'critical',
        expandable: true, // expands the persisted residual trend — the maintenance evidence
        afddZone: afddNodes[0].zoneId,
        icon: <AlertTriangle size={18} color="var(--accent-red)" />,
        title: 'AFDD: Physics Divergence',
        message: `${afddNodes.map((n) => (n.zoneId || '').replace('zone-', '')).join(', ')} reading ${afddNodes.map((n) => `${(n.residual || 0).toFixed(1)}°C`).join(', ')} away from the calibrated thermal model — possible coil/damper fault, blocked diffuser or open window.`,
        action: 'VIEW DRIFT HISTORY',
      });
    }

    // 2c. LEARNED anomaly recommendations from the engine's online baseline model
    // (server/simulation/baselines.go). These REPLACE the old hardcoded threshold cards
    // (co2 > 1000, temp > setpoint + deadband): each is scored in σ against what the zone
    // actually does at THIS hour, with the ASHRAE 1000 ppm guideline as a clearly-labelled
    // cold-start floor. The message is authored server-side (learned mean±σ, deviation,
    // maturity), and every card's action is the real remediation the model chose — a purge
    // override, a cooling flood, a pre-cool window — dispatched over the same websocket.
    const recIcon = (metric, color) =>
      metric === 'co2' ? <Wind size={18} color={color} />
      : metric === 'temp' ? <ThermometerSnowflake size={18} color={color} />
      : metric === 'buildingLoadMw' ? <TrendingDown size={18} color={color} />
      : <Activity size={18} color={color} />;
    const recActionLabel = { purge: 'PURGE ZONE', cool: 'FLOOD COOLING', precool: 'ACTIVATE PRE-COOLING' };
    recommendations.forEach((rec) => {
      const color = rec.severity === 'critical' ? 'var(--accent-red)' : rec.severity === 'warning' ? 'var(--accent-yellow)' : 'var(--accent-blue)';
      const label = recActionLabel[rec.action];
      generated.push({
        id: `rec-${rec.id}`,
        type: rec.severity,
        icon: recIcon(rec.metric, color),
        title: rec.title,
        message: rec.message,
        badge: rec.basis === 'learned' ? 'LEARNED' : 'ASHRAE STD',
        badgeColor: rec.basis === 'learned' ? 'var(--accent-blue)' : 'var(--text-muted)',
        action: label,
        once: !!label,
        onAction: label ? () => sendManualOverride && sendManualOverride(rec.action, rec.zone) : undefined,
      });
    });

    // 3. High grid demand — driven by the EVN TOU CLOCK, not a demo toggle: warn while
    // cao điểm is running, and ahead of it when it starts within 90 minutes. The card
    // reflects the engine's real pre-cool window state and its action opens one.
    const tou = touPeriod();
    const toPeak = minutesToPeak();
    if (tou === 'peak' || (toPeak !== null && toPeak <= 90)) {
      const hvacMw = cop > 0 ? Math.min(loadMw, (simData.coolingOutputMw || 0) / cop) : 0;
      const shedKw = 0.05 * hvacMw * 1000; // ≈5% coast from a charged thermal mass (estimate)
      const windowOpen = !!precool?.active;
      generated.push({
        id: 'peak',
        type: tou === 'peak' ? 'warning' : 'info',
        icon: <TrendingDown size={18} color={tou === 'peak' ? 'var(--accent-yellow)' : 'var(--accent-blue)'} />,
        title: tou === 'peak' ? 'Peak Tariff Running Now' : `Peak Tariff in ${toPeak} min`,
        message: `${tou === 'peak' ? 'The 17:30–22:30 cao điểm window is charging ' + rateStr('peak') + '/kWh right now.' : `Cao điểm (${rateStr('peak')}/kWh vs ${rateStr('normal')} normal) begins at 17:30.`} ${windowOpen ? `A pre-cool window is OPEN until ${untilLabel(precool.until)} — thermal mass is charging so chillers can coast.` : `Pre-cooling now charges the thermal mass at the cheaper rate — shifting ≈ ${shedKw.toFixed(0)} kW off peak saves roughly ${money(peakShiftSavingPerMonth(shedKw))}/month.`}`,
        action: windowOpen ? 'PRE-COOLING' : 'ACTIVATE PRE-COOLING',
        done: windowOpen,
        doneLabel: `✓ OPEN UNTIL ${untilLabel(precool?.until)}`,
        once: true,
        onAction: () => sendManualOverride && sendManualOverride('precool', 'GLOBAL'),
      });
    }

    if (aiForecast && aiForecast.predicted_peak_load) {
      const weatherNote = aiForecast.weather_source === 'engine'
        ? 'Weather from the engine’s live Open-Meteo feed — same numbers the envelope physics uses.'
        : aiForecast.weather_source === 'fallback' ? '(Using fallback weather.)' : '(Live weather data incorporated.)';
      const realN = aiForecast.window_real_samples;
      const winLen = aiForecast.window_len || 12;
      const warmup = realN != null && realN < winLen
        ? ` Input window warming up: ${realN}/${winLen} real 5-min samples since boot.`
        : '';
      generated.push({
        id: 'forecast',
        type: 'info',
        expandable: true,
        icon: <Activity size={18} color="var(--accent-blue)" />,
        title: 'LSTM Load Forecast',
        message: `Deep Learning model predicts an upcoming peak load of ${aiForecast.predicted_peak_load.toFixed(2)} MW over the sampled last hour. ${weatherNote}${warmup}`,
        action: 'VIEW PREDICTIONS'
      });
    }

    // 3b. The envelope's weather feed has gone stale: the physics is integrating against
    // the 30 °C climatological fallback, so loads and forecasts degrade together.
    if (weather && !weather.live) {
      generated.push({
        id: 'weather',
        type: 'warning',
        icon: <CloudOff size={18} color="var(--accent-yellow)" />,
        title: 'Weather Feed Stale',
        message: `The Open-Meteo feed has not refreshed${weather.ageSec > 0 ? ` in ${(weather.ageSec / 3600).toFixed(1)} h` : ''}. The 2R1C envelope is running on the ${weather.outdoorC.toFixed(1)} °C climatological fallback — envelope loads and the LSTM forecast are less trustworthy until the feed recovers.`,
      });
    }

    // 3c. Plug loads (APLC): the sweep's live state, from the engine. Disabled after
    // hours = the phantom runs unmanaged, which is a cost, not a preference.
    if (plugStatus) {
      const saved = simData.plugSavedKwh ?? plugStatus.savedKwh ?? 0;
      if (!plugStatus.config?.enabled) {
        generated.push({
          id: 'plugs',
          type: 'warning',
          icon: <Plug size={18} color="var(--accent-yellow)" />,
          title: 'Plug Sweep Disabled',
          message: `${(simData.plugStandbyKw ?? plugStatus.standbyKw ?? 0).toFixed(1)} kW of always-on standby is running with no after-hours control. The case-study buildings lost 26.4% of their energy to exactly this. Enable the sweep in the PLUGS tab.`,
          action: onOpenPlugs ? 'OPEN PLUGS TAB' : undefined,
          onAction: onOpenPlugs,
        });
      } else if (plugStatus.armed) {
        generated.push({
          id: 'plugs',
          type: 'success',
          icon: <Plug size={18} color="var(--accent-green)" />,
          title: 'Plug Sweep Armed (After Hours)',
          message: `${plugStatus.shedZones} vacant zone${plugStatus.shedZones === 1 ? '' : 's'} swept — ${(simData.plugShedKw ?? plugStatus.shedKw ?? 0).toFixed(1)} kW of switchable standby off. Cumulative avoided: ${saved.toFixed(1)} kWh ≈ ${money(saved * TARIFF.normalPerKwh)}. Sockets restore the instant presence returns.`,
          action: onOpenPlugs ? 'OPEN PLUGS TAB' : undefined,
          onAction: onOpenPlugs,
        });
      }
    }

    // 4. Unoccupied zones still holding occupied setpoints. Priced honestly: their heat
    // load through the LIVE plant COP at today's tariff — and attributed honestly: the
    // optimizer sets back instrumented zones itself; unmetered zones wait for sensors.
    // 24/7-critical types are excluded — an empty server room being cooled is correct
    // operation, not waste, exactly as the plug sweep's critical list already encodes.
    const wastingZones = zones.filter((z) =>
      z.occupancy === 0 && z.load > 0 && z.lightsOn !== false
      && z.type !== 'server-room' && z.type !== 'mechanical');
    if (wastingZones.length > 0 && cop > 0) {
      const wasteKw = wastingZones.reduce((acc, z) => acc + z.load, 0) / cop;
      generated.push({
        id: 'wasting',
        type: 'info',
        icon: <Zap size={18} color="var(--accent-blue)" />,
        title: 'Unoccupied Zones at Occupied Setpoints',
        message: `${wastingZones.length} unoccupied zone${wastingZones.length === 1 ? '' : 's'} still cooled and lit as if occupied — ≈ ${wasteKw.toFixed(1)} kW electrical at the plant's live COP (${money(energyCostPerDay(wasteKw))}/day at the current rate). Zones with presence sensors set back automatically; the rest are why the after-hours plug sweep and more edge nodes pay for themselves.`,
        action: onOpenPlugs ? 'OPEN PLUGS TAB' : undefined,
        onAction: onOpenPlugs,
      });
    }

    // 5. (Thermal-drift hotspots are now handled by the learned-baseline recommendations
    // above: a zone many σ hotter than its own hourly normal, scored server-side, instead
    // of a fixed temp > setpoint + deadband rule that fires on every warm afternoon.)

    // 6. Autonomous operations status (always present) — real engine state.
    const apOn = simData.autoPilot !== false;
    const inSetback = simData.zonesInSetback || 0;
    generated.push({
      id: 'general',
      type: apOn ? 'success' : 'warning',
      expandable: true,
      icon: <Brain size={18} color={apOn ? 'var(--accent-green)' : 'var(--accent-yellow)'} />,
      title: apOn ? 'Autonomous Operations Active' : 'Auto-Pilot Suspended',
      message: apOn
        ? `Occupancy-driven optimizer is holding ${inSetback} zone${inSetback === 1 ? '' : 's'} in setback — ${savingsPct.toFixed(1)}% of plant load (${(savedMw * 1000).toFixed(0)} kW ≈ ${money(energyCostPerDay(savedMw * 1000))}/day). Streamed from the engine.`
        : 'The optimizer is off — it released its setbacks to the occupied baseline and the operator is in manual control. Re-engage to resume autonomous setback.',
      action: 'VIEW MODEL METRICS'
    });

    return generated;
  }, [simData, activeScenario, faultTarget, aiForecast, hwList, hwOnline, savingsPct, savedMw, loadMw, precool, weather, plugStatus, recommendations, sendManualOverride, setSelectedZone, onOpenPlugs]);

  // ---- Inline detail sections for the expandable cards ----
  const renderDetail = (id) => {
    if (id === 'forecast') {
      if (!forecastSeries.length) return null;
      const peak = aiForecast.predicted_peak_load;
      return (
        <div style={{ marginTop: '6px' }}>
          <div style={{ fontSize: '9px', color: 'var(--text-muted)', marginBottom: '4px', letterSpacing: '0.04em' }}>
            PROJECTED RAMP · LIVE LOAD → PREDICTED PEAK
          </div>
          <div style={{ width: '100%', height: 120 }}>
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={forecastSeries} margin={{ top: 6, right: 8, bottom: 0, left: -22 }}>
                <XAxis dataKey="t" tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={{ stroke: 'rgba(255,255,255,0.1)' }} interval={3} />
                <YAxis tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={false} domain={['auto', 'auto']} />
                <Tooltip contentStyle={{ background: 'rgba(10,10,10,0.95)', border: '1px solid var(--border-glass)', borderRadius: 6, fontSize: 10 }} labelStyle={{ color: 'var(--text-secondary)' }} formatter={(v) => [`${v} MW`, 'load']} />
                <ReferenceLine y={peak} stroke="var(--accent-red)" strokeDasharray="4 4" label={{ value: 'PEAK', fontSize: 8, fill: 'var(--accent-red)', position: 'insideTopRight' }} />
                <Line type="monotone" dataKey="mw" stroke="var(--accent-blue)" strokeWidth={2} dot={false} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </div>
      );
    }
    if (id === 'general') {
      const rows = [
        ['Auto-Pilot', simData.autoPilot !== false ? 'engaged' : 'suspended (manual)'],
        ['Zones in setback', `${simData.zonesInSetback || 0}`],
        ['Live savings', `${(savedMw * 1000).toFixed(0)} kW (${savingsPct.toFixed(1)}%)`],
        ['Utility saving', `${money(energyCostPerDay(savedMw * 1000))}/day`],
        ['Plant COP', (simData.plantCop || 0).toFixed(2)],
        ['Cooling delivered', `${(simData.coolingOutputMw || 0).toFixed(2)} MW thermal`],
        ['Zones simulated', `${Object.keys(simData.zones || {}).length}`],
        ['Physical nodes', `${hwList.length} (${hwOnline} online)`],
        ['Forecaster', aiForecast ? `LSTM · ${aiForecast.weather_source === 'fallback' ? 'fallback weather' : 'live weather'}` : 'offline'],
        // Live control-loop state, straight from the engine's own endpoints.
        ['Outdoor (envelope)', weather ? `${weather.outdoorC.toFixed(1)} °C · ${weather.live ? 'live Open-Meteo' : 'fallback'}` : '—'],
        ['TOU band now', rateStr(touPeriod()) + '/kWh'],
        ['Pre-cool window', precool?.active ? `open until ${untilLabel(precool.until)}` : 'closed'],
        ['Plug sweep', plugStatus ? (plugStatus.config?.enabled ? (plugStatus.armed ? `armed · ${plugStatus.shedZones} zones swept` : 'disarmed (work hours)') : 'disabled') : '—'],
        ['Plug energy avoided', `${(simData.plugSavedKwh ?? plugStatus?.savedKwh ?? 0).toFixed(1)} kWh`],
      ];
      return (
        <div style={{ marginTop: '6px', display: 'grid', gridTemplateColumns: '1fr auto', rowGap: '4px', columnGap: '10px' }}>
          {rows.map(([k, v]) => (
            <React.Fragment key={k}>
              <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>{k}</span>
              <span style={{ fontSize: '10px', fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--text-primary)', textAlign: 'right' }}>{v}</span>
            </React.Fragment>
          ))}
        </div>
      );
    }
    if (id === 'hardware') {
      // Per-sensor coverage: which of the node's intended sensors is DELIVERING right
      // now. These come from /api/hardware, where each field is freshness-gated —
      // 0/false means "not measuring", never "measuring zero". A lit badge is a live
      // sensor; a dim one is absent, failed, or stale — the honest wiring checklist.
      const sensorBadge = (on, label, title) => (
        <span
          title={title}
          style={{
            fontSize: '8px', fontWeight: 'bold', padding: '1px 4px', borderRadius: '3px', flexShrink: 0,
            color: on ? 'var(--accent-green)' : 'var(--text-muted)',
            border: `1px solid ${on ? 'var(--accent-green)' : 'rgba(127,139,150,0.3)'}`,
            opacity: on ? 1 : 0.55,
          }}
        >
          {label}
        </span>
      );
      return (
        <div style={{ marginTop: '6px', display: 'flex', flexDirection: 'column', gap: '4px' }}>
          {hwList.map((n) => (
            <div
              key={n.zoneId}
              onClick={() => setSelectedZone && setSelectedZone(n.zoneId)}
              title="Click to fly the 3D view to this zone"
              style={{ display: 'flex', alignItems: 'center', gap: '6px', padding: '5px 6px', borderRadius: '4px', background: 'rgba(255,255,255,0.03)', cursor: setSelectedZone ? 'pointer' : 'default', fontFamily: 'monospace', fontSize: '10px', flexWrap: 'wrap' }}
            >
              <span style={{ width: 6, height: 6, borderRadius: '50%', flexShrink: 0, background: n.online ? 'var(--accent-green)' : 'var(--text-muted)', boxShadow: n.online ? '0 0 4px var(--accent-green)' : 'none' }} />
              <span style={{ color: 'var(--text-primary)', fontWeight: 'bold' }}>{(n.source || 'edge').toUpperCase()}</span>
              <span style={{ color: 'var(--text-secondary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', minWidth: '60px' }}>{(n.zoneId || '').replace('zone-', '')}</span>
              {sensorBadge(n.tempPinned, 'T', n.tempPinned ? `temperature ${(n.hwTemp || 0).toFixed(1)}°C measured` : 'no live temperature sensor')}
              {sensorBadge((n.humidity || 0) > 0, 'H', (n.humidity || 0) > 0 ? `humidity ${(n.humidity).toFixed(0)}%RH measured` : 'no live humidity sensor')}
              {sensorBadge((n.co2 || 0) > 0, 'CO₂', (n.co2 || 0) > 0 ? `${Math.round(n.co2)} ppm measured (NDIR)` : 'no live CO₂ sensor')}
              {sensorBadge((n.plugW || 0) > 0, 'W', (n.plugW || 0) > 0 ? `plug circuit ${(n.plugW).toFixed(0)} W measured (SCT-013)` : 'no live power clamp')}
              {n.shadowTemp > 0 && (
                <span title="AFDD residual: |measured − 2R1C shadow model|" style={{ color: n.afddAlert ? 'var(--accent-red)' : 'var(--text-muted)' }}>
                  Δ{(n.residual || 0).toFixed(1)}°
                </span>
              )}
              <span style={{ color: 'var(--text-secondary)' }}>{n.occupancy ?? 0}P</span>
              <span style={{ color: n.lightsOn ? 'var(--accent-green)' : 'var(--text-muted)' }}>{n.lightsOn ? 'LIT' : 'DARK'}</span>
              {n.plugShed && <span style={{ color: 'var(--accent-green)' }}>SWEPT</span>}
            </div>
          ))}
        </div>
      );
    }
    return null;
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '16px', animation: 'fadeIn 0.5s ease-out' }}>

      {/* Header Section */}
      <div style={{ paddingBottom: '16px', borderBottom: '1px solid var(--border-glass)' }}>
        <h3 style={{ margin: '0 0 8px 0', fontSize: '14px', display: 'flex', alignItems: 'center', gap: '8px', color: 'var(--text-primary)' }}>
          <Brain size={18} color="var(--accent-blue)" /> AI Operations Engine
        </h3>
        <p style={{ margin: 0, fontSize: '11px', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
          Learned-baseline anomaly detection — each signal scored against this building’s own normal for the hour — plus the LSTM load forecast. Total building load is currently at <span style={{ color: 'var(--text-primary)', fontWeight: 'bold' }}>{simData.buildingLoadMw?.toFixed(2)} MW</span>.
          {recModel && (
            <span style={{ display: 'block', marginTop: '4px', color: 'var(--text-muted)', fontSize: '10px' }}>
              Baseline model: {recModel.established} signal{recModel.established === 1 ? '' : 's'} established, {recModel.learning} still learning (a bucket matures after {recModel.matureAfter} samples).
            </span>
          )}
        </p>
      </div>

      {/* Insight Cards */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        {insights.map((insight, idx) => {
          let bg, border, titleColor;
          switch (insight.type) {
            case 'critical':
              bg = 'rgba(239, 68, 68, 0.05)';
              border = 'rgba(239, 68, 68, 0.3)';
              titleColor = 'var(--accent-red)';
              break;
            case 'warning':
              bg = 'rgba(234, 179, 8, 0.05)';
              border = 'rgba(234, 179, 8, 0.3)';
              titleColor = 'var(--accent-yellow)';
              break;
            case 'success':
              bg = 'rgba(34, 197, 94, 0.05)';
              border = 'rgba(34, 197, 94, 0.3)';
              titleColor = 'var(--accent-green)';
              break;
            case 'info':
            default:
              bg = 'rgba(0, 163, 224, 0.05)';
              border = 'rgba(0, 163, 224, 0.3)';
              titleColor = 'var(--accent-blue)';
              break;
          }

          const isExpanded = !!expanded[insight.id];

          return (
            <div
              key={insight.id}
              style={{
                background: bg,
                border: `1px solid ${border}`,
                borderRadius: '10px',
                padding: '14px',
                display: 'flex',
                flexDirection: 'column',
                gap: '8px',
                position: 'relative',
                overflow: 'hidden',
                animation: `slideInRight 0.4s ease-out ${idx * 0.1}s backwards`
              }}
            >
              {/* Decorative side accent */}
              <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: '4px', background: titleColor }} />

              <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                <div style={{ padding: '6px', background: 'rgba(255,255,255,0.05)', borderRadius: '6px', display: 'flex' }}>
                  {insight.icon}
                </div>
                <span style={{ fontSize: '12px', fontWeight: 'bold', color: titleColor }}>
                  {insight.title}
                </span>
                {insight.badge && (
                  <span
                    title={insight.badge === 'LEARNED' ? "Scored against this zone's learned normal for the hour" : 'Recognized fixed standard (baseline still learning this zone)'}
                    style={{
                      marginLeft: 'auto', fontSize: '8px', fontWeight: 'bold', letterSpacing: '0.05em',
                      padding: '1px 5px', borderRadius: '3px', flexShrink: 0,
                      color: insight.badgeColor || 'var(--text-muted)',
                      border: `1px solid ${insight.badgeColor || 'var(--text-muted)'}`,
                    }}
                  >
                    {insight.badge}
                  </span>
                )}
              </div>

              <p style={{ margin: '4px 0 0 0', fontSize: '11px', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
                {insight.message}
              </p>

              {insight.expandable && isExpanded && (
                insight.id === 'afdd'
                  ? <AfddDriftDetail zoneId={insight.afddZone} />
                  : renderDetail(insight.id)
              )}

              {insight.action && (() => {
                // Expandables toggle inline detail; the rest run their REAL action —
                // an override, a window, a navigation. `once` actions latch as engaged;
                // `done` reflects engine state (e.g. a pre-cool window already open).
                const settled = insight.done || (insight.once && !!engaged[insight.id]);
                const label = insight.expandable
                  ? (isExpanded ? '▴ COLLAPSE' : insight.action)
                  : insight.done ? insight.doneLabel
                  : (insight.once && engaged[insight.id]) ? '✓ ENGAGED'
                  : insight.action;
                return (
                  <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '4px' }}>
                    <button
                      onClick={() => {
                        if (insight.expandable) return toggle(insight.id);
                        if (settled) return;
                        insight.onAction && insight.onAction();
                        if (insight.once) setEngaged((e) => ({ ...e, [insight.id]: true }));
                      }}
                      disabled={!insight.expandable && settled}
                      style={{
                        background: !insight.expandable && settled ? titleColor : 'transparent',
                        border: `1px solid ${border}`,
                        color: !insight.expandable && settled ? '#000' : titleColor,
                        padding: '6px 12px',
                        borderRadius: '4px',
                        fontSize: '10px',
                        fontWeight: 'bold',
                        cursor: !insight.expandable && settled ? 'default' : 'pointer',
                        transition: 'all 0.2s ease',
                      }}
                      onMouseOver={(e) => { if (insight.expandable || !settled) { e.currentTarget.style.background = titleColor; e.currentTarget.style.color = '#000'; } }}
                      onMouseOut={(e) => { if (insight.expandable || !settled) { e.currentTarget.style.background = 'transparent'; e.currentTarget.style.color = titleColor; } }}
                    >
                      {label}
                    </button>
                  </div>
                );
              })()}
            </div>
          );
        })}
      </div>

      {/* Take the intelligence offline: download the learned models + recommender. */}
      <ModelExportCard />

      {/* Embedded CSS for animations */}
      <style>{`
        @keyframes slideInRight {
          from { opacity: 0; transform: translateX(20px); }
          to { opacity: 1; transform: translateX(0); }
        }
        @keyframes fadeIn {
          from { opacity: 0; }
          to { opacity: 1; }
        }
      `}</style>
    </div>
  );
}

// ModelExportCard is the "take the intelligence with you" surface: it downloads the
// learned baseline model, the LSTM forecaster artifacts, and a dependency-free recommender
// as one zip (GET /api/model/export), so an operator can run the SAME σ-scored
// recommendations and alerts offline from the twin's own processed state — no server
// required. It reads /api/model for the model's live maturity so the card is honest about
// what the download will actually be able to do.
function ModelExportCard() {
  const [info, setInfo] = useState(null);
  useEffect(() => {
    let alive = true;
    fetch(`${API_BASE}/api/model`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => { if (alive) setInfo(d); })
      .catch(() => {});
    return () => { alive = false; };
  }, []);

  const est = info?.baseline?.established ?? 0;
  const learning = info?.baseline?.learning ?? 0;
  const forecaster = info?.forecaster;
  const rows = [
    ['Learned baseline', `${est} signal${est === 1 ? '' : 's'} established · ${learning} learning`],
    ['LSTM forecaster', forecaster ? (forecaster.ready ? 'trained · included' : forecaster.reachable ? 'reachable · not yet trained' : 'offline · omitted') : '—'],
    ['Recommender', 'recommender.py · Python 3 stdlib only'],
  ];

  return (
    <div style={{ marginTop: '4px', background: 'rgba(0,163,224,0.05)', border: '1px solid rgba(0,163,224,0.3)', borderRadius: '10px', padding: '14px', position: 'relative', overflow: 'hidden' }}>
      <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: '4px', background: 'var(--accent-blue)' }} />
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' }}>
        <div style={{ padding: '6px', background: 'rgba(255,255,255,0.05)', borderRadius: '6px', display: 'flex' }}>
          <Cpu size={18} color="var(--accent-blue)" />
        </div>
        <span style={{ fontSize: '12px', fontWeight: 'bold', color: 'var(--accent-blue)' }}>Local Models</span>
      </div>
      <p style={{ margin: '0 0 8px 0', fontSize: '11px', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
        Download the learned baseline model, the LSTM forecaster, and a dependency-free recommender as one package — run the same recommendations and alerts offline, on your own machine, from the twin’s own processed state.
      </p>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', rowGap: '3px', columnGap: '10px', marginBottom: '10px' }}>
        {rows.map(([k, v]) => (
          <React.Fragment key={k}>
            <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>{k}</span>
            <span style={{ fontSize: '10px', fontFamily: 'monospace', color: 'var(--text-primary)', textAlign: 'right' }}>{v}</span>
          </React.Fragment>
        ))}
      </div>
      <a
        href={`${API_BASE}/api/model/export`}
        download
        style={{
          display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '8px',
          padding: '10px', borderRadius: '6px', textDecoration: 'none',
          background: 'var(--accent-blue)', color: '#000', fontSize: '11px', fontWeight: 'bold', letterSpacing: '0.02em',
        }}
      >
        <Download size={14} /> DOWNLOAD MODEL BUNDLE (.zip)
      </a>
    </div>
  );
}

// AfddDriftDetail pulls the zone's persisted AFDD residual from TimescaleDB
// (/api/series) and charts it — the maintenance evidence behind a physics-divergence
// alert. A residual that has been climbing for an hour is a developing fault; a spike
// that just appeared is worth a second look before dispatching anyone. The 2.0 °C
// reference line is the engine's own afddThreshold (the level that raised this card).
function AfddDriftDetail({ zoneId }) {
  const [series, setSeries] = useState(null); // null = loading, [] = no history yet
  useEffect(() => {
    let alive = true;
    fetch(`${API_BASE}/api/series?zone=${encodeURIComponent(zoneId)}&metric=afddResidual&minutes=120`)
      .then((r) => (r.ok ? r.json() : []))
      .then((d) => { if (alive) setSeries(Array.isArray(d) ? d : []); })
      .catch(() => { if (alive) setSeries([]); });
    return () => { alive = false; };
  }, [zoneId]);

  if (series === null) {
    return <div style={{ marginTop: '6px', fontSize: '10px', color: 'var(--text-muted)' }}>Loading residual history…</div>;
  }
  if (series.length === 0) {
    return (
      <div style={{ marginTop: '6px', fontSize: '10px', color: 'var(--text-muted)' }}>
        No persisted residual yet — history begins once TimescaleDB has logged this sensor-bound zone (≈1 Hz). The live residual is on the node badge above.
      </div>
    );
  }
  const data = series.map((p) => ({ t: p.t.slice(11, 16), r: +p.v.toFixed(2) }));
  const peak = data.reduce((m, d) => Math.max(m, d.r), 0);
  return (
    <div style={{ marginTop: '6px' }}>
      <div style={{ fontSize: '9px', color: 'var(--text-muted)', marginBottom: '4px', letterSpacing: '0.04em' }}>
        AFDD RESIDUAL · |MEASURED − 2R1C MODEL| · LAST 2H · PEAK {peak.toFixed(1)}°C
      </div>
      <div style={{ width: '100%', height: 120 }}>
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={data} margin={{ top: 6, right: 8, bottom: 0, left: -22 }}>
            <XAxis dataKey="t" tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={{ stroke: 'rgba(255,255,255,0.1)' }} interval="preserveStartEnd" minTickGap={40} />
            <YAxis tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={false} domain={[0, 'auto']} />
            <Tooltip contentStyle={{ background: 'rgba(10,10,10,0.95)', border: '1px solid var(--border-glass)', borderRadius: 6, fontSize: 10 }} labelStyle={{ color: 'var(--text-secondary)' }} formatter={(v) => [`${v}°C`, 'residual']} />
            <ReferenceLine y={2.0} stroke="var(--accent-red)" strokeDasharray="4 4" label={{ value: 'FAULT', fontSize: 8, fill: 'var(--accent-red)', position: 'insideTopRight' }} />
            <Line type="monotone" dataKey="r" stroke="var(--accent-red)" strokeWidth={2} dot={false} isAnimationActive={false} />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
