// One reading of a floor's geometry, for every consumer.
//
// Floor geometry is generated, and there is more than one generator: the office fixture
// comes from tools/officeize_fixture.py and carries `exteriorPolygon` / `corePolygon` /
// `wallThickness`; the house fixture comes from tools/housify_fixture.py and carried only
// `outline`. Nothing in the Go engine reads any of it — it is a dashboard-only structure —
// so the mismatch went unnoticed until the house fixture was deployed, at which point
// every consumer that opened with `floor.geometry.exteriorPolygon.map(...)` threw on
// undefined. AirflowWindow's throw reached the root error boundary and replaced the entire
// desktop console with the fallback screen; BuildingModel's was swallowed by the canvas
// boundary and simply rendered no building.
//
// The generator now emits the canonical field names. This module is the second half of
// that fix: read through one accessor that accepts either spelling and, more importantly,
// answers null instead of throwing when a floor carries neither. A panel that cannot draw
// a layout should say so; it should never take the console down with it.

// Canonical exterior outline as [[x, y], …], or null when the floor has none.
export function exteriorPolygon(floor) {
  const g = floor?.geometry;
  if (!g) return null;
  const poly = Array.isArray(g.exteriorPolygon) ? g.exteriorPolygon
    : Array.isArray(g.outline) ? g.outline
    : null;
  return poly && poly.length >= 3 ? poly : null;
}

// Service-core outline, or [] — a building without a core is a normal building, not an
// error, so this never returns null.
export function corePolygon(floor) {
  const c = floor?.geometry?.corePolygon;
  return Array.isArray(c) ? c : [];
}

// Wall thickness in metres. The fallback is the value the 3D walls were drawn at before
// any fixture carried the field, so an older building looks exactly as it always did.
export function wallThickness(floor, fallback = 0.3) {
  const t = Number(floor?.geometry?.wallThickness);
  return Number.isFinite(t) && t > 0 ? t : fallback;
}

// True when there is enough geometry to draw or solve a layout for this floor.
export function hasLayout(floor) {
  return exteriorPolygon(floor) !== null;
}

// --- the world origin the whole 3D layer shares -----------------------------

import { getBuilding } from './buildingStore';

// Every 3D and airflow surface maps a fixture point (px, py) to world (px − Ox, Oy − py).
// That offset was the literal 20 in a dozen places, with a comment explaining that "the
// 60×40 plate therefore centres on x=−20, z=−20" — true of the office fixture it was
// written for, and of nothing else. On the house pilot (13.6 × 5.5 m) the model rendered
// into the far corner of a frame sized for a building twenty times larger, which reads as
// the building simply not being there.
//
// The offset is now the loaded building's own footprint centre, so any building lands on
// the world origin. It is computed once at module load, from the same geometry the engine
// serves, and exported as a single constant because the model, the walls, the zones, the
// flow solvers and the camera must all agree on it — a mismatch between any two of them
// puts the airflow field somewhere the building is not.
const footprint = (() => {
  const b = getBuilding();
  let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
  for (const f of b.floors || []) {
    const poly = exteriorPolygon(f);
    const pts = poly || (f.zones || []).flatMap((z) => z.polygon || []);
    for (const p of pts) {
      if (p[0] < minX) minX = p[0];
      if (p[0] > maxX) maxX = p[0];
      if (p[1] < minY) minY = p[1];
      if (p[1] > maxY) maxY = p[1];
    }
  }
  // A building with no usable geometry keeps the historical frame rather than collapsing
  // everything onto (0,0) — the old behaviour, for the one case where there is nothing
  // better to say.
  if (!Number.isFinite(minX) || !Number.isFinite(minY)) {
    return { minX: 0, maxX: 40, minY: 0, maxY: 40, cx: 20, cy: 20, width: 40, depth: 40 };
  }
  return {
    minX, maxX, minY, maxY,
    cx: (minX + maxX) / 2,
    cy: (minY + maxY) / 2,
    width: maxX - minX,
    depth: maxY - minY,
  };
})();

export const FOOTPRINT = footprint;

// ORIGIN.x / ORIGIN.y are the offsets above. Read them; never re-derive them.
export const ORIGIN = { x: footprint.cx, y: footprint.cy };

// toWorld maps a fixture point to the (x, z) the 3D scene draws it at. The y flip is part
// of the convention: fixture y grows "down" the plan, world z grows toward the viewer.
export function toWorld(p) {
  return [p[0] - ORIGIN.x, ORIGIN.y - p[1]];
}
