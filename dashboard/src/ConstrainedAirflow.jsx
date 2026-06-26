import React, { useRef, useMemo, useEffect } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';
import { mergeGeometries } from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import { buildFlowField, flowKeyOf, heat, CELL } from './flowfield';

// ----------------------------------------------------------------------------
// ConstrainedAirflow — a Manim-style rendering of the solved supply-air field that
// is physically constrained by the room layout. It draws what the air respects
// (walls, doorways, windows, diffusers, return grilles, occupants, the electrical
// bus) and the field itself as (a) a grid of heat-coloured arrows and (b) animated
// tracer particles that advect along the velocity field and never cross a wall.
//
// Every layer can be toggled; defaults keep walls/arrows/streams/people on.
// ----------------------------------------------------------------------------

const heatColor = (t) => { const [r, g, b] = heat(t); return new THREE.Color(r, g, b); };

// round soft sprite for the glowing tracer points
function makeDotTexture() {
  const s = 64, c = document.createElement('canvas');
  c.width = c.height = s;
  const ctx = c.getContext('2d');
  const g = ctx.createRadialGradient(s / 2, s / 2, 0, s / 2, s / 2, s / 2);
  g.addColorStop(0, 'rgba(255,255,255,1)');
  g.addColorStop(0.4, 'rgba(255,255,255,0.6)');
  g.addColorStop(1, 'rgba(255,255,255,0)');
  ctx.fillStyle = g; ctx.fillRect(0, 0, s, s);
  const tex = new THREE.CanvasTexture(c);
  tex.needsUpdate = true;
  return tex;
}

export default function ConstrainedAirflow({ floor, simState, layers = {} }) {
  const show = {
    walls: true, arrows: true, streams: true, people: true,
    windows: true, hvac: true, electrical: false, thermal: true, ...layers,
  };

  const flowKey = useMemo(() => flowKeyOf(floor, simState), [floor, simState]);
  // The solve is the expensive step — only recompute when the field meaningfully changes.
  const field = useMemo(() => buildFlowField(floor, simState), [floor, flowKey]); // eslint-disable-line

  if (!field) return null;
  return (
    <group>
      {show.thermal && <ThermalFloor field={field} />}
      {show.walls && <Walls field={field} />}
      {show.windows && <Windows field={field} />}
      {show.electrical && <Electrical field={field} />}
      {show.hvac && <Diffusers field={field} simState={simState} floor={floor} />}
      {show.hvac && <Returns field={field} />}
      {show.people && <Occupants field={field} />}
      {show.arrows && <Arrows field={field} />}
      {show.streams && <Streams field={field} />}
    </group>
  );
}

// Faint per-room thermal fill so heat/occupancy context reads under the flow.
function ThermalFloor({ field }) {
  const geom = useMemo(() => {
    const geoms = [];
    field.zones.forEach((z) => {
      const shape = new THREE.Shape();
      z.poly.forEach((p, i) => (i === 0 ? shape.moveTo(p[0], p[1]) : shape.lineTo(p[0], p[1])));
      const g = new THREE.ShapeGeometry(shape);
      g.rotateX(Math.PI / 2); // shape XY -> floor XZ
      const dev = (z.temp - z.setpoint) / z.deadband;
      const t = Math.max(0, Math.min(1, 0.5 + dev * 0.25));
      const [r, gr, b] = heat(t);
      const col = [];
      const n = g.attributes.position.count;
      for (let i = 0; i < n; i++) col.push(r, gr, b);
      g.setAttribute('color', new THREE.Float32BufferAttribute(col, 3));
      geoms.push(g);
    });
    return geoms.length ? mergeGeometries(geoms) : null;
  }, [field]);
  if (!geom) return null;
  return (
    <mesh geometry={geom} position={[0, 0.02, 0]}>
      <meshBasicMaterial vertexColors transparent opacity={0.16} depthWrite={false} />
    </mesh>
  );
}

function Walls({ field }) {
  const geom = useMemo(() => {
    const pts = [];
    field.wallSegments.forEach(([x1, z1, x2, z2]) => { pts.push(x1, 0, z1, x2, 0, z2); });
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(pts, 3));
    return g;
  }, [field]);
  return (
    <lineSegments geometry={geom} position={[0, 0.12, 0]}>
      <lineBasicMaterial color="#6f7a85" transparent opacity={0.55} />
    </lineSegments>
  );
}

function Windows({ field }) {
  return (
    <group>
      {field.windowSegments.map((w, i) => (
        <mesh key={i} position={[w.x, 0.2, w.z]}>
          <boxGeometry args={[0.9, 0.4, 0.9]} />
          <meshBasicMaterial color="#36d6ff" toneMapped={false} transparent opacity={0.85} />
        </mesh>
      ))}
    </group>
  );
}

