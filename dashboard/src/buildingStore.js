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

import bundledTower from './building-data.json' with { type: 'json' };
import bundledHome from './building-data-home.json' with { type: 'json' };
import { API_BASE } from './api.js';



let activeModelType = 'multi-level'; // 'multi-level' or 'domestic-home'
let towerData = bundledTower;
let homeData = bundledHome;
let live = false;
const listeners = new Set();

export function getBuilding() {
  return activeModelType === 'domestic-home' ? homeData : towerData;
}

export function getAllKnownBuildings() {
  return [towerData, homeData];
}

export function getBuildingModelType() {
  return activeModelType;
}

export function setBuildingModelType(type) {
  if (type !== 'multi-level' && type !== 'domestic-home') return;
  if (activeModelType !== type) {
    activeModelType = type;
    // Notify Go backend of the active building model switch
    fetch(`${API_BASE}/api/building/switch`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ model: type }),
    }).catch(() => {
      // Backend might be offline or running in mock testbed mode
    });
    listeners.forEach((fn) => {
      try { fn(getBuilding(), activeModelType); } catch (e) { console.error(e); }
    });
  }
}

export function subscribeBuildingChange(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

// True when the geometry came from the engine rather than the bundle — surfaces let the
// user know when they are looking at the fallback.
export function buildingIsLive() {
  return live && activeModelType === 'multi-level';
}

export async function bootBuilding() {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), 5000);
  try {
    const r = await fetch(`${API_BASE}/api/building-data`, { signal: ctl.signal });
    if (r.ok) {
      const j = await r.json();
      if (j && Array.isArray(j.floors) && j.floors.length > 0) {
        towerData = j;
        live = true;
      }
    }
  } catch {
    // Engine unreachable: the bundled fallback stands, flagged via buildingIsLive().
  } finally {
    clearTimeout(timer);
  }
  return getBuilding();
}

