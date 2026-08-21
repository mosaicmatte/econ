import React from 'react';
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { money, energyCostPerDay, rateStr, touPeriod, touPeriodLabel, TARIFF } from './tariff';
import { powerMw, powerKw } from './units';
import { GRID_EF_KG_PER_KWH } from './sustainability';
import { usePlugs } from './usePlugs';

export default function MobileEnergyScreen({ simData, globalMetrics, loadHistory }) {
  const loadMw   = globalMetrics?.buildingLoadMw ?? simData?.buildingLoadMw ?? 0;
  const hvacMw   = globalMetrics?.hvacElectricalMw ?? 0; // cooling thermal / live plant COP
  const baseMw   = globalMetrics?.baseLoadMw ?? 0;       // whatever the plant isn't drawing
  const coolMw   = globalMetrics?.coolingOutputMw ?? 0;  // thermal MW delivered
  const coolTons = coolMw * 1000 / 3.517;
  const cop      = globalMetrics?.plantCop ?? 0;
  const savedMw  = simData?.energySavedMw ?? 0;
  const period   = touPeriod();

  const soc      = globalMetrics?.bessSocPct ?? simData?.bessSocPct ?? 0;
  const dischMw  = globalMetrics?.bessDischargeMw ?? simData?.bessDischargeMw ?? 0;
  const gridMw   = globalMetrics?.gridPowerMw ?? Math.max(0, loadMw - dischMw);
  const charging = dischMw < -0.001;
  const idle     = Math.abs(dischMw) <= 0.001;
  const stateLabel = idle ? 'Idle' : charging ? `Charging ${powerMw(Math.abs(dischMw))}`
                     : `Discharging ${powerMw(dischMw)}`;

  // Plug loads (APLC): live stream numbers + the sweep policy from /api/plugs.
  const { status: plugStatus, updateConfig: updatePlugConfig, error: plugError, saving: plugSaving } = usePlugs();
  const plugMw     = (simData?.plugKw ?? 0) / 1000;
  const plugShedKw = simData?.plugShedKw ?? 0;
  const plugSaved  = simData?.plugSavedKwh ?? 0;
  // Lighting + fans is whatever the plant and the sockets are not drawing. Clamping at
  // zero alone was not enough: when metered plug draw exceeds the non-HVAC baseline (the
  // normal case on a small building with no chiller) the clamp hid the overflow and the
  // three shares silently summed past 100%. Show the plug share against the load it is
  // actually part of, and flag the inconsistency rather than papering over it.
  const otherMw     = Math.max(0, baseMw - plugMw); // lighting + fans, plug split out
  const plugExceeds = plugMw > baseMw + 1e-9;
  const plugPct     = loadMw > 0 ? (plugMw / loadMw * 100).toFixed(0) + '%' : '0%';
  const sweepOn    = plugStatus?.config?.enabled ?? false;

  // The window is ~60 one-second samples, so slicing the "HH:MM:SS" stamp to "HH:MM"
  // collapsed every tick to the same one or two labels — a time axis that could not tell
  // you when anything happened. Minutes:seconds is the resolution the data actually has.
  const chartData = (loadHistory || []).map(item => ({
    t: (item.time || '').slice(3) || item.time,
    kw: item.pwr,
  }));

  const hvacPct = loadMw > 0 ? (hvacMw / loadMw * 100).toFixed(0) + '%' : '0%';
  const otherPct = loadMw > 0 ? (otherMw / loadMw * 100).toFixed(0) + '%' : '0%';
  const totalPct = '100%';
  const savedPct = (loadMw + savedMw) > 0 ? (savedMw / (loadMw + savedMw) * 100).toFixed(0) + '%' : '0%';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', color: '#ffffff', fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", sans-serif' }}>
      
      {/* 1) Header */}
      <div style={{ display: 'flex', justifyContent: 'flex-start', alignItems: 'center', marginBottom: '24px' }}>
        <h2 style={{ margin: 0, fontSize: '24px', fontWeight: '600' }}>Energy</h2>
      </div>

      {/* 2) Live tariff banner card */}
      <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '16px', marginBottom: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
        <div style={{ fontSize: '14px', color: 'rgba(255,255,255,0.6)', marginBottom: '8px' }}>Current Tariff ({touPeriodLabel(period)})</div>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end', marginBottom: '4px' }}>
          <span style={{ fontSize: '24px', fontWeight: 'bold' }}>{rateStr(period)}/kWh</span>
          <span style={{ fontSize: '16px', color: '#3DDC84', fontWeight: '600' }}>{money(energyCostPerDay(loadMw * 1000))}/day</span>
        </div>
        <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.4)', marginTop: '8px' }}>
          EVN business time-of-use tariff — Decision 963/QĐ-BCT (2026)
        </div>
      </div>

      <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '16px', marginBottom: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '16px' }}>
          <div style={{ fontSize: '16px', fontWeight: '600' }}>Battery Storage (BESS)</div>
          <div style={{ fontSize: '12px', fontWeight: 'bold', padding: '4px 8px', borderRadius: '4px', background: idle ? 'rgba(184,184,184,0.1)' : charging ? 'rgba(74,144,226,0.1)' : 'rgba(61,220,132,0.1)', color: idle ? '#B8B8B8' : charging ? '#4A90E2' : '#3DDC84' }}>
            {stateLabel}
          </div>
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '12px', color: 'rgba(255,255,255,0.6)', marginBottom: '8px' }}>
          <span>State of charge</span>
          <span>{soc.toFixed(0)}%</span>
        </div>
        <div style={{ width: '100%', height: '12px', background: 'rgba(255,255,255,0.08)', borderRadius: '6px', overflow: 'hidden', marginBottom: '12px' }}>
          <div style={{ width: `${soc}%`, height: '100%', background: '#3DDC84', borderRadius: '6px' }} />
        </div>
        <div style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)' }}>
          Grid draw {powerMw(gridMw)} {dischMw > 0.001 && `(battery shaving ${powerMw(dischMw)} off the grid)`}
        </div>
      </div>

      {/* 3) Two big stat columns */}
      <div style={{ display: 'flex', gap: '20px', marginBottom: '20px' }}>
        <div style={{ flex: 1, background: 'rgba(255,255,255,0.04)', borderRadius: '12px', padding: '12px' }}>
          <div style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)', marginBottom: '4px' }}>HVAC Load</div>
          <div style={{ fontSize: '20px', fontWeight: 'bold', color: '#F5C242' }}>{powerMw(hvacMw)}</div>
        </div>
        <div style={{ flex: 1, background: 'rgba(255,255,255,0.04)', borderRadius: '12px', padding: '12px' }}>
          <div style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)', marginBottom: '4px' }}>Cooling Output</div>
          <div style={{ fontSize: '20px', fontWeight: 'bold', color: '#4FC3F7' }}>{coolTons.toFixed(0)} Tons</div>
        </div>
      </div>
      <div style={{ fontSize: '13px', color: 'rgba(255,255,255,0.6)', textAlign: 'center', marginBottom: '24px' }}>
        Plant COP <span style={{ color: '#fff', fontWeight: 'bold' }}>{cop.toFixed(2)}</span>
      </div>

      {/* 4) Recent-load area chart */}
      <div style={{ marginBottom: '24px' }}>
        <h3 style={{ margin: '0 0 16px 0', fontSize: '16px', fontWeight: '600' }}>Building load — last minutes (kW)</h3>
        {chartData.length > 0 ? (
          <div style={{ height: '240px', width: '100%', marginLeft: '-15px' }}>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData}>
                <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.1)" vertical={false} />
                <XAxis dataKey="t" stroke="#888" tick={{ fill: '#888', fontSize: 12 }} axisLine={false} tickLine={false} interval={Math.max(1, Math.floor(chartData.length / 6))} />
                <YAxis stroke="#888" tick={{ fill: '#888', fontSize: 12 }} axisLine={false} tickLine={false} domain={['auto', 'auto']} width={50} />
                <Tooltip contentStyle={{ background: '#111', border: 'none', borderRadius: '8px', color: '#fff' }} />
                <Area type="monotone" dataKey="kw" stroke="#F5C242" fill="#F5C242" fillOpacity={0.2} isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        ) : (
          <div style={{ height: '240px', display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'rgba(255,255,255,0.02)', borderRadius: '12px', border: '1px dashed rgba(255,255,255,0.1)' }}>
            <span style={{ color: 'rgba(255,255,255,0.4)', fontSize: '14px' }}>Capturing telemetry…</span>
          </div>
        )}
      </div>

      {/* 5) Plug loads (APLC) — the load the case-study BMS couldn't meter or switch */}
      <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '16px', marginBottom: '20px', border: '1px solid rgba(255,255,255,0.08)' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '4px' }}>
          <div style={{ fontSize: '16px', fontWeight: '600' }}>Plug Loads</div>
          <button
            onClick={() => updatePlugConfig({ enabled: !sweepOn })}
            disabled={plugSaving || !plugStatus}
            style={{
              fontSize: '12px', fontWeight: 'bold', padding: '6px 12px', borderRadius: '8px',
              cursor: plugSaving || !plugStatus ? 'default' : 'pointer',
              opacity: plugSaving || !plugStatus ? 0.5 : 1,
              background: sweepOn ? 'rgba(61,220,132,0.12)' : 'rgba(255,255,255,0.08)',
              border: `1px solid ${sweepOn ? '#3DDC84' : 'rgba(255,255,255,0.15)'}`,
              color: sweepOn ? '#3DDC84' : 'rgba(255,255,255,0.6)',
            }}
          >
            {plugSaving ? 'Saving…' : sweepOn ? 'Sweep ON' : 'Sweep OFF'}
          </button>
        </div>
        <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.4)', marginBottom: '12px' }}>
          Largest end use in the Hanoi case study (26.4%) — its BMS couldn't switch sockets. This one can.
        </div>
        {/* A refused policy change has to say so. The toggle used to drop the failure on
            the floor, so on a token-protected engine tapping it simply did nothing. */}
        {plugError && (
          <div style={{ fontSize: '11px', color: '#F5C242', marginBottom: '12px', lineHeight: 1.4 }}>
            {plugError}
          </div>
        )}
        <div style={{ display: 'flex', gap: '12px', marginBottom: '12px' }}>
          <div style={{ flex: 1, background: 'rgba(255,255,255,0.04)', borderRadius: '10px', padding: '10px' }}>
            <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.6)' }}>Draw now ({plugPct} of load)</div>
            <div style={{ fontSize: '18px', fontWeight: 'bold', color: '#F5C242' }}>{powerMw(plugMw)}</div>
          </div>
          <div style={{ flex: 1, background: 'rgba(255,255,255,0.04)', borderRadius: '10px', padding: '10px' }}>
            <div style={{ fontSize: '11px', color: 'rgba(255,255,255,0.6)' }}>Swept off · {plugStatus?.shedZones ?? 0} zones</div>
            <div style={{ fontSize: '18px', fontWeight: 'bold', color: '#3DDC84' }}>{powerKw(plugShedKw)}</div>
          </div>
        </div>
        <div style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)' }}>
          Avoided so far: <span style={{ color: '#3DDC84', fontWeight: '600' }}>{plugSaved.toFixed(1)} kWh</span>
          {' · '}<span style={{ color: '#3DDC84', fontWeight: '600' }}>{money(plugSaved * TARIFF.normalPerKwh)}</span>
          {' · '}<span style={{ color: '#3DDC84', fontWeight: '600' }}>{(plugSaved * GRID_EF_KG_PER_KWH).toFixed(1)} kg CO₂</span>
        </div>
        {plugStatus?.armed && (
          <div style={{ fontSize: '11px', color: '#4A90E2', marginTop: '8px', fontWeight: '600' }}>
            ● Sweep armed (after hours) — vacant zones shed after {plugStatus?.config?.graceMinutes ?? 15} min
          </div>
        )}
      </div>

      {/* 6) "Energy Flow" breakdown card */}
      <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)', marginBottom: '40px' }}>
        <h3 style={{ margin: '0 0 20px 0', fontSize: '18px', fontWeight: '600' }}>Energy Flow</h3>

        <FlowRow label="HVAC (cooling electrical)" value={powerMw(hvacMw)} pct={hvacPct} color="#F5C242" />
        <FlowRow label="Plug loads (APLC)" value={powerMw(plugMw)} pct={plugPct} color="#F58C42" />
        <FlowRow label="Lighting + fans" value={powerMw(otherMw)} pct={otherPct} color="#4A90E2" />
        {plugExceeds && (
          <div style={{ fontSize: '11px', color: '#F5C242', marginTop: '-8px', marginBottom: '16px', lineHeight: 1.4 }}>
            Metered plug draw exceeds the non-HVAC share of building load, so these rows do not sum to the total. That usually means a clamp is reading a circuit the load model does not know about — worth checking before trusting the split.
          </div>
        )}
        <FlowRow label="Total building load" value={powerMw(loadMw)} pct={totalPct} color="#B8B8B8" />
        <FlowRow label="Grid draw (after battery)" value={powerMw(gridMw)} pct="" color="#B8B8B8" />
        {/* Only shown as a credit when there IS one: "-0.00 MW" against a green "saving"
            label reads as a rounding artifact of a real number, which it is not. */}
        <FlowRow
          label="Autonomous saving (setback)"
          value={savedMw > 0 ? `-${powerMw(savedMw)}` : 'none active'}
          pct={savedMw > 0 ? savedPct : ''}
          color="#3DDC84"
          isCredit={savedMw > 0}
        />
        <FlowRow label="Battery (SoC)" value={`${soc.toFixed(0)}%`} pct="" color="#3DDC84" />
      </div>

    </div>
  );
}

function FlowRow({ label, value, pct, color, isCredit }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '12px', flex: 1 }}>
        <div style={{ width: '12px', height: '12px', borderRadius: '6px', background: color, flexShrink: 0 }} />
        <span style={{ fontSize: '15px', fontWeight: '500', color: isCredit ? '#3DDC84' : '#fff' }}>{label}</span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
        <span style={{ fontSize: '14px', color: 'rgba(255,255,255,0.5)', fontWeight: '500', width: '36px', textAlign: 'right' }}>{pct}</span>
        <span style={{ fontSize: '15px', fontWeight: '600', width: '70px', textAlign: 'right', color: isCredit ? '#3DDC84' : '#fff' }}>{value}</span>
      </div>
    </div>
  );
}
