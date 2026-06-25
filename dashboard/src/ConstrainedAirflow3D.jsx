import React, { useRef, useMemo } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';
import { mergeGeometries } from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import { buildFlowField3D } from './flowfield3d';
import { flowKeyOf, heat } from './flowfield';

// ----------------------------------------------------------------------------
// ConstrainedAirflow3D — volumetric (3D) airflow for one floor. Supply diffusers
// inject at the CEILING, returns pull LOW near the core, windows relieve mid-height,
// and full-height walls constrain everything — so the HVAC drives a real top-to-bottom
// circulation you can orbit around. Manim style: heat-coloured 3D arrows + tracer
// particles advecting through the room volume.
// ----------------------------------------------------------------------------

export default function ConstrainedAirflow3D({ floor, simState, layers = {} }) {
  const show = { walls: true, arrows: true, people: true, windows: true, hvac: true, electrical: false, ...layers };
  const flowKey = useMemo(() => flowKeyOf(floor, simState), [floor, simState]);
  const field = useMemo(() => buildFlowField3D(floor, simState), [floor, flowKey]); // eslint-disable-line

  if (!field) return null;
  return (
    <group>
      <FloorAndCeiling field={field} />
      {show.walls && <Walls field={field} />}
      {show.windows && <Windows field={field} />}
      {show.electrical && <Electrical field={field} />}
      {show.hvac && <HvacFixtures field={field} />}
      {show.people && <Occupants field={field} />}
      {show.arrows && <Arrows3D field={field} />}
    </group>
  );
}

function FloorAndCeiling({ field }) {
  const { minX, maxX, minZ, maxZ, H } = field.grid;
  const w = maxX - minX, d = maxZ - minZ, cx = (minX + maxX) / 2, cz = (minZ + maxZ) / 2;
  return (
    <group>
      <mesh position={[cx, 0, cz]} rotation={[-Math.PI / 2, 0, 0]}>
        <planeGeometry args={[w, d]} />
        <meshBasicMaterial color="#0d0d0d" transparent opacity={0.6} side={THREE.DoubleSide} />
      </mesh>
      {/* ceiling kept faint so we can see the volume from above */}
      <mesh position={[cx, H, cz]} rotation={[-Math.PI / 2, 0, 0]}>
        <planeGeometry args={[w, d]} />
        <meshBasicMaterial color="#0a0a0a" transparent opacity={0.06} side={THREE.DoubleSide} depthWrite={false} />
      </mesh>
    </group>
  );
}

// Full-height translucent walls + crisp edge lines.
function Walls({ field }) {
  const { H } = field.grid;
  const { quads, lines } = useMemo(() => {
    const pos = [], idxs = [], lpos = [];
    let v = 0;
    field.wallSegments.forEach(([x1, z1, x2, z2]) => {
      pos.push(x1, 0, z1, x2, 0, z2, x2, H, z2, x1, H, z1);
      idxs.push(v, v + 1, v + 2, v, v + 2, v + 3); v += 4;
      lpos.push(x1, H, z1, x2, H, z2, x1, 0, z1, x1, H, z1, x2, 0, z2, x2, H, z2); // top + verticals
    });
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(pos, 3));
    g.setIndex(idxs); g.computeVertexNormals();
    const lg = new THREE.BufferGeometry();
    lg.setAttribute('position', new THREE.Float32BufferAttribute(lpos, 3));
    return { quads: g, lines: lg };
  }, [field, H]);
  return (
    <group>
      <mesh geometry={quads}>
        <meshBasicMaterial color="#5a646e" transparent opacity={0.10} side={THREE.DoubleSide} depthWrite={false} />
      </mesh>
      <lineSegments geometry={lines}>
        <lineBasicMaterial color="#7f8b96" transparent opacity={0.5} />
      </lineSegments>
    </group>
  );
}

// Window panes on the envelope at sill height, oriented to the wall they sit on.
function Windows({ field }) {
  const { minX, maxX, minZ, maxZ } = field.grid;
  const eps = (maxX - minX) * 0.04;
  return (
    <group>
      {field.windowSegments.map((w, i) => {
        const onVertWall = Math.abs(w.x - minX) < eps || Math.abs(w.x - maxX) < eps;
        const size = onVertWall ? [0.15, 1.6, 2.0] : [2.0, 1.6, 0.15];
        return (
          <mesh key={i} position={[w.x, 1.5, w.z]}>
            <boxGeometry args={size} />
            <meshBasicMaterial color="#36d6ff" transparent opacity={0.55} toneMapped={false} />
          </mesh>
        );
      })}
    </group>
  );
}

