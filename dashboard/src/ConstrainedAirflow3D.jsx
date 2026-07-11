import React, { useRef, useMemo, useLayoutEffect } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';
import { mergeGeometries } from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import { buildFlowField3D } from './flowfield3d';
import { flowKeyOf, heat } from './flowfield';

// ----------------------------------------------------------------------------
// ConstrainedAirflow3D — volumetric (3D) airflow for one floor. Supply diffusers
// inject at the CEILING, returns pull LOW near the core, windows relieve mid-height,
// and full-height walls constrain everything — so the HVAC drives a real top-to-bottom
// circulation you can orbit around. Manim style: heat-coloured 3D arrows.
//
// Perf notes (a digitized floor carries ~90 zones): every repeated fixture —
// windows, diffuser plates, throw cones, return grilles, occupants — renders as ONE
// instanced mesh (a handful of draw calls total instead of hundreds), occupant
// density is capped, and the arrow pulse animates at 20 fps rather than every frame.
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

// One instanced mesh of identical boxes placed once (static fixtures).
function StaticInstances({ items, size, color, opacity = 1, place }) {
  const ref = useRef();
  const geo = useMemo(() => new THREE.BoxGeometry(size[0], size[1], size[2]), [size]);
  useLayoutEffect(() => {
    if (!ref.current) return;
    const dummy = new THREE.Object3D();
    items.forEach((it, i) => {
      place(dummy, it);
      dummy.updateMatrix();
      ref.current.setMatrixAt(i, dummy.matrix);
    });
    ref.current.instanceMatrix.needsUpdate = true;
  }, [items, place]);
  if (!items.length) return null;
  return (
    <instancedMesh key={items.length} ref={ref} args={[geo, null, items.length]}>
      <meshBasicMaterial color={color} transparent={opacity < 1} opacity={opacity} toneMapped={false} />
    </instancedMesh>
  );
}

