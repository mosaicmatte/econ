import { useState, useCallback, useEffect, useRef, useMemo } from 'react';
import { Users, Wind, Box, Zap, AlertTriangle, Activity, Settings, Map, Camera, Cpu, Thermometer, Lightbulb } from 'lucide-react';
import { ReactFlow, Background, Controls, Handle, Position, applyNodeChanges, applyEdgeChanges } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import BuildingModel, { SingleFloorLayout } from './BuildingModel';
import { getBuilding } from './buildingStore';
const buildingData = getBuilding(); // live geometry — fetched before this module evaluates (see main.jsx)
import TelemetryPanel from './TelemetryPanel';
import GlobalMetricsPanel from './GlobalMetricsPanel';
import TelemetryLogs from './TelemetryLogs';
import MaintenanceDrawer from './MaintenanceDrawer';
import BlueprintImportPanel from './BlueprintImportPanel';
import AiInsightsPanel from './AiInsightsPanel';
import * as flatbuffers from 'flatbuffers';
import MobileImpactScreen from './MobileImpactScreen';
import LiveWeatherBackground from './LiveWeatherBackground';
import UIErrorBoundary from './UIErrorBoundary';
import { API_BASE } from './api';
import { rateNow, money, touPeriodLabel, touPeriod } from './tariff';
import { SimState } from './telemetry';
import { useDigitalTwin, FAULT_ZONES, DEFAULT_FAULT_TARGET } from './useDigitalTwin';
import AirflowWindow from './AirflowWindow';
import CanvasErrorBoundary from './CanvasErrorBoundary';
import { LineChart, Line, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from 'recharts';
import { Canvas } from '@react-three/fiber';


// --- P&ID ENGINEERING CUSTOM NODES ---
// Custom smoothstep implementation for heatmap color
const smoothstep = (min, max, value) => {
  const x = Math.max(0, Math.min(1, (value - min) / (max - min)));
  return x * x * (3 - 2 * x);
};

const ThermalNode = ({ data, selected }) => {
  const setpoint = data.setpoint || 24.0;
  const deadband = data.deadband || 2.0;
  
  const deviation = (parseFloat(data.temp) - setpoint) / deadband;
  
  const rFloat = smoothstep(0.3, 1.0, deviation);
  const bFloat = smoothstep(0.3, 1.0, -deviation);
  const gFloat = 1.0 - Math.max(smoothstep(0.8, 1.5, deviation), smoothstep(0.8, 1.5, -deviation));

  const r = Math.round(rFloat * 255);
  const g = Math.round(gFloat * 255);
  const b = Math.round(bFloat * 255);

  const borderColor = `rgb(${r}, ${g}, ${b})`;
  const bgColor = `rgba(${r}, ${g}, ${b}, 0.1)`;

  return (
    <div className={`thermal-node ${selected ? 'selected' : ''} ${data.alert ? 'pulse-red-node' : ''}`} style={{ borderColor, backgroundColor: bgColor, transition: 'all 0.5s ease' }}>
      <Handle type="target" position={Position.Top} style={{ visibility: 'hidden' }} />
      <div className="thermal-label">{data.label}</div>
      <div className="thermal-value">{data.temp}°C</div>
      <div style={{ fontSize: '9px', color: 'var(--text-muted)' }}>{data.occupancy} PAX</div>
      <div style={{ fontSize: '7px', color: 'var(--accent-blue)', opacity: 0.8, marginTop: '2px', wordBreak: 'break-all', fontFamily: 'monospace' }}>BIM: {data.bim_asset_id?.split('-')[0]}</div>
      <Handle type="source" position={Position.Bottom} style={{ visibility: 'hidden' }} />
    </div>
  );
};

const AHUNode = ({ data, selected }) => (
  <div className={`node-ahu ${selected ? 'selected' : ''} ${data.status === 'FAULT' ? 'fault' : ''}`}>
    <div className="thermal-label" style={{ color: 'var(--accent-blue)' }}>{data.label}</div>
    <div className="thermal-value">SP: {data.pressure?.toFixed(0) || 500} Pa</div>
    <div style={{ fontSize: '9px', color: 'var(--text-muted)' }}>M: AUTO</div>
    <Handle type="source" position={Position.Bottom} style={{ visibility: 'hidden' }} />
  </div>
);

const VAVNode = ({ data, selected }) => (
  <div className={`node-vav ${selected ? 'selected' : ''}`}>
    <Handle type="target" position={Position.Top} style={{ visibility: 'hidden' }} />
    <div style={{ fontSize: '9px', fontWeight: 'bold', color: 'var(--text-primary)' }}>VAV</div>
    <div style={{ fontSize: '8px', color: 'var(--text-secondary)' }}>{data.flow.split(' ')[0]}</div>
    <Handle type="source" position={Position.Bottom} style={{ visibility: 'hidden' }} />
  </div>
);

// Professional BMS-style terminal-unit card: one card per zone, merging the zone and
// the VAV that feeds it (they map 1:1 here). Reads like an equipment-schedule row —
// status LED + colored rail (green in-band / amber drifting / red alarm), temperature
// against setpoint, occupancy, and the VAV airflow as a bar.
const UnitNode = ({ data, selected }) => {
  const temp = Number(data.temp || 0);
  const dev = Math.abs(temp - (data.setpoint || 24)) / (data.deadband || 2);
  const state = data.alert === true ? 'alarm' : (dev > 1 ? 'drift' : 'ok');
  const c = state === 'alarm' ? 'var(--accent-red)' : state === 'drift' ? 'var(--accent-yellow)' : 'var(--accent-green)';
  const flowFrac = Math.max(0, Math.min(1, (data.flowVal || 0) / 0.8));
  // Sides are set individually: mixing the `border` shorthand with `borderLeft` makes React
  // warn on rerender, because whichever lands last wins non-deterministically.
  const edge = `1px solid ${selected ? '#ffffff' : 'rgba(127, 139, 150, 0.35)'}`;
  return (
    <div style={{
      width: 148, background: 'rgba(13, 17, 20, 0.95)', borderRadius: 4, padding: '5px 7px',
      borderTop: edge, borderRight: edge, borderBottom: edge,
      borderLeft: `3px solid ${c}`, fontFamily: 'monospace',
      boxShadow: state === 'alarm' ? '0 0 10px rgba(255, 69, 58, 0.45)' : 'none',
    }}>
      <Handle type="target" position={Position.Top} style={{ visibility: 'hidden' }} />
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 4 }}>
        <span style={{ fontSize: '8px', fontWeight: 700, letterSpacing: '0.04em', color: 'var(--text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
          {(data.label || '').toUpperCase()}
        </span>
        <span style={{ flexShrink: 0, width: 6, height: 6, borderRadius: '50%', background: c, boxShadow: `0 0 4px ${c}` }} />
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 3 }}>
        <span style={{ fontSize: '9px', fontWeight: 700, color: c }}>{temp.toFixed(1)}°C</span>
        <span style={{ fontSize: '8px', color: 'var(--text-muted)' }}>SP {(data.setpoint ?? 24).toFixed(0)}°</span>
        <span style={{ fontSize: '8px', color: 'var(--text-secondary)' }}>{data.occupancy ?? 0} PAX</span>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginTop: 3 }}>
        <span style={{ fontSize: '7px', color: 'var(--text-muted)', flexShrink: 0 }}>{data.vavLabel}</span>
        <div style={{ flex: 1, height: 3, background: 'rgba(127, 139, 150, 0.25)', borderRadius: 2 }}>
          <div style={{ width: `${flowFrac * 100}%`, height: '100%', background: 'var(--accent-blue)', borderRadius: 2, transition: 'width 0.5s' }} />
        </div>
        <span style={{ fontSize: '7px', color: 'var(--accent-blue)', flexShrink: 0 }}>{(data.flowVal || 0).toFixed(1)}</span>
      </div>
    </div>
  );
};