function Electrical({ field }) {
  const { H } = field.grid;
  const geom = useMemo(() => {
    const pts = [];
    field.electrical.forEach((e) => { pts.push(e.x1, H - 0.25, e.z1, e.x2, H - 0.25, e.z2); });
    const g = new THREE.BufferGeometry();
    g.setAttribute('position', new THREE.Float32BufferAttribute(pts, 3));
    return g;
  }, [field, H]);
  return (
    <group>
      <lineSegments geometry={geom}>
        <lineBasicMaterial color="#ffaa00" transparent opacity={0.6} />
      </lineSegments>
      <mesh position={[field.panel.x, H - 0.5, field.panel.z]}>
        <boxGeometry args={[1.2, 1.0, 0.4]} />
        <meshBasicMaterial color="#ffaa00" toneMapped={false} />
      </mesh>
    </group>
  );
}

// Ceiling supply diffusers (with a downward throw cone) + low return grilles + ceiling duct.
function HvacFixtures({ field }) {
  const { H } = field.grid;
  const ringRefs = useRef([]);
  useFrame((s) => {
    const t = s.clock.elapsedTime;
    field.diffusers3d.forEach((d, i) => {
      const r = ringRefs.current[i]; if (!r) return;
      const sc = 0.7 + 0.3 * (0.5 + 0.5 * Math.sin(t * 3 + i)) * Math.min(1, d.strength);
      r.scale.set(sc, 1, sc);
    });
  });
  return (
    <group>
      {field.diffusers3d.map((d, i) => (
        <group key={i} position={[d.x, d.y, d.z]}>
          <mesh>
            <boxGeometry args={[1.0, 0.18, 1.0]} />
            <meshBasicMaterial color={d.alert ? '#ff5a3c' : '#21d4ff'} toneMapped={false} />
          </mesh>
          {/* downward throw cone */}
          <mesh ref={(el) => (ringRefs.current[i] = el)} position={[0, -0.7, 0]}>
            <coneGeometry args={[0.6, 1.2, 16, 1, true]} />
            <meshBasicMaterial color={d.alert ? '#ff5a3c' : '#21d4ff'} transparent opacity={0.18} side={THREE.DoubleSide} toneMapped={false} depthWrite={false} />
          </mesh>
        </group>
      ))}
      {field.returns.map((r, i) => (
        <mesh key={i} position={[r.x, 0.3, r.z]}>
          <boxGeometry args={[1.1, 0.5, 1.1]} />
          <meshBasicMaterial color="#ff8a3d" transparent opacity={0.85} toneMapped={false} />
        </mesh>
      ))}
    </group>
  );
}

function Occupants({ field }) {
  const bodyGeo = useMemo(() => new THREE.CapsuleGeometry(0.28, 0.8, 4, 8), []);
  const ref = useRef();
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const count = field.occupants.length;
  const applied = useRef(null);
  useFrame(() => {
    if (!ref.current || !count) return;
    if (applied.current === field.occupants) return; // positions are static per field
    field.occupants.forEach((o, i) => { dummy.position.set(o.x, 0.8, o.z); dummy.updateMatrix(); ref.current.setMatrixAt(i, dummy.matrix); });
    ref.current.instanceMatrix.needsUpdate = true;
    applied.current = field.occupants;
  });
  if (!count) return null;
  return (
    <instancedMesh key={count} ref={ref} args={[bodyGeo, null, count]}>
      <meshBasicMaterial color="#ffd27f" toneMapped={false} />
    </instancedMesh>
  );
}

// 3D arrow field oriented by the full (vx,vy,vz) velocity, heat-coloured by speed.
function Arrows3D({ field }) {
  const ref = useRef();
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const UP = useMemo(() => new THREE.Vector3(0, 1, 0), []);
  const arrowGeo = useMemo(() => {
    const shaft = new THREE.CylinderGeometry(0.05, 0.05, 0.6, 6); shaft.translate(0, 0.3, 0);
    const head = new THREE.ConeGeometry(0.15, 0.32, 10); head.translate(0, 0.76, 0);
    return mergeGeometries([shaft, head]);
  }, []);
  const items = useMemo(() => field.arrows.map((a) => {
    const dir = new THREE.Vector3(a.vx, a.vy, a.vz).normalize();
    const quat = new THREE.Quaternion().setFromUnitVectors(UP, dir);
    return { x: a.x, y: a.y, z: a.z, quat, len: 0.5 + a.norm * 1.7, rgb: heat(a.norm), phase: (a.x + a.z + a.y) * 0.3 };
  }), [field, UP]);
  const count = items.length;
  const colorArray = useMemo(() => {
    const arr = new Float32Array(Math.max(1, count) * 3);
    items.forEach((a, i) => { arr[i * 3] = a.rgb[0]; arr[i * 3 + 1] = a.rgb[1]; arr[i * 3 + 2] = a.rgb[2]; });
    return arr;
  }, [items, count]);
  useFrame((s) => {
    if (!ref.current || !count) return;
    const t = s.clock.elapsedTime;
    for (let i = 0; i < count; i++) {
      const a = items[i];
      const pulse = 0.82 + 0.3 * Math.sin(t * 2 - a.phase);
      dummy.position.set(a.x, a.y, a.z);
      dummy.quaternion.copy(a.quat);
      dummy.scale.set(1, a.len * pulse, 1);
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

