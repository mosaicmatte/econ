import React, { useRef, useMemo, useEffect } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';
import { mergeGeometries } from 'three/examples/jsm/utils/BufferGeometryUtils.js';

// Thermal heatmap: t in [0,1] -> rgb (deep blue -> cyan -> green -> yellow -> red).
const STOPS = [
  [0.0, [0.10, 0.20, 0.75]],
  [0.25, [0.00, 0.70, 0.95]],
  [0.5, [0.15, 0.85, 0.30]],
  [0.75, [0.97, 0.85, 0.12]],
  [1.0, [0.92, 0.16, 0.12]],
];
function heat(t) {
  t = Math.max(0, Math.min(1, t));
  for (let i = 0; i < STOPS.length - 1; i++) {
    const [a, ca] = STOPS[i], [b, cb] = STOPS[i + 1];
    if (t <= b) {
      const f = (t - a) / (b - a || 1);
      return [ca[0] + (cb[0] - ca[0]) * f, ca[1] + (cb[1] - ca[1]) * f, ca[2] + (cb[2] - ca[2]) * f];
    }
  }
  return STOPS[STOPS.length - 1][1];
}

function pointInPoly(x, z, poly) {
  let inside = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const xi = poly[i][0], zi = poly[i][1], xj = poly[j][0], zj = poly[j][1];
    if (((zi > z) !== (zj > z)) && (x < (xj - xi) * (z - zi) / (zj - zi) + xi)) inside = !inside;
  }
  return inside;
}

// Manim-style ArrowVectorField for HVAC supply airflow: a regular grid of arrows that follow the
// velocity field (air radiating from each VAV diffuser, i.e. the zone centroids), with arrow length
// AND color encoding speed via a gradient heatmap. A slow phase wave along the flow animates it.
export default function VectorFieldFlow({ floor, simState }) {
  const meshRef = useRef();
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const color = useMemo(() => new THREE.Color(), []);
  const UP = useMemo(() => new THREE.Vector3(0, 1, 0), []);

  // Unit arrow (shaft + head) pointing +Y, base at origin, length ~1.
  const arrowGeo = useMemo(() => {
    const shaft = new THREE.CylinderGeometry(0.055, 0.055, 0.66, 6);
    shaft.translate(0, 0.33, 0);
    const head = new THREE.ConeGeometry(0.17, 0.34, 10);
    head.translate(0, 0.83, 0);
    return mergeGeometries([shaft, head]);
  }, []);

  // Recompute the field only when live flows/faults change meaningfully (not every telemetry tick).
  const flowKey = useMemo(() => {
    if (!floor) return '';
    return (floor.zones || []).map((z) => {
      const f = simState?.vavs?.[z.hvacMapping?.vavId]?.flow ?? 0;
      const alert = simState?.zones?.[z.zoneId]?.alert ? 1 : 0;
      return `${Math.round(f * 2) / 2}:${alert}`;
    }).join('|');
  }, [floor, simState]);

  const { arrows, count } = useMemo(() => {
    if (!floor) return { arrows: [], count: 0 };
    // local frame matches the zones: x = px-20, z = 20-py
    const ext = floor.geometry.exteriorPolygon.map((p) => [p[0] - 20, 20 - p[1]]);
    const core = (floor.geometry.corePolygon || []).map((p) => [p[0] - 20, 20 - p[1]]);
    const xs = ext.map((p) => p[0]), zs = ext.map((p) => p[1]);
    const minX = Math.min(...xs), maxX = Math.max(...xs);
    const minZ = Math.min(...zs), maxZ = Math.max(...zs);

    // Each diffuser's strength tracks its VAV's LIVE airflow (and is boosted when its zone faults,
    // since the engine ramps flow there) — so the field reacts accurately to sim changes.
    const sources = (floor.zones || []).map((z) => {
      const flow = simState?.vavs?.[z.hvacMapping?.vavId]?.flow ?? 4;
      const alert = !!(simState?.zones?.[z.zoneId]?.alert);
      const strength = (0.5 + 0.8 * Math.min(flow, 12) / 12) * (alert ? 2.4 : 1);
      return { x: z.centroid.x - 20, z: 20 - z.centroid.y, strength };
    });

    // velocity field: air pushed radially outward from each diffuser, decaying with distance.
    const field = (x, z) => {
      let vx = 0, vz = 0;
      for (const s of sources) {
        const dx = x - s.x, dz = z - s.z;
        const d2 = dx * dx + dz * dz + 4.0;
        const inv = (6.0 * s.strength) / d2;
        vx += dx * inv; vz += dz * inv;
      }
      return [vx, vz];
    };

    // square-ish grid across the footprint
    const nx = 24, nz = Math.max(6, Math.round(nx * (maxZ - minZ) / (maxX - minX)));
    const raw = [];
    let maxMag = 1e-6;
    for (let i = 0; i < nx; i++) {
      for (let j = 0; j < nz; j++) {
        const x = minX + ((i + 0.5) / nx) * (maxX - minX);
        const z = minZ + ((j + 0.5) / nz) * (maxZ - minZ);
        if (core.length && pointInPoly(x, z, core)) continue; // no airflow in the elevator/stair core
        const [vx, vz] = field(x, z);
        const mag = Math.hypot(vx, vz);
        if (mag > maxMag) maxMag = mag;
        raw.push({ x, z, vx, vz, mag });
      }
    }

    const arrows = raw.map((p) => {
      const norm = Math.sqrt(p.mag / maxMag); // gamma for a fuller spread
      const dir = new THREE.Vector3(p.vx, 0, p.vz).normalize();
      const quat = new THREE.Quaternion().setFromUnitVectors(UP, dir);
      return {
        x: p.x, y: 0.6, z: p.z,
        quat,
        len: 0.7 + norm * 2.1,                 // faster flow -> longer arrow
        rgb: heat(norm),                       // heatmap by speed
        phase: (p.x * dir.x + p.z * dir.z) * 0.35,
      };
    });
    return { arrows, count: arrows.length };
  }, [floor, flowKey, UP]);

  // colors are static — set them once.
  useEffect(() => {
    if (!meshRef.current || count === 0) return;
    arrows.forEach((a, i) => { color.setRGB(a.rgb[0], a.rgb[1], a.rgb[2]); meshRef.current.setColorAt(i, color); });
    if (meshRef.current.instanceColor) meshRef.current.instanceColor.needsUpdate = true;
  }, [arrows, count, color]);

  // animate only a length pulse traveling along the flow (direction/position/color stay fixed).
  useFrame((state) => {
    if (!meshRef.current || count === 0) return;
    const t = state.clock.elapsedTime;
    for (let i = 0; i < count; i++) {
      const a = arrows[i];
      const pulse = 0.82 + 0.32 * Math.sin(t * 2.0 - a.phase);
      dummy.position.set(a.x, a.y, a.z);
      dummy.quaternion.copy(a.quat);
      dummy.scale.set(1, a.len * pulse, 1);
      dummy.updateMatrix();
      meshRef.current.setMatrixAt(i, dummy.matrix);
    }
    meshRef.current.instanceMatrix.needsUpdate = true;
  });

  if (count === 0) return null;
  return (
    <instancedMesh ref={meshRef} args={[arrowGeo, null, count]}>
      <meshBasicMaterial toneMapped={false} />
    </instancedMesh>
  );
}
