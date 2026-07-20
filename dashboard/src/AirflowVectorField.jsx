import React, { useRef, useMemo } from 'react';
import { useFrame } from '@react-three/fiber';
import * as THREE from 'three';
import { getBuilding } from './buildingStore';
const buildingData = getBuilding(); // live geometry — fetched before this module evaluates (see main.jsx)

// Curl noise helper
function makeNoise() {
  const fract = (v) => v - Math.floor(v);
  const hash = (x, y, z) => fract(Math.sin(x * 127.1 + y * 311.7 + z * 74.7) * 43758.5453);
  const smooth = (t) => t * t * (3 - 2 * t);
  const lerp = (a, b, t) => a + (b - a) * t;
  const valueNoise = (x, y, z) => {
    const xi = Math.floor(x), yi = Math.floor(y), zi = Math.floor(z);
    const xf = x - xi, yf = y - yi, zf = z - zi;
    const u = smooth(xf), v = smooth(yf), w = smooth(zf);
    const c = (i, j, k) => hash(xi + i, yi + j, zi + k);
    const x00 = lerp(c(0, 0, 0), c(1, 0, 0), u), x10 = lerp(c(0, 1, 0), c(1, 1, 0), u);
    const x01 = lerp(c(0, 0, 1), c(1, 0, 1), u), x11 = lerp(c(0, 1, 1), c(1, 1, 1), u);
    return lerp(lerp(x00, x10, v), lerp(x01, x11, v), w) * 2 - 1; // [-1, 1]
  };

  // Compute curl noise by finite difference of a vector potential
  return (x, y, z) => {
    const e = 0.1;
    // Vector potential fields (using offset inputs to decorrelate)
    const p1 = (vx, vy, vz) => valueNoise(vx, vy, vz);
    const p2 = (vx, vy, vz) => valueNoise(vx + 43.2, vy + 12.3, vz + 9.8);
    const p3 = (vx, vy, vz) => valueNoise(vx - 23.4, vy + 41.5, vz - 17.6);

    const dP3dy = (p3(x, y + e, z) - p3(x, y - e, z)) / (2 * e);
    const dP2dz = (p2(x, y, z + e) - p2(x, y, z - e)) / (2 * e);
    
    const dP1dz = (p1(x, y, z + e) - p1(x, y, z - e)) / (2 * e);
    const dP3dx = (p3(x + e, y, z) - p3(x - e, y, z)) / (2 * e);

    const dP2dx = (p2(x + e, y, z) - p2(x - e, y, z)) / (2 * e);
    const dP1dy = (p1(x, y + e, z) - p1(x, y - e, z)) / (2 * e);

    return new THREE.Vector3(dP3dy - dP2dz, dP1dz - dP3dx, dP2dx - dP1dy);
  };
}

function pointInPolygon(px, pz, poly) {
  let inside = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    let xi = poly[i][0] - 20, zi = 20 - poly[i][1];
    let xj = poly[j][0] - 20, zj = 20 - poly[j][1];
    let intersect = ((zi > pz) !== (zj > pz))
        && (px < (xj - xi) * (pz - zi) / (zj - zi) + xi);
    if (intersect) inside = !inside;
  }
  return inside;
}