// Window panes on the envelope at sill height, split by wall orientation into two
// instanced draws (was: one mesh per pane — 100+ draw calls on a digitized floor).
function Windows({ field }) {
  const { minX, maxX } = field.grid;
  const eps = (maxX - minX) * 0.04;
  const { vert, horiz } = useMemo(() => {
    const vert = [], horiz = [];
    field.windowSegments.forEach((w) => {
      (Math.abs(w.x - minX) < eps || Math.abs(w.x - maxX) < eps ? vert : horiz).push(w);
    });
    return { vert, horiz };
  }, [field, minX, maxX, eps]);
  const place = useMemo(() => (dummy, w) => { dummy.position.set(w.x, 1.5, w.z); dummy.rotation.set(0, 0, 0); dummy.scale.set(1, 1, 1); }, []);
  return (
    <group>
      <StaticInstances items={vert} size={[0.15, 1.6, 2.0]} color="#36d6ff" opacity={0.4} place={place} />
      <StaticInstances items={horiz} size={[2.0, 1.6, 0.15]} color="#36d6ff" opacity={0.4} place={place} />
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

// Ceiling supply diffusers (plate + static throw cone, strength baked into the cone
// scale) + low return grilles. All instanced with per-instance colour: 3 draw calls
// regardless of how many fixtures the floor carries.
function HvacFixtures({ field }) {
  const plateRef = useRef();
  const coneRef = useRef();
  const plateGeo = useMemo(() => new THREE.BoxGeometry(1.0, 0.18, 1.0), []);
  const coneGeo = useMemo(() => {
    const g = new THREE.ConeGeometry(0.6, 1.2, 12, 1, true);
    g.translate(0, -0.7, 0);
    // Luminance gradient baked into vertex colors — bright at the diffuser mouth,
    // fading toward the floor. Multiplied by the per-instance heat color it shows the
    // supply rate as a static gradient instead of the old pulsing animation.
    const pos = g.attributes.position;
    const cols = new Float32Array(pos.count * 3);
    for (let i = 0; i < pos.count; i++) {
      const t = Math.max(0, Math.min(1, (pos.getY(i) + 1.3) / 1.2)); // 0 tip -> 1 mouth
      const l = 0.12 + 0.88 * t;
      cols[i * 3] = l; cols[i * 3 + 1] = l; cols[i * 3 + 2] = l;
    }
    g.setAttribute('color', new THREE.BufferAttribute(cols, 3));
    return g;
  }, []);
  const diffusers = field.diffusers3d;

  useLayoutEffect(() => {
    if (!plateRef.current || !coneRef.current) return;
    const dummy = new THREE.Object3D();
    const color = new THREE.Color();
    diffusers.forEach((d, i) => {
      dummy.position.set(d.x, d.y, d.z);
      dummy.rotation.set(0, 0, 0);
      dummy.scale.set(1, 1, 1);
      dummy.updateMatrix();
      plateRef.current.setMatrixAt(i, dummy.matrix);
      coneRef.current.setMatrixAt(i, dummy.matrix);
      // Plate = equipment status (cyan / alarm red); cone = supply rate on the same
      // heat scale as the arrows, so cone color reads directly against the flow field.
      color.set(d.alert ? '#ff5a3c' : '#21d4ff');
      plateRef.current.setColorAt(i, color);
      if (d.alert) {
        color.set('#ff5a3c');
      } else {
        const [hr, hg, hb] = heat(Math.min(1, d.strength));
        color.setRGB(hr, hg, hb);
      }
      coneRef.current.setColorAt(i, color);
    });
    plateRef.current.instanceMatrix.needsUpdate = true;
    coneRef.current.instanceMatrix.needsUpdate = true;
    if (plateRef.current.instanceColor) plateRef.current.instanceColor.needsUpdate = true;
    if (coneRef.current.instanceColor) coneRef.current.instanceColor.needsUpdate = true;
  }, [diffusers]);

  const placeReturn = useMemo(() => (dummy, r) => { dummy.position.set(r.x, 0.3, r.z); dummy.rotation.set(0, 0, 0); dummy.scale.set(1, 1, 1); }, []);

  return (
    <group>
      {diffusers.length > 0 && (
        <instancedMesh key={`p${diffusers.length}`} ref={plateRef} args={[plateGeo, null, diffusers.length]}>
          <meshBasicMaterial toneMapped={false} />
        </instancedMesh>
      )}
      {diffusers.length > 0 && (
        <instancedMesh key={`c${diffusers.length}`} ref={coneRef} args={[coneGeo, null, diffusers.length]}>
          <meshBasicMaterial vertexColors transparent opacity={0.32} side={THREE.DoubleSide} toneMapped={false} depthWrite={false} />
        </instancedMesh>
      )}
      <StaticInstances items={field.returns} size={[1.1, 0.5, 1.1]} color="#ff8a3d" opacity={0.6} place={placeReturn} />
    </group>
  );
}

// Occupant markers, instanced and density-capped: a fully staffed digitized floor can
// mean 400+ people, which as a forest of capsules buries the flow field the window
// exists to show. An even sample keeps the distribution honest at a readable density.
const MAX_OCCUPANTS_SHOWN = 140;
function Occupants({ field }) {
  const bodyGeo = useMemo(() => new THREE.CapsuleGeometry(0.22, 0.7, 4, 8), []);
  const ref = useRef();
  const shown = useMemo(() => {
    const all = field.occupants;
    if (all.length <= MAX_OCCUPANTS_SHOWN) return all;
    const step = all.length / MAX_OCCUPANTS_SHOWN;
    const out = [];
    for (let i = 0; i < MAX_OCCUPANTS_SHOWN; i++) out.push(all[Math.floor(i * step)]);
    return out;
  }, [field]);

  useLayoutEffect(() => {
    if (!ref.current) return;
    const dummy = new THREE.Object3D();
    shown.forEach((o, i) => {
      dummy.position.set(o.x, 0.75, o.z);
      dummy.updateMatrix();
      ref.current.setMatrixAt(i, dummy.matrix);
    });
    ref.current.instanceMatrix.needsUpdate = true;
  }, [shown]);

  if (!shown.length) return null;
  return (
    <instancedMesh key={shown.length} ref={ref} args={[bodyGeo, null, shown.length]}>
      <meshBasicMaterial color="#ffd27f" transparent opacity={0.85} toneMapped={false} />
    </instancedMesh>
  );
}

// 3D arrow field oriented by the full (vx,vy,vz) velocity, heat-coloured by speed.
// The pulse animates at 20 fps — indistinguishable from per-frame, third of the CPU.
function Arrows3D({ field }) {
  const ref = useRef();
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const UP = useMemo(() => new THREE.Vector3(0, 1, 0), []);
  const lastTick = useRef(-1);
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
    if (t - lastTick.current < 0.05) return;
    lastTick.current = t;
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
