// ============================================================================
// flowfield3d.js — volumetric (3D) HVAC airflow for one floor.
//
// Upgrades the 2D plan solve (flowfield.js) to a real voxel volume: walls are
// full-height barriers, the floor slab and ceiling are no-flux, supply diffusers
// inject at the CEILING and return grilles pull LOW near the core, so the HVAC
// actually drives a top-to-bottom circulation (air drops from the ceiling diffuser,
// spreads across the room, and is drawn back to the low returns) — with windows as
// mid-height relief. Air never crosses a wall and only passes through doorways.
//
// We reuse buildFlowField() purely for the layout-derived feature POSITIONS
// (diffusers / returns / windows / doors / occupants / electrical / walls), which
// are in resolution-independent local metric coords, then build + solve our own
// coarser voxel grid (3D is heavier, so it runs at lower resolution than the 2D viz).
//
// Anisotropic 7-point Poisson  ∇²φ = S  (Gauss–Seidel), velocity v = ∇φ.
// ============================================================================
import { buildFlowField, pointInPoly, flowKeyOf } from './flowfield';

const toLocal = (p) => [p[0] - 20, 20 - p[1]];

export { flowKeyOf };

export function buildFlowField3D(floor, simState, opts = {}) {
  if (!floor) return null;
  const f2 = buildFlowField(floor, simState); // 2D structures + feature positions (local coords)
  if (!f2) return null;

  const ext = floor.geometry.exteriorPolygon.map(toLocal);
  const zonesPoly = floor.zones.map((z) => ({ poly: z.polygon.map(toLocal), type: z.zoneType }));
  const { minX, maxX, minZ, maxZ } = f2.grid;
  const W = maxX - minX, D = maxZ - minZ;
  const H = floor.height || 4;

  const target = opts.targetCells || 60;       // long-axis cells (coarser than 2D)
  const hx = Math.max(W, D) / target;           // horizontal cell size (x = z)
  const nx = Math.max(8, Math.ceil(W / hx));
  const nz = Math.max(8, Math.ceil(D / hx));
  const nk = opts.layers || 8;                  // vertical layers
  const hy = H / nk;                            // vertical cell size

  const idx = (i, j, k) => (k * nz + j) * nx + i;
  const cx = (i) => minX + (i + 0.5) * hx;
  const cy = (k) => (k + 0.5) * hy;
  const cz = (j) => minZ + (j + 0.5) * hx;

  // ---- 2D classification (open/zone/wall) at this resolution ----
  const T_SOLID = 0, T_OPEN = 1, T_WALL = 2, T_DOOR = 3;
  const t2 = new Uint8Array(nx * nz);
  const z2 = new Int16Array(nx * nz).fill(-1);
  for (let j = 0; j < nz; j++) {
    for (let i = 0; i < nx; i++) {
      const x = cx(i), z = cz(j);
      if (!pointInPoly(x, z, ext)) { t2[j * nx + i] = T_SOLID; continue; }
      let zi = -1;
      for (let q = 0; q < zonesPoly.length; q++) { if (pointInPoly(x, z, zonesPoly[q].poly)) { zi = q; break; } }
      if (zi < 0) { t2[j * nx + i] = T_SOLID; continue; }
      t2[j * nx + i] = T_OPEN; z2[j * nx + i] = zi;
    }
  }
  // walls along zone/exterior boundaries (neighbour-difference)
  const baseT = t2.slice(), baseZ = z2.slice();
  for (let j = 0; j < nz; j++) for (let i = 0; i < nx; i++) {
    if (baseT[j * nx + i] !== T_OPEN) continue;
    const z0 = baseZ[j * nx + i];
    let border = false;
    for (const [di, dj] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
      const ni = i + di, nj = j + dj;
      if (ni < 0 || nj < 0 || ni >= nx || nj >= nz) { border = true; break; }
      if (baseT[nj * nx + ni] === T_SOLID || baseZ[nj * nx + ni] !== z0) { border = true; break; }
    }
    if (border) t2[j * nx + i] = T_WALL;
  }
  // carve doors (reuse the 2D field's resolved door centres, local coords)
  const doorR = Math.max(1, Math.round(0.9 / hx));
  for (const d of f2.doors) {
    const ci = Math.floor((d.x - minX) / hx), cj = Math.floor((d.z - minZ) / hx);
    for (let dj = -doorR; dj <= doorR; dj++) for (let di = -doorR; di <= doorR; di++) {
      const ni = ci + di, nj = cj + dj;
      if (ni < 1 || nj < 1 || ni >= nx - 1 || nj >= nz - 1) continue;
      if (Math.abs(di * (d.tz ?? 0) - dj * (d.tx ?? 1)) > 0.7) continue;
      if (t2[nj * nx + ni] === T_WALL) t2[nj * nx + ni] = T_DOOR;
    }
  }
  const open2 = (i, j) => { const t = t2[j * nx + i]; return t === T_OPEN || t === T_DOOR; };

  // ---- 3D solid mask: vertical walls full height; floor/ceiling are no-flux ----
  const N = nx * nz * nk;
  const solid = new Uint8Array(N);
  for (let k = 0; k < nk; k++) for (let j = 0; j < nz; j++) for (let i = 0; i < nx; i++) {
    solid[idx(i, j, k)] = open2(i, j) ? 0 : 1;
  }

  // ---- sources / sinks ----
  const S = new Float32Array(N);
  let supply = 0;
  const diffusers3d = [];
  const kCeil = nk - 1, kLow = 0;
  for (const d of f2.diffusers) {
    const di = Math.floor((d.x - minX) / hx), dj = Math.floor((d.z - minZ) / hx);
    if (di < 0 || dj < 0 || di >= nx || dj >= nz || solid[idx(di, dj, kCeil)]) continue;
    S[idx(di, dj, kCeil)] += d.strength;        // inject at the ceiling
    supply += d.strength;
    diffusers3d.push({ x: d.x, y: H - 0.15, z: d.z, strength: d.strength, type: d.type, alert: d.alert });
  }

  // returns: low cells across the corridor/core (where f2 placed return glyphs)
  const corridorIdx = zonesPoly.findIndex((z) => z.type === 'corridor');
  const returnCells = [];
  if (corridorIdx >= 0) {
    for (let j = 1; j < nz - 1; j++) for (let i = 1; i < nx - 1; i++) {
      if (open2(i, j) && z2[j * nx + i] === corridorIdx) returnCells.push([i, j]);
    }
  }
  if (!returnCells.length) {
    const ci = Math.floor((-minX) / hx), cj = Math.floor((-minZ) / hx);
    if (open2(ci, cj)) returnCells.push([ci, cj]);
  }
  // windows: interior relief cells at mid height
  const windowCells = [];
  for (const w of f2.windows) {
    const wx = w.ix ?? w.x, wz = w.iz ?? w.z;
    const ci = Math.floor((wx - minX) / hx), cj = Math.floor((wz - minZ) / hx);
    let best = null, bestD = 1e9;
    for (let dj = -3; dj <= 3; dj++) for (let di = -3; di <= 3; di++) {
      const ni = ci + di, nj = cj + dj;
      if (ni <= 0 || nj <= 0 || ni >= nx - 1 || nj >= nz - 1 || !open2(ni, nj)) continue;
      const dd = di * di + dj * dj;
      if (dd < bestD) { bestD = dd; best = [ni, nj]; }
    }
    if (best) windowCells.push(best);
  }
  const haveR = returnCells.length > 0, haveW = windowCells.length > 0;
  const rFrac = haveR && haveW ? 0.72 : (haveR ? 1 : 0);
  const wFrac = haveR && haveW ? 0.28 : (haveW ? 1 : 0);
  if (haveR) {
    const per = -(supply * rFrac) / returnCells.length;
    for (const [i, j] of returnCells) S[idx(i, j, kLow)] += per; // pull LOW near the core
  }
  if (haveW) {
    const kMid = Math.floor(nk * 0.45);
    const per = -(supply * wFrac) / windowCells.length;
    for (const [i, j] of windowCells) S[idx(i, j, kMid)] += per;
  }

  // ---- solve  ∇²φ = S  (anisotropic 7-point Gauss–Seidel, no-flux at walls) ----
  const phi = new Float32Array(N);
  const ix2 = 1 / (hx * hx), iy2 = 1 / (hy * hy), iz2 = 1 / (hx * hx);
  const sweeps = Math.min(500, Math.round(3.2 * Math.max(nx, nz, nk)));
  for (let it = 0; it < sweeps; it++) {
    for (let k = 0; k < nk; k++) for (let j = 1; j < nz - 1; j++) for (let i = 1; i < nx - 1; i++) {
      const c = idx(i, j, k);
      if (solid[c]) continue;
      let num = 0, den = 0;
      if (!solid[idx(i - 1, j, k)]) { num += phi[idx(i - 1, j, k)] * ix2; den += ix2; }
      if (!solid[idx(i + 1, j, k)]) { num += phi[idx(i + 1, j, k)] * ix2; den += ix2; }
      if (!solid[idx(i, j - 1, k)]) { num += phi[idx(i, j - 1, k)] * iz2; den += iz2; }
      if (!solid[idx(i, j + 1, k)]) { num += phi[idx(i, j + 1, k)] * iz2; den += iz2; }
      if (k > 0 && !solid[idx(i, j, k - 1)]) { num += phi[idx(i, j, k - 1)] * iy2; den += iy2; }
      if (k < nk - 1 && !solid[idx(i, j, k + 1)]) { num += phi[idx(i, j, k + 1)] * iy2; den += iy2; }
      if (den === 0) continue;
      phi[c] = (num - S[c]) / den;
    }
  }

  // ---- velocity field v = ∇φ ----
  const vx = new Float32Array(N), vy = new Float32Array(N), vz = new Float32Array(N);
  let maxSpeed = 1e-6;
  for (let k = 0; k < nk; k++) for (let j = 1; j < nz - 1; j++) for (let i = 1; i < nx - 1; i++) {
    const c = idx(i, j, k);
    if (solid[c]) continue;
    const xR = !solid[idx(i + 1, j, k)] ? phi[idx(i + 1, j, k)] : phi[c];
    const xL = !solid[idx(i - 1, j, k)] ? phi[idx(i - 1, j, k)] : phi[c];
    const zR = !solid[idx(i, j + 1, k)] ? phi[idx(i, j + 1, k)] : phi[c];
    const zL = !solid[idx(i, j - 1, k)] ? phi[idx(i, j - 1, k)] : phi[c];
    const yU = (k < nk - 1 && !solid[idx(i, j, k + 1)]) ? phi[idx(i, j, k + 1)] : phi[c];
    const yD = (k > 0 && !solid[idx(i, j, k - 1)]) ? phi[idx(i, j, k - 1)] : phi[c];
    vx[c] = (xR - xL) / (2 * hx);
    vz[c] = (zR - zL) / (2 * hx);
    vy[c] = (yU - yD) / (2 * hy);
    const m = Math.hypot(vx[c], vy[c], vz[c]);
    if (m > maxSpeed) maxSpeed = m;
  }

  // trilinear velocity sampler in metric coords
  const sample = (x, y, z) => {
    const fi = (x - minX) / hx - 0.5, fj = (z - minZ) / hx - 0.5, fk = y / hy - 0.5;
    const i0 = Math.floor(fi), j0 = Math.floor(fj), k0 = Math.floor(fk);
    if (i0 < 0 || j0 < 0 || k0 < 0 || i0 >= nx - 1 || j0 >= nz - 1 || k0 >= nk - 1) return [0, 0, 0, true];
    const tx = fi - i0, tz = fj - j0, ty = fk - k0;
    let any = false, sx = 0, sy = 0, sz = 0, wsum = 0;
    for (let dk = 0; dk <= 1; dk++) for (let dj = 0; dj <= 1; dj++) for (let di = 0; di <= 1; di++) {
      const c = idx(i0 + di, j0 + dj, k0 + dk);
      if (solid[c]) { any = true; continue; }
      const w = (di ? tx : 1 - tx) * (dj ? tz : 1 - tz) * (dk ? ty : 1 - ty);
      sx += vx[c] * w; sy += vy[c] * w; sz += vz[c] * w; wsum += w;
    }
    if (wsum < 1e-6) return [0, 0, 0, true];
    return [sx / wsum, sy / wsum, sz / wsum, any];
  };

  // coarse 3D arrow samples (stride in plan, a few height layers)
  const stride = opts.arrowStride || 3;
  const kLayers = [Math.floor(nk * 0.2), Math.floor(nk * 0.55), Math.floor(nk * 0.85)];
  const arrows = [];
  for (const k of kLayers) {
    for (let j = 1; j < nz - 1; j += stride) for (let i = 1; i < nx - 1; i += stride) {
      const c = idx(i, j, k);
      if (solid[c]) continue;
      const m = Math.hypot(vx[c], vy[c], vz[c]);
      if (m < maxSpeed * 0.04) continue;
      arrows.push({ x: cx(i), y: cy(k), z: cz(j), vx: vx[c], vy: vy[c], vz: vz[c], norm: Math.sqrt(m / maxSpeed) });
    }
  }

  return {
    grid: { minX, minZ, maxX, maxZ, hx, hy, nx, nz, nk, H },
    sample, arrows, maxSpeed,
    diffusers3d,
    // reuse the 2D feature geometry for rendering the layout the 3D flow respects
    diffusers: f2.diffusers, returns: f2.returns, windows: f2.windows, doors: f2.doors,
    occupants: f2.occupants, electrical: f2.electrical, panel: f2.panel,
    wallSegments: f2.wallSegments, windowSegments: f2.windowSegments, zones: f2.zones,
    center: f2.center, span: f2.span,
  };
}
