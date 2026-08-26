import React, { useMemo, useState, useEffect } from 'react';
import { Brain, Zap, AlertTriangle, TrendingDown, ThermometerSnowflake, Activity, Radio, Plug, CloudOff, Wind, WifiOff, Download, Cpu, Clock } from 'lucide-react';
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, ReferenceLine } from 'recharts';
import { money, energyCostPerDay, peakShiftSavingPerMonth, rateStr, touPeriod, minutesToPeak, TARIFF } from './tariff';
import { useOpsStatus, untilLabel } from './useOpsStatus';
import { usePlugs } from './usePlugs';
import { useRecommendations } from './useRecommendations';
import { useLocalModel } from './useLocalModel';
import { useForecastCompare } from './useForecastCompare';
import { useLibrary } from './useLibrary';
import { useRoomModels } from './useRoomModels';
import RecommendationEvidence from './RecommendationEvidence';
import { API_BASE } from './api';
import { powerMw } from './units';

// fmtEta renders a predicted time-to-breach the way an operator reads it.
function fmtEta(sec) {
  if (!sec || sec <= 0) return '';
  return sec >= 5400 ? `${(sec / 3600).toFixed(1)}h` : `${Math.round(sec / 60)}min`;
}

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
  // Building coefficients and the critical-zone list, from the engine's programme library
  // rather than from constants typed into this file. See useLibrary.
  const { isCritical, precoolShift, calibrated: libCalibrated } = useLibrary();
  // What the twin has actually identified about each room — the model behind every
  // prediction, so a card can show its own reasoning instead of asserting a conclusion.
  // matureAfter is deliberately taken from THIS endpoint, not from the baseline model's:
  // the two models mature on different evidence and at different counts (baselines at 20
  // observations, room identification at 36 accepted samples), so borrowing one figure for
  // the other's progress bar would report a room as further along than it is.
  const { byZone: roomModels, identified: roomsIdentified, rooms: roomList, matureAfter: roomsMatureAfter } = useRoomModels();

  const hwList = Object.values(hardwareNodes || {});
  const hwOnline = hwList.filter((n) => n.online).length;

  // Real streamed savings (engine energySavedMw), as a share of what load WOULD be without setbacks.
  const savedMw = simData.energySavedMw || 0;
  const loadMw = simData.buildingLoadMw || 0;
  const savingsPct = savedMw + loadMw > 0 ? (100 * savedMw) / (savedMw + loadMw) : 0;

  // The forecast card plots the series that EXISTS, and nothing else.
  //
  // The supervised LSTM returns one scalar — its predicted peak. It has no trajectory to
  // give. This card used to synthesize one anyway: thirteen points eased between the live
  // load and that peak with a smoothstep, captioned "PROJECTED RAMP", with nothing on the
  // chart to say the shape was a rendering artifact rather than a forecast. The zero-shot
  // forecaster does return a full horizon, so when it has answered that real series is
  // drawn; when it has not, the card shows the two numbers it actually has.
  const { timesfm, lstm: lstmCompare, series: tfmSeries, stepMinutes, agreement,
          upperBand, upperQuantile, peakUpperMw } = useForecastCompare();
  // Central path plus the zero-shot model's own upper decile, so the chart shows how wide
  // the forecast is rather than implying a single confident line.
  const forecastSeries = useMemo(() => {
    if (!tfmSeries) return [];
    return tfmSeries.map((mw, i) => ({
      t: `+${(i + 1) * stepMinutes}m`,
      mw: +Number(mw).toFixed(4),
      hi: upperBand && upperBand[i] != null ? +Number(upperBand[i]).toFixed(4) : undefined,
    }));
  }, [tfmSeries, upperBand, stepMinutes]);

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
    // Three kinds of judgement, badged distinctly so a forecast never reads as a
    // present-tense fact. "anomaly" = the baselines say this is far from normal right now.
    // "prediction" = this room's own identified physics says it breaches in N minutes.
    // "capability" = its learned cooling authority cannot hold setpoint at all.
    const recBadge = (rec) => {
      if (rec.kind === 'prediction') {
        return { text: rec.etaSec > 0 ? `PREDICTED · ${fmtEta(rec.etaSec)}` : 'PREDICTED', color: 'var(--accent-yellow)',
          title: "Forward prediction from this room's identified thermal/CO₂ response, not a threshold" };
      }
      if (rec.kind === 'capability') {
        return { text: 'CAPABILITY', color: 'var(--accent-red)',
          title: "Learned cooling authority cannot hold this room's setpoint even at full flow" };
      }
      return rec.basis === 'learned'
        ? { text: 'LEARNED', color: 'var(--accent-blue)', title: "Scored against this zone's learned normal for the hour" }
        : { text: 'ASHRAE STD', color: 'var(--text-muted)', title: 'Recognized fixed standard (baseline still learning this zone)' };
    };
    recommendations.forEach((rec) => {
      const color = rec.severity === 'critical' ? 'var(--accent-red)' : rec.severity === 'warning' ? 'var(--accent-yellow)' : 'var(--accent-blue)';
      const label = recActionLabel[rec.action];
      const badge = recBadge(rec);
      generated.push({
        id: `rec-${rec.id}`,
        type: rec.severity,
        icon: rec.kind === 'prediction' ? <Clock size={18} color={color} /> : recIcon(rec.metric, color),
        title: rec.title,
        message: rec.message,
        badge: badge.text,
        badgeColor: badge.color,
        badgeTitle: badge.title,
        // The engine ships the reasoning — learned mean and σ, sample count, the hour
        // bucket that answered, the predicted and settling values, the identified time
        // constant. Carry it through so the card can be opened and checked instead of
        // being taken on faith.
        rec,
        action: label,
        // A card with a real remediation gets the action button AND an evidence toggle;
        // an advisory-only card gets the toggle alone.
        evidence: true,
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
      // The shift fraction is a BUILDING coefficient, so it comes from the engine's
      // programme library (physics.precoolShiftFraction) rather than a literal here. It
      // had been typed in three files with three different justifications — "5% coast
      // from a charged thermal mass" here, "~5%-per-°C deadband shave" on mobile — which
      // is how you end up unable to say what the number means. Null until the library
      // arrives, and the card then states the rate without pricing a shift it cannot size.
      const shedKw = precoolShift != null ? precoolShift * hvacMw * 1000 : null;
      const windowOpen = !!precool?.active;
      generated.push({
        id: 'peak',
        type: tou === 'peak' ? 'warning' : 'info',
        icon: <TrendingDown size={18} color={tou === 'peak' ? 'var(--accent-yellow)' : 'var(--accent-blue)'} />,
        title: tou === 'peak' ? 'Peak Tariff Running Now' : `Peak Tariff in ${toPeak} min`,
        message: `${tou === 'peak' ? 'The 17:30–22:30 peak window is charging ' + rateStr('peak') + '/kWh right now.' : `Peak rate (${rateStr('peak')}/kWh vs ${rateStr('normal')} normal) begins at 17:30.`} ${
          windowOpen
            ? `A pre-cool window is OPEN until ${untilLabel(precool.until)} — thermal mass is charging so chillers can coast.`
            : shedKw != null
              ? `Pre-cooling now charges the thermal mass at the cheaper rate — shifting an estimated ${shedKw.toFixed(0)} kW off peak, worth roughly ${money(peakShiftSavingPerMonth(shedKw))}/month at the rate gap. The shift fraction is the library's ${(precoolShift * 100).toFixed(0)}% planning estimate, not a measured coast.`
              : 'Pre-cooling now charges the thermal mass at the cheaper rate. The size of the shift is not shown: it depends on the plant coefficient in the engine\'s programme library, which this dashboard has not been able to read.'
        }`,
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
      // A forecast the engine has judged out of distribution is not reported as this
      // building's coming peak. The LSTM is supervised, so its weights encode whichever
      // building train.py last saw; pointed at a different one it answers with that
      // building's numbers and nothing in the arithmetic objects. The engine checks each
      // answer against this building's own observed load range and says so, and the card
      // leads with the finding rather than burying it under a confident MW figure.
      const ood = aiForecast.implausible === true;
      generated.push({
        id: 'forecast',
        type: ood ? 'warning' : 'info',
        expandable: true,
        icon: <Activity size={18} color={ood ? 'var(--accent-yellow)' : 'var(--accent-blue)'} />,
        title: ood ? 'LSTM Forecast Out Of Distribution' : 'LSTM Load Forecast',
        badge: ood ? 'NOT THIS BUILDING' : undefined,
        badgeColor: ood ? 'var(--accent-yellow)' : undefined,
        badgeTitle: ood ? "Checked against this building's own recorded load range" : undefined,
        message: ood
          ? `The supervised model returns ${aiForecast.predicted_peak_load.toFixed(2)} MW, which the engine has flagged: ${aiForecast.plausibility} Retrain it on this building (backend/forecasting/train.py) or read the zero-shot forecaster instead — it needs no training and forecasts this building's own recorded series.`
          : `Deep Learning model predicts an upcoming peak load of ${aiForecast.predicted_peak_load.toFixed(2)} MW over the sampled last hour. ${weatherNote}${warmup}`,
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
    // 24/7-critical types are excluded via the ENGINE's programme library, not a list
    // typed here. The list that used to live here named `server-room` and `mechanical` —
    // types this digitizer has not minted since it began emitting `comms-room` and
    // `plant-room` — so on the current fixture it excluded nothing and this card counted
    // the comms room as waste and priced shutting it down as a saving.
    //
    // While the library has not arrived isCritical returns null, and the card is withheld
    // rather than shown with an exclusion that may be wrong.
    const wastingZones = isCritical(zones[0]?.type) === null ? [] : zones.filter((z) =>
      z.occupancy === 0 && z.load > 0 && z.lightsOn !== false && !isCritical(z.type));
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
  }, [simData, activeScenario, faultTarget, aiForecast, hwList, hwOnline, savingsPct, savedMw, loadMw, precool, weather, plugStatus, recommendations, sendManualOverride, setSelectedZone, onOpenPlugs, isCritical, precoolShift]);

  // ---- Inline detail sections for the expandable cards ----
  const renderDetail = (id) => {
    if (id === 'forecast') {
      // An out-of-distribution LSTM peak is not drawn as a reference line on the chart or
      // subtracted from the live load as "headroom": both would state that this building
      // is heading for a number the engine has already said belongs to a different one.
      const lstmOod = aiForecast?.implausible === true;
      const peak = lstmOod ? null : aiForecast?.predicted_peak_load;
      // No horizon to draw. Show what the LSTM actually produced — two numbers — rather
      // than a curve interpolated between them.
      if (!forecastSeries.length) {
        const delta = peak != null ? peak - loadMw : null;
        return (
          <div style={{ marginTop: '6px' }}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', rowGap: '4px', columnGap: '10px' }}>
              <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>Live load</span>
              <span style={{ fontSize: '10px', fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--text-primary)', textAlign: 'right' }}>{powerMw(loadMw)}</span>
              <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>LSTM predicted peak</span>
              <span style={{ fontSize: '10px', fontFamily: 'monospace', fontWeight: 'bold', color: lstmOod ? 'var(--accent-yellow)' : 'var(--accent-blue)', textAlign: 'right' }}>
                {peak != null ? `${peak.toFixed(2)} MW`
                  : lstmOod ? `${aiForecast.predicted_peak_load.toFixed(2)} MW · out of distribution`
                  : '—'}
              </span>
              {delta != null && (
                <>
                  <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>Headroom to peak</span>
                  <span style={{ fontSize: '10px', fontFamily: 'monospace', fontWeight: 'bold', color: delta > 0 ? 'var(--accent-yellow)' : 'var(--accent-green)', textAlign: 'right' }}>{delta >= 0 ? '+' : ''}{delta.toFixed(2)} MW</span>
                </>
              )}
              <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>Zero-shot horizon</span>
              <span style={{ fontSize: '10px', fontFamily: 'monospace', color: 'var(--text-muted)', textAlign: 'right' }}>
                {timesfm ? (timesfm.available ? 'loading…' : 'unavailable') : '—'}
              </span>
            </div>
            <p style={{ margin: '6px 0 0 0', fontSize: '9px', color: 'var(--text-muted)', lineHeight: 1.45 }}>
              The supervised LSTM emits a predicted peak, not a trajectory, so there is no curve to plot.
              {timesfm && !timesfm.available && timesfm.error ? ` The zero-shot forecaster, which does return a full horizon, is unavailable: ${timesfm.error}` : ' Start the zero-shot forecaster to see the shape of the coming hour.'}
            </p>
          </div>
        );
      }
      const peakOfSeries = forecastSeries.reduce((m, d) => Math.max(m, d.mw), 0);
      return (
        <div style={{ marginTop: '6px' }}>
          <div style={{ fontSize: '9px', color: 'var(--text-muted)', marginBottom: '4px', letterSpacing: '0.04em' }}>
            TIMESFM HORIZON · {forecastSeries.length} × {stepMinutes} MIN · PEAK {peakOfSeries.toFixed(3)} MW
            {peakUpperMw != null && ` · ${upperQuantile?.toUpperCase()} ${Number(peakUpperMw).toFixed(3)} MW`}
          </div>
          <div style={{ width: '100%', height: 120 }}>
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={forecastSeries} margin={{ top: 6, right: 8, bottom: 0, left: -22 }}>
                <XAxis dataKey="t" tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={{ stroke: 'rgba(255,255,255,0.1)' }} interval={3} />
                <YAxis tick={{ fontSize: 8, fill: 'var(--text-muted)' }} tickLine={false} axisLine={false} domain={['auto', 'auto']} />
                <Tooltip contentStyle={{ background: 'rgba(10,10,10,0.95)', border: '1px solid var(--border-glass)', borderRadius: 6, fontSize: 10 }} labelStyle={{ color: 'var(--text-secondary)' }} formatter={(v) => [`${v} MW`, 'load']} />
                {peak != null && <ReferenceLine y={peak} stroke="var(--accent-red)" strokeDasharray="4 4" label={{ value: 'LSTM PEAK', fontSize: 8, fill: 'var(--accent-red)', position: 'insideTopRight' }} />}
                {upperBand && (
                  <Line type="monotone" dataKey="hi" stroke="var(--accent-blue)" strokeWidth={1} strokeDasharray="3 3" strokeOpacity={0.6} dot={false} isAnimationActive={false} />
                )}
                <Line type="monotone" dataKey="mw" stroke="var(--accent-blue)" strokeWidth={2} dot={false} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
          {/* Both forecasters, side by side. Their disagreement is the useful signal: two
              models built on entirely different evidence converging is worth more than
              either one alone, and their diverging is worth knowing before acting. */}
          <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', rowGap: '3px', columnGap: '10px', marginTop: '6px' }}>
            <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>Blue line — TimesFM (zero-shot)</span>
            <span style={{ fontSize: '10px', fontFamily: 'monospace', color: 'var(--accent-blue)', textAlign: 'right' }}>{timesfm?.peakMw != null ? `${timesfm.peakMw.toFixed(3)} MW peak` : '—'}</span>
            {peakUpperMw != null && (
              <>
                <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }} title="The zero-shot model's own upper decile — how firm the peak is, not just where it sits">
                  Dashed — its {upperQuantile} band
                </span>
                <span style={{ fontSize: '10px', fontFamily: 'monospace', color: 'var(--text-muted)', textAlign: 'right' }}>
                  {Number(peakUpperMw).toFixed(3)} MW
                </span>
              </>
            )}
            <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>{lstmOod ? 'LSTM (supervised)' : 'Red line — LSTM (supervised)'}</span>
            <span style={{ fontSize: '10px', fontFamily: 'monospace', color: lstmOod ? 'var(--accent-yellow)' : 'var(--accent-red)', textAlign: 'right' }}>
              {lstmOod ? `${aiForecast.predicted_peak_load.toFixed(2)} MW · not this building`
                : lstmCompare?.peakMw != null ? `${lstmCompare.peakMw.toFixed(2)} MW peak`
                : peak != null ? `${peak.toFixed(2)} MW peak` : '—'}
            </span>
            {agreement?.comparable && (
              <>
                <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>They differ by</span>
                <span style={{
                  fontSize: '10px', fontFamily: 'monospace', textAlign: 'right',
                  color: agreement.relativeDiff > 0.25 ? 'var(--accent-yellow)' : 'var(--text-primary)',
                }}>
                  {Math.abs(agreement.deltaMw).toFixed(2)} MW ({(agreement.relativeDiff * 100).toFixed(0)}%) · {agreement.higher.toUpperCase()} higher
                </span>
              </>
            )}
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
        ['Cooling delivered', `${powerMw(simData.coolingOutputMw || 0)} thermal`],
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
          Two learned models: baselines score each signal against this building’s own normal for the hour, and every room’s identified physics predicts where it is heading. Total building load is currently at <span style={{ color: 'var(--text-primary)', fontWeight: 'bold' }}>{powerMw(simData.buildingLoadMw || 0)}</span>.
          {recModel && (
            <span style={{ display: 'block', marginTop: '4px', color: 'var(--text-muted)', fontSize: '10px' }}>
              Baselines: {recModel.established} signal{recModel.established === 1 ? '' : 's'} established, {recModel.learning} learning (matures after {recModel.matureAfter} samples).
              {typeof recModel.roomsIdentified === 'number' && (
                <> · Room models: {recModel.roomsIdentified} identified, {recModel.roomsLearning} learning — predicting {recModel.horizonMin} min ahead.</>
              )}
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
                    title={insight.badgeTitle || (insight.badge === 'LEARNED' ? "Scored against this zone's learned normal for the hour" : 'Recognized fixed standard (baseline still learning this zone)')}
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

              {/* The learned/predicted cards carry their own evidence: the σ position
                  against the learned band, the identified response curve, the sample
                  counts, and the supply-air provenance behind the fit. */}
              {insight.evidence && isExpanded && (
                <RecommendationEvidence
                  rec={insight.rec}
                  model={roomModels[insight.rec.zone]}
                  matureAfter={recModel?.matureAfter}
                  horizonMin={recModel?.horizonMin ?? 30}
                  limit={insight.rec.metric === 'temp'
                    ? (simData.zones?.[insight.rec.zone]?.setpoint ?? 0) + 1
                    : insight.rec.metric === 'co2' ? 1000 : undefined}
                />
              )}

              {/* Evidence toggle. A card with a real remediation keeps its action button
                  and gains this beside it, so opening the reasoning is never a choice
                  between reading why and being able to act. */}
              {insight.evidence && (
                <div style={{ display: 'flex', justifyContent: 'flex-start', marginTop: '2px' }}>
                  <button
                    onClick={() => toggle(insight.id)}
                    style={{ background: 'transparent', border: 'none', color: 'var(--text-muted)', fontSize: '10px', cursor: 'pointer', padding: 0, textDecoration: 'underline' }}
                  >
                    {isExpanded ? '▴ hide the evidence' : '▾ why this fired'}
                  </button>
                </div>
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

      {/* What the twin has actually identified. This is the reasoning layer the panel was
          asserting conclusions from without ever showing. */}
      <RoomModelsCard rooms={roomList} identified={roomsIdentified} matureAfter={roomsMatureAfter} learning={recModel?.roomsLearning ?? 0} setSelectedZone={setSelectedZone} />

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
  const { profile, rec, tier, selected, setOverride, exportUrl, error } = useLocalModel();
  const [showTiers, setShowTiers] = useState(false);

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
  const roomsId = info?.rooms?.identified ?? 0;
  const roomsLearning = info?.rooms?.learning ?? 0;
  const forecaster = info?.forecaster;

  // What this machine looks like, in the browser's own (limited) terms. Reported exactly
  // as measured — an unavailable value says so rather than showing a plausible guess.
  const machine = rec
    ? [
        `${rec.profile.cores || '?'} cores`,
        rec.memoryBasis === 'reported' ? `${rec.effectiveMemoryGb} GB` : `~${rec.effectiveMemoryGb} GB est.`,
        // gpu.name is the product on its own ("Apple M4"); gpu.label is the raw WebGL
        // renderer string, which browsers wrap in ANGLE boilerplate and which reads as
        // noise in a summary line.
        rec.gpu?.name || rec.gpu?.label || 'no GPU detected',
      ].join(' · ')
    : 'measuring…';

  const rows = [
    ['Learned baselines', `${est} signal${est === 1 ? '' : 's'} established · ${learning} learning`],
    ['Room models', `${roomsId} identified · ${roomsLearning} learning`],
    ['LSTM forecaster', forecaster ? (forecaster.ready ? 'trained · included' : forecaster.reachable ? 'reachable · not yet trained' : 'offline · omitted') : '—'],
    ['This machine', machine],
  ];

  return (
    <div style={{ marginTop: '4px', background: 'rgba(0,163,224,0.05)', border: '1px solid rgba(0,163,224,0.3)', borderRadius: '10px', padding: '14px', position: 'relative', overflow: 'hidden' }}>
      <div style={{ position: 'absolute', left: 0, top: 0, bottom: 0, width: '4px', background: 'var(--accent-blue)' }} />
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' }}>
        <div style={{ padding: '6px', background: 'rgba(255,255,255,0.05)', borderRadius: '6px', display: 'flex' }}>
          <Cpu size={18} color="var(--accent-blue)" />
        </div>
        <span style={{ fontSize: '12px', fontWeight: 'bold', color: 'var(--accent-blue)' }}>Local Models</span>
        {tier && (
          <span style={{ marginLeft: 'auto', fontSize: '8px', fontWeight: 'bold', letterSpacing: '0.05em', padding: '1px 5px', borderRadius: '3px', color: 'var(--accent-green)', border: '1px solid var(--accent-green)' }}>
            MATCHED TO THIS MACHINE
          </span>
        )}
      </div>

      <p style={{ margin: '0 0 8px 0', fontSize: '11px', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
        Take the intelligence offline: the learned baselines, every room’s identified physical model, and a runtime that reproduces the same predictions on your own machine — no server, no network.
      </p>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', rowGap: '3px', columnGap: '10px', marginBottom: '10px' }}>
        {rows.map(([k, v]) => (
          <React.Fragment key={k}>
            <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>{k}</span>
            <span style={{ fontSize: '10px', fontFamily: 'monospace', color: 'var(--text-primary)', textAlign: 'right' }}>{v}</span>
          </React.Fragment>
        ))}
      </div>

      {/* The recommendation, with its reasoning — a pick the operator can overrule. */}
      {tier && (
        <div style={{ background: 'rgba(255,255,255,0.03)', border: '1px solid var(--border-glass)', borderRadius: '6px', padding: '10px', marginBottom: '10px' }}>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: '6px', marginBottom: '4px' }}>
            <span style={{ fontSize: '11px', fontWeight: 'bold', color: 'var(--text-primary)' }}>{tier.name}</span>
            <span style={{ fontSize: '9px', color: 'var(--text-muted)', fontFamily: 'monospace' }}>
              {tier.approxSizeMb >= 1024 ? `${(tier.approxSizeMb / 1024).toFixed(1)} GB` : `${tier.approxSizeMb} MB`} · {tier.runtime}
            </span>
          </div>
          <p style={{ margin: '0 0 6px 0', fontSize: '10px', color: 'var(--text-secondary)', lineHeight: 1.5 }}>{tier.summary}</p>
          {rec?.rationale && selected === rec.recommended && (
            <p style={{ margin: '0 0 6px 0', fontSize: '10px', color: 'var(--accent-green)', lineHeight: 1.45 }}>
              ✓ {rec.rationale}
            </p>
          )}
          {!tier.fits && (tier.blockers || []).map((b) => (
            <p key={b} style={{ margin: '0 0 3px 0', fontSize: '10px', color: 'var(--accent-yellow)', lineHeight: 1.45 }}>⚠ {b}</p>
          ))}
          <ul style={{ margin: '4px 0 0 0', paddingLeft: '14px' }}>
            {(tier.capabilities || []).map((c) => (
              <li key={c} style={{ fontSize: '10px', color: 'var(--text-muted)', lineHeight: 1.5 }}>{c}</li>
            ))}
          </ul>
          {(rec?.notes || []).map((n) => (
            <p key={n} style={{ margin: '6px 0 0 0', fontSize: '9px', color: 'var(--text-muted)', lineHeight: 1.45, fontStyle: 'italic' }}>{n}</p>
          ))}
        </div>
      )}

      {error && (
        <p style={{ margin: '0 0 8px 0', fontSize: '10px', color: 'var(--accent-yellow)' }}>
          Could not size the bundle to this machine ({error}) — the default package is still available.
        </p>
      )}

      {/* Overrule the pick: the server reasons from what the browser admits to, and a
          machine that reports nothing is exactly the one whose owner knows better. */}
      {rec?.tiers?.length > 0 && (
        <div style={{ marginBottom: '10px' }}>
          <button
            onClick={() => setShowTiers((v) => !v)}
            style={{ background: 'transparent', border: 'none', color: 'var(--text-muted)', fontSize: '10px', cursor: 'pointer', padding: 0, textDecoration: 'underline' }}
          >
            {showTiers ? '▴ hide other tiers' : '▾ choose a different tier'}
          </button>
          {showTiers && (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '4px', marginTop: '6px' }}>
              {rec.tiers.map((t) => (
                <button
                  key={t.id}
                  onClick={() => setOverride(t.id)}
                  style={{
                    textAlign: 'left', cursor: 'pointer', borderRadius: '4px', padding: '6px 8px',
                    background: t.id === selected ? 'rgba(0,163,224,0.15)' : 'transparent',
                    border: `1px solid ${t.id === selected ? 'var(--accent-blue)' : 'var(--border-glass)'}`,
                    color: t.fits ? 'var(--text-primary)' : 'var(--text-muted)', fontSize: '10px',
                  }}
                >
                  {t.recommended ? '★ ' : ''}{t.name}
                  <span style={{ color: 'var(--text-muted)', fontFamily: 'monospace' }}>
                    {' '}· {t.approxSizeMb >= 1024 ? `${(t.approxSizeMb / 1024).toFixed(1)} GB` : `${t.approxSizeMb} MB`}
                    {!t.fits ? ' · exceeds this machine' : ''}
                  </span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}

      <a
        href={exportUrl}
        download
        style={{
          display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '8px',
          padding: '10px', borderRadius: '6px', textDecoration: 'none',
          background: 'var(--accent-blue)', color: '#000', fontSize: '11px', fontWeight: 'bold', letterSpacing: '0.02em',
        }}
      >
        <Download size={14} /> DOWNLOAD {tier ? tier.name.toUpperCase() : 'MODEL BUNDLE'} (.zip)
      </a>
      {rec?.workers > 1 && (
        <p style={{ margin: '6px 0 0 0', fontSize: '9px', color: 'var(--text-muted)', textAlign: 'center' }}>
          Bundle is configured for {rec.workers} parallel workers on this machine.
        </p>
      )}
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


// RoomModelsCard renders the identified physics behind every prediction this panel makes.
//
// /api/rooms/models has shipped complete and correct with no consumer at all, which meant
// the twin's genuine differentiator — that it recovers each room's own thermal and
// ventilation constants from that room's own history — was invisible in the product built
// on top of it. The panel could say "this room breaches in 14 minutes" but could not show
// the time constant that produced the number, how many samples backed it, or how closely
// the fit reproduces what the room then did.
//
// The provenance column is the point. A cooling authority identified entirely against the
// library's DESIGN supply temperature is still a real fit, but it inherits that
// assumption's error; one identified against a measured discharge probe does not. An
// engineer deciding whether to dispatch someone on a capability finding needs to know
// which of the two they are reading, and until this card there was nowhere to find out.
function RoomModelsCard({ rooms = [], identified = 0, matureAfter, learning = 0, setSelectedZone }) {
  const [open, setOpen] = useState(false);

  // Nothing identified yet is a real state with a real explanation, not an empty card:
  // identification needs a room to MOVE, so a cold start legitimately shows zero.
  if (!rooms.length) {
    return (
      <div style={{ marginTop: '4px', background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '10px', padding: '14px' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '6px' }}>
          <Activity size={16} color="var(--text-muted)" />
          <span style={{ fontSize: '12px', fontWeight: 'bold', color: 'var(--text-secondary)' }}>Room Models</span>
        </div>
        <p style={{ margin: 0, fontSize: '10px', color: 'var(--text-muted)', lineHeight: 1.5 }}>
          No room identified yet{learning > 0 ? ` — ${learning} still learning` : ''}. Identification needs a room to actually move: the engine samples every 5 minutes of simulated time and a fit matures at {matureAfter || 36} accepted samples, so a freshly-started twin has nothing here for roughly the first three hours. That is the model being honest, not a fault.
        </p>
      </div>
    );
  }

  const shown = open ? rooms : rooms.slice(0, 4);

  return (
    <div style={{ marginTop: '4px', background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '10px', padding: '14px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '4px' }}>
        <Activity size={16} color="var(--accent-green)" />
        <span style={{ fontSize: '12px', fontWeight: 'bold', color: 'var(--accent-green)' }}>Room Models</span>
        <span style={{ marginLeft: 'auto', fontSize: '9px', fontFamily: 'monospace', color: 'var(--text-muted)' }}>
          {identified} identified{learning > 0 ? ` · ${learning} learning` : ''}
        </span>
      </div>
      <p style={{ margin: '0 0 10px 0', fontSize: '10px', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
        Recovered from each room's own history by recursive least squares — not configured, not assumed. Every prediction above is this model integrated forward.
      </p>

      <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
        {shown.map((m) => (
          <div
            key={m.zone}
            onClick={() => setSelectedZone && setSelectedZone(m.zone)}
            title="Click to fly the 3D view to this room"
            style={{ background: 'rgba(255,255,255,0.03)', borderRadius: '6px', padding: '8px', cursor: setSelectedZone ? 'pointer' : 'default' }}
          >
            <div style={{ display: 'flex', alignItems: 'baseline', gap: '6px', marginBottom: '4px' }}>
              <span style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{m.label}</span>
              <span style={{ marginLeft: 'auto', fontSize: '8px', fontWeight: 'bold', padding: '1px 4px', borderRadius: '3px', flexShrink: 0,
                color: m.thermalReady ? 'var(--accent-green)' : 'var(--text-muted)',
                border: `1px solid ${m.thermalReady ? 'var(--accent-green)' : 'rgba(127,139,150,0.3)'}` }}>
                {m.thermalReady ? 'THERMAL' : `THERMAL ${m.thermalSamples}/${matureAfter || 36}`}
              </span>
              <span style={{ fontSize: '8px', fontWeight: 'bold', padding: '1px 4px', borderRadius: '3px', flexShrink: 0,
                color: m.co2Ready ? 'var(--accent-green)' : 'var(--text-muted)',
                border: `1px solid ${m.co2Ready ? 'var(--accent-green)' : 'rgba(127,139,150,0.3)'}` }}>
                {m.co2Ready ? 'CO₂' : 'CO₂ —'}
              </span>
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', rowGap: '2px', columnGap: '8px' }}>
              {m.thermalReady && (
                <>
                  <span style={{ fontSize: '9px', color: 'var(--text-muted)' }}>Time constant</span>
                  <span style={{ fontSize: '9px', fontFamily: 'monospace', color: 'var(--text-primary)', textAlign: 'right' }}>{m.timeConstantMin.toFixed(0)} min</span>
                  <span style={{ fontSize: '9px', color: 'var(--text-muted)' }}>Fit residual</span>
                  <span style={{ fontSize: '9px', fontFamily: 'monospace', color: 'var(--text-primary)', textAlign: 'right' }}>{m.thermalResidual.toFixed(2)} °C/h</span>
                  <span style={{ fontSize: '9px', color: 'var(--text-muted)' }} title="Share of the cooling fit referenced to a MEASURED discharge-air probe rather than the library's design supply temperature">Supply-air basis</span>
                  <span style={{ fontSize: '9px', fontFamily: 'monospace', textAlign: 'right', color: (m.supplyMeasuredFrac || 0) > 0 ? 'var(--accent-green)' : 'var(--accent-yellow)' }}>
                    {(m.supplyMeasuredFrac || 0) > 0 ? `${((m.supplyMeasuredFrac) * 100).toFixed(0)}% measured` : 'design value'}
                  </span>
                </>
              )}
              {m.co2Ready && (
                <>
                  <span style={{ fontSize: '9px', color: 'var(--text-muted)' }}>Measured air change</span>
                  <span style={{ fontSize: '9px', fontFamily: 'monospace', color: 'var(--text-primary)', textAlign: 'right' }}>{m.achPerHour.toFixed(1)} ACH</span>
                </>
              )}
            </div>
          </div>
        ))}
      </div>

      {rooms.length > 4 && (
        <button
          onClick={() => setOpen((v) => !v)}
          style={{ marginTop: '8px', background: 'transparent', border: 'none', color: 'var(--text-muted)', fontSize: '10px', cursor: 'pointer', padding: 0, textDecoration: 'underline' }}
        >
          {open ? '▴ show fewer' : `▾ show all ${rooms.length} identified rooms`}
        </button>
      )}
    </div>
  );
}
