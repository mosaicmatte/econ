// ============================================================================
// flowfield.js — physically-constrained HVAC airflow for one floor.
//
// The old AirflowVectorField just radiated air from each zone centroid, ignoring
// the room layout. This module builds a real 2D flow domain from the floor's
// geometry — walls (zone boundaries + envelope), doorways (shared-edge openings),
// windows (perimeter relief), supply diffusers (VAVs), return grilles (the core/
// corridor), and occupants — then solves a masked potential flow so the air
// ACTUALLY respects that layout: it streams out of each diffuser, bends through
// doorways, converges on the returns, and leaks at windows, never crossing a wall.
//
// Pure JS (no React/three) so it can be unit-reasoned about and memoized. The
// React layer (ConstrainedAirflow.jsx) only renders what this returns.
//
// Coordinate frame matches the 3D zones exactly: local (x, z) = (px - 20, 20 - py),
// so everything lines up with BuildingModel/AirflowWindow without a second mapping.
// ============================================================================

export const CELL = {
  SOLID: 0,   // outside the envelope, or unassigned interior -> no flow
  OPEN: 1,    // inside a room
  WALL: 2,    // zone boundary / envelope -> no-flux boundary for the solver
  DOOR: 3,    // carved opening in a wall -> air may pass
  WINDOW: 4,  // perimeter relief sink (infiltration / exhaust)
  RETURN: 5,  // return-air grille sink (to the AHU, in the core/corridor)
};

const toLocal = (p) => [p[0] - 20, 20 - p[1]];

export function pointInPoly(x, z, poly) {
  let inside = false;
  for (let i = 0, j = poly.length - 1; i < poly.length; j = i++) {
    const xi = poly[i][0], zi = poly[i][1], xj = poly[j][0], zj = poly[j][1];
    if (((zi > z) !== (zj > z)) && (x < (xj - xi) * (z - zi) / (zj - zi) + xi)) inside = !inside;
  }
  return inside;
}

// Deterministic per-zone RNG so occupant dots are stable across frames/re-solves.
function mulberry32(seed) {
  let a = seed >>> 0;
  return () => {
    a |= 0; a = (a + 0x6D2B79F5) | 0;
    let t = Math.imul(a ^ (a >>> 15), 1 | a);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
function hashStr(s) {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 16777619); }
  return h >>> 0;
}

// Collinear-overlap test between two axis-aligned-ish edges -> the shared wall they
// form, returned as a midpoint + tangent so we can carve one neat doorway there.
function sharedDoor(a0, a1, b0, b1, tol) {
  // vertical shared edge (same x), overlap in z
  if (Math.abs(a0[0] - a1[0]) < tol && Math.abs(b0[0] - b1[0]) < tol &&
      Math.abs(a0[0] - b0[0]) < tol) {
    const az0 = Math.min(a0[1], a1[1]), az1 = Math.max(a0[1], a1[1]);
    const bz0 = Math.min(b0[1], b1[1]), bz1 = Math.max(b0[1], b1[1]);
    const lo = Math.max(az0, bz0), hi = Math.min(az1, bz1);
    if (hi - lo > 1.2) return { x: a0[0], z: (lo + hi) / 2, tx: 0, tz: 1 };
  }
  // horizontal shared edge (same z), overlap in x
  if (Math.abs(a0[1] - a1[1]) < tol && Math.abs(b0[1] - b1[1]) < tol &&
      Math.abs(a0[1] - b0[1]) < tol) {
    const ax0 = Math.min(a0[0], a1[0]), ax1 = Math.max(a0[0], a1[0]);
    const bx0 = Math.min(b0[0], b1[0]), bx1 = Math.max(b0[0], b1[0]);
    const lo = Math.max(ax0, bx0), hi = Math.min(ax1, bx1);
    if (hi - lo > 1.2) return { x: (lo + hi) / 2, z: a0[1], tx: 1, tz: 0 };
  }
  return null;
}