function Electrical({ field }) {
  const geom = useMemo(() => {
    const pts = [];
    field.electrical.forEach((e) => { pts.push(e.x1, 0, e.z1, e.x2, 0, e.z2); });
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(pts, 3));
    return g;
  }, [field]);
  return (
    <group>
      <lineSegments geometry={geom} position={[0, 0.08, 0]}>
        <lineBasicMaterial color="#ffaa00" transparent opacity={0.5} />
      </lineSegments>
      <mesh position={[field.panel.x, 0.3, field.panel.z]}>
        <boxGeometry args={[1.2, 0.6, 0.5]} />
        <meshBasicMaterial color="#ffaa00" toneMapped={false} />
      </mesh>
    </group>
  );
}

// Supply diffusers (one per VAV) — cyan plates with a flow-driven pulsing ring.
function Diffusers({ field }) {
  const ringRefs = useRef([]);
  useFrame((state) => {
    const t = state.clock.elapsedTime;
    field.diffusers.forEach((d, i) => {
      const r = ringRefs.current[i];
      if (!r) return;
      const s = 0.7 + 0.25 * Math.sin(t * 3 + i) * (0.5 + 0.5 * d.strength);
      r.scale.set(s, s, s);
    });
  });
  return (
    <group>
      {field.diffusers.map((d, i) => (
        <group key={i} position={[d.x, 0.35, d.z]}>
          <mesh rotation={[-Math.PI / 2, 0, 0]}>
            <boxGeometry args={[1.0, 1.0, 0.15]} />
            <meshBasicMaterial color={d.alert ? '#ff5a3c' : '#21d4ff'} toneMapped={false} />
          </mesh>
          <mesh ref={(el) => (ringRefs.current[i] = el)} rotation={[-Math.PI / 2, 0, 0]}>
            <ringGeometry args={[0.7, 0.95, 24]} />
            <meshBasicMaterial color={d.alert ? '#ff5a3c' : '#21d4ff'} transparent opacity={0.55} toneMapped={false} side={THREE.DoubleSide} />
          </mesh>
        </group>
      ))}
    </group>
  );
}

// Return-air grilles — magenta-orange, where air is drawn back to the AHU (core).
function Returns({ field }) {
  return (
    <group>
      {field.returns.map((r, i) => (
        <mesh key={i} position={[r.x, 0.28, r.z]} rotation={[-Math.PI / 2, 0, 0]}>
          <boxGeometry args={[1.1, 1.1, 0.1]} />
          <meshBasicMaterial color="#ff8a3d" transparent opacity={0.8} toneMapped={false} />
        </mesh>
      ))}
    </group>
  );
}

// Occupants ("humans") as warm capsule markers with a soft ground halo.
function Occupants({ field }) {
  const bodyGeo = useMemo(() => new THREE.CapsuleGeometry(0.28, 0.7, 4, 8), []);
  const ref = useRef();
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const count = field.occupants.length;
  useEffect(() => {
    if (!ref.current || !count) return;
    field.occupants.forEach((o, i) => {
      dummy.position.set(o.x, 0.7, o.z);
      dummy.updateMatrix();
      ref.current.setMatrixAt(i, dummy.matrix);
    });
    ref.current.instanceMatrix.needsUpdate = true;
  }, [field, count, dummy]);
  if (!count) return null;
  return (
    <group>
      <instancedMesh ref={ref} args={[bodyGeo, null, count]}>
        <meshBasicMaterial color="#ffd27f" toneMapped={false} />
      </instancedMesh>
      {field.occupants.map((o, i) => (
        <mesh key={i} position={[o.x, 0.05, o.z]} rotation={[-Math.PI / 2, 0, 0]}>
          <circleGeometry args={[0.55, 16]} />
          <meshBasicMaterial color="#ff9a3c" transparent opacity={0.22} depthWrite={false} />
        </mesh>
      ))}
    </group>
  );
}

