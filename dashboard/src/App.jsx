import { useState, useCallback, useEffect, useRef, useMemo } from 'react';
import { Users, Wind, Box, Zap, AlertTriangle, Activity, Settings, Map } from 'lucide-react';
import { ReactFlow, Background, Controls, Handle, Position, applyNodeChanges, applyEdgeChanges, MarkerType } from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import BuildingModel from './BuildingModel';
import buildingData from './building-data.json';
import * as flatbuffers from 'flatbuffers';
import { SimState } from './telemetry';

const INTEGRATION_BY_TYPE = {
  'server-room': 1.05,
  'corridor': 0.95,
  'lobby': 0.90,
  'mechanical': 0.85,
  'retail': 0.75,
  'conference': 0.70,
  'office': 0.55,
};



const getInitialSimData = () => {
  const data = { scenario: 'peak', ahuPressure: 500, buildingLoadMw: 0, systemHealth: 100, totalOccupants: 0, vavs: {}, zones: {}, logs: [] };
  buildingData.floors.forEach(floor => {
    floor.zones.forEach(z => {
      // Use pre-calculated centroid from building-data.json if available
      let cx = 20, cy = 20;
      if (z.centroid) {
        cx = z.centroid.x;
        cy = z.centroid.y;
      }
      
      if (z.hvacMapping) {
        data.vavs[z.hvacMapping.vavId] = { id: z.hvacMapping.vavId, targetZone: z.zoneId, flow: 0 };
      }
      data.zones[z.zoneId] = {
        id: z.zoneId,
        level: floor.level,
        label: z.name,
        type: z.zoneType,
        bim_asset_id: z.bim_asset_id,
        temp: z.thermalProperties?.setpoint || 24.0,
        setpoint: z.thermalProperties?.setpoint || 24.0,
        deadband: z.thermalProperties?.deadband || 2.0,
        alert: false,
        occupancy: z.thermalProperties?.occupancy || 0,
        integration_score: INTEGRATION_BY_TYPE[z.zoneType] || 0.6,
        baseHeatGain: z.thermalProperties?.internalHeatLoad || 0,
        centroid: { x: cx, y: cy }
      };
    });
  });
  return data;
};

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
  
  const rFloat = smoothstep(0.5, 1.5, deviation);
  const bFloat = smoothstep(0.5, 1.5, -deviation);
  const gFloat = 1.0 - Math.max(rFloat, bFloat);

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

const FloorplanImageNode = () => (
  <div className="floorplan-img-node" style={{ width: 800, height: 600, pointerEvents: 'none' }}>
    <img src="/floorplan.png" alt="Blueprint" style={{ width: '100%', height: '100%', opacity: 0.6, objectFit: 'contain' }} />
  </div>
);

const nodeTypes = {
  zone: ThermalNode,
  ahu: AHUNode,
  vav: VAVNode,
  floorplan: FloorplanImageNode
};

