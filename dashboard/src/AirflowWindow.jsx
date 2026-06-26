import React, { useState } from 'react';
import { Canvas } from '@react-three/fiber';
import { OrbitControls } from '@react-three/drei';
import CanvasErrorBoundary from './CanvasErrorBoundary';
import ConstrainedAirflow3D from './ConstrainedAirflow3D';

// Corner resize handle, mirroring the topology panel's handle.
function ResizeHandle({ size, setSize }) {
  return (
    <div
      className="resize-handle"
      onPointerDown={(e) => {
        e.preventDefault();
        const startW = size.w, startH = size.h, startX = e.clientX, startY = e.clientY;
        const onMove = (m) => {
          const dx = startX - m.clientX, dy = startY - m.clientY;
          setSize({
            w: Math.max(320, Math.min(startW + dx, window.innerWidth * 0.9)),
            h: Math.max(240, Math.min(startH + dy, window.innerHeight * 0.9)),
          });
        };
        const onUp = () => {
          document.removeEventListener('pointermove', onMove);
          document.removeEventListener('pointerup', onUp);
        };
        document.addEventListener('pointermove', onMove);
        document.addEventListener('pointerup', onUp);
      }}
      style={{
        position: 'absolute', top: -10, left: -10, width: 20, height: 20,
        background: 'var(--accent-blue)', cursor: 'nwse-resize', zIndex: 100,
        borderRadius: '50%', border: '2px solid #000',
      }}
    />
  );
}

// Small inline toggle chip for an airflow layer.
function LayerChip({ label, on, onClick, color }) {
  return (
    <button
      onClick={onClick}
      style={{
        background: on ? `${color}22` : 'transparent',
        border: `1px solid ${on ? color : 'var(--border-glass)'}`,
        color: on ? color : 'var(--text-muted)',
        fontSize: '8px', fontWeight: 'bold', letterSpacing: '0.04em',
        padding: '3px 6px', borderRadius: '4px', cursor: 'pointer',
      }}
    >
      {label}
    </button>
  );
}

// Standalone, resizable airflow viewer — its own panel + WebGL canvas (like the topology
// window). Renders the layout-constrained supply-air field (ConstrainedAirflow): a Manim
// arrow field + tracer streams that respect walls, doors, windows, diffusers, returns and
// occupants, instead of the old "radiate from every centroid" approximation.
export default function AirflowWindow({ floor, activeFloor, simState, size, setSize, onClose, bottom = 90, right = 24 }) {
  const [layers, setLayers] = useState({
    walls: true, arrows: true, people: true,
    windows: true, hvac: true, electrical: false, thermal: true,
  });
  const toggle = (k) => setLayers((l) => ({ ...l, [k]: !l[k] }));

  // Frame the camera on the floor footprint (same local centering the zones use:
  // x = px-20, z = 20-py). A high, slightly-tilted angle reads as a plan view.
  const xs = floor ? floor.geometry.exteriorPolygon.map((p) => p[0] - 20) : [-20, 20];
  const zs = floor ? floor.geometry.exteriorPolygon.map((p) => 20 - p[1]) : [-20, 20];
  const minX = Math.min(...xs), maxX = Math.max(...xs);
  const minZ = Math.min(...zs), maxZ = Math.max(...zs);
  const cx = (minX + maxX) / 2, cz = (minZ + maxZ) / 2;
  const span = Math.max(maxX - minX, maxZ - minZ, 10);
  const H = floor?.height || 4;

  return (
    <div style={{ position: 'absolute', width: size.w, height: size.h, bottom, right, zIndex: 11 }}>
      <ResizeHandle size={size} setSize={setSize} />
      <div
        className="topology-panel"
        style={{
          width: '100%', height: '100%', position: 'relative', overflow: 'hidden',
          borderRadius: '12px', border: '1px solid var(--border-glass)', background: 'var(--bg-panel)',
        }}
      >
        <div
          className="panel-header"
          style={{
            position: 'absolute', top: 0, left: 0, right: 0, zIndex: 10, padding: '10px 14px',
            background: 'var(--bg-panel)', display: 'flex', justifyContent: 'space-between', alignItems: 'center',
          }}
        >
          <span style={{ fontSize: '10px', color: 'var(--text-primary)', fontWeight: 'bold' }}>
            🌬 AIRFLOW — LEVEL {activeFloor}
          </span>
          <button
            onClick={onClose}
            style={{
              background: 'transparent', border: '1px solid var(--accent-blue)', color: 'var(--accent-blue)',
              fontSize: '9px', padding: '4px 8px', cursor: 'pointer', fontWeight: 'bold',
            }}
          >
            ✕ CLOSE
          </button>
        </div>

        {/* layer toggles */}
        <div style={{ position: 'absolute', top: 36, left: 14, right: 14, zIndex: 10, display: 'flex', gap: '4px', flexWrap: 'wrap' }}>
          <LayerChip label="ARROWS" on={layers.arrows} onClick={() => toggle('arrows')} color="#21d4ff" />
          <LayerChip label="WALLS" on={layers.walls} onClick={() => toggle('walls')} color="#8893a0" />
          <LayerChip label="HVAC" on={layers.hvac} onClick={() => toggle('hvac')} color="#21d4ff" />
          <LayerChip label="WINDOWS" on={layers.windows} onClick={() => toggle('windows')} color="#36d6ff" />
          <LayerChip label="PEOPLE" on={layers.people} onClick={() => toggle('people')} color="#ffd27f" />
          <LayerChip label="POWER" on={layers.electrical} onClick={() => toggle('electrical')} color="#ffaa00" />
        </div>

        <CanvasErrorBoundary>
          <Canvas
            frameloop="always"
            dpr={[1, 1.5]}
            gl={{ antialias: true, powerPreference: 'high-performance' }}
            camera={{ position: [cx + span * 0.62, H + span * 0.5, cz + span * 0.62], fov: 42 }}
            style={{ background: 'transparent' }}
          >
            <ambientLight intensity={0.9} />
            <directionalLight position={[cx, H * 4, cz]} intensity={0.5} />
            {/* full orbit so the 3D volume can be inspected from any angle */}
            <OrbitControls target={[cx, H * 0.4, cz]} maxPolarAngle={Math.PI * 0.92} />
            <gridHelper args={[span * 1.5, 24, '#1b3a2e', '#141414']} position={[cx, -0.02, cz]} />
            {floor && <ConstrainedAirflow3D floor={floor} simState={simState} layers={layers} />}
          </Canvas>
        </CanvasErrorBoundary>

        {/* heatmap legend */}
        <div style={{ position: 'absolute', bottom: 10, left: 16, right: 16, display: 'flex', alignItems: 'center', gap: '8px', pointerEvents: 'none' }}>
          <span style={{ fontSize: '8px', color: 'var(--text-secondary)', fontWeight: 'bold' }}>SLOW</span>
          <div style={{ flex: 1, height: '6px', borderRadius: '3px', background: 'linear-gradient(90deg, rgb(26,51,191), rgb(0,179,242), rgb(38,217,77), rgb(247,217,31), rgb(235,41,31))' }} />
          <span style={{ fontSize: '8px', color: 'var(--text-secondary)', fontWeight: 'bold' }}>FAST</span>
        </div>
      </div>
    </div>
  );
}
