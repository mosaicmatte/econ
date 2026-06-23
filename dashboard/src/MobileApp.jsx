import React, { useState, useCallback, useMemo, useEffect } from 'react';
import { Activity, AlertTriangle, Settings, Zap, ChevronRight, ChevronUp, ChevronDown, User, X, BarChart2, ShieldAlert } from 'lucide-react';
import { useDigitalTwin } from './useDigitalTwin';
import BuildingModel from './BuildingModel';
import CanvasErrorBoundary from './CanvasErrorBoundary';
import TelemetryPanel from './TelemetryPanel';
import TelemetryLogs from './TelemetryLogs';
import MobileEnergyScreen from './MobileEnergyScreen';
import MobileImpactScreen from './MobileImpactScreen';
import LiveWeatherBackground from './LiveWeatherBackground';
import buildingData from './building-data.json';

export default function MobileApp() {
  const [activeFloor, setActiveFloor] = useState(8);
  const [selectedZone, setSelectedZone] = useState(null);
  const [activeModal, setActiveModal] = useState(null); // 'analytics', 'logs', 'controls'

  const onSimUpdate = useCallback((newSimData, currentScenario) => {
    // Mobile Viewer only uses the 3D map, so we don't need to update React Flow nodes/edges here
  }, []);

  const {
    simData,
    activeScenario,
    autoPilot,
    setAutoPilot,
    faultTarget,
    setFaultTarget,
    loadHistory,
    globalMetrics,
    loadScenario
  } = useDigitalTwin(onSimUpdate);

  // The zone currently in trouble (drives the "point straight at the problem" UX).
  const failingZone = useMemo(() => {
    if (!simData?.zones) return null;
    const zones = Object.values(simData.zones);
    // worst offender first: hard alert, then biggest deviation above setpoint
    const alerting = zones.filter(z => z.alert === true || z.alert === 'REMEDIATING');
    if (alerting.length) {
      return alerting.sort((a, b) => (b.temp - b.setpoint) - (a.temp - a.setpoint))[0];
    }
    return null;
  }, [simData]);

  // When a fault appears, fly the camera to its floor + highlight it; clear when resolved.
  useEffect(() => {
    if (failingZone && failingZone.level) setActiveFloor(failingZone.level);
  }, [failingZone?.id, failingZone?.level]);

  const focusZone = failingZone ? failingZone.id : selectedZone;

  // Refined system health (engine: severity-weighted comfort score), surfaced on mobile.
  const health = Math.round(simData?.systemHealth ?? 100);
  const healthColor = health >= 95 ? '#34C759' : health >= 80 ? '#FFD60A' : '#FF3B30';

  // Floor navigation bounds + stepper (manual browsing without a precise 3D tap).
  const levels = useMemo(() => buildingData.floors.map(f => f.level).sort((a, b) => a - b), []);
  const minLevel = levels[0], maxLevel = levels[levels.length - 1];
  const stepFloor = (d) => {
    setSelectedZone(null);
    setActiveFloor(f => Math.max(minLevel, Math.min(maxLevel, f + d)));
  };

  return (
    <div style={{ position: 'relative', height: '100dvh', width: '100vw', background: 'transparent', overflow: 'hidden', color: '#fff', fontFamily: 'system-ui, -apple-system, sans-serif', display: 'flex', flexDirection: 'column' }}>
      
      {/* Dynamic Weather Background */}
      <LiveWeatherBackground lat={10.8231} lon={106.6297} />

      {/* FLOATING HEADER */}
      <div style={{ position: 'absolute', top: 0, left: 0, right: 0, padding: '24px 20px', display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', zIndex: 10, pointerEvents: 'none' }}>
        <div>
          <div style={{ fontSize: '24px', fontWeight: '600' }}>ECON Center</div>
          <div style={{ fontSize: '14px', color: failingZone ? '#FF3B30' : '#34C759', fontWeight: '500', marginTop: '2px' }}>
            {failingZone
              ? `⚠ ${failingZone.label} · ${Number(failingZone.temp).toFixed(1)}°C`
              : 'Nominal Operation'}
          </div>
        </div>
        <div style={{ textAlign: 'right' }}>
          <div style={{ fontSize: '10px', color: 'rgba(255,255,255,0.55)', fontWeight: '600', letterSpacing: '0.14em', textTransform: 'uppercase' }}>Health</div>
          <div style={{ fontSize: '22px', fontWeight: '600', color: healthColor, lineHeight: 1.1 }}>{health}<span style={{ fontSize: '13px', color: 'rgba(255,255,255,0.5)' }}>%</span></div>
        </div>
      </div>

      {/* CSS for animating power flow lines */}
      <style>
        {`
          @keyframes flow-forward {
            to { stroke-dashoffset: -20; }
          }
          @keyframes flow-reverse {
            to { stroke-dashoffset: 20; }
          }
          .flow-line-forward {
            stroke-dasharray: 4 4;
            animation: flow-forward 1s linear infinite;
          }
          .flow-line-reverse {
            stroke-dasharray: 4 4;
            animation: flow-reverse 1s linear infinite;
          }
        `}
      </style>

      {/* 3D HERO VIEWPORT */}
      <div style={{ flex: '1', position: 'relative', pointerEvents: 'auto', minHeight: '300px' }}>
        <CanvasErrorBoundary>
        <BuildingModel
          simState={simData}
          activeFloor={activeFloor}
          onFloorClick={setActiveFloor} // Restored interaction
          showAirflow={false}
          selectedZone={selectedZone}
          setSelectedZone={setSelectedZone}
          viewMode="hybrid"
          isMobile={true}
        />
        </CanvasErrorBoundary>
        
        {/* TESLA-STYLE DATA POINTERS (ABSOLUTE FLOATING OVER 3D) */}
        {!selectedZone && (
          <>
            {/* HVAC Load (Top Left) */}
            {/* Total HVAC Load (Top Left) */}
            <div style={{ position: 'absolute', top: '100px', left: '20px', display: 'flex', flexDirection: 'column', alignItems: 'flex-start', zIndex: 10, pointerEvents: 'none' }}>
               <div style={{ color: '#ffffff', fontSize: '28px', fontWeight: '600', lineHeight: 1, marginBottom: '4px' }}>{(globalMetrics?.buildingLoadMw * 1000 || 0).toFixed(0)} <span style={{fontSize: '14px', color:'rgba(255,255,255,0.55)'}}>kW</span></div>
               <div style={{ color: 'rgba(255,255,255,0.68)', fontSize: '11px', fontWeight: '600', letterSpacing: '0.16em', textTransform: 'uppercase' }}>Total HVAC Load</div>
               {/* Tesla-Style Vertical Drop */}
               <div style={{ width: '1px', height: '35px', backgroundColor: 'rgba(245, 194, 66, 0.4)', marginLeft: '16px', marginTop: '8px' }} />
               <div style={{ width: '5px', height: '5px', borderRadius: '50%', backgroundColor: '#F5C242', marginLeft: '14px', boxShadow: '0 0 8px #F5C242' }} />
            </div>

            {/* Cooling Output (Top Right) */}
            <div style={{ position: 'absolute', top: '100px', right: '20px', display: 'flex', flexDirection: 'column', alignItems: 'flex-end', zIndex: 10, pointerEvents: 'none' }}>
               <div style={{ color: '#ffffff', fontSize: '28px', fontWeight: '600', lineHeight: 1, marginBottom: '4px' }}>{((globalMetrics?.coolingOutputMw * 1000) / 3.517 || 0).toFixed(0)} <span style={{fontSize: '14px', color:'rgba(255,255,255,0.55)'}}>Tons</span></div>
               <div style={{ color: 'rgba(255,255,255,0.68)', fontSize: '11px', fontWeight: '600', letterSpacing: '0.16em', textTransform: 'uppercase' }}>Cooling Output</div>
               {/* Tesla-Style Vertical Drop */}
               <div style={{ width: '1px', height: '35px', backgroundColor: 'rgba(74, 144, 226, 0.4)', marginRight: '16px', marginTop: '8px' }} />
               <div style={{ width: '5px', height: '5px', borderRadius: '50%', backgroundColor: '#4A90E2', marginRight: '14px', boxShadow: '0 0 8px #4A90E2' }} />
            </div>

            {/* Chiller COP (Bottom Left) */}
            <div style={{ position: 'absolute', top: '280px', left: '20px', display: 'flex', flexDirection: 'column', alignItems: 'flex-start', zIndex: 10, pointerEvents: 'none' }}>
               {/* Tesla-Style Vertical Shoot-Up */}
               <div style={{ width: '5px', height: '5px', borderRadius: '50%', backgroundColor: '#3DDC84', marginLeft: '14px', boxShadow: '0 0 8px #3DDC84' }} />
               <div style={{ width: '1px', height: '35px', backgroundColor: 'rgba(61, 220, 132, 0.4)', marginLeft: '16px', marginBottom: '8px' }} />
               <div style={{ color: '#ffffff', fontSize: '28px', fontWeight: '600', lineHeight: 1, marginBottom: '4px' }}>{(globalMetrics?.plantCop || 0).toFixed(1)} <span style={{fontSize: '14px', color:'rgba(255,255,255,0.55)'}}>COP</span></div>
               <div style={{ color: 'rgba(255,255,255,0.68)', fontSize: '11px', fontWeight: '600', letterSpacing: '0.16em', textTransform: 'uppercase' }}>Plant Efficiency</div>
            </div>

            {/* Grid Power (Bottom Right) */}
            <div style={{ position: 'absolute', top: '280px', right: '20px', display: 'flex', flexDirection: 'column', alignItems: 'flex-end', zIndex: 10, pointerEvents: 'none' }}>
               {/* Tesla-Style Vertical Shoot-Up */}
               <div style={{ width: '5px', height: '5px', borderRadius: '50%', backgroundColor: '#B8B8B8', marginRight: '14px', boxShadow: '0 0 8px rgba(255,255,255,0.5)' }} />
               <div style={{ width: '1px', height: '35px', backgroundColor: 'rgba(184, 184, 184, 0.4)', marginRight: '16px', marginBottom: '8px' }} />
               <div style={{ color: '#ffffff', fontSize: '28px', fontWeight: '600', lineHeight: 1, marginBottom: '4px' }}>{(globalMetrics?.gridPowerMw || 0).toFixed(1)} <span style={{fontSize: '14px', color:'rgba(255,255,255,0.55)'}}>MW</span></div>
               <div style={{ color: 'rgba(255,255,255,0.68)', fontSize: '11px', fontWeight: '600', letterSpacing: '0.16em', textTransform: 'uppercase' }}>Grid Power</div>
            </div>
          </>
        )}
        
        {/* FLOOR STEPPER — browse levels without a precise 3D tap */}
        {!selectedZone && (
          <div style={{ position: 'absolute', right: '16px', bottom: '24px', zIndex: 11, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: '2px', background: 'rgba(20,20,22,0.72)', backdropFilter: 'blur(12px)', borderRadius: '18px', padding: '6px', border: '1px solid rgba(255,255,255,0.08)', pointerEvents: 'auto' }}>
            <button onClick={() => stepFloor(1)} disabled={activeFloor >= maxLevel}
              style={{ background: 'transparent', border: 'none', color: activeFloor >= maxLevel ? 'rgba(255,255,255,0.25)' : '#fff', padding: '6px', display: 'flex', cursor: 'pointer' }}>
              <ChevronUp size={22} />
            </button>
            <div style={{ fontSize: '13px', fontWeight: '700', minWidth: '30px', textAlign: 'center', letterSpacing: '0.02em' }}>L{activeFloor}</div>
            <button onClick={() => stepFloor(-1)} disabled={activeFloor <= minLevel}
              style={{ background: 'transparent', border: 'none', color: activeFloor <= minLevel ? 'rgba(255,255,255,0.25)' : '#fff', padding: '6px', display: 'flex', cursor: 'pointer' }}>
              <ChevronDown size={22} />
            </button>
          </div>
        )}

        {/* Subtle gradient to blend into the list below */}
        <div style={{ position: 'absolute', bottom: 0, left: 0, right: 0, height: '40px', background: 'linear-gradient(to bottom, rgba(0,0,0,0), rgba(0,0,0,1))', pointerEvents: 'none' }} />
      </div>

      {/* BOTTOM SECTION (LIST MENU OR DRAWER) */}
      {!selectedZone ? (
        <div style={{ height: '35vh', padding: '0 20px 80px 20px', overflowY: 'auto', WebkitOverflowScrolling: 'touch', zIndex: 5 }}>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '4px', marginTop: '10px' }}>
          
          <MenuItem 
            icon={<Zap size={20} color="#FFD60A" />} 
            title="Energy" 
            onClick={() => setActiveModal('energy')} 
          />
          <MenuItem 
            icon={<BarChart2 size={20} color="#34C759" />} 
            title="Impact" 
            onClick={() => setActiveModal('impact')} 
          />
          <MenuItem 
            icon={<Activity size={20} color="#fff" />} 
            title="Analytics & Telemetry" 
            onClick={() => setActiveModal('analytics')} 
          />
          <MenuItem 
            icon={<Activity size={20} color="#fff" />} 
            title="System Logs" 
            onClick={() => setActiveModal('logs')} 
          />
          <MenuItem
            icon={<Settings size={20} color="#888" />}
            title="Scenario Controls"
            onClick={() => setActiveModal('controls')}
            bottomText={`Active: ${activeScenario}`}
          />
          <MenuItem
            icon={<ShieldAlert size={20} color={failingZone ? "#FF3B30" : "#fff"} />}
            title="Diagnostics"
            onClick={() => setActiveModal('faults')}
            highlight={!!failingZone}
            bottomText={failingZone ? `${failingZone.label} · ${Number(failingZone.temp).toFixed(1)}°C` : undefined}
          />
          
        </div>
        <div style={{ height: '40px' }} /> {/* Bottom padding */}
      </div>
      ) : (
        <RoomDetailDrawer
          zone={simData.zones[selectedZone]}
          simData={simData}
          onClose={() => setSelectedZone(null)}
        />
      )}

      {/* FULL SCREEN MODAL */}
      <div style={{ 
        position: 'absolute', top: activeModal ? 0 : '100dvh', left: 0, width: '100vw', height: '100dvh', 
        background: '#000000', zIndex: 50, transition: 'top 0.4s cubic-bezier(0.2, 0.8, 0.2, 1)',
        display: 'flex', flexDirection: 'column', color: '#fff',
        fontFamily: 'system-ui, -apple-system, BlinkMacSystemFont, "SF Pro Display", "SF Pro Text", sans-serif',
        overflow: 'hidden'
      }}>
        <div style={{ padding: '20px', display: 'flex', justifyContent: 'flex-end' }}>
          <button onClick={() => setActiveModal(null)} style={{ background: 'rgba(255,255,255,0.1)', border: 'none', borderRadius: '50%', padding: '8px', color: '#fff' }}>
            <X size={24} />
          </button>
        </div>

        <div style={{ flex: 1, overflowY: 'auto', WebkitOverflowScrolling: 'touch', padding: '20px 20px calc(20px + env(safe-area-inset-bottom))' }}>
          {activeModal === 'energy' && (
             <MobileEnergyScreen simData={simData} onClose={() => setActiveModal(null)} />
          )}
          {activeModal === 'impact' && (
             <MobileImpactScreen simData={simData} onClose={() => setActiveModal(null)} />
          )}
          {activeModal === 'analytics' && (
             <TelemetryPanel 
                simData={simData} 
                loadHistory={loadHistory} 
                activeScenario={activeScenario} 
                faultTarget={faultTarget}
                autoPilot={autoPilot}
                onOpenMaintenance={() => {}}
                isMobile={true}
             />
          )}
          {activeModal === 'logs' && (
             <TelemetryLogs simData={simData} />
          )}
          {activeModal === 'controls' && (
             <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
                <div style={{ fontSize: '12px', fontWeight: 'bold', color: '#888', letterSpacing: '1px' }}>SYSTEM OVERRIDES</div>
                <button 
                   onClick={() => { loadScenario('peak', setActiveFloor); setActiveModal(null); }}
                   style={{ padding: '16px', background: 'rgba(255,255,255,0.05)', color: '#fff', border: 'none', borderRadius: '12px', fontWeight: '600', fontSize: '16px', display: 'flex', justifyContent: 'center' }}
                >
                   TRIGGER NOMINAL PEAK LOAD
                </button>
                <button 
                   onClick={() => { loadScenario('fault:' + faultTarget, setActiveFloor); setActiveModal(null); }}
                   style={{ padding: '16px', background: 'rgba(255,59,48,0.1)', color: '#FF3B30', border: '1px solid rgba(255,59,48,0.3)', borderRadius: '12px', fontWeight: '600', fontSize: '16px', display: 'flex', justifyContent: 'center', gap: '8px' }}
                >
                   <AlertTriangle size={20} /> INJECT CRITICAL FAULT
                </button>
             </div>
           )}
           {activeModal === 'faults' && (
             <div style={{ padding: '20px', background: failingZone ? 'rgba(255,59,48,0.1)' : 'rgba(52,199,89,0.08)', border: `1px solid ${failingZone ? 'rgba(255,59,48,0.3)' : 'rgba(52,199,89,0.25)'}`, borderRadius: '12px' }}>
                <div style={{ color: failingZone ? '#FF3B30' : '#34C759', fontWeight: 'bold', marginBottom: '8px' }}>DIAGNOSTICS</div>
                <div style={{ color: '#fff' }}>
                  {failingZone
                    ? `CRITICAL: ${failingZone.label} at ${Number(failingZone.temp).toFixed(1)}°C — cooling capacity degraded. Root-cause analysis is available in Analytics.`
                    : 'All systems operating normally.'}
                </div>
             </div>
           )}
        </div>
      </div>

    </div>
  );
}