const FloorplanNode = ({ data }) => {
  return (
    <div style={{ width: 800, height: 600, pointerEvents: 'none', position: 'relative' }}>
      <svg width="100%" height="100%" viewBox="0 0 800 600" preserveAspectRatio="none">
        {data.zones && data.zones.map((z, i) => {
          const points = z.polygon.map(p => `${(p[0] - 30) * 22 + 400},${(p[1] - 20) * 22 + 300}`).join(' ');
          return <polygon key={i} points={points} fill="none" stroke="rgba(255,255,255,0.15)" strokeWidth="2" />;
        })}
      </svg>
    </div>
  );
};

const CameraNode = ({ selected }) => (
  <div className={`node-icon ${selected ? 'selected' : ''}`} style={{ background: 'var(--bg-panel)', padding: '4px', border: '1px solid var(--text-secondary)', borderRadius: '4px', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
    <Camera size={12} color="var(--accent-blue)" />
    <Handle type="target" position={Position.Top} style={{ visibility: 'hidden' }} />
  </div>
);

const SensorNode = ({ selected }) => (
  <div className={`node-icon ${selected ? 'selected' : ''}`} style={{ background: 'var(--bg-panel)', padding: '4px', border: '1px solid var(--accent-yellow)', borderRadius: '4px', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
    <Thermometer size={12} color="var(--accent-yellow)" />
    <Handle type="target" position={Position.Top} style={{ visibility: 'hidden' }} />
  </div>
);

const ElectricalPanelNode = ({ data, selected }) => (
  <div className={`node-panel ${selected ? 'selected' : ''}`} style={{ background: 'var(--bg-panel)', padding: '8px', border: '2px solid var(--accent-red)', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
    <Zap size={16} color="var(--accent-red)" />
    <span style={{ fontSize: '8px', color: 'var(--text-primary)' }}>{data.label}</span>
    <Handle type="source" position={Position.Bottom} style={{ visibility: 'hidden' }} />
  </div>
);

const CircuitNode = ({ selected }) => (
  <div className={`node-circuit ${selected ? 'selected' : ''}`} style={{ background: 'var(--bg-panel)', padding: '4px', border: '1px solid var(--accent-red)', display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
    <Cpu size={12} color="var(--accent-red)" />
    <Handle type="target" position={Position.Top} style={{ visibility: 'hidden' }} />
    <Handle type="source" position={Position.Bottom} style={{ visibility: 'hidden' }} />
  </div>
);

const nodeTypes = {
  zone: ThermalNode,
  ahu: AHUNode,
  vav: VAVNode,
  unit: UnitNode,
  floorplan: FloorplanNode,
  camera: CameraNode,
  sensor: SensorNode,
  panel: ElectricalPanelNode,
  circuit: CircuitNode
};

// Professional riser layout: the AHU plant node sits on top and one terminal-unit
// card per zone (zone + its feeding VAV merged — they map 1:1) fills a sorted grid
// beneath it, like an equipment schedule. Replaces the old centroid-scattered boxes,
// which overlapped unreadably on a 90-zone digitized floor.
const buildTopologyFromSim = (simState, activeFloor, ontology) => {
  const nodes = [];
  const edges = [];

  // Normalize the Brick ontology to {source, target, predicate}. Tolerates both the legacy
  // shape ({ relationships: [{source, predicate, target}] }) and the digitized-pipeline shape
  // (a flat array of {subject, predicate, object}). Missing/empty ontology -> no edges (no crash).
  const rawRels = Array.isArray(ontology) ? ontology : (ontology?.relationships ?? []);
  const relationships = rawRels.map(r => ({
    source: r.source ?? r.subject,
    target: r.target ?? r.object,
    predicate: r.predicate,
  }));

  const activeZones = Object.values(simState.zones)
    .filter(z => z.level === activeFloor)
    .sort((a, b) => (a.label || a.id).localeCompare(b.label || b.id));

  const PER_ROW = 9, STEP_X = 166, STEP_Y = 92;
  const gridW = Math.min(Math.max(activeZones.length, 1), PER_ROW) * STEP_X - 18;

  nodes.push({
    id: 'ahu-main', type: 'ahu', draggable: false,
    position: { x: gridW / 2 - 70, y: -130 },
    data: { label: 'AHU-MAIN', status: simState.scenario === 'fault' ? 'FAULT' : 'NOMINAL', pressure: simState.ahuPressure }
  });

  activeZones.forEach((z, i) => {
    const col = i % PER_ROW, row = Math.floor(i / PER_ROW);

    // Topology driven by the Brick Schema semantic ontology: find the VAV feeding this zone.
    const feedsRel = relationships.find(r => r.target === z.id && r.predicate === 'brick:feeds' && r.source.startsWith('vav'));
    const vavId = feedsRel ? feedsRel.source : null;
    const v = vavId ? simState.vavs[vavId] : null;

    nodes.push({
      id: z.id, type: 'unit', draggable: false,
      position: { x: col * STEP_X, y: row * STEP_Y },
      data: {
        label: z.label, temp: z.temp, setpoint: z.setpoint, deadband: z.deadband,
        occupancy: z.occupancy, alert: z.alert,
        vavId, vavLabel: vavId ? vavId.toUpperCase() : 'DIRECT',
        flowVal: v?.flow || 0,
      }
    });

    // One thin supply line per unit off the AHU — together they read as the supply bus.
    edges.push({
      id: `e-ahu-${z.id}`, source: 'ahu-main', target: z.id, type: 'step',
      animated: false, style: { stroke: 'rgba(0, 163, 224, 0.16)', strokeWidth: 1 },
      data: { isFlow: true },
    });
  });

  return { nodes, edges };
};



// The floor that "needs attention" when the dashboard opens: the one holding the default
// critical asset (a server room, via DEFAULT_FAULT_TARGET). Data-driven so a regenerated
// building-data.json just works — no hard-coded level.
const ATTENTION_FLOOR = (() => {
  const f = buildingData.floors.find(fl => fl.zones.some(z => z.zoneId === DEFAULT_FAULT_TARGET));
  return f ? f.level : (buildingData.floors[Math.floor(buildingData.floors.length / 2)]?.level || 1);
})();

function App() {
  const [activeFloor, setActiveFloor] = useState(ATTENTION_FLOOR);
  const [selectedZone, setSelectedZone] = useState(null);
  const [showAiModal, setShowAiModal] = useState(false);
  const [panelSize, setPanelSize] = useState({ w: 600, h: 400 });
  const [airflowSize, setAirflowSize] = useState({ w: 560, h: 380 });
  const [rightPanelWidth, setRightPanelWidth] = useState(360);
  const [activeLeftTab, setActiveLeftTab] = useState('ai');
  const [isLeftPanelOpen, setIsLeftPanelOpen] = useState(true);
  const [leftPanelSize, setLeftPanelSize] = useState({ w: 360 });
  const [showWindSim, setShowWindSim] = useState(true);
  const [maintenanceTarget, setMaintenanceTarget] = useState(null);
  const [showImport, setShowImport] = useState(false);
  const [ontology, setOntology] = useState(null);
  const [viewMode, setViewMode] = useState('hybrid');
  const ontologyRef = useRef(null);

  // Phones get the lightweight live Impact screen instead of mounting the
  // WebGL-heavy desktop stack (three canvases + React Flow on a 1350-zone building).
  const [isMobile, setIsMobile] = useState(() => typeof window !== 'undefined' && window.matchMedia('(max-width: 820px)').matches);
  useEffect(() => {
    const mq = window.matchMedia('(max-width: 820px)');
    const onChange = (e) => setIsMobile(e.matches);
    mq.addEventListener('change', onChange);
    return () => mq.removeEventListener('change', onChange);
  }, []);

  const [nodes, setNodes] = useState([]);
  const [edges, setEdges] = useState([]);
  const [liveLogs, setLiveLogs] = useState([]);
  const logEndRef = useRef(null);
  const activeFloorRef = useRef(activeFloor);

  useEffect(() => {
    fetch(`${API_BASE}/api/ontology`)
      .then(res => res.json())
      .then(data => { setOntology(data); ontologyRef.current = data; })
      .catch(err => console.error("Failed to load Brick ontology:", err));
  }, []);

  // Physical edge nodes (ESP32 / Pico): poll which zones mirror real hardware so the
  // micro-HUD can badge them. Stays an empty map when no node has ever reported.
  const [hardwareNodes, setHardwareNodes] = useState({});
  useEffect(() => {
    let alive = true;
    const load = () => fetch(`${API_BASE}/api/hardware`)
      .then(res => res.json())
      .then(list => { if (alive) setHardwareNodes(Object.fromEntries((list || []).map(n => [n.zoneId, n]))); })
      .catch(() => {});
    load();
    const id = setInterval(load, 5000);
    return () => { alive = false; clearInterval(id); };
  }, []);

  // Kick the 3D canvas after mount so it paints on load. The main building <Canvas> fills a
  // 100vw/vh wrapper that can measure 0 during initial layout, leaving R3F's render loop idle
  // until something forces a re-measure (previously: toggling airflow). A couple of resize
  // pulses make it size + start drawing without any interaction.
  useEffect(() => {
    const ids = [60, 250, 600].map(ms => setTimeout(() => window.dispatchEvent(new Event('resize')), ms));
    return () => ids.forEach(clearTimeout);
  }, []);

  const onSimUpdate = useCallback((newSimData, currentScenario) => {
    setNodes(nds => nds.map(n => {
      if (n.type === 'unit' && newSimData.zones[n.id]) {
          const zz = newSimData.zones[n.id];
          const v = n.data.vavId ? newSimData.vavs[n.data.vavId] : null;
          return { ...n, data: { ...n.data, temp: zz.temp, alert: zz.alert, occupancy: zz.occupancy, flowVal: v ? v.flow : n.data.flowVal } };
      }
      if (n.type === 'ahu') {
          return { ...n, data: { ...n.data, pressure: newSimData.ahuPressure } };
      }
      return n;
    }));

    // Professional restraint: the supply bus stays a calm thin line in normal
    // operation and only animates (colored dashes) while a scenario is in flight.
    setEdges(eds => eds.map(e => {
      if (!e.data?.isFlow) return e;
      const isFault = currentScenario === 'fault';
      const isRem = currentScenario === 'remediating';
      return {
         ...e,
         animated: isFault || isRem,
         style: {
           ...e.style,
           stroke: isFault ? 'rgba(239, 68, 68, 0.45)' : isRem ? 'rgba(234, 179, 8, 0.45)' : 'rgba(0, 163, 224, 0.16)',
           strokeDasharray: (isFault || isRem) ? '4 4' : undefined,
         },
         markerEnd: undefined,
      };
    }));
  }, []);

  const onNodesChange = useCallback((changes) => setNodes((nds) => applyNodeChanges(changes, nds)), []);
  const onEdgesChange = useCallback((changes) => setEdges((eds) => applyEdgeChanges(changes, eds)), []);

  const {
    simData,
    initialData,
    activeScenario,
    autoPilot,
    setAutoPilot,
    faultTarget,
    setFaultTarget,
    loadHistory,
    globalMetrics,
    loadScenario,
    sendManualOverride,
    aiForecast
  } = useDigitalTwin(onSimUpdate);

  const executeRemediation = () => {
    setShowAiModal(false);
    loadScenario('remediating');
    setTimeout(() => {
      loadScenario('peak');
    }, 8000);
  };

  // When activeFloor changes or ontology loads, completely rebuild the topology
  useEffect(() => {
    activeFloorRef.current = activeFloor;
    // NOTE: In the original App.jsx, buildTopologyFromSim needs simData.
    // We pass simData to it to build initial nodes
    // Wait, buildTopologyFromSim is defined below this component? Yes.
    // We can just use simData directly.
    const topo = buildTopologyFromSim(simData, activeFloor, ontology);
    setNodes(topo.nodes);
    setEdges(topo.edges);
  }, [activeFloor, ontology]); // Only rebuild on floor or ontology change

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [liveLogs]);

  const failingZone = (activeScenario === 'fault' || activeScenario === 'remediating') && faultTarget
    ? (simData.zones[faultTarget] || { label: faultTarget, temp: 0 }) 
    : Object.values(simData.zones).find(z => z.alert === true || z.alert === 'REMEDIATING') || { label: 'Unknown Zone', temp: 0 };

  const selectedNode = nodes.find(n => n.selected);

  // Small viewport: serve the live mobile Impact screen (same stream, no WebGL).
  if (isMobile) {
    return (
      <MobileImpactScreen
        simData={simData}
        aiForecast={aiForecast}
        hardwareNodes={hardwareNodes}
      />
    );
  }

  return (
    <div className="hud-container">

      {/* Live time-of-day sky, shared with the mobile view: golden hour, morning, afternoon,
          sunset, evening. Sits behind the transparent 3D canvas. */}
      <LiveWeatherBackground lat={10.8231} lon={106.6297} />

      <div className="three-d-canvas-wrapper">
        <CanvasErrorBoundary>
          <BuildingModel
            simState={simData}
            activeFloor={activeFloor}
            onFloorClick={setActiveFloor}
            showAirflow={showWindSim}
            selectedZone={selectedZone}
            setSelectedZone={(zoneId) => {
              setSelectedZone(zoneId);
              setFaultTarget(zoneId);
            }}
            viewMode={viewMode}
          />
        </CanvasErrorBoundary>
      </div>

      {/* AI INTERACTIVE MODAL (Non-blocking so user can watch the building fail) */}
      {showAiModal && (
        <div style={{ position: 'absolute', top: '24px', left: '24px', zIndex: 50 }}>
          <div style={{ background: 'var(--bg-panel)', border: '1px solid var(--accent-red)', padding: '2rem', width: '450px', boxShadow: '0 10px 30px rgba(255,0,0,0.2)' }}>
            <h2 style={{ color: 'var(--accent-red)', display: 'flex', alignItems: 'center', gap: '0.5rem', marginTop: 0 }}>
              <AlertTriangle size={24} /> ALARM DETECTED
            </h2>
            <p style={{ color: 'var(--text-primary)', lineHeight: 1.5, fontFamily: 'monospace' }}>
              ERR: THERMAL_RUNAWAY<br/>
              LOCATION: {failingZone ? failingZone.label : 'Unknown Zone'}<br/>
              ASSET: {failingZone ? failingZone.bim_asset_id : '---'}
            </p>
            <div style={{ background: '#000', padding: '1rem', margin: '1.5rem 0', border: '1px solid var(--border-glass)' }}>
              <span style={{ fontSize: '0.75rem', color: 'var(--accent-blue)', textTransform: 'uppercase', letterSpacing: '1px' }}>AI Override Recommendation</span>
              <p style={{ margin: '0.5rem 0 0 0', color: 'var(--text-secondary)', fontFamily: 'monospace', lineHeight: 1.4 }}>
                The system detects critical thermal runaway.<br/>
                Would you like AI Auto-Pilot to automatically alleviate the problem by routing 100% cooling capacity to {failingZone ? failingZone.label : 'this specific room'}?
              </p>
            </div>
            <div style={{ display: 'flex', gap: '1rem', justifyContent: 'flex-end' }}>
              <button className="cmd-btn" onClick={() => setShowAiModal(false)}>IGNORE</button>
              <button className="cmd-btn active-fault" onClick={executeRemediation} style={{ background: 'var(--accent-red)' }}>EXECUTE RECOMMENDATION</button>
            </div>
          </div>
        </div>
      )}

      {/* LAYER 5: Micro-HUD for Drill-down */}
      {selectedZone && simData.zones[selectedZone] && (
        <div style={{
          position: 'absolute', top: '50%', left: '50%', transform: 'translate(-50%, -50%)',
          width: '100vw', height: '100vh', pointerEvents: 'none', zIndex: 40
        }}>
          {/* Cinematic Overlay gradient */}
          <div style={{ position: 'absolute', top: 0, left: 0, right: 0, bottom: 0, background: 'radial-gradient(circle, transparent 30%, rgba(0,0,0,0.85) 100%)' }} />
          
          <div style={{
            position: 'absolute', top: '25%', right: '25%',
            background: 'rgba(10,10,10,0.95)', border: '1px solid var(--accent-blue)',
            padding: '1.5rem', borderRadius: '12px', width: '320px', pointerEvents: 'auto',
            boxShadow: '0 0 40px rgba(0, 163, 224, 0.15)'
          }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1rem', borderBottom: '1px solid var(--border-glass)', paddingBottom: '0.75rem' }}>
              <h3 style={{ margin: 0, fontSize: '12px', color: 'var(--text-primary)', letterSpacing: '1px' }}>MICRO-TELEMETRY: {simData.zones[selectedZone].label.toUpperCase()}</h3>
              <button onClick={() => setSelectedZone(null)} style={{ background: 'transparent', border: '1px solid var(--accent-red)', borderRadius: '4px', padding: '4px 8px', color: 'var(--accent-red)', cursor: 'pointer', fontSize: '10px', fontWeight: 'bold' }}>EXIT [X]</button>
            </div>
            
            {hardwareNodes[selectedZone] && (
              <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '1rem' }}>
                <span style={{
                  fontSize: '9px', fontWeight: 'bold', letterSpacing: '0.06em', padding: '3px 8px', borderRadius: '4px',
                  color: hardwareNodes[selectedZone].online ? 'var(--accent-green)' : 'var(--text-secondary)',
                  border: `1px solid ${hardwareNodes[selectedZone].online ? 'var(--accent-green)' : 'var(--border-glass)'}`,
                  background: hardwareNodes[selectedZone].online ? 'rgba(46, 204, 113, 0.08)' : 'transparent',
                }}>
                  ⚡ LIVE HARDWARE — {(hardwareNodes[selectedZone].source || 'edge').toUpperCase()}{hardwareNodes[selectedZone].online ? '' : ' (OFFLINE)'}
                </span>
                {hardwareNodes[selectedZone].tempPinned && (
                  <span style={{ fontSize: '9px', color: 'var(--text-secondary)', fontFamily: 'monospace' }}>
                    sensor {hardwareNodes[selectedZone].hwTemp.toFixed(1)}°C
                  </span>
                )}
              </div>
            )}

            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              {/* CO₂ and humidity come straight off the telemetry stream: non-zero means a
                  real sensor on this zone is reporting, so the measured value wins. Zero means
                  nothing is measuring it and we say so rather than passing a model off as data. */}
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ color: 'var(--text-secondary)', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <Users size={14}/> CO₂ {simData.zones[selectedZone].co2 > 0 ? 'Level (measured)' : 'Level (modeled)'}
                </span>
                <span style={{ color: 'var(--accent-green)', fontFamily: 'monospace', fontWeight: 'bold', fontSize: '14px' }}>
                  {Math.round(simData.zones[selectedZone].co2 > 0
                    ? simData.zones[selectedZone].co2
                    : 400 + simData.zones[selectedZone].occupancy * 15)} ppm
                </span>
              </div>

              {simData.zones[selectedZone].humidity > 0 && (
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span style={{ color: 'var(--text-secondary)', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '6px' }}><Wind size={14}/> Humidity (measured)</span>
                  <span style={{ color: 'var(--accent-blue)', fontFamily: 'monospace', fontWeight: 'bold', fontSize: '14px' }}>
                    {simData.zones[selectedZone].humidity.toFixed(1)} %RH
                  </span>
                </div>
              )}
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ color: 'var(--text-secondary)', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '6px' }}><Activity size={14}/> Thermostat</span>
                <span style={{ color: 'var(--accent-blue)', fontFamily: 'monospace', fontWeight: 'bold', fontSize: '14px' }}>
                  {simData.zones[selectedZone].temp.toFixed(1)}°C / {simData.zones[selectedZone].setpoint.toFixed(1)}°C
                </span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ color: 'var(--text-secondary)', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '6px' }}><Lightbulb size={14}/> Lights</span>
                <span style={{ color: simData.zones[selectedZone].lightsOn === false ? 'var(--text-secondary)' : 'var(--accent-green)', fontFamily: 'monospace', fontWeight: 'bold', fontSize: '14px' }}>
                  {simData.zones[selectedZone].lightsOn === false ? 'OFF · SETBACK' : 'ON'}
                </span>
              </div>
              {/* Cost rate at the live EVN TOU band: the zone's thermal load becomes electrical
                  draw at the plant's COP, priced in đồng at whatever band the clock is in. */}
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ color: 'var(--text-secondary)', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <Zap size={14}/> Cost Rate ({touPeriodLabel(touPeriod())})
                </span>
                <span style={{ color: 'var(--accent-yellow)', fontFamily: 'monospace', fontWeight: 'bold', fontSize: '14px' }}>
                  {money((simData.zones[selectedZone].load / (simData.plantCop || 3.0)) * rateNow())} / hr
                </span>
              </div>
              {/* [GEMINI IMPLEMENTATION START] */}
              {/* Added by Gemini (Antigravity) on June 2026. */}
              {/* Added a Manual Veto section to the Micro-Telemetry panel */}
              {/* to allow operators to override the autonomous system. */}
              <div style={{ display: 'flex', gap: '8px', marginTop: '8px', borderTop: '1px solid rgba(255,255,255,0.1)', paddingTop: '12px' }}>
                <span style={{ fontSize: '10px', color: 'var(--text-secondary)', alignSelf: 'center' }}>MANUAL VETO:</span>
                <button onClick={() => sendManualOverride('LIGHTS_OFF;SETPOINT=26.0', selectedZone)} style={{ flex: 1, background: 'rgba(0,0,0,0.5)', border: '1px solid var(--accent-blue)', color: 'var(--accent-blue)', fontSize: '10px', padding: '6px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold' }}>FORCE OFF</button>
                <button onClick={() => sendManualOverride('LIGHTS_ON;SETPOINT=20.0', selectedZone)} style={{ flex: 1, background: 'rgba(0,0,0,0.5)', border: '1px solid var(--accent-red)', color: 'var(--accent-red)', fontSize: '10px', padding: '6px', cursor: 'pointer', borderRadius: '4px', fontWeight: 'bold' }}>MAX COOL</button>
              </div>
              {/* [GEMINI IMPLEMENTATION END] */}
            </div>
          </div>
        </div>
      )}

      {/* LAYER 1: React Flow Topology in Bottom Right Corner */}
      <div 
        className="minimap-wrapper" 
        style={{ 
          position: 'absolute', width: panelSize.w, height: panelSize.h, bottom: '90px', right: '24px', padding: 0, overflow: 'visible', zIndex: 10
        }}
      >
        <div 
          className="resize-handle" 
          onPointerDown={(e) => {
            e.preventDefault();
            const startW = panelSize.w;
            const startH = panelSize.h;
            const startX = e.clientX;
            const startY = e.clientY;
            const onPointerMove = (moveEvent) => {
              const dx = startX - moveEvent.clientX;
              const dy = startY - moveEvent.clientY;
              setPanelSize({
                w: Math.max(360, Math.min(startW + dx, window.innerWidth * 0.9)),
                h: Math.max(260, Math.min(startH + dy, window.innerHeight * 0.9))
              });
            };
            const onPointerUp = () => {
              document.removeEventListener('pointermove', onPointerMove);
              document.removeEventListener('pointerup', onPointerUp);
            };
            document.addEventListener('pointermove', onPointerMove);
            document.addEventListener('pointerup', onPointerUp);
          }}
          style={{
            position: 'absolute', top: -10, left: -10, width: 20, height: 20, background: 'var(--accent-blue)', 
            cursor: 'nwse-resize', zIndex: 100, borderRadius: '50%', border: '2px solid #000'
          }} 
        />
        <div className="topology-panel" style={{ width: '100%', height: '100%', position: 'relative', overflow: 'hidden', borderRadius: '12px', border: '1px solid var(--border-glass)', background: 'var(--bg-panel)' }}>
          <div className="panel-header" style={{ position: 'absolute', top: 0, left: 0, right: 0, zIndex: 10, padding: '12px 16px', background: 'var(--bg-panel)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <span style={{ fontSize: '10px', color: 'var(--text-primary)', fontWeight: 'bold' }}>MAP LEVEL {activeFloor} TOPOLOGY</span>
            
            <div style={{ display: 'flex', gap: '16px', alignItems: 'center' }}>
              <button 
                 onClick={() => setShowWindSim(!showWindSim)}
                 style={{ 
                   background: 'transparent', 
                   border: '1px solid var(--accent-blue)', 
                   color: 'var(--accent-blue)', 
                   fontSize: '9px', 
                   padding: '4px 8px', 
                   cursor: 'pointer',
                   fontWeight: 'bold',
                   pointerEvents: 'auto'
                 }}
              >
                 {showWindSim ? '⏸ HIDE AIRFLOW' : '🌬 SHOW AIRFLOW'}
              </button>
              <span style={{ fontSize: '10px', color: 'var(--accent-blue)' }}>{Math.max(0, nodes.length - 1)} TERMINAL UNITS · 1 AHU</span>
            </div>
          </div>

          <UIErrorBoundary name="Topology Map">
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeClick={(e, node) => {
              if (node.type === 'zone' || node.type === 'unit') setSelectedZone(node.id);
            }}
            nodeTypes={nodeTypes}
            fitView
            fitViewOptions={{ padding: 0.15 }}
            proOptions={{ hideAttribution: true }}
            minZoom={0.1}
            maxZoom={1.5}
            translateExtent={[[-350, -400], [1900, 1400]]}
            nodesDraggable={false}
          >
            <Background gap={40} size={1} color="rgba(255,255,255,0.05)" />
            
            {/* SVG Defs for Airflow Vector Gradients */}
            <svg style={{ position: 'absolute', width: 0, height: 0 }}>
              <defs>
                <linearGradient id="flow-nominal" x1="0%" y1="0%" x2="100%" y2="100%">
                  <stop offset="0%" stopColor="#3b82f6" />
                  <stop offset="100%" stopColor="#10b981" />
                </linearGradient>
                <linearGradient id="flow-fault" x1="0%" y1="0%" x2="100%" y2="100%">
                  <stop offset="0%" stopColor="#3b82f6" />
                  <stop offset="100%" stopColor="#ef4444" />
                </linearGradient>
                <linearGradient id="flow-rem" x1="0%" y1="0%" x2="100%" y2="100%">
                  <stop offset="0%" stopColor="#3b82f6" />
                  <stop offset="100%" stopColor="#eab308" />
                </linearGradient>
              </defs>
            </svg>
          </ReactFlow>
          </UIErrorBoundary>
        </div>
      </div>

      {/* Standalone, resizable airflow window (its own WebGL canvas), toggled by the
          "SHOW/HIDE AIRFLOW" control in the topology header. */}
      {showWindSim && (
        <AirflowWindow
          floor={buildingData.floors.find(f => f.level === activeFloor)}
          activeFloor={activeFloor}
          simState={simData}
          size={airflowSize}
          setSize={setAirflowSize}
          onClose={() => setShowWindSim(false)}
          right={24}
          bottom={90 + panelSize.h + 12}
        />
      )}

      {/* LAYER 4: AI & TELEMETRY (Left Dock) */}
      <div style={{ position: 'absolute', top: '1.5rem', left: '1.5rem', zIndex: 50, display: 'flex', gap: '8px', maxHeight: 'calc(100vh - 3rem)' }}>
        <button 
           onClick={() => setIsLeftPanelOpen(!isLeftPanelOpen)}
           style={{ 
             background: 'var(--bg-panel)', border: '1px solid var(--border-glass)', color: 'var(--text-primary)', 
             borderRadius: '12px', padding: '12px', cursor: 'pointer', height: 'fit-content', display: 'flex', alignItems: 'center'
           }}
        >
          <Activity size={20} color="var(--accent-blue)" />
        </button>

        {isLeftPanelOpen && (
          <div style={{ width: leftPanelSize.w, height: 'calc(100vh - 3rem)', background: 'var(--bg-panel)', border: '1px solid var(--border-glass)', borderRadius: '12px', display: 'flex', flexDirection: 'column', overflow: 'hidden', position: 'relative' }}>
            <div 
              className="resize-handle" 
              onPointerDown={(e) => {
                e.preventDefault();
                const startW = leftPanelSize.w;
                const startX = e.clientX;
                const onPointerMove = (moveEvent) => {
                  const dx = moveEvent.clientX - startX;
                  setLeftPanelSize({
                    w: Math.max(280, Math.min(startW + dx, window.innerWidth * 0.5)),
                  });
                };
                const onPointerUp = () => {
                  document.removeEventListener('pointermove', onPointerMove);
                  document.removeEventListener('pointerup', onPointerUp);
                };
                document.addEventListener('pointermove', onPointerMove);
                document.addEventListener('pointerup', onPointerUp);
              }}
              style={{
                position: 'absolute', top: '50%', right: 0, transform: 'translateY(-50%)', width: 10, height: 40, cursor: 'ew-resize', zIndex: 100
              }} 
            />
            <div style={{ display: 'flex', borderBottom: '1px solid var(--border-glass)' }}>
              <button 
                onClick={() => setActiveLeftTab('ai')}
                style={{ flex: 1, padding: '12px', background: activeLeftTab === 'ai' ? 'rgba(0, 163, 224, 0.1)' : 'transparent', color: activeLeftTab === 'ai' ? 'var(--accent-blue)' : 'var(--text-secondary)', border: 'none', borderBottom: activeLeftTab === 'ai' ? '2px solid var(--accent-blue)' : 'none', cursor: 'pointer', fontWeight: 'bold', fontSize: '10px' }}
              >
                AI INSIGHTS
              </button>
              <button 
                onClick={() => setActiveLeftTab('telemetry')}
                style={{ flex: 1, padding: '12px', background: activeLeftTab === 'telemetry' ? 'rgba(0, 163, 224, 0.1)' : 'transparent', color: activeLeftTab === 'telemetry' ? 'var(--accent-blue)' : 'var(--text-secondary)', border: 'none', borderBottom: activeLeftTab === 'telemetry' ? '2px solid var(--accent-blue)' : 'none', cursor: 'pointer', fontWeight: 'bold', fontSize: '10px' }}
              >
                PROFILER
              </button>
              <button 
                onClick={() => setActiveLeftTab('logs')}
                style={{ flex: 1, padding: '12px', background: activeLeftTab === 'logs' ? 'rgba(0, 163, 224, 0.1)' : 'transparent', color: activeLeftTab === 'logs' ? 'var(--accent-blue)' : 'var(--text-secondary)', border: 'none', borderBottom: activeLeftTab === 'logs' ? '2px solid var(--accent-blue)' : 'none', cursor: 'pointer', fontWeight: 'bold', fontSize: '10px' }}
              >
                LOGS
              </button>
            </div>
            
            <div style={{ flex: 1, overflowY: 'auto', padding: '16px' }}>
              {activeLeftTab === 'logs' ? (
                <TelemetryLogs simData={simData} />
              ) : activeLeftTab === 'telemetry' ? (
                <TelemetryPanel
                  simData={simData}
                  loadHistory={loadHistory}
                  activeScenario={activeScenario}
                  faultTarget={faultTarget}
                  onOpenMaintenance={() => setMaintenanceTarget(failingZone ? failingZone.bim_asset_id : null)}
                  autoPilot={autoPilot}
                  setAutoPilot={setAutoPilot}
                />
              ) : (
                <AiInsightsPanel
                  simData={simData}
                  activeScenario={activeScenario}
                  faultTarget={faultTarget}
                  aiForecast={aiForecast}
                  setAutoPilot={setAutoPilot}
                  hardwareNodes={hardwareNodes}
                  setSelectedZone={setSelectedZone}
                  sendManualOverride={sendManualOverride}
                />
              )}
            </div>
          </div>
        )}
      </div>

      {/* LAYER 2: Global Metrics (Right Dock) */}
      <GlobalMetricsPanel
        simData={simData}
        globalMetrics={globalMetrics}
        loadHistory={loadHistory}
        activeScenario={activeScenario}
        selectedNode={selectedNode}
        activeFloor={activeFloor}
        width={rightPanelWidth}
        setWidth={setRightPanelWidth}
        sendManualOverride={sendManualOverride}
        hardwareNodes={hardwareNodes}
      />

      {maintenanceTarget && (
        <MaintenanceDrawer
          zoneId={maintenanceTarget}
          simData={simData}
          onClose={() => setMaintenanceTarget(null)}
        />
      )}

      {/* VIEW MODE TOGGLE (Floating Top Center-Left) */}
      <div style={{
        position: 'absolute',
        top: '1.5rem',
        left: '50%',
        transform: 'translateX(-50%)',
        zIndex: 20,
        display: 'flex', 
        gap: '4px', 
        background: 'rgba(0,0,0,0.6)', 
        padding: '4px', 
        borderRadius: '8px', 
        border: '1px solid var(--border-glass)',
        backdropFilter: 'blur(10px)'
      }}>
        <button 
          onClick={() => setViewMode('physical')}
          style={{ padding: '6px 12px', fontSize: '11px', borderRadius: '4px', border: 'none', cursor: 'pointer', background: viewMode === 'physical' ? 'var(--accent-blue)' : 'transparent', color: viewMode === 'physical' ? '#fff' : 'var(--text-secondary)', fontWeight: 'bold' }}
        >
          PHYSICAL
        </button>
        <button 
          onClick={() => setViewMode('hybrid')}
          style={{ padding: '6px 12px', fontSize: '11px', borderRadius: '4px', border: 'none', cursor: 'pointer', background: viewMode === 'hybrid' ? 'var(--accent-blue)' : 'transparent', color: viewMode === 'hybrid' ? '#fff' : 'var(--text-secondary)', fontWeight: 'bold' }}
        >
          HYBRID
        </button>
        <button
          onClick={() => setViewMode('logical')}
          style={{ padding: '6px 12px', fontSize: '11px', borderRadius: '4px', border: 'none', cursor: 'pointer', background: viewMode === 'logical' ? 'var(--accent-blue)' : 'transparent', color: viewMode === 'logical' ? '#fff' : 'var(--text-secondary)', fontWeight: 'bold' }}
        >
          LOGICAL
        </button>
        <button
          onClick={() => setShowImport(true)}
          title="Digitize a DXF / PDF / scanned floorplan into a new building"
          style={{ padding: '6px 12px', fontSize: '11px', borderRadius: '4px', border: 'none', cursor: 'pointer', background: 'transparent', color: 'var(--accent-green)', fontWeight: 'bold', borderLeft: '1px solid var(--border-glass)' }}
        >
          + BLUEPRINT
        </button>
      </div>

      {showImport && <BlueprintImportPanel onClose={() => setShowImport(false)} />}

      {/* COMMAND BAR (Floating Bottom Center) */}
      <div className="hud-command-bar">
        <button 
          className={`cmd-btn ${activeScenario === 'peak' ? 'active-peak' : ''}`} 
          onClick={() => loadScenario('peak')}
        >
          <Zap size={16} /> Peak Load
        </button>

        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', background: 'rgba(0,0,0,0.3)', padding: '0 0.5rem', borderRadius: '12px' }}>
          <select 
            value={faultTarget}
            onChange={(e) => setFaultTarget(e.target.value)}
            style={{ background: 'transparent', color: 'var(--text-secondary)', border: 'none', padding: '0.5rem', outline: 'none', fontFamily: 'Inter', fontSize: '12px', cursor: 'pointer' }}
          >
            {FAULT_ZONES.map(z => (
              <option key={z.id} value={z.id}>{z.label}</option>
            ))}
          </select>
          <button
            className={`cmd-btn ${activeScenario === 'fault' ? 'active-fault' : ''}`}
            onClick={() => loadScenario(`fault:${faultTarget}`, (level) => setActiveFloor(level))}
            style={{ paddingLeft: '0.5rem' }}
          >
            <AlertTriangle size={16} /> Inject
          </button>
        </div>

        <button 
          className={`cmd-btn ${autoPilot ? 'active-auto' : ''}`} 
          onClick={() => setAutoPilot(!autoPilot)}
        >
          <Settings size={16} /> AI: {autoPilot ? 'ON' : 'OFF'}
        </button>
      </div>

    </div>
  );
}

export default App;
