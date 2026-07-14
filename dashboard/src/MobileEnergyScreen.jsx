import React from 'react';
import { X } from 'lucide-react';
import { AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { money, energyCostPerDay, rateStr, touPeriod, touPeriodLabel } from './tariff';

export default function MobileEnergyScreen({ simData, globalMetrics, loadHistory, onClose }) {
  const loadMw   = globalMetrics?.buildingLoadMw ?? simData?.buildingLoadMw ?? 0;
  const hvacMw   = Math.max(0, loadMw - 2.0);            // engine bakes a ~2 MW non-HVAC baseline
  const baseMw   = Math.min(loadMw, 2.0);
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
  const stateLabel = idle ? 'Idle' : charging ? `Charging ${Math.abs(dischMw).toFixed(2)} MW`
                     : `Discharging ${dischMw.toFixed(2)} MW`;

  const chartData = (loadHistory || []).map(item => ({
    t: item.time.slice(0, 5),
    kw: item.pwr
  }));

  const hvacPct = loadMw > 0 ? (hvacMw / loadMw * 100).toFixed(0) + '%' : '0%';
  const basePct = loadMw > 0 ? (baseMw / loadMw * 100).toFixed(0) + '%' : '0%';
  const totalPct = '100%';
  const savedPct = (loadMw + savedMw) > 0 ? (savedMw / (loadMw + savedMw) * 100).toFixed(0) + '%' : '0%';

  return (
    <div style={{ flex: 1, display: 'flex', flexDirection: 'column', color: '#ffffff', background: '#000000', fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", sans-serif', padding: '20px', minHeight: '100dvh', overflowY: 'auto' }}>
      
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
          EVN business TOU (Decision 963/QĐ-BCT, 2026)
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
          Grid draw {gridMw.toFixed(2)} MW {dischMw > 0.001 && `(battery shaving ${dischMw.toFixed(2)} MW off the grid)`}
        </div>
      </div>

      {/* 3) Two big stat columns */}
      <div style={{ display: 'flex', gap: '20px', marginBottom: '20px' }}>
        <div style={{ flex: 1, background: 'rgba(255,255,255,0.04)', borderRadius: '12px', padding: '12px' }}>
          <div style={{ fontSize: '12px', color: 'rgba(255,255,255,0.6)', marginBottom: '4px' }}>HVAC Load</div>
          <div style={{ fontSize: '20px', fontWeight: 'bold', color: '#F5C242' }}>{hvacMw.toFixed(2)} MW</div>
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

      {/* 5) "Energy Flow" breakdown card */}
      <div style={{ background: 'rgba(255,255,255,0.06)', borderRadius: '16px', padding: '20px', border: '1px solid rgba(255,255,255,0.08)', marginBottom: '40px' }}>
        <h3 style={{ margin: '0 0 20px 0', fontSize: '18px', fontWeight: '600' }}>Energy Flow</h3>
        
        <FlowRow label="HVAC (cooling electrical)" value={`${hvacMw.toFixed(2)} MW`} pct={hvacPct} color="#F5C242" />
        <FlowRow label="Lighting + plug + fans" value={`${baseMw.toFixed(2)} MW`} pct={basePct} color="#4A90E2" />
        <FlowRow label="Total building load" value={`${loadMw.toFixed(2)} MW`} pct={totalPct} color="#B8B8B8" />
        <FlowRow label="Grid draw (after battery)" value={`${gridMw.toFixed(2)} MW`} pct="" color="#B8B8B8" />
        <FlowRow label="Autonomous saving (setback)" value={`-${savedMw.toFixed(2)} MW`} pct={savedPct} color="#3DDC84" isCredit />
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
