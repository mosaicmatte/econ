import React from 'react';
import { Activity, Users, Thermometer, Zap, BarChart2 } from 'lucide-react';
import { ResponsiveContainer, AreaChart, Area } from 'recharts';
import useMeanLoad from './useMeanLoad';
import { API_BASE } from './api';
import { useLibrary } from './useLibrary';
import { money, energyCostPerDay, touPeriod, touPeriodLabel } from './tariff';
import { powerMw, splitPowerMw } from './units';
import {
  FLOOR_AREA_M2, EUI_BENCHMARK, IS_IT_DOMINATED, ZONE_MIX,
  euiRunRateFromLoadMw, euiFromMeanLoadMw, EUI_MIN_WINDOW_H,
  carbonTonnesPerYear, carbonAvoidedTonnesPerYear, tonnesStr,
} from './sustainability';

// (Plant utilization is measured against the peak load this building has actually been
// observed at — see useMeanLoad. There is no synthesized nameplate here on purpose: the
// previous `Math.max(3.6, Σ baseHeatLoad / 3 + 2)` was wrong twice over. The 3.6 MW floor
// was borrowed from a large commercial tower, so on the house pilot the bar read 0.3% and
// would have kept reading 0.3% through a total plant failure. And Σ baseHeatLoad is only
// the zones' internal EQUIPMENT gain — it excludes the envelope, solar, occupants and
// ventilation that the engine's load model integrates, and ventilation alone dominates
// this building's cooling load. Any nameplate built from it would have been off by more
// than an order of magnitude in the other direction.)