// Auto-generates a clean AHU -> VAV -> Zone P&ID for the active floor from
// whatever zones/VAVs exist in the loaded blueprint. Layout is structural (a
// single evenly-spaced row of zone columns fanning out from the AHU) rather than
// raw physical centroids, so it stays readable for any number of zones and any
// building. Columns are ordered west->east by centroid so they still echo the
// floor plan. Zones without a mapped VAV connect straight to the AHU.
const COL_SPACING = 210;
const buildTopologyFromSim = (simState, activeFloor) => {
  const nodes = [];
  const edges = [];

  const activeZones = Object.values(simState.zones)
    .filter(z => z.level === activeFloor)
    .sort((a, b) => (a.centroid.x - b.centroid.x) || (a.centroid.y - b.centroid.y));

  const totalWidth = Math.max(0, activeZones.length - 1) * COL_SPACING;
  const x0 = -totalWidth / 2;

  // AHU root, centered above the fan-out
  nodes.push({
    id: 'ahu-main', type: 'ahu', position: { x: -45, y: -300 },
    data: { label: 'AHU-MAIN', status: simState.scenario === 'fault' ? 'FAULT' : 'NOMINAL', pressure: simState.ahuPressure }
  });

  // Index VAVs by the zone they serve
  const vavByZone = {};
  Object.values(simState.vavs).forEach(v => { vavByZone[v.targetZone] = v; });

  activeZones.forEach((z, i) => {
    const x = x0 + i * COL_SPACING;
    const isServerFault = simState.scenario === 'fault' && z.type === 'server-room';
    const isRem = simState.scenario === 'remediating' && (z.type === 'server-room' || z.type === 'core');
    const stroke = isServerFault ? 'var(--accent-red)' : (isRem ? 'var(--accent-yellow)' : 'var(--accent-blue)');
    const edgeStyle = { stroke, strokeWidth: 1.5, strokeDasharray: isServerFault ? '4 4' : '5 5' };
    const markerEnd = { type: MarkerType.ArrowClosed, color: stroke };
    const className = !isServerFault ? 'edge-flow-vector-fast' : 'edge-flow-vector';

    nodes.push({
      id: z.id, type: 'zone', position: { x, y: 120 }, draggable: false,
      data: {
        label: z.label, temp: z.temp, setpoint: z.setpoint, deadband: z.deadband,
        occupancy: z.occupancy, alert: z.alert, integration_score: z.integration_score,
        bim_asset_id: z.bim_asset_id
      }
    });

    const v = vavByZone[z.id];
    if (v) {
      nodes.push({
        id: v.id, type: 'vav', position: { x: x + 20, y: -50 }, draggable: false,
        data: { label: v.id.toUpperCase(), flow: (v.flow || 0).toFixed(1) + ' m³/m' }
      });
      edges.push({ id: `e-ahu-${v.id}`, source: 'ahu-main', target: v.id, type: 'step', animated: true, className, style: edgeStyle, markerEnd });
      edges.push({ id: `e-${v.id}-${z.id}`, source: v.id, target: z.id, type: 'step', animated: true, className, style: edgeStyle, markerEnd });
    } else {
      // No dedicated VAV in this blueprint -> hang the zone directly off the AHU
      edges.push({ id: `e-ahu-${z.id}`, source: 'ahu-main', target: z.id, type: 'step', animated: true, className, style: edgeStyle, markerEnd });
    }
  });

  return { nodes, edges };
};