function MenuItem({ icon, title, onClick, highlight, bottomText, hideChevron }) {
  return (
    <div 
      onClick={onClick}
      style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '16px 0', borderBottom: '1px solid rgba(255,255,255,0.08)', cursor: 'pointer'
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
        {icon}
        <div style={{ display: 'flex', flexDirection: 'column' }}>
          <span style={{ fontSize: '16px', fontWeight: '500', color: highlight ? '#FF3B30' : '#fff' }}>{title}</span>
          {bottomText && <span style={{ fontSize: '12px', color: '#888', marginTop: '4px' }}>{bottomText}</span>}
        </div>
      </div>
      {!hideChevron && <ChevronRight size={20} color="#666" />}
    </div>
  );
}

function RoomDetailDrawer({ zone, simData, onClose }) {
  if (!zone) return null;
  // Real derived figures (no fabricated/flickering numbers): airflow from the zone's live VAV
  // flow (m³/min -> CFM), CO2 estimated from occupancy above the ~420 ppm outdoor baseline.
  const vav = simData ? Object.values(simData.vavs || {}).find(v => v.targetZone === zone.id) : null;
  const cfm = Math.round((vav?.flow || 0) * 35.3147) || Math.round(zone.occupancy * 17 + 120);
  const co2 = Math.min(1400, Math.round(420 + zone.occupancy * 22));
  return (
    <div style={{ 
      height: '45vh', 
      background: '#0B0B0D', 
      borderTopLeftRadius: '24px', 
      borderTopRightRadius: '24px', 
      borderTop: '1px solid rgba(255,255,255,0.08)', 
      padding: '20px 24px calc(24px + env(safe-area-inset-bottom))', 
      backdropFilter: 'blur(16px)',
      display: 'flex', flexDirection: 'column',
      zIndex: 10,
      position: 'relative',
      overflowY: 'auto',
      WebkitOverflowScrolling: 'touch'
    }}>
      {/* Handle */}
      <div style={{ width: '36px', height: '4px', borderRadius: '999px', background: 'rgba(255,255,255,0.20)', margin: '0 auto 16px' }} />
      
      {/* Header with Back button */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '20px' }}>
         <div style={{ display: 'flex', flexDirection: 'column' }}>
           <h3 style={{ margin: 0, fontSize: '18px', fontWeight: '600', color: '#fff' }}>{zone.label || zone.id}</h3>
           <span style={{ fontSize: '13px', color: 'rgba(255,255,255,0.60)', marginTop: '4px' }}>Floor {zone.level} • Zone ID: {zone.id.split('-').pop()}</span>
         </div>
         <button onClick={onClose} style={{ background: 'rgba(255,255,255,0.08)', border: 'none', borderRadius: '50%', padding: '6px', color: '#fff', display: 'flex', alignItems: 'center', cursor: 'pointer' }}>
           <X size={16} />
         </button>
      </div>

      {/* Primary Stat */}
      <div style={{ marginBottom: '24px' }}>
         <div style={{ fontSize: '34px', fontWeight: '600', lineHeight: 1, color: '#FFFFFF', marginBottom: '8px' }}>{zone.temp.toFixed(1)}<span style={{fontSize: '20px', color: 'rgba(255,255,255,0.60)'}}>°C</span></div>
         <div style={{ fontSize: '12px', fontWeight: '500', letterSpacing: '0.12em', color: 'rgba(255,255,255,0.60)', textTransform: 'uppercase' }}>Current Temp</div>
      </div>

      {/* 2x2 Grid */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '12px' }}>
         <StatCard label="SETPOINT" value={`${zone.setpoint.toFixed(1)}°C`} />
         <StatCard label="AIRFLOW" value={`${cfm} CFM`} />
         <StatCard label="CO₂ LEVEL" value={`${co2} ppm`} />
         <StatCard label="OCCUPANCY" value={`${zone.occupancy} People`} />
      </div>
      <div style={{ height: '40px', flexShrink: 0 }} /> {/* Extra space for scrolling */}
    </div>
  );
}

function StatCard({ label, value }) {
  return (
    <div style={{ background: 'rgba(255,255,255,0.04)', borderRadius: '14px', padding: '16px', border: '1px solid rgba(255,255,255,0.04)' }}>
      <div style={{ fontSize: '11px', fontWeight: '500', letterSpacing: '0.12em', color: 'rgba(255,255,255,0.60)', textTransform: 'uppercase', marginBottom: '8px' }}>{label}</div>
      <div style={{ fontSize: '18px', fontWeight: '600', color: '#fff' }}>{value}</div>
    </div>
  );
}