function Sparkline({ data, dataKey, color }) {
  return (
    <div style={{ width: '60px', height: '20px' }}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data}>
          <Area type="monotone" dataKey={dataKey} stroke={color} fill={color} fillOpacity={0.2} strokeWidth={1.5} isAnimationActive={false} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

// `caption` carries the context that used to be crammed into `unit` — "of 25 kW seen in
// 31 min", "setpoints avg 27.8". Inline, those strings ran the header past the panel edge
// and pushed the label and the value into each other; on their own line they read.
function BulletGraph({ label, value, max, target, color, unit, caption, digits = 1, isLast }) {
  const numValue = typeof value === 'number' ? value : parseFloat(value) || 0;
  const safeMax = Number.isFinite(max) && max > 0 ? max : 1;
  const percent = Math.max(0, Math.min(100, (numValue / safeMax) * 100));
  const targetPercent = Math.max(0, Math.min(100, (target / safeMax) * 100));
  return (
    <div style={{ marginBottom: isLast ? 0 : '14px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', gap: '8px', fontSize: '10px', color: 'var(--text-secondary)', marginBottom: caption ? '2px' : '5px' }}>
        <span style={{ whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{label}</span>
        <span style={{ fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--text-primary)', flexShrink: 0 }}>
          {numValue.toFixed(digits)}{unit ? <span style={{ color: 'var(--text-secondary)', fontWeight: 'normal' }}> {unit}</span> : null}
        </span>
      </div>
      {caption && (
        <div style={{ fontSize: '9px', color: 'var(--text-muted)', marginBottom: '5px' }}>{caption}</div>
      )}
      {/* Track deliberately NOT overflow-hidden: the target tick extends above/below it so
          it reads as a reference marker crossing the track, not a detached bar fragment. */}
      <div style={{ position: 'relative', height: '6px', background: 'rgba(255,255,255,0.06)', borderRadius: '3px' }}>
        <div style={{ position: 'absolute', top: 0, left: 0, height: '100%', width: `${percent}%`, background: color, borderRadius: '3px', transition: 'width 0.3s ease' }} />
        <div style={{ position: 'absolute', top: '-4px', left: `calc(${targetPercent}% - 1px)`, height: '14px', width: '2px', background: 'rgba(255,255,255,0.85)', borderRadius: '1px' }} />
      </div>
    </div>
  );
}

function DeltaCard({ title, icon: Icon, value, unit, delta, isGood, historyData, dataKey, sparkColor }) {
  return (
    <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '12px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '10px', color: 'var(--text-secondary)' }}>
          <Icon size={12} /> {title}
        </div>
        <Sparkline data={historyData} dataKey={dataKey} color={sparkColor} />
      </div>
      <div style={{ display: 'flex', alignItems: 'flex-end', gap: '8px' }}>
        <div style={{ fontSize: '20px', fontWeight: 'bold', color: 'var(--text-primary)', fontFamily: 'monospace', lineHeight: 1 }}>
          {value} <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>{unit}</span>
        </div>
        {/* A delta of exactly zero is "no change", not a fall. The old chip rendered a
            red ▼ 0 whenever nothing had moved, which on the zone cards (whose delta was
            hardcoded to 0) meant every room permanently displayed a downward arrow. */}
        {(() => {
          const flat = !Number.isFinite(delta) || Math.abs(delta) < 1e-9;
          const good = flat ? null : isGood;
          return (
            <div style={{
              fontSize: '10px', fontWeight: 'bold',
              color: flat ? 'var(--text-secondary)' : good ? 'var(--accent-green)' : 'var(--accent-red)',
              background: flat ? 'rgba(255,255,255,0.05)' : good ? 'rgba(0,255,0,0.1)' : 'rgba(255,0,0,0.1)',
              padding: '2px 4px', borderRadius: '4px',
            }}>
              {flat ? '—' : `${delta > 0 ? '▲' : '▼'} ${Math.abs(delta)}`}
            </div>
          );
        })()}
      </div>
    </div>
  );
}

export default function GlobalMetricsPanel({ simData, globalMetrics, loadHistory, activeFloor, selectedNode, width = 320, setWidth, sendManualOverride, hardwareNodes = {} }) {
  const [zoneHistory, setZoneHistory] = React.useState([]);
  // Programme coefficients from the engine (see useLibrary): used here for the zone's
  // design occupant density, so the occupancy bar is scaled by the building's own
  // calibration rather than a fixed 80-person axis.
  const { areaPerOccupant, seriesId } = useLibrary();
  // Annual intensity has to be built on the load actually observed over time, not on
  // whatever the load happens to be this second. See useMeanLoad.
  const { meanMw, hours: observedH, peakMw } = useMeanLoad(simData?.buildingLoadMw, seriesId);

  React.useEffect(() => {
    if (selectedNode?.type === 'zone') {
      fetch(`${API_BASE}/api/history?zone=${selectedNode.id}`)
        .then(res => res.json())
        .then(data => {
          if (data && data.length > 0) setZoneHistory(data);
        })
        .catch(err => console.log('Zone history error:', err));
    } else {
      setZoneHistory([]);
    }
  }, [selectedNode?.id]);
  
  // The building's own mean setpoint — what "on target" means for THIS building.
  const meanSetpoint = React.useMemo(() => {
    const zs = Object.values(simData.zones || {});
    if (!zs.length) return 24;
    return zs.reduce((s, z) => s + (z.setpoint ?? 24), 0) / zs.length;
  }, [simData.zones]);

  const bldgLoad = simData.buildingLoadMw ?? 0;
  const sysHealth = simData.systemHealth ?? 100;
  const occupants = simData.totalOccupants ?? 0;

  // Real "active critical faults": zones the live stream has flagged in alarm. This matches the
  // red zones in the 3D model / topology and works regardless of building size (the old
  // sysHealth<80 proxy never tripped once the building grew past ~20 zones).
  const criticalFaults = Object.values(simData.zones || {}).filter(z => z.alert === true).length;
  const hasFault = criticalFaults > 0;
  // Plant utilization against the highest load this building has been SEEN at. Both
  // numbers are measurements, so the ratio is one too. Withheld until a peak has actually
  // been observed rather than shown against a denominator nobody measured.
  const coolingCapacityPct = peakMw > 0
    ? Math.max(0, Math.min(100, (bldgLoad / peakMw) * 100))
    : null;

  // Real deltas: change between the last two history samples. Occupancy delta reads the
  // occupancy field directly — it used to be reverse-engineered from the co2 series as
  // (Δco2)/0.85, which stopped being occupancy the moment co2 came from a real NDIR
  // sensor instead of the estimate. No more random demo numbers on the HMI cards.
  const h = loadHistory || [];
  const a = h[h.length - 2], b = h[h.length - 1];
  const loadDelta = a && b ? +(((b.pwr - a.pwr) / 1000)).toFixed(2) : 0;
  const occDelta = a && b && b.occ !== undefined ? b.occ - a.occ : 0;

  // The per-zone history endpoint reuses the GLOBAL column names for different
  // quantities: `pwr` carries temperature and `occ` carries occupancy. Rename once here
  // so each card names the series it actually plots, instead of a chart labelled
  // OCCUPANCY being handed a key called `co2`.
  const zoneSeries = React.useMemo(
    () => (zoneHistory || []).map((r) => ({ time: r.time, temp: r.pwr, occ: r.occ ?? r.co2 })),
    [zoneHistory],
  );
  const zs = zoneSeries;
  const za = zs[zs.length - 2], zb = zs[zs.length - 1];
  const zoneTempDelta = za && zb ? +(zb.temp - za.temp).toFixed(2) : 0;
  const zoneOccDelta = za && zb ? +(zb.occ - za.occ).toFixed(0) : 0;

  // Design occupancy for the selected zone: its digitized floor area over the library's
  // area-per-occupant for that programme. Zero when either is unknown, and the caller
  // shows a plain count rather than a bar against an invented capacity.
  const zoneDesignPax = React.useMemo(() => {
    if (selectedNode?.type !== 'zone') return 0;
    const areaM2 = selectedNode.data?.areaM2 || 0;
    const perOcc = areaPerOccupant(selectedNode.data?.type);
    if (!(areaM2 > 0) || !(perOcc > 0)) return 0;
    return Math.max(1, Math.round(areaM2 / perOcc));
  }, [selectedNode?.id, selectedNode?.data?.areaM2, selectedNode?.data?.type, areaPerOccupant]);
  
  return (
    <aside className="hud-dock-right" style={{ display: 'flex', flexDirection: 'column', gap: '1rem', width, padding: '1rem', position: 'absolute' }}>
      {setWidth && (
        <div
          className="resize-handle"
          onPointerDown={(e) => {
            e.preventDefault();
            const startW = width, startX = e.clientX;
            const onMove = (m) => setWidth(Math.max(280, Math.min(startW + (startX - m.clientX), window.innerWidth * 0.5)));
            const onUp = () => {
              document.removeEventListener('pointermove', onMove);
              document.removeEventListener('pointerup', onUp);
            };
            document.addEventListener('pointermove', onMove);
            document.addEventListener('pointerup', onUp);
          }}
          style={{ position: 'absolute', top: '50%', left: 0, transform: 'translateY(-50%)', width: 10, height: 40, cursor: 'ew-resize', zIndex: 100 }}
        />
      )}
      <div style={{ paddingBottom: '0.5rem', borderBottom: '1px solid var(--border-glass)' }}>
        <h2 style={{ fontSize: '14px', color: 'var(--text-primary)', margin: 0, letterSpacing: '1px', display: 'flex', alignItems: 'center', gap: '8px' }}>
          <BarChart2 size={16} color="var(--accent-blue)" />
          {selectedNode ? 'NODE DIAGNOSTICS' : 'ENTERPRISE OVERVIEW'}
        </h2>
        <span className="mono" style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>{selectedNode?.id || 'GLOBAL METRICS'}</span>
      </div>

      {!selectedNode ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
          {/* Delta Cards */}
          {/* Unit follows the magnitude (units.js). It read "0.01 MW" on a building drawing
              twelve kilowatts, while the utilization bar below it correctly said kW. */}
          <DeltaCard 
            title="TOTAL LOAD" icon={Zap} value={splitPowerMw(bldgLoad).value} unit={splitPowerMw(bldgLoad).unit}
            delta={loadDelta} isGood={false} historyData={loadHistory} dataKey="pwr" sparkColor="var(--accent-yellow)" 
          />
          <DeltaCard
            title="OCCUPANCY" icon={Users} value={occupants} unit="Pax"
            delta={occDelta} isGood={true} historyData={loadHistory} dataKey="occ" sparkColor="var(--accent-blue)"
          />

          {/* Live savings from the engine's occupancy-driven setbacks (streamed energySavedMw),
              priced through the tariff model (see tariff.js) into $/day energy + $/mo demand. */}
          {(() => {
            const savedKw = (simData.energySavedMw || 0) * 1000;
            return (
              <div style={{ background: 'rgba(34, 197, 94, 0.05)', border: '1px solid rgba(34, 197, 94, 0.3)', borderRadius: '8px', padding: '12px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '10px', color: 'var(--text-secondary)' }}>
                  <Zap size={12} color="var(--accent-green)" /> ENERGY SAVED
                </div>
                <div style={{ textAlign: 'right' }}>
                  <div style={{ fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--accent-green)', fontSize: '16px' }}>
                    {splitPowerMw(simData.energySavedMw || 0).value} <span style={{ fontSize: '10px' }}>{splitPowerMw(simData.energySavedMw || 0).unit}</span>
                  </div>
                  <div style={{ fontSize: '9px', color: 'var(--text-secondary)', marginTop: '2px' }}>
                    ≈ {money(energyCostPerDay(savedKw))}/day ({touPeriodLabel(touPeriod())})
                  </div>
                </div>
              </div>
            );
          })()}

          {/* Energy intensity. Two different numbers that were previously conflated: the live
              run-rate (what a year at this instant's load would come to) and the annualised
              EUI built on the mean load actually observed. Only the second may sit beside a
              cohort benchmark — comparing a 09:00 peak to an annual survey figure reads
              roughly 3x high and says nothing about the building. The benchmark is withheld
              entirely until a representative window has been seen, and when the load is
              dominated by a non-office programme. */}
          {(() => {
            const runRate = euiRunRateFromLoadMw(simData.buildingLoadMw || 0);
            const eui = euiFromMeanLoadMw(meanMw);
            const ratio = eui / EUI_BENCHMARK.hcmc;
            const settled = observedH >= EUI_MIN_WINDOW_H;
            const comparable = !IS_IT_DOMINATED && settled;
            return (
              <div style={{ background: 'rgba(0,0,0,0.25)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '12px' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '10px', color: 'var(--text-secondary)' }}>
                    <BarChart2 size={12} color="var(--accent-blue)" /> ENERGY INTENSITY (EUI)
                  </div>
                  <div style={{ textAlign: 'right' }}>
                    <div style={{ fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--accent-blue)', fontSize: '16px' }}>
                      {(settled ? eui : runRate).toFixed(0)} <span style={{ fontSize: '9px' }}>kWh/m²·yr</span>
                    </div>
                    <div style={{ fontSize: '9px', color: 'var(--text-muted)', marginTop: '2px' }}>
                      {settled
                        ? `annualised from ${observedH.toFixed(0)} h observed`
                        : 'run-rate at this instant'} · {FLOOR_AREA_M2.toLocaleString('en-US', { maximumFractionDigits: 0 })} m²
                    </div>
                  </div>
                </div>
                <div style={{ fontSize: '9px', color: 'var(--text-secondary)', marginTop: '6px', lineHeight: 1.4 }}>
                  {IS_IT_DOMINATED
                    ? `Not comparable to the ${EUI_BENCHMARK.hcmc} kWh/m²·yr office cohort — ${(ZONE_MIX.dominant.loadShare * 100).toFixed(0)}% of connected load is ${ZONE_MIX.dominant.type}.`
                    : comparable
                      ? `${ratio.toFixed(2)}× the HCMC office cohort (${EUI_BENCHMARK.hcmc} kWh/m²·yr, 57-building survey).`
                      : `Benchmark held back until a full day is observed (${observedH.toFixed(1)} of ${EUI_MIN_WINDOW_H} h). A run-rate taken at one moment is not an annual intensity and would not be a fair comparison.`}
                </div>
              </div>
            );
          })()}

          {/* Operational carbon (Scope 2) — the subject of the Hanoi case study, and what an
              ESG reviewer asks for before anything else. */}
          {(() => {
            // Annualised from the MEAN load once a representative window has been observed,
            // and from the instantaneous load before that — with the basis stated either
            // way. This card used to headline "71 t CO2e/yr" computed from whatever the
            // load happened to be that second, with no caveat at all, directly beneath an
            // EUI card that withholds its own benchmark for exactly that reason. The two
            // are the same quantity over the same hours; they cannot have different
            // standards of evidence.
            const settledC = observedH >= EUI_MIN_WINDOW_H;
            const tYr = carbonTonnesPerYear(settledC ? meanMw : (simData.buildingLoadMw || 0));
            const avoided = carbonAvoidedTonnesPerYear(simData.energySavedMw || 0);
            return (
              <div style={{ background: 'rgba(34, 197, 94, 0.05)', border: '1px solid rgba(34, 197, 94, 0.25)', borderRadius: '8px', padding: '12px' }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '10px', color: 'var(--text-secondary)' }}>
                    <Activity size={12} color="var(--accent-green)" /> OPERATIONAL CARBON
                  </div>
                  <div style={{ textAlign: 'right' }}>
                    <div style={{ fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--text-primary)', fontSize: '16px' }}>
                      {tonnesStr(tYr)} <span style={{ fontSize: '9px' }}>CO₂e/yr</span>
                    </div>
                    <div style={{ fontSize: '9px', color: 'var(--accent-green)', marginTop: '2px' }}>
                      {avoided > 0.05 ? `${tonnesStr(avoided)}/yr avoided by setback` : 'no setback active'}
                    </div>
                    <div style={{ fontSize: '9px', color: 'var(--text-muted)', marginTop: '2px' }}>
                      {settledC
                        ? `annualised from ${observedH.toFixed(0)} h observed`
                        : 'run-rate at this instant, not an annual total'}
                    </div>
                  </div>
                </div>
              </div>
            );
          })()}

          {/* BESS Card */}
          {simData.bessSocPct != null && (() => {
            const dischMw = simData.bessDischargeMw || 0;
            const soc = simData.bessSocPct || 0;
            const charging = dischMw < -0.001;
            const idle = Math.abs(dischMw) <= 0.001;
            return (
              <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '12px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '10px', color: 'var(--text-secondary)' }}>
                    <Zap size={12} color="var(--accent-green)" /> BESS
                  </div>
                  <div style={{ fontSize: '9px', color: 'var(--text-secondary)', marginTop: '4px' }}>
                    State of charge
                  </div>
                </div>
                <div style={{ textAlign: 'right' }}>
                  <div style={{ fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--accent-green)', fontSize: '16px' }}>
                    {soc.toFixed(0)}<span style={{ fontSize: '10px' }}>%</span>
                  </div>
                  <div style={{ fontSize: '9px', color: 'var(--text-secondary)', marginTop: '2px' }}>
                    {idle ? 'idle' : charging ? `charging ${powerMw(Math.abs(dischMw))}` : `discharging ${powerMw(dischMw)}`}
                  </div>
                </div>
              </div>
            );
          })()}

          {/* Bullet Graphs */}
          <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '16px 12px' }}>
            <BulletGraph label="System Health" value={sysHealth} max={100} target={95} color={sysHealth < 80 ? 'var(--accent-red)' : 'var(--accent-green)'} unit="%" digits={0} />
            {/* Target tick is the building's OWN mean setpoint, from the live zone state.
                It was pinned at 23.5 °C — a figure matching neither the office fixture
                (26 °C) nor the house pilot (28 °C), so the reference marker sat well left
                of where every zone is actually controlled and made a correctly-running
                building look permanently overheated. */}
            <BulletGraph
              label="Avg Temperature"
              value={Number(globalMetrics.avgTemp) || 0}
              max={Math.max(35, meanSetpoint + 10)}
              target={meanSetpoint}
              color="var(--accent-blue)"
              unit="°C"
              caption={`target tick = this building's mean setpoint, ${meanSetpoint.toFixed(1)} °C`}
            />
            {coolingCapacityPct !== null ? (
              <BulletGraph
                label="Load vs Observed Peak"
                value={coolingCapacityPct}
                max={100}
                target={80}
                color={coolingCapacityPct > 95 ? 'var(--accent-red)' : 'var(--accent-yellow)'}
                unit="%"
                caption={`of ${powerMw(peakMw)}, the highest seen in ${observedH < 1 ? `${(observedH * 60).toFixed(0)} min` : `${observedH.toFixed(0)} h`} observed`}
              />
            ) : (
              <div style={{ fontSize: '10px', color: 'var(--text-secondary)', marginBottom: '14px' }}>
                Load vs observed peak — no load observed yet.
              </div>
            )}
            <BulletGraph label="Plant COP" value={simData.plantCop || 0} max={Math.max(4, (simData.plantCop || 0) * 1.2)} target={3.4} color="var(--accent-green)" unit="" digits={2} isLast />
          </div>

          <div style={{ display: 'flex', justifyContent: 'space-between', padding: '12px', background: hasFault ? 'rgba(255,0,0,0.1)' : 'rgba(0,0,0,0.2)', border: hasFault ? '1px solid rgba(255,0,0,0.3)' : '1px solid var(--border-glass)', borderRadius: '8px', alignItems: 'center', transition: '0.3s' }}>
            <div style={{ fontSize: '11px', color: hasFault ? 'var(--accent-red)' : 'var(--text-secondary)', fontWeight: 'bold' }}>ACTIVE CRITICAL FAULTS</div>
            <div style={{ fontSize: '16px', fontWeight: 'bold', color: hasFault ? 'var(--accent-red)' : 'var(--accent-green)' }}>
               {criticalFaults}
            </div>
          </div>

          {/* Physical edge nodes (ESP32 / Pico) currently bound into the twin */}
          {Object.values(hardwareNodes || {}).length > 0 && (
            <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '12px' }}>
              <div style={{ fontSize: '10px', color: 'var(--text-secondary)', fontWeight: 'bold', letterSpacing: '0.05em', marginBottom: '8px' }}>
                ⚡ EDGE HARDWARE
              </div>
              {Object.values(hardwareNodes).map((n) => (
                <div key={n.zoneId} style={{ display: 'flex', alignItems: 'center', gap: '6px', padding: '4px 0', fontSize: '10px', fontFamily: 'monospace' }}>
                  <span style={{ width: 6, height: 6, borderRadius: '50%', flexShrink: 0, background: n.online ? 'var(--accent-green)' : 'var(--text-muted)', boxShadow: n.online ? '0 0 4px var(--accent-green)' : 'none' }} />
                  <span style={{ color: 'var(--text-primary)', fontWeight: 'bold' }}>{(n.source || 'edge').toUpperCase()}</span>
                  <span style={{ color: 'var(--text-secondary)', flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{(n.zoneId || '').replace('zone-', '')}</span>
                  {n.tempPinned && <span style={{ color: 'var(--accent-blue)' }}>{(n.hwTemp || 0).toFixed(1)}°C</span>}
                  <span style={{ color: 'var(--text-secondary)' }}>{n.occupancy ?? 0}P</span>
                </div>
              ))}
            </div>
          )}

          {/* Static Info */}
          <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '11px', padding: '8px 12px', background: 'rgba(0,0,0,0.3)', borderRadius: '8px', border: '1px solid var(--border-glass)' }}>
             <span style={{ color: 'var(--text-secondary)' }}>Selected Level:</span>
             <span style={{ color: 'var(--accent-blue)', fontWeight: 'bold' }}>L{activeFloor}</span>
          </div>
        </div>
      ) : selectedNode?.type === 'zone' ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', background: 'rgba(255,255,255,0.02)', padding: '12px', borderRadius: '8px', border: '1px solid var(--border-glass)' }}>
            <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>STATUS</span>
            <span style={{ fontSize: '10px', fontWeight: 'bold', padding: '4px 8px', borderRadius: '4px', background: selectedNode.data.alert === true ? 'rgba(255,0,0,0.1)' : 'rgba(0,255,0,0.1)', color: selectedNode.data.alert === true ? 'var(--accent-red)' : 'var(--accent-green)' }}>
              {selectedNode.data.alert === true ? 'ALARM' : 'NOMINAL'}
            </span>
          </div>

          {/* Delta Cards for Zone. The series come from /api/history?zone=…, where the
              per-zone query returns temperature in `pwr` and occupancy in `occ` — so the
              keys are remapped once, here, rather than each card silently plotting
              whatever the column happened to hold. Deltas are the real change between the
              last two recorded samples; they used to be hardcoded to 0, which rendered a
              permanent "▼ 0" chip that looked like a measurement. */}
          <DeltaCard 
            title="LOCAL TEMP" icon={Thermometer} value={parseFloat(selectedNode.data.temp).toFixed(1)} unit="°C" 
            delta={zoneTempDelta} isGood={zoneTempDelta <= 0} historyData={zoneSeries} dataKey="temp" sparkColor="var(--accent-yellow)" 
          />
          <DeltaCard 
            title="OCCUPANCY" icon={Users} value={selectedNode.data.occupancy} unit="Pax" 
            delta={zoneOccDelta} isGood={true} historyData={zoneSeries} dataKey="occ" sparkColor="var(--accent-blue)" 
          />

          <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '16px 12px' }}>
            <BulletGraph
              label="Local Temp"
              value={parseFloat(selectedNode.data.temp)}
              max={Math.max(35, (selectedNode.data.setpoint ?? 24) + 10)}
              target={selectedNode.data.setpoint ?? 24}
              color={selectedNode.data.alert ? 'var(--accent-red)' : 'var(--accent-yellow)'}
              unit="°C"
            />
            {/* Occupancy scaled against the zone's own design capacity (its digitized
                floor area at the library's area-per-occupant), not a fixed 80-person
                bar that made every real room look empty. Withheld when the fixture
                carries no area for the zone rather than shown against a guess. */}
            {zoneDesignPax > 0 ? (
              <BulletGraph label="Occupancy" value={selectedNode.data.occupancy} max={zoneDesignPax} target={Math.round(zoneDesignPax * 0.6)} color="var(--accent-blue)" unit={`of ~${zoneDesignPax} Pax`} />
            ) : (
              <div style={{ fontSize: '10px', color: 'var(--text-secondary)', marginBottom: '14px', display: 'flex', justifyContent: 'space-between' }}>
                <span>Occupancy</span>
                <span style={{ fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--text-primary)' }}>{selectedNode.data.occupancy ?? 0} Pax</span>
              </div>
            )}
            {/* Plug draw replaces what used to be an "Integration Score" — a unitless
                index with no definition, computed from a lookup table keyed on zone-type
                names the digitizer stopped minting, so it read 0.6 for every zone in the
                building. This is a metered or modelled watt figure with a stated basis. */}
            <BulletGraph
              label={selectedNode.data.plugShed ? 'Plug Draw (swept)' : 'Plug Draw'}
              value={(selectedNode.data.plugW ?? 0) / 1000}
              max={Math.max(0.5, ((selectedNode.data.baseHeatGain ?? 0) / 1000) || 0.5)}
              target={((selectedNode.data.baseHeatGain ?? 0) / 1000) * 0.5}
              color={selectedNode.data.plugShed ? 'var(--accent-green)' : 'var(--accent-yellow)'}
              unit="kW"
              isLast
            />
          </div>

          {/* Manual Override Panel */}
          <div style={{ marginTop: '0.5rem' }}>
             <h3 style={{ fontSize: '10px', color: 'var(--text-secondary)', marginBottom: '8px' }}>MANUAL OVERRIDE / VETO</h3>
             <div style={{ display: 'flex', gap: '8px' }}>
               <button 
                 onClick={() => sendManualOverride && sendManualOverride('purge', selectedNode.id)}
                 style={{ flex: 1, padding: '8px', background: 'rgba(255,0,0,0.1)', border: '1px solid var(--accent-red)', color: 'var(--accent-red)', borderRadius: '4px', cursor: 'pointer', fontSize: '10px', fontWeight: 'bold' }}>
                 PURGE
               </button>
               <button 
                 onClick={() => sendManualOverride && sendManualOverride('cool', selectedNode.id)}
                 style={{ flex: 1, padding: '8px', background: 'rgba(0,150,255,0.1)', border: '1px solid var(--accent-blue)', color: 'var(--accent-blue)', borderRadius: '4px', cursor: 'pointer', fontSize: '10px', fontWeight: 'bold' }}>
                 MAX COOL
               </button>
               <button 
                 onClick={() => sendManualOverride && sendManualOverride('reset', selectedNode.id)}
                 style={{ flex: 1, padding: '8px', background: 'rgba(255,255,255,0.1)', border: '1px solid var(--border-glass)', color: 'var(--text-primary)', borderRadius: '4px', cursor: 'pointer', fontSize: '10px', fontWeight: 'bold' }}>
                 RESET
               </button>
             </div>
          </div>
        </div>
      ) : (
        <p style={{ fontSize: '11px', color: 'var(--text-secondary)', textAlign: 'center', marginTop: '2rem' }}>Detailed micro-metrics are only available for Zone nodes.</p>
      )}
    </aside>
  );
}