function CircularGauge({ value, max, label, unit, color }) {
  const radius = 30;
  const stroke = 4;
  const circumference = radius * 2 * Math.PI;
  const offset = circumference - (Math.min(value, max) / max) * circumference;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center' }}>
      <div style={{ position: 'relative', width: '70px', height: '70px', display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
        <svg style={{ position: 'absolute', transform: 'rotate(-90deg)' }} width="70" height="70">
          <circle cx="35" cy="35" r={radius} fill="none" stroke="var(--border-glass)" strokeWidth={stroke} />
          <circle cx="35" cy="35" r={radius} fill="none" stroke={color} strokeWidth={stroke} strokeDasharray={circumference} strokeDashoffset={offset} style={{ transition: 'stroke-dashoffset 0.5s ease' }} />
        </svg>
        <span className="mono" style={{ fontSize: '1rem', fontWeight: '500', color: 'var(--text-primary)' }}>{value}</span>
      </div>
      <span className="label-sm" style={{ marginTop: '8px', fontSize: '11px', color: 'var(--text-secondary)' }}>{label} ({unit})</span>
    </div>
  );
}

function App() {
  const [activeScenario, setActiveScenario] = useState('peak');
  const [autoPilot, setAutoPilot] = useState(true);
  const [activeFloor, setActiveFloor] = useState(6);
  const [selectedZone, setSelectedZone] = useState(null);
  const [faultTarget, setFaultTarget] = useState('zone-server-lvl8');
  const [showAiModal, setShowAiModal] = useState(false);
  const [isLeftPanelOpen, setIsLeftPanelOpen] = useState(false);
  const [showWindSim, setShowWindSim] = useState(false);
  
  // Initial topology
  const initialData = useMemo(() => getInitialSimData(), []);
  const initialTopo = useMemo(() => buildTopologyFromSim(initialData, 6), [initialData]);
  const [nodes, setNodes] = useState(initialTopo.nodes);
  const [edges, setEdges] = useState(initialTopo.edges);
  const [liveLogs, setLiveLogs] = useState([]);
  const [simData, setSimData] = useState(initialData);
  const wsRef = useRef(null);
  const logEndRef = useRef(null);
  const activeFloorRef = useRef(activeFloor);
  const activeScenarioRef = useRef(activeScenario);
  const simDataRef = useRef(initialData);

  const globalMetrics = useMemo(() => {
    if (!simData || !simData.zones) return { occupants: 0, avgTemp: 0 };
    let occupants = 0;
    let tempSum = 0;
    const zones = Object.values(simData.zones);
    zones.forEach(z => {
      occupants += (z.occupancy || 0);
      tempSum += parseFloat(z.temp) || 24.0;
    });
    return {
      occupants,
      avgTemp: zones.length ? (tempSum / zones.length).toFixed(1) : 0
    };
  }, [simData]);

  const selectedNode = nodes.find(n => n.selected);

  const onNodesChange = useCallback((changes) => setNodes((nds) => applyNodeChanges(changes, nds)), []);
  const onEdgesChange = useCallback((changes) => setEdges((eds) => applyEdgeChanges(changes, eds)), []);

  const loadScenario = (key) => {
    const baseScenario = key.startsWith('fault:') ? 'fault' : key;
    setActiveScenario(baseScenario);
    activeScenarioRef.current = baseScenario;
    if (baseScenario === 'fault' && autoPilot) {
      setShowAiModal(true);
    } else {
      setShowAiModal(false);
    }
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(key);
    }
  };

  const executeRemediation = () => {
    setShowAiModal(false);
    loadScenario('remediating');
    setTimeout(() => {
      loadScenario('peak');
    }, 8000);
  };

  // When activeFloor changes, completely rebuild the topology
  useEffect(() => {
    activeFloorRef.current = activeFloor;
    const topo = buildTopologyFromSim(simDataRef.current, activeFloor);
    setNodes(topo.nodes);
    setEdges(topo.edges);
  }, [activeFloor]);

  // Physics Engine Loop (WebSocket FlatBuffers Stream)
  useEffect(() => {
    const ws = new WebSocket('ws://localhost:8080/ws');
    ws.binaryType = 'arraybuffer';
    wsRef.current = ws;

    ws.onmessage = (event) => {
      const buf = new flatbuffers.ByteBuffer(new Uint8Array(event.data));
      const state = SimState.getRootAsSimState(buf);
      
      const prevData = simDataRef.current;
      const newSimData = { ...prevData, logs: liveLogs };
      newSimData.zones = { ...prevData.zones };
      newSimData.vavs = { ...prevData.vavs };

      const zonesLen = state.zonesLength();
      for(let i = 0; i < zonesLen; i++) {
        const z = state.zones(i);
        const id = z.id();
        if (newSimData.zones[id]) {
            const temp = z.temp();
            const type = newSimData.zones[id].type || newSimData.zones[id].zoneType;
            let alert = false;
            const isHot = (type === 'server-room' && temp > 25.0) || (type !== 'server-room' && temp > 28.0);
            if (isHot && (activeScenarioRef.current === 'fault' || activeScenarioRef.current === 'remediating')) {
                alert = activeScenarioRef.current === 'remediating' ? 'REMEDIATING' : true;
            }
            newSimData.zones[id] = { ...newSimData.zones[id], temp, load: z.load(), alert };
        }
      }

      const vavsLen = state.vavsLength();
      for(let i = 0; i < vavsLen; i++) {
        const v = state.vavs(i);
        const id = v.id();
        if (newSimData.vavs[id]) {
            newSimData.vavs[id] = { ...newSimData.vavs[id], flow: v.airflow() };
        }
      }

      const g = state.global();
      if (g) {
        newSimData.buildingLoadMw = g.buildingLoadMw();
        newSimData.systemHealth = g.systemHealth();
        newSimData.totalOccupants = g.totalOccupants();
      }

      simDataRef.current = newSimData;
      setSimData(newSimData);

      // Carefully update React Flow nodes
      setNodes(nds => nds.map(n => {
        if (n.type === 'zone' && newSimData.zones[n.id]) {
            return { ...n, data: { ...n.data, temp: newSimData.zones[n.id].temp.toFixed(1), alert: newSimData.zones[n.id].alert } };
        }
        if (n.type === 'vav' && newSimData.vavs[n.id]) {
            return { ...n, data: { ...n.data, flow: newSimData.vavs[n.id].flow.toFixed(1) + ' m³/m' } };
        }
        if (n.type === 'ahu') {
            return { ...n, data: { ...n.data, pressure: newSimData.ahuPressure } };
        }
        return n;
      }));

      // Update edge styles based on flow
      const newTopo = buildTopologyFromSim(newSimData, activeFloorRef.current);
      setEdges(newTopo.edges);
    };

    return () => {
      ws.close();
      wsRef.current = null;
    };
  }, []);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [liveLogs]);

  const failingZone = Object.values(simData.zones).find(z => z.alert === true || z.alert === 'REMEDIATING');

  return (
    <div className="hud-container">
      
      <div className="three-d-canvas-wrapper">
        <BuildingModel 
          simState={simData} 
          activeFloor={activeFloor} 
          onFloorClick={(level) => {
            setActiveFloor(level);
            setSelectedZone(null);
          }}
          showAirflow={showWindSim}
          selectedZone={selectedZone}
          setSelectedZone={setSelectedZone}
        />
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
            
            <div style={{ display: 'flex', flexDirection: 'column', gap: '1rem' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ color: 'var(--text-secondary)', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '6px' }}><Users size={14}/> Est. CO₂ Level</span>
                <span style={{ color: 'var(--accent-green)', fontFamily: 'monospace', fontWeight: 'bold', fontSize: '14px' }}>
                  {(simData.zones[selectedZone].occupancy * 15) + 400} ppm
                </span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ color: 'var(--text-secondary)', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '6px' }}><Activity size={14}/> Thermostat</span>
                <span style={{ color: 'var(--accent-blue)', fontFamily: 'monospace', fontWeight: 'bold', fontSize: '14px' }}>
                  {simData.zones[selectedZone].temp.toFixed(1)}°C / {simData.zones[selectedZone].setpoint.toFixed(1)}°C
                </span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                <span style={{ color: 'var(--text-secondary)', fontSize: '12px', display: 'flex', alignItems: 'center', gap: '6px' }}><Zap size={14}/> Est. Cost Rate</span>
                <span style={{ color: 'var(--accent-yellow)', fontFamily: 'monospace', fontWeight: 'bold', fontSize: '14px' }}>
                  ${(simData.zones[selectedZone].load * 0.12).toFixed(2)} / hr
                </span>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* LAYER 1: React Flow Topology in Bottom Right Corner */}
      <div className="minimap-wrapper" style={{ width: '600px', height: '400px', bottom: '24px', right: '24px', padding: 0, overflow: 'hidden' }}>
        <div className="topology-panel" style={{ width: '100%', height: '100%', position: 'relative' }}>
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
              <span style={{ fontSize: '10px', color: 'var(--accent-blue)' }}>{nodes.length - 1} ACTIVE NODES</span>
            </div>
          </div>

          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onNodeClick={(e, node) => {
              if (node.type === 'zone') setSelectedZone(node.id);
            }}
            nodeTypes={nodeTypes}
            fitView
            fitViewOptions={{ padding: 0.15 }}
            proOptions={{ hideAttribution: true }}
            minZoom={0.1}
            maxZoom={1.5}
            translateExtent={[[-1600, -800], [1600, 800]]}
            nodesDraggable={false}
          >
            <Background gap={40} size={1} color="rgba(255,255,255,0.05)" />
          </ReactFlow>
        </div>
      </div>

      {/* LAYER 4: AI & TELEMETRY (Left Dock) */}
      <div style={{ position: 'absolute', top: '1.5rem', left: '1.5rem', zIndex: 50 }}>
        <button 
           onClick={() => setIsLeftPanelOpen(!isLeftPanelOpen)}
           style={{ 
             background: 'var(--bg-panel)', 
             border: '1px solid var(--border-glass)', 
             color: 'var(--text-primary)', 
             padding: '8px 12px', 
             cursor: 'pointer',
             display: 'flex',
             alignItems: 'center',
             gap: '8px'
           }}
        >
           <Activity size={16} color="var(--accent-blue)" /> 
           <span style={{ fontSize: '10px', fontWeight: 'bold' }}>{isLeftPanelOpen ? 'HIDE AI & TELEMETRY' : 'SHOW AI & TELEMETRY'}</span>
        </button>
      </div>

      {isLeftPanelOpen && (
        <aside className="hud-dock-left" style={{ top: '4.5rem', height: 'calc(100vh - 6rem)' }}>
          <div className={`ai-synthesis-card ${activeScenario === 'fault' ? 'critical' : activeScenario === 'remediating' ? 'remediating' : ''}`}>
            <div className="ai-header">
              <Activity size={16} />
              <span>AI Scenario Synthesis</span>
            </div>
            <div className="ai-text">
              {activeScenario === 'fault' ? "CRITICAL: Server Room IT Load overriding cooling capacity. Airflow restriction detected via Hardy Cross pressure balance iteration. Temperature rising continuously." 
               : activeScenario === 'remediating' ? "AI OVERRIDE: Closing office dampers (10.0 Resistance) to force maximum VAV cooling capacity into the core. Thermodynamics converging." 
               : "Physics simulation running dynamically at 10 ticks per second using discrete-time Euler method equations for transient heat transfer."}
            </div>
          </div>

          <div style={{ flexGrow: 1, display: 'flex', flexDirection: 'column', minHeight: 0, marginTop: '1rem' }}>
            <span style={{ fontSize: '0.75rem', color: 'var(--text-secondary)', marginBottom: '0.5rem', textTransform: 'uppercase', letterSpacing: '0.1em' }}>
              Live Telemetry Stream
            </span>
            <div className="terminal-log-window mono">
              {liveLogs.map(log => (
                <div key={log.id} className={`log-line ${log.critical ? 'log-critical' : ''}`} style={{ color: activeScenario === 'remediating' ? 'var(--accent-yellow)' : undefined }}>
                  <span className="log-timestamp">[{log.time}]</span>
                  <span className="log-source">{log.source}</span>
                  <span className="log-payload">{log.msg}</span>
                </div>
              ))}
              <div ref={logEndRef} />
            </div>
          </div>
        </aside>
      )}

      {/* LAYER 2: Right Dock (Node Inspector moved up) */}
      <aside className="hud-dock-right">
        <div className="hud-header" style={{ marginBottom: '0', paddingBottom: '0.5rem', borderBottom: 'none' }}>
          <h2 style={{ fontSize: '1.25rem', color: 'var(--text-primary)' }}>{selectedNode ? 'Node Inspector' : 'Global Metrics'}</h2>
          <span className="mono" style={{ fontSize: '0.75rem', color: 'var(--text-secondary)' }}>{selectedNode?.id || 'OVERVIEW'}</span>
        </div>

        {!selectedNode ? (
          <>
            <div className="data-group" style={{ marginBottom: '0' }}>
               <div className="data-row">
                  <span className="data-label">Selected Level</span>
                  <span className="data-value mono" style={{ color: 'var(--accent-blue)' }}>L{activeFloor}</span>
               </div>
               <div className="data-row">
                  <span className="data-label">Space Syntax Hub</span>
                  <span className="data-value mono" style={{ color: 'var(--accent-red)' }}>Server-Room</span>
               </div>
               <div className="data-row">
                  <span className="data-label">Sys Health</span>
                  <span className="data-value mono" style={{ color: (simData.systemHealth ?? 100) < 70 ? 'var(--accent-red)' : (simData.systemHealth ?? 100) < 90 ? 'var(--accent-yellow)' : 'var(--accent-green)' }}>
                    {Math.round(simData.systemHealth ?? 100)}%
                  </span>
               </div>
            </div>

            <div className="gauge-cluster" style={{ border: 'none', margin: '0', padding: '1rem 0' }}>
              <CircularGauge value={simData.totalOccupants ?? 0} max={800} label="Occupants" unit="pax" color="var(--accent-blue)" />
              <CircularGauge value={globalMetrics.avgTemp} max={35} label="Avg Temp" unit="°C" color="var(--accent-yellow)" />
            </div>
          </>
        ) : selectedNode?.type === 'zone' ? (
          <>
            <div className="data-group" style={{ marginBottom: '0' }}>
              <div className="data-row">
                <span className="data-label">Identifier</span>
                <span className="data-value mono">{selectedNode.data.label}</span>
              </div>
              <div className="data-row">
                <span className="data-label">Status</span>
                <span className="data-value" style={{ fontSize: '0.75rem', padding: '2px 6px', borderRadius: '4px', background: selectedNode.data.alert === true || selectedNode.data.alert === 'FAULT' ? 'rgba(239,68,68,0.2)' : selectedNode.data.alert === 'REMEDIATING' ? 'rgba(234,179,8,0.2)' : 'rgba(34,197,94,0.2)', color: selectedNode.data.alert === true || selectedNode.data.alert === 'FAULT' ? 'var(--accent-red)' : selectedNode.data.alert === 'REMEDIATING' ? 'var(--accent-yellow)' : 'var(--accent-green)' }}>
                  {selectedNode.data.alert === true ? 'ALARM' : selectedNode.data.alert === 'REMEDIATING' ? 'REMEDIATING' : 'NOMINAL'}
                </span>
              </div>
              <div className="data-row">
                <span className="data-label">Integ. Score</span>
                <span className="data-value mono" style={{ color: selectedNode.data.integration_score > 1 ? 'var(--accent-red)' : 'var(--accent-blue)' }}>
                  {(selectedNode.data.integration_score ?? 0).toFixed(3)}
                </span>
              </div>
            </div>

            <div className="gauge-cluster" style={{ border: 'none', margin: '0', padding: '1rem 0' }}>
              <CircularGauge value={selectedNode.data.occupancy} max={80} label="Pax" unit="pax" color={selectedNode.data.alert ? 'var(--accent-red)' : 'var(--accent-blue)'} />
              <CircularGauge value={parseFloat(selectedNode.data.temp)} max={35} label="Temp" unit="°C" color={selectedNode.data.alert ? 'var(--accent-red)' : 'var(--accent-yellow)'} />
            </div>
          </>
        ) : (
          <p className="label-sm" style={{ color: 'var(--text-secondary)' }}>Detailed metrics only available for Zone Nodes.</p>
        )}
      </aside>

      {/* LAYER 3: Command Bar & Top Badges */}
      <div className="hud-top-badge">
        <Activity size={18} color="var(--accent-blue)" />
        <span style={{ fontSize: '0.875rem', color: 'var(--text-secondary)' }}>Bldg Load</span>
        <span className="mono" style={{ fontSize: '1.25rem', fontWeight: 600, color: 'var(--text-primary)' }}>
          {(simData.buildingLoadMw ?? 0).toFixed(2)} MW
        </span>
      </div>

      <div className="hud-command-bar">
        <button 
          className={`cmd-btn ${activeScenario === 'peak' ? 'active-peak' : ''}`} 
          onClick={() => loadScenario('peak')}
        >
          <Zap size={16} /> Peak Load Scenario
        </button>

        <div style={{ display: 'flex', gap: '0.5rem', alignItems: 'center', background: 'rgba(0,0,0,0.3)', padding: '0 0.5rem', borderRadius: '12px' }}>
          <select 
            value={faultTarget}
            onChange={(e) => setFaultTarget(e.target.value)}
            style={{ background: 'transparent', color: 'var(--text-secondary)', border: 'none', padding: '0.75rem', outline: 'none', fontFamily: 'Inter', fontSize: '14px', cursor: 'pointer' }}
          >
            <option value="zone-server-lvl8">L8 Server Room</option>
            <option value="zone-server-lvl12">L12 Server Room</option>
            <option value="zone-south-lvl3">L3 South Perimeter</option>
            <option value="zone-open-a-lvl6">L6 Open Office A</option>
            <option value="zone-core-lvl10">L10 Core Lobby</option>
          </select>
          <button 
            className={`cmd-btn ${activeScenario === 'fault' ? 'active-fault' : ''}`} 
            onClick={() => loadScenario(`fault:${faultTarget}`)}
            style={{ paddingLeft: '0.5rem' }}
          >
            <AlertTriangle size={16} /> Inject Fault
          </button>
        </div>

        <button 
          className={`cmd-btn ${autoPilot ? 'active-auto' : ''}`} 
          onClick={() => setAutoPilot(!autoPilot)}
        >
          <Settings size={16} /> AI Auto-Pilot: {autoPilot ? 'ON' : 'OFF'}
        </button>
      </div>

    </div>
  );
}

export default App;