export default function AirflowVectorField({ simState, activeFloor, selectedZone }) {
  const meshRef = useRef();
  const curlNoise = useMemo(() => makeNoise(), []);

  // 1. Determine bounding box for the target
  const { bounds, count, gridPositions, vavCentroids } = useMemo(() => {
    let targetZones = [];
    let floors = buildingData.floors;
    const floor = floors.find(f => f.level === activeFloor);
    if (!floor) return { count: 0 };
    
    if (selectedZone) {
      const z = floor.zones.find(z => z.zoneId === selectedZone);
      if (z) targetZones = [z];
    } else {
      targetZones = floor.zones;
    }

    if (targetZones.length === 0) return { count: 0 };

    let minX = Infinity, maxX = -Infinity, minZ = Infinity, maxZ = -Infinity;
    targetZones.forEach(z => {
      z.polygon.forEach(p => {
        const x = p[0] - 20;
        const zCoord = 20 - p[1];
        if (x < minX) minX = x;
        if (x > maxX) maxX = x;
        if (zCoord < minZ) minZ = zCoord;
        if (zCoord > maxZ) maxZ = zCoord;
      });
    });

    const maxY = floor.height || 4;

    // Seed a coarser grid than before — fewer, larger, *moving* arrows read as airflow far better
    // than a dense static blob. Single-zone view stays denser since it covers less area.
    const resX = selectedZone ? 10 : 18;
    const resY = 4;
    const resZ = selectedZone ? 10 : 12;

    let validPositions = [];
    for (let x = 0; x < resX; x++) {
      for (let y = 0; y < resY; y++) {
        for (let z = 0; z < resZ; z++) {
          const px = minX + (x / (resX - 1)) * (maxX - minX);
          const py = 0.5 + (y / (resY - 1)) * (maxY - 1);
          const pz = minZ + (z / (resZ - 1)) * (maxZ - minZ);

          let insideAny = false;
          for (const zone of targetZones) {
             if (pointInPolygon(px, pz, zone.polygon)) {
                insideAny = true;
                break;
             }
          }
          if (insideAny) {
             validPositions.push(px, py, pz);
          }
        }
      }
    }

    const count = validPositions.length / 3;
    const seeds = new Float32Array(validPositions);
    const live = Float32Array.from(seeds);           // mutable advected positions
    const ages = new Float32Array(count);            // staggered so they don't all recycle at once
    for (let i = 0; i < count; i++) ages[i] = Math.random();

    const vavCentroids = targetZones.map(z => ({
      x: z.centroid.x - 20,
      z: 20 - z.centroid.y,
      vavId: z.hvacMapping?.vavId,
      zoneId: z.zoneId,
      temp: simState?.zones?.[z.zoneId]?.temp || 24,
      setpoint: z.thermalProperties?.setpoint || 24,
      deadband: z.thermalProperties?.deadband || 2.0
    }));

    return { bounds: { minX, maxX, minZ, maxZ, maxY }, count, gridPositions: seeds, live, ages, vavCentroids };
  }, [activeFloor, selectedZone, simState]);

  const dummy = useMemo(() => new THREE.Object3D(), []);
  const color = useMemo(() => new THREE.Color(), []);
  const vel = useMemo(() => new THREE.Vector3(), []);
  const quat = useMemo(() => new THREE.Quaternion(), []);
  const UP = useMemo(() => new THREE.Vector3(0, 1, 0), []);
  const LIFE = 2.6;          // seconds a particle travels before recycling to its seed
  const FLOW = 3.2;          // advection speed (model units / s)

  // Velocity of the flow field at a point: divergence-free curl noise + a supply jet pushing
  // outward from the nearest VAV diffuser, biased gently downward (cold supply air falls).
  const fieldVelocity = (x, y, z, time, out) => {
    const s = 0.15;
    const curl = curlNoise(x * s, y * s + time, z * s);
    let closestVavDist = Infinity, closestVav = null;
    for (const v of vavCentroids) {
      const dx = x - v.x, dz = z - v.z;
      const d = Math.sqrt(dx * dx + dz * dz);
      if (d < closestVavDist) { closestVavDist = d; closestVav = v; }
    }
    out.set(curl.x + 0.5, curl.y - 0.25, curl.z + 0.5);
    if (closestVav && closestVavDist < 12) {
      const inv = 1 / (closestVavDist || 1);
      const jet = Math.max(0, 2.0 - closestVavDist / 6);
      out.x += (x - closestVav.x) * inv * jet;
      out.y += -0.2 * jet;
      out.z += (z - closestVav.z) * inv * jet;
    }
    return closestVav;
  };

  useFrame((state, delta) => {
    if (!meshRef.current || count === 0) return;
    const time = state.clock.elapsedTime * 0.5;
    const dt = Math.min(delta, 0.05);
    const { minX, maxX, minZ, maxZ, maxY } = bounds;
    const m = 0.6; // bounds margin before recycling

    for (let i = 0; i < count; i++) {
      const ix = i * 3;
      let x = live[ix], y = live[ix + 1], z = live[ix + 2];

      const closestVav = fieldVelocity(x, y, z, time, vel);
      // Unit flow direction, hardened against a zero/NaN field. A bad direction here would make a
      // NaN instance matrix and — with depthTest off — smear one garbage triangle over the whole
      // frame (blanking the canvas). `!(speed > 1e-4)` also catches NaN.
      let speed = vel.length();
      if (!(speed > 1e-4)) { vel.set(0, -1, 0); speed = 1; }
      vel.multiplyScalar(1 / speed); // vel is now a unit direction

      // advect at a steady speed, then recycle on lifetime / leaving the box. The bounds test is
      // positive-logic so a non-finite position FAILS it and resets to the seed (NaN can't persist).
      x += vel.x * FLOW * dt; y += vel.y * FLOW * dt; z += vel.z * FLOW * dt;
      ages[i] += dt / LIFE;
      const inBounds = x > minX - m && x < maxX + m && z > minZ - m && z < maxZ + m && y > 0.2 && y < maxY;
      if (!inBounds || ages[i] >= 1) {
        x = seeds[ix]; y = seeds[ix + 1]; z = seeds[ix + 2];
        ages[i] = 0;
      }
      live[ix] = x; live[ix + 1] = y; live[ix + 2] = z;

      // fade in at birth, fade out near death so recycling isn't a visible pop
      const fade = Math.sin(Math.min(1, Math.max(0, ages[i])) * Math.PI);
      const scale = 0.55 + 0.6 * fade;

      // orient the cone (+Y axis) along the flow — setFromUnitVectors has no up-vector degeneracy
      quat.setFromUnitVectors(UP, vel);
      dummy.position.set(x, y, z);
      dummy.quaternion.copy(quat);
      dummy.scale.setScalar(scale);
      dummy.updateMatrix();
      meshRef.current.setMatrixAt(i, dummy.matrix);

      // color by the supplied zone's thermal deviation: cyan (cold supply) -> green (in band)
      // -> amber -> red (warm / faulting). fade dims dying particles.
      let deviation = 0;
      if (closestVav) deviation = (closestVav.temp - closestVav.setpoint) / closestVav.deadband;
      const d = THREE.MathUtils.clamp(deviation, -1, 2);
      let r, g, b;
      if (d < 0)      { r = 0.0;          g = 0.7 + 0.3 * (1 + d); b = 1.0; }        // cyan-ish, over-cooled
      else if (d < 1) { r = d;            g = 1.0;                 b = 1.0 - d; }     // green -> yellow
      else            { r = 1.0;          g = 1.0 - (d - 1);       b = 0.0; }         // yellow -> red
      const dim = 0.35 + 0.65 * fade;
      color.setRGB(r * dim, g * dim, b * dim);
      meshRef.current.setColorAt(i, color);
    }

    meshRef.current.instanceMatrix.needsUpdate = true;
    if (meshRef.current.instanceColor) {
      meshRef.current.instanceColor.needsUpdate = true;
    }
  });

  if (count === 0) return null;

  return (
    <instancedMesh ref={meshRef} args={[null, null, count]} renderOrder={2}>
      <coneGeometry args={[0.3, 1.3, 8]} />
      {/* Unlit basic material so each cone shows its per-instance thermal color at full brightness
          (setColorAt). The previous meshStandardMaterial set emissiveIntensity but no emissive
          color, so the cones only caught the dim scene lighting and were effectively invisible. */}
      <meshBasicMaterial transparent opacity={0.95} toneMapped={false} depthTest={false} depthWrite={false} />
    </instancedMesh>
  );
}