// Manim ArrowVectorField — a regular grid of arrows along the SOLVED velocity field,
// length + colour encoding speed, with a gentle travelling pulse.
function Arrows({ field }) {
  const ref = useRef();
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const UP = useMemo(() => new THREE.Vector3(0, 1, 0), []);
  const arrowGeo = useMemo(() => {
    const shaft = new THREE.CylinderGeometry(0.05, 0.05, 0.66, 6); shaft.translate(0, 0.33, 0);
    const head = new THREE.ConeGeometry(0.16, 0.34, 10); head.translate(0, 0.83, 0);
    return mergeGeometries([shaft, head]);
  }, []);

  const items = useMemo(() => field.arrows.map((a) => {
    const dir = new THREE.Vector3(a.vx, 0, a.vz).normalize();
    const quat = new THREE.Quaternion().setFromUnitVectors(UP, dir);
    return { x: a.x, z: a.z, quat, len: 0.55 + a.norm * 1.9, rgb: heat(a.norm), phase: (a.x * dir.x + a.z * dir.z) * 0.35 };
  }), [field, UP]);
  const count = items.length;

  // Per-instance colours as an instancedBufferAttribute so the buffer exists when the
  // material first compiles — this is what makes USE_INSTANCING_COLOR get defined (setting
  // it lazily after first render silently leaves arrows white).
  const colorArray = useMemo(() => {
    const arr = new Float32Array(Math.max(1, count) * 3);
    items.forEach((a, i) => { arr[i * 3] = a.rgb[0]; arr[i * 3 + 1] = a.rgb[1]; arr[i * 3 + 2] = a.rgb[2]; });
    return arr;
  }, [items, count]);

  useFrame((state) => {
    if (!ref.current || !count) return;
    const t = state.clock.elapsedTime;
    for (let i = 0; i < count; i++) {
      const a = items[i];
      const pulse = 0.82 + 0.3 * Math.sin(t * 2.0 - a.phase);
      dummy.position.set(a.x, 0.5, a.z);
      dummy.quaternion.copy(a.quat);
      dummy.scale.set(1, a.len * pulse, 1); // arrows lie flat; +Y arrow tips into the XZ plane
      dummy.updateMatrix();
      ref.current.setMatrixAt(i, dummy.matrix);
    }
    ref.current.instanceMatrix.needsUpdate = true;
  });

  if (!count) return null;
  return (
    <instancedMesh key={count} ref={ref} args={[arrowGeo, null, count]}>
      <meshBasicMaterial toneMapped={false} />
      <instancedBufferAttribute attach="instanceColor" args={[colorArray, 3]} />
    </instancedMesh>
  );
}

// Animated tracer particles: seeded at diffusers, advected along the field, recoloured
// by local speed, and respawned when they stall or hit a wall/return/window.
function Streams({ field }) {
  const N = 260;
  const tex = useMemo(() => makeDotTexture(), []);
  const ref = useRef();
  const positions = useMemo(() => new Float32Array(N * 3), []);
  const colors = useMemo(() => new Float32Array(N * 3), []);

  // weighted diffuser picker so busier rooms emit more tracers
  const seeds = useMemo(() => {
    const total = field.diffusers.reduce((s, d) => s + d.strength, 0) || 1;
    return field.diffusers.map((d) => ({ x: d.x, z: d.z, w: d.strength / total }));
  }, [field]);

  const respawn = (i) => {
    let r = Math.random(), pick = seeds[0] || { x: field.center.x, z: field.center.z };
    for (const s of seeds) { r -= s.w; if (r <= 0) { pick = s; break; } }
    positions[i * 3] = pick.x + (Math.random() - 0.5) * 1.5;
    positions[i * 3 + 1] = 0.6;
    positions[i * 3 + 2] = pick.z + (Math.random() - 0.5) * 1.5;
    life.current[i] = 0.6 + Math.random() * 2.4;
  };
  const life = useRef(new Float32Array(N));

  useEffect(() => {
    for (let i = 0; i < N; i++) respawn(i);
    if (ref.current) { ref.current.geometry.attributes.position.needsUpdate = true; }
  }, [field]); // eslint-disable-line

  useFrame((_, delta) => {
    if (!ref.current) return;
    const dt = Math.min(delta, 0.05);
    const SPEED = 7.5 / field.maxSpeed; // normalise so motion reads regardless of solve scale
    for (let i = 0; i < N; i++) {
      let x = positions[i * 3], z = positions[i * 3 + 2];
      const [vx, vz, flag] = field.sample(x, z);
      const m = Math.hypot(vx, vz);
      life.current[i] -= dt;
      if (flag === CELL.WALL || m < field.maxSpeed * 0.012 || life.current[i] <= 0) { respawn(i); continue; }
      x += vx * SPEED * dt; z += vz * SPEED * dt;
      positions[i * 3] = x; positions[i * 3 + 2] = z;
      const t = Math.sqrt(m / field.maxSpeed);
      const [r, g, b] = heat(t);
      colors[i * 3] = r; colors[i * 3 + 1] = g; colors[i * 3 + 2] = b;
    }
    ref.current.geometry.attributes.position.needsUpdate = true;
    ref.current.geometry.attributes.color.needsUpdate = true;
  });

  return (
    <points ref={ref}>
      <bufferGeometry>
        <bufferAttribute attach="attributes-position" count={N} array={positions} itemSize={3} />
        <bufferAttribute attach="attributes-color" count={N} array={colors} itemSize={3} />
      </bufferGeometry>
      <pointsMaterial
        map={tex} size={1.1} vertexColors transparent depthWrite={false}
        blending={THREE.AdditiveBlending} sizeAttenuation toneMapped={false} opacity={0.95}
      />
    </points>
  );
}
