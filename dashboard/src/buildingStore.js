// Building geometry source-of-truth for the frontend.
//
// The dashboard used to bundle building-data.json at build time, which made the blueprint
// import flow impossible: a building deployed through /api/building would run in the
// engine while every panel kept rendering the geometry compiled into the JS bundle.
//
// Now the app boots in two stages (see main.jsx): this store fetches the engine's copy
// FIRST, and only then is the app module graph imported — so the module-scope constants
// derived from geometry (FLOOR_AREA_M2, FAULT_ZONES, DESIGN_PEAK_MW, ...) all compute
// from the live building. The bundled copy remains solely the offline fallback, keeping
// the 3D shell renderable with no backend.

import bundled from './building-data.json';
import { API_BASE } from './api';

let current = bundled;
let live = false;

export function getBuilding() {
  return current;
}

// True when the geometry came from the engine rather than the bundle — surfaces let the
// user know when they are looking at the fallback.
export function buildingIsLive() {
  return live;
}

export async function bootBuilding() {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), 5000);
  try {
    const r = await fetch(`${API_BASE}/api/building-data`, { signal: ctl.signal });
    if (r.ok) {
      const j = await r.json();
      if (j && Array.isArray(j.floors) && j.floors.length > 0) {
        current = j;
        live = true;
      }
    }
  } catch {
    // Engine unreachable: the bundled fallback stands, flagged via buildingIsLive().
  } finally {
    clearTimeout(timer);
  }
  return current;
}