// A stable signature so the (expensive) solve only re-runs when the field meaningfully
// changes — live VAV flow rounded to 0.5, occupancy bucketed, fault flips.
export function flowKeyOf(floor, simState) {
  if (!floor) return '';
  return (floor.zones || []).map((z) => {
    const f = simState?.vavs?.[z.hvacMapping?.vavId]?.flow ?? 0;
    const occ = simState?.zones?.[z.zoneId]?.occupancy ?? 0;
    const alert = simState?.zones?.[z.zoneId]?.alert ? 1 : 0;
    return `${Math.round(f * 2) / 2}:${Math.round(occ / 3)}:${alert}`;
  }).join('|');
}

// ============================================================================
// buildFlowField — the whole pipeline: rasterize -> mark boundaries/doors/windows
// -> place sources/sinks -> solve potential flow -> expose a velocity sampler plus
// the render primitives (walls/doors/windows/diffusers/returns/occupants/electrical).
// ============================================================================
export function buildFlowField(floor, simState, opts = {}) {
  if (!floor) return null;
  const targetCells = opts.targetCells || 96; // cells along the long axis

  const ext = floor.geometry.exteriorPolygon.map(toLocal);
  const zones = (floor.zones || []).map((z, i) => ({
    i,
    type: z.zoneType,
    zoneId: z.zoneId,
    vavId: z.hvacMapping?.vavId,
    poly: z.polygon.map(toLocal),
    cx: z.centroid.x - 20,
    cz: 20 - z.centroid.y,
    temp: simState?.zones?.[z.zoneId]?.temp ?? z.thermalProperties?.setpoint ?? 24,
    setpoint: z.thermalProperties?.setpoint ?? 24,
    deadband: z.thermalProperties?.deadband ?? 2,
    flow: simState?.vavs?.[z.hvacMapping?.vavId]?.flow ?? 4,
    occ: Math.round(simState?.zones?.[z.zoneId]?.occupancy ?? 0),
    alert: !!simState?.zones?.[z.zoneId]?.alert,
  }));

  const xs = ext.map((p) => p[0]), zs = ext.map((p) => p[1]);
  const minX = Math.min(...xs), maxX = Math.max(...xs);
  const minZ = Math.min(...zs), maxZ = Math.max(...zs);
  const W = maxX - minX, D = maxZ - minZ;
  const h = Math.max(W, D) / targetCells;        // cell size (metres)
  const nx = Math.max(8, Math.ceil(W / h));
  const nz = Math.max(8, Math.ceil(D / h));
  const idx = (i, j) => j * nx + i;
  const cellCenter = (i, j) => [minX + (i + 0.5) * h, minZ + (j + 0.5) * h];

  const type = new Uint8Array(nx * nz);
  const zoneOf = new Int16Array(nx * nz).fill(-1);

  // 1) Assign every cell: inside a room -> OPEN+zone, else SOLID.
  for (let j = 0; j < nz; j++) {
    for (let i = 0; i < nx; i++) {
      const [x, z] = cellCenter(i, j);
      if (!pointInPoly(x, z, ext)) { type[idx(i, j)] = CELL.SOLID; continue; }
      let zi = -1;
      for (const zn of zones) { if (pointInPoly(x, z, zn.poly)) { zi = zn.i; break; } }
      if (zi < 0) { type[idx(i, j)] = CELL.SOLID; continue; } // wall gap / unmodeled
      type[idx(i, j)] = CELL.OPEN;
      zoneOf[idx(i, j)] = zi;
    }
  }

  // 2) Walls: a cell becomes WALL where it borders a DIFFERENT zone or the exterior.
  //    This draws a 1-cell partition along every room boundary + the envelope, which
  //    works whether zones tile the plate (testbed) or leave gaps (real plans).
  const baseType = type.slice();
  const baseZone = zoneOf.slice();
  for (let j = 0; j < nz; j++) {
    for (let i = 0; i < nx; i++) {
      if (baseType[idx(i, j)] !== CELL.OPEN) continue;
      const z0 = baseZone[idx(i, j)];
      let border = false;
      for (const [di, dj] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
        const ni = i + di, nj = j + dj;
        if (ni < 0 || nj < 0 || ni >= nx || nj >= nz) { border = true; break; }
        const nt = baseType[idx(ni, nj)];
        if (nt === CELL.SOLID || baseZone[idx(ni, nj)] !== z0) { border = true; break; }
      }
      if (border) { type[idx(i, j)] = CELL.WALL; }
    }
  }

  // 3) Doorways: detect shared edges between adjacent rooms and carve one opening each.
  //    Prefer real openings if the digitizer exported them (airflowDomain.doors).
  const doors = [];
  const tol = Math.max(0.4, h);
  const provided = floor.airflowDomain?.doors;
  if (Array.isArray(provided) && provided.length) {
    for (const d of provided) doors.push({ x: d.x - 20, z: 20 - d.y, tx: d.tx ?? 1, tz: d.tz ?? 0 });
  } else {
    for (let a = 0; a < zones.length; a++) {
      for (let b = a + 1; b < zones.length; b++) {
        const pa = zones[a].poly, pb = zones[b].poly;
        for (let ea = 0; ea < pa.length; ea++) {
          const a0 = pa[ea], a1 = pa[(ea + 1) % pa.length];
          for (let eb = 0; eb < pb.length; eb++) {
            const b0 = pb[eb], b1 = pb[(eb + 1) % pb.length];
            const d = sharedDoor(a0, a1, b0, b1, tol);
            if (d) doors.push(d);
          }
        }
      }
    }
  }
  // Carve each door: set wall cells within a ~0.9 m radius of the door centre to OPEN.
  const doorR = Math.max(1, Math.round(0.9 / h));
  for (const d of doors) {
    const ci = Math.floor((d.x - minX) / h), cj = Math.floor((d.z - minZ) / h);
    for (let dj = -doorR; dj <= doorR; dj++) {
      for (let di = -doorR; di <= doorR; di++) {
        const ni = ci + di, nj = cj + dj;
        if (ni < 1 || nj < 1 || ni >= nx - 1 || nj >= nz - 1) continue;
        // only carve along the door's tangent so we open a slit, not a hole in the corner
        if (Math.abs(di * d.tz - dj * d.tx) > 0.7) continue;
        if (type[idx(ni, nj)] === CELL.WALL) {
          type[idx(ni, nj)] = CELL.DOOR;
          if (zoneOf[idx(ni, nj)] < 0) {
            // inherit a neighbouring room so the door belongs to the open network
            for (const [ddi, ddj] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
              const z = baseZone[idx(ni + ddi, nj + ddj)];
              if (z >= 0) { zoneOf[idx(ni, nj)] = z; break; }
            }
          }
        }
      }
    }
  }

  // 4) Sources / sinks. Supply diffusers (one per VAV, at the zone centroid) inject air;
  //    the core/corridor return grilles + perimeter windows remove it. Net must be ~0.
  const S = new Float32Array(nx * nz);
  const isFlow = (t) => t === CELL.OPEN || t === CELL.DOOR;

  const diffusers = [];
  let supplyTotal = 0;
  for (const zn of zones) {
    if (zn.type === 'corridor') continue; // the corridor is the return path, not supplied
    const ci = Math.floor((zn.cx - minX) / h), cj = Math.floor((zn.cz - minZ) / h);
    if (ci < 0 || cj < 0 || ci >= nx || cj >= nz) continue;
    const strength = (0.5 + 0.85 * Math.min(zn.flow, 14) / 14) * (zn.alert ? 2.6 : 1);
    diffusers.push({ x: zn.cx, z: zn.cz, strength, type: zn.type, alert: zn.alert });
    if (isFlow(type[idx(ci, cj)])) { S[idx(ci, cj)] += strength; supplyTotal += strength; }
  }

  // Returns: along the interior of the corridor/core. Fall back to the floor centre.
  const returns = [];
  const corridor = zones.find((z) => z.type === 'corridor');
  const returnCells = [];
  if (corridor) {
    for (let j = 1; j < nz - 1; j++) for (let i = 1; i < nx - 1; i++) {
      if (isFlow(type[idx(i, j)]) && zoneOf[idx(i, j)] === corridor.i) returnCells.push([i, j]);
    }
  }
  if (!returnCells.length) {
    const ci = Math.floor((-minX) / h), cj = Math.floor((-minZ) / h);
    if (isFlow(type[idx(ci, cj)])) returnCells.push([ci, cj]);
  }

  // Windows: perimeter relief. Use the digitizer's windows if present, else stamp them
  // along the envelope at the same cadence the 3D facade uses.
  const windows = [];
  const winSpacing = floor.floorType === 'typical-office' ? 4.0 : 6.0;
  const providedWins = floor.airflowDomain?.windows;
  if (Array.isArray(providedWins) && providedWins.length) {
    for (const w of providedWins) windows.push({ x: w.x - 20, z: 20 - w.y });
  } else {
    for (let e = 0; e < ext.length; e++) {
      const p0 = ext[e], p1 = ext[(e + 1) % ext.length];
      const ex = p1[0] - p0[0], ez = p1[1] - p0[1];
      const len = Math.hypot(ex, ez);
      const ux = ex / len, uz = ez / len;
      const nxn = -uz, nzn = ux;                 // inward-ish normal (sign fixed below)
      for (let t = winSpacing / 2; t < len - winSpacing / 4; t += winSpacing) {
        const wx = p0[0] + ux * t, wz = p0[1] + uz * t;
        // nudge to the interior so the sink lands on a flow cell, not the wall itself
        const inx = wx + nxn * (1.6 * h) * (pointInPoly(wx + nxn, wz + nzn, ext) ? 1 : -1);
        const inz = wz + nzn * (1.6 * h) * (pointInPoly(wx + nxn, wz + nzn, ext) ? 1 : -1);
        windows.push({ x: wx, z: wz, ix: inx, iz: inz });
      }
    }
  }
  const windowCells = [];
  for (const w of windows) {
    const wx = w.ix ?? w.x, wz = w.iz ?? w.z;
    const ci = Math.floor((wx - minX) / h), cj = Math.floor((wz - minZ) / h);
    // Snap to the nearest interior flow cell within a small radius — a window centre often
    // sits on the wall itself (esp. digitizer-provided ones), so search inward for a sink.
    let best = null, bestD = 1e9;
    for (let dj = -3; dj <= 3; dj++) for (let di = -3; di <= 3; di++) {
      const ni = ci + di, nj = cj + dj;
      if (ni <= 0 || nj <= 0 || ni >= nx - 1 || nj >= nz - 1) continue;
      if (!isFlow(type[idx(ni, nj)])) continue;
      const d = di * di + dj * dj;
      if (d < bestD) { bestD = d; best = [ni, nj]; }
    }
    if (best) windowCells.push(best);
  }

  // Balance: 70 % of supply pulled by returns, 30 % by windows (all by whichever exists).
  const haveR = returnCells.length > 0, haveW = windowCells.length > 0;
  const rFrac = haveR && haveW ? 0.7 : (haveR ? 1 : 0);
  const wFrac = haveR && haveW ? 0.3 : (haveW ? 1 : 0);
  if (haveR) {
    const per = -(supplyTotal * rFrac) / returnCells.length;
    for (const [i, j] of returnCells) S[idx(i, j)] += per;
    // a few representative grille glyphs (don't draw one per cell)
    const step = Math.max(1, Math.floor(returnCells.length / 6));
    for (let k = 0; k < returnCells.length; k += step) {
      const [i, j] = returnCells[k]; const [x, z] = cellCenter(i, j); returns.push({ x, z });
    }
  }
  if (haveW) {
    const per = -(supplyTotal * wFrac) / windowCells.length;
    for (const [i, j] of windowCells) S[idx(i, j)] += per;
  }

  // 5) Solve  ∇²φ = S  (Gauss–Seidel, red-black-ish single pass) with no-flux at walls:
  //    walls are simply excluded from each cell's stencil, giving ∂φ/∂n = 0 there.
  const phi = new Float32Array(nx * nz);
  const h2 = h * h;
  const sweeps = Math.min(700, Math.round(4 * Math.max(nx, nz)));
  for (let it = 0; it < sweeps; it++) {
    for (let j = 1; j < nz - 1; j++) {
      for (let i = 1; i < nx - 1; i++) {
        const c = idx(i, j);
        if (!(type[c] === CELL.OPEN || type[c] === CELL.DOOR || type[c] === CELL.RETURN || type[c] === CELL.WINDOW)) continue;
        let sum = 0, n = 0;
        const L = idx(i - 1, j), R = idx(i + 1, j), U = idx(i, j - 1), Dn = idx(i, j + 1);
        if (isSolverOpen(type[L])) { sum += phi[L]; n++; }
        if (isSolverOpen(type[R])) { sum += phi[R]; n++; }
        if (isSolverOpen(type[U])) { sum += phi[U]; n++; }
        if (isSolverOpen(type[Dn])) { sum += phi[Dn]; n++; }
        if (n === 0) continue;
        phi[c] = (sum - h2 * S[c]) / n;
      }
    }
  }

  // velocity = ∇φ on open faces (central difference, one-sided at walls).
  const vx = new Float32Array(nx * nz);
  const vz = new Float32Array(nx * nz);
  let maxSpeed = 1e-6;
  for (let j = 1; j < nz - 1; j++) {
    for (let i = 1; i < nx - 1; i++) {
      const c = idx(i, j);
      if (!isSolverOpen(type[c])) continue;
      const L = idx(i - 1, j), R = idx(i + 1, j), U = idx(i, j - 1), Dn = idx(i, j + 1);
      const gx = ((isSolverOpen(type[R]) ? phi[R] : phi[c]) - (isSolverOpen(type[L]) ? phi[L] : phi[c])) / (2 * h);
      const gz = ((isSolverOpen(type[Dn]) ? phi[Dn] : phi[c]) - (isSolverOpen(type[U]) ? phi[U] : phi[c])) / (2 * h);
      vx[c] = gx; vz[c] = gz;
      const m = Math.hypot(gx, gz);
      if (m > maxSpeed) maxSpeed = m;
    }
  }

  // 6) Occupants ("humans") — stable jittered points per zone, capped for legibility.
  const occupants = [];
  for (const zn of zones) {
    const n = Math.max(0, Math.min(14, zn.occ));
    if (!n) continue;
    const rng = mulberry32(hashStr(zn.zoneId));
    const bxs = zn.poly.map((p) => p[0]), bzs = zn.poly.map((p) => p[1]);
    const bx0 = Math.min(...bxs), bx1 = Math.max(...bxs), bz0 = Math.min(...bzs), bz1 = Math.max(...bzs);
    let placed = 0, guard = 0;
    while (placed < n && guard < n * 40) {
      guard++;
      const x = bx0 + rng() * (bx1 - bx0), z = bz0 + rng() * (bz1 - bz0);
      if (pointInPoly(x, z, zn.poly)) { occupants.push({ x, z, type: zn.type }); placed++; }
    }
  }

  // 7) Electrical grid — a radial bus from a floor panel along the core to each room.
  const panel = corridor ? { x: corridor.cx, z: corridor.cz } : { x: (minX + maxX) / 2, z: (minZ + maxZ) / 2 };
  const electrical = zones
    .filter((z) => z.type !== 'corridor')
    .map((z) => ({ x1: panel.x, z1: panel.z, x2: z.cx, z2: z.cz }));

  // Wall + window render segments (clean lines, not per-cell rectangles).
  const wallSegments = [];
  const pushEdges = (poly) => {
    for (let i = 0; i < poly.length; i++) {
      const a = poly[i], b = poly[(i + 1) % poly.length];
      wallSegments.push([a[0], a[1], b[0], b[1]]);
    }
  };
  zones.forEach((z) => pushEdges(z.poly));
  pushEdges(ext);
  const windowSegments = windows.map((w) => ({ x: w.x, z: w.z }));

  // Bilinear velocity sampler in metric coordinates (for tracer advection).
  const sample = (x, z) => {
    const fi = (x - minX) / h - 0.5, fj = (z - minZ) / h - 0.5;
    const i0 = Math.floor(fi), j0 = Math.floor(fj);
    if (i0 < 0 || j0 < 0 || i0 >= nx - 1 || j0 >= nz - 1) return [0, 0, CELL.SOLID];
    const tx = fi - i0, tz = fj - j0;
    const c00 = idx(i0, j0), c10 = idx(i0 + 1, j0), c01 = idx(i0, j0 + 1), c11 = idx(i0 + 1, j0 + 1);
    const blocked = !isSolverOpen(type[c00]) || !isSolverOpen(type[c10]) || !isSolverOpen(type[c01]) || !isSolverOpen(type[c11]);
    const lerp = (a, b, t) => a + (b - a) * t;
    const sx = lerp(lerp(vx[c00], vx[c10], tx), lerp(vx[c01], vx[c11], tx), tz);
    const sz = lerp(lerp(vz[c00], vz[c10], tx), lerp(vz[c01], vz[c11], tx), tz);
    return [sx, sz, blocked ? CELL.WALL : CELL.OPEN];
  };

  // Coarse arrow samples (every `stride` cells), only on flow cells.
  const arrowStride = opts.arrowStride || 3;
  const arrows = [];
  for (let j = 1; j < nz - 1; j += arrowStride) {
    for (let i = 1; i < nx - 1; i += arrowStride) {
      const c = idx(i, j);
      if (!isSolverOpen(type[c])) continue;
      const m = Math.hypot(vx[c], vz[c]);
      if (m < maxSpeed * 0.02) continue;
      const [x, z] = cellCenter(i, j);
      arrows.push({ x, z, vx: vx[c], vz: vz[c], norm: Math.sqrt(m / maxSpeed) });
    }
  }

  return {
    grid: { minX, minZ, maxX, maxZ, h, nx, nz, W, D },
    type, zoneOf, vx, vz, maxSpeed, sample,
    arrows, diffusers, returns, windows, doors, occupants, electrical,
    wallSegments, windowSegments, panel,
    zones: zones.map((z) => ({ poly: z.poly, temp: z.temp, setpoint: z.setpoint, deadband: z.deadband, type: z.type, alert: z.alert, cx: z.cx, cz: z.cz })),
    center: { x: (minX + maxX) / 2, z: (minZ + maxZ) / 2 },
    span: Math.max(W, D),
  };
}

function isSolverOpen(t) {
  return t === CELL.OPEN || t === CELL.DOOR || t === CELL.RETURN || t === CELL.WINDOW;
}

// Thermal heatmap: speed/temperature t in [0,1] -> rgb (blue -> cyan -> green -> yellow -> red).
const STOPS = [
  [0.0, [0.10, 0.20, 0.75]],
  [0.25, [0.00, 0.70, 0.95]],
  [0.5, [0.15, 0.85, 0.30]],
  [0.75, [0.97, 0.85, 0.12]],
  [1.0, [0.92, 0.16, 0.12]],
];
export function heat(t) {
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
