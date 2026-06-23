import React, { useMemo, useRef, useEffect } from 'react';
import * as THREE from 'three';
import { mergeGeometries } from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import { buildFlowField, flowKeyOf } from './flowfield';

// ----------------------------------------------------------------------------
// FloorInfrastructure — the physical building services for the ACTIVE floor, drawn
// into the main 3D model: detailed HVAC (AHU + ceiling supply ducts + diffusers +
// return grilles), a detailed electrical grid (panel + ceiling cable trays/conduits +
// junction boxes), per-zone sensors (ceiling camera, wall thermostat, CO₂ monitor),
// and live occupants (people). Positions come from the SAME flow domain that drives
// the airflow window, so the two views stay consistent.
//
// PERF: everything is either MERGED into one geometry per colour (the static duct /
// conduit / fixture runs) or INSTANCED (sensors, people), and uses unlit basic
// materials — so the whole services layer is ~8 draw calls with no per-fragment
// lighting, instead of ~70 lit meshes. No element is dropped, only the cost.
//
// Frame: a plan point (px,py) is placed at (px-20, y, 20-py) — same as ZoneRenderer.
// ----------------------------------------------------------------------------

// A box positioned + (optionally) rotated about Y, baked into geometry for merging.
function box(cx, cy, cz, sx, sy, sz, rotY = 0) {
  const g = new THREE.BoxGeometry(sx, sy, sz);
  if (rotY) g.rotateY(rotY);
  g.translate(cx, cy, cz);
  return g;
}
// A box stretched to run between two plan points at height y (duct / cable tray).
function run(x1, z1, x2, z2, y, w, h) {
  const dx = x2 - x1, dz = z2 - z1;
  const len = Math.hypot(dx, dz) || 0.01;
  return box((x1 + x2) / 2, y, (z1 + z2) / 2, len, h, w, -Math.atan2(dz, dx));
}
function mergeOrNull(geos) {
  return geos.length ? mergeGeometries(geos) : null;
}

export default function FloorInfrastructure({ floor, simState, viewMode = 'hybrid' }) {
  const flowKey = useMemo(() => flowKeyOf(floor, simState), [floor, simState]);
  const field = useMemo(() => buildFlowField(floor, simState), [floor, flowKey]); // eslint-disable-line

  const isLogical = viewMode === 'logical';
  const isHybrid = viewMode === 'hybrid';
  const op = isLogical ? 0.25 : (isHybrid ? 0.7 : 1.0);
  const wf = isLogical;

  const built = useMemo(() => {
    if (!field) return null;
    const H = floor.height || 4;
    const yCeil = H - 0.18, yTray = H - 0.5;
    const ahu = field.panel;

    const cyan = [], red = [], amber = [], orange = [];
    // HVAC: AHU + supply duct star + diffuser boxes
    cyan.push(box(ahu.x, yCeil - 0.3, ahu.z, 2.2, 1.0, 1.6));
    field.diffusers.forEach((d) => {
      const bucket = d.alert ? red : cyan;
      bucket.push(run(ahu.x, ahu.z, d.x, d.z, yCeil, 0.28, 0.28));
      bucket.push(box(d.x, yCeil, d.z, 0.9, 0.18, 0.9));
    });
    // returns low near the core
    field.returns.forEach((r) => orange.push(box(r.x, 0.4, r.z, 1.0, 0.5, 1.0)));
    // Electrical: panel + cable trays + junction boxes
    amber.push(box(ahu.x + 1.6, 1.4, ahu.z, 0.4, 1.6, 1.0));
    field.electrical.forEach((e) => {
      amber.push(run(ahu.x + 1.6, ahu.z, e.x2, e.z2, yTray, 0.12, 0.08));
      amber.push(box(e.x2, yTray - 0.2, e.z2, 0.3, 0.4, 0.3));
    });

    // Sensors (per non-corridor zone): instanced matrices
    const sensorZones = field.zones.filter((z) => z.type !== 'corridor');
    const m = new THREE.Matrix4();
    const cam = [], thermo = [], co2 = [];
    sensorZones.forEach((z) => {
      cam.push(m.clone().setPosition(z.cx, yCeil - 0.1, z.cz));
      thermo.push(m.clone().setPosition(z.cx - 1.4, 1.4, z.cz - 1.4));
      co2.push(m.clone().setPosition(z.cx + 1.4, 1.4, z.cz - 1.4));
    });

    return {
      cyan: mergeOrNull(cyan), red: mergeOrNull(red), amber: mergeOrNull(amber), orange: mergeOrNull(orange),
      cam, thermo, co2, occupants: field.occupants,
    };
  }, [field, floor]);

  if (!built) return null;
  return (
    <group>
      {built.cyan && <mesh geometry={built.cyan}><meshBasicMaterial color="#21d4ff" transparent opacity={op} wireframe={wf} toneMapped={false} /></mesh>}
      {built.red && <mesh geometry={built.red}><meshBasicMaterial color="#ff5a3c" transparent opacity={op} wireframe={wf} toneMapped={false} /></mesh>}
      {built.amber && <mesh geometry={built.amber}><meshBasicMaterial color="#ffaa00" transparent opacity={op} wireframe={wf} toneMapped={false} /></mesh>}
      {built.orange && <mesh geometry={built.orange}><meshBasicMaterial color="#ff8a3d" transparent opacity={op} wireframe={wf} toneMapped={false} /></mesh>}

      <InstancedFixtures matrices={built.cam} kind="sphere" color="#ff00ff" opacity={op} />
      <InstancedFixtures matrices={built.thermo} kind="thermo" color="#00e5ff" opacity={op} />
      <InstancedFixtures matrices={built.co2} kind="co2" color="#00ff88" opacity={op} />

      {built.occupants.length > 0 && <People occupants={built.occupants} opacity={op} />}
    </group>
  );
}

const FIXTURE_GEO = {
  sphere: () => new THREE.SphereGeometry(0.16, 10, 10),
  thermo: () => new THREE.BoxGeometry(0.28, 0.42, 0.1),
  co2: () => new THREE.BoxGeometry(0.36, 0.26, 0.1),
};

function InstancedFixtures({ matrices, kind, color, opacity }) {
  const ref = useRef();
  const geo = useMemo(() => FIXTURE_GEO[kind](), [kind]);
  const count = matrices.length;
  useEffect(() => {
    if (!ref.current || !count) return;
    matrices.forEach((mat, i) => ref.current.setMatrixAt(i, mat));
    ref.current.instanceMatrix.needsUpdate = true;
  }, [matrices, count]);
  if (!count) return null;
  return (
    <instancedMesh key={`${kind}-${count}`} ref={ref} args={[geo, null, count]}>
      <meshBasicMaterial color={color} transparent opacity={opacity} toneMapped={false} />
    </instancedMesh>
  );
}

function People({ occupants, opacity }) {
  const ref = useRef();
  const geo = useMemo(() => new THREE.CapsuleGeometry(0.26, 0.8, 4, 8), []);
  const dummy = useMemo(() => new THREE.Object3D(), []);
  const count = occupants.length;
  useEffect(() => {
    if (!ref.current) return;
    occupants.forEach((o, i) => { dummy.position.set(o.x, 0.8, o.z); dummy.updateMatrix(); ref.current.setMatrixAt(i, dummy.matrix); });
    ref.current.instanceMatrix.needsUpdate = true;
  }, [occupants, dummy]);
  return (
    <instancedMesh key={count} ref={ref} args={[geo, null, count]}>
      <meshBasicMaterial color="#ffd27f" transparent opacity={Math.min(1, opacity + 0.2)} toneMapped={false} />
    </instancedMesh>
  );
}
