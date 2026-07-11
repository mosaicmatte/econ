import React, { useMemo, useState } from 'react';
import { Brain, Zap, AlertTriangle, TrendingDown, ThermometerSnowflake, Activity, Radio } from 'lucide-react';
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer, ReferenceLine } from 'recharts';

export default function AiInsightsPanel({ simData, activeScenario, faultTarget, aiForecast, setAutoPilot, hardwareNodes = {}, setSelectedZone }) {
  // Insight actions that have been engaged (id set). Clicking an action hands the recommendation
  // to the autonomous controller (autoPilot) and marks it engaged — so the button does something.
  const [engaged, setEngaged] = useState({});
  // Cards that carry live detail (forecast chart, model metrics, hardware nodes) expand inline
  // instead of engaging the controller.
  const [expanded, setExpanded] = useState({});
  const engage = (id) => { setEngaged((e) => ({ ...e, [id]: true })); if (setAutoPilot) setAutoPilot(true); };
  const toggle = (id) => setExpanded((e) => ({ ...e, [id]: !e[id] }));

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

  // Dynamically generate insights based on simulation state
  const insights = useMemo(() => {
    const generated = [];
    const zones = Object.values(simData.zones || {});

    // 1. Critical Scenario Fault
    if (activeScenario === 'fault' && faultTarget) {
      generated.push({
        id: 'fault',
        type: 'critical',
        icon: <AlertTriangle size={18} color="var(--accent-red)" />,
        title: 'Thermal Runaway Detected',
        message: `Zone ${faultTarget} is experiencing a critical thermal failure. Cooling capacity is degraded.`,
        action: 'OVERRIDE VAV SETTINGS'
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

    // 3. High Demand Period
    if (activeScenario === 'peak') {
      generated.push({
        id: 'peak',
        type: 'warning',
        icon: <TrendingDown size={18} color="var(--accent-yellow)" />,
        title: 'High Grid Demand',
        message: 'Grid load is nearing peak capacity. Pre-cooling sequence is recommended to offset afternoon prices.',
        action: 'ACTIVATE PRE-COOLING'
      });
    }

    if (aiForecast && aiForecast.predicted_peak_load) {
      const isFallback = aiForecast.weather_source === 'fallback';
      generated.push({
        id: 'forecast',
        type: 'info',
        expandable: true,
        icon: <Activity size={18} color="var(--accent-blue)" />,
        title: 'LSTM Load Forecast',
        message: `Deep Learning model predicts an upcoming peak load of ${aiForecast.predicted_peak_load.toFixed(2)} MW. ${isFallback ? '(Using fallback weather)' : '(Live weather data incorporated)'}`,
        action: 'VIEW PREDICTIONS'
      });
    }

    // 4. Unoccupied Wasting
    const wastingZones = zones.filter((z) => z.occupancy === 0 && z.load > 0);
    if (wastingZones.length > 0) {
      generated.push({
        id: 'wasting',
        type: 'info',
        icon: <Zap size={18} color="var(--accent-blue)" />,
        title: 'Energy Optimization Opportunity',
        message: `${wastingZones.length} zones are currently unoccupied but consuming ${wastingZones.reduce((acc, z) => acc + z.load, 0).toFixed(1)} kW of cooling power.`,
        action: 'APPLY ECO SETBACK'
      });
    }

    // 5. Out of Band (Hot spots)
    const hotZones = zones.filter((z) => z.temp > (z.setpoint + (z.deadband || 1)));
    if (hotZones.length > 0 && activeScenario !== 'fault') {
      generated.push({
        id: 'hot',
        type: 'warning',
        icon: <ThermometerSnowflake size={18} color="var(--accent-yellow)" />,
        title: 'Thermal Drift Detected',
        message: `${hotZones.length} zones have drifted above their cooling deadband. Neural net suggests increasing supply static pressure by 0.2 inWC.`,
        action: 'OPTIMIZE PRESSURE'
      });
    }

    // 6. General AI Status (Always present to avoid empty state)
    generated.push({
      id: 'general',
      type: 'success',
      expandable: true,
      icon: <Brain size={18} color="var(--accent-green)" />,
      title: 'Autonomous Operations Nominal',
      message: `Occupancy-driven setback controller is actively balancing comfort and energy. Live savings: ${savingsPct.toFixed(1)}% of plant load (${(savedMw * 1000).toFixed(0)} kW).`,
      action: 'VIEW MODEL METRICS'
    });

    return generated;
  }, [simData, activeScenario, faultTarget, aiForecast, hwList, hwOnline, savingsPct, savedMw]);

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
        ['Live savings', `${(savedMw * 1000).toFixed(0)} kW (${savingsPct.toFixed(1)}%)`],
        ['Plant COP', (simData.plantCop || 0).toFixed(2)],
        ['Cooling delivered', `${(simData.coolingOutputMw || 0).toFixed(2)} MW thermal`],
        ['Zones simulated', `${Object.keys(simData.zones || {}).length}`],
        ['Physical nodes', `${hwList.length} (${hwOnline} online)`],
        ['Forecaster', aiForecast ? `LSTM · ${aiForecast.weather_source === 'fallback' ? 'fallback weather' : 'live weather'}` : 'offline'],
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
      return (
        <div style={{ marginTop: '6px', display: 'flex', flexDirection: 'column', gap: '4px' }}>
          {hwList.map((n) => (
            <div
              key={n.zoneId}
              onClick={() => setSelectedZone && setSelectedZone(n.zoneId)}
              title="Click to fly the 3D view to this zone"
              style={{ display: 'flex', alignItems: 'center', gap: '6px', padding: '5px 6px', borderRadius: '4px', background: 'rgba(255,255,255,0.03)', cursor: setSelectedZone ? 'pointer' : 'default', fontFamily: 'monospace', fontSize: '10px' }}
            >
              <span style={{ width: 6, height: 6, borderRadius: '50%', flexShrink: 0, background: n.online ? 'var(--accent-green)' : 'var(--text-muted)', boxShadow: n.online ? '0 0 4px var(--accent-green)' : 'none' }} />
              <span style={{ color: 'var(--text-primary)', fontWeight: 'bold' }}>{(n.source || 'edge').toUpperCase()}</span>
              <span style={{ color: 'var(--text-secondary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{(n.zoneId || '').replace('zone-', '')}</span>
              {n.tempPinned && <span style={{ color: 'var(--accent-blue)' }}>{(n.hwTemp || 0).toFixed(1)}°C</span>}
              <span style={{ color: 'var(--text-secondary)' }}>{n.occupancy ?? 0}P</span>
              <span style={{ color: n.lightsOn ? 'var(--accent-green)' : 'var(--text-muted)' }}>{n.lightsOn ? 'LIT' : 'DARK'}</span>
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
          Real-time neural network diagnostics and actionable insights. Total building load is currently at <span style={{ color: 'var(--text-primary)', fontWeight: 'bold' }}>{simData.buildingLoadMw?.toFixed(2)} MW</span>.
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
              </div>

              <p style={{ margin: '4px 0 0 0', fontSize: '11px', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
                {insight.message}
              </p>

              {insight.expandable && isExpanded && renderDetail(insight.id)}

              <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '4px' }}>
                <button
                  onClick={() => (insight.expandable ? toggle(insight.id) : engage(insight.id))}
                  disabled={!insight.expandable && !!engaged[insight.id]}
                  style={{
                    background: !insight.expandable && engaged[insight.id] ? titleColor : 'transparent',
                    border: `1px solid ${border}`,
                    color: !insight.expandable && engaged[insight.id] ? '#000' : titleColor,
                    padding: '6px 12px',
                    borderRadius: '4px',
                    fontSize: '10px',
                    fontWeight: 'bold',
                    cursor: !insight.expandable && engaged[insight.id] ? 'default' : 'pointer',
                    transition: 'all 0.2s ease',
                  }}
                  onMouseOver={(e) => { if (insight.expandable || !engaged[insight.id]) { e.currentTarget.style.background = titleColor; e.currentTarget.style.color = '#000'; } }}
                  onMouseOut={(e) => { if (insight.expandable || !engaged[insight.id]) { e.currentTarget.style.background = 'transparent'; e.currentTarget.style.color = titleColor; } }}
                >
                  {insight.expandable ? (isExpanded ? '▴ COLLAPSE' : insight.action) : (engaged[insight.id] ? '✓ ENGAGED' : insight.action)}
                </button>
              </div>
            </div>
          );
        })}
      </div>

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
