// Energy Use Intensity (EUI) and operational carbon — the two metrics the Vietnamese
// literature this project benchmarks against actually reports, and the two an ESG reviewer
// asks for first.
//
// Both are derived from the live load stream and the building's own geometry: nothing here is
// a constant standing in for data. Floor area comes from the digitized zone polygons (the same
// building-data.json the engine loads), so a regenerated building recomputes everything.

import { getBuilding, subscribeBuildingChange } from './buildingStore.js';

const num = (v, d) => (v != null && !Number.isNaN(Number(v)) ? Number(v) : d);

// Shoelace formula: signed area of a simple polygon, in m² (zone polygons are metres).
export const polygonArea = (p) => {
  if (!Array.isArray(p) || p.length < 3) return 0;
  let a = 0;
  for (let i = 0; i < p.length; i++) {
    const [x1, y1] = p[i];
    const [x2, y2] = p[(i + 1) % p.length];
    a += x1 * y2 - x2 * y1;
  }
  return Math.abs(a) / 2;
};

// Dynamic helper functions evaluating against active or provided building model
export function getFloorAreaM2(building = getBuilding()) {
  const b = building || getBuilding();
  const zones = (b?.floors || []).flatMap((f) => f.zones || []);
  return zones.reduce((s, z) => s + polygonArea(z.polygon), 0);
}

export function getZoneMix(building = getBuilding()) {
  const b = building || getBuilding();
  const zones = (b?.floors || []).flatMap((f) => f.zones || []);
  const by = {};
  zones.forEach((z) => {
    const t = z.zoneType || 'unknown';
    const w = z.thermalProperties?.baseHeatLoad || 0;
    by[t] = by[t] || { count: 0, watts: 0, area: 0 };
    by[t].count += 1;
    by[t].watts += w;
    by[t].area += polygonArea(z.polygon);
  });
  const totalW = Object.values(by).reduce((s, v) => s + v.watts, 0) || 1;
  const ranked = Object.entries(by)
    .map(([type, v]) => ({ type, ...v, loadShare: v.watts / totalW }))
    .sort((a, b) => b.watts - a.watts);
  return { byType: ranked, dominant: ranked[0] || { type: 'unknown', loadShare: 0, watts: 0, area: 0 }, totalW };
}

const IT_PROGRAMME = /(^|[-_\s])(server|comms|data|datacentre|datacenter|it)([-_\s]|$)/i;

export function getIsItDominated(building = getBuilding()) {
  const mix = getZoneMix(building);
  return (mix.dominant?.loadShare ?? 0) > 0.5
    && IT_PROGRAMME.test(mix.dominant?.type || '');
}

// Live module-scope exports for backwards compatibility & direct usage
export let FLOOR_AREA_M2 = getFloorAreaM2();
export let ZONE_MIX = getZoneMix();
export let IS_IT_DOMINATED = getIsItDominated();

// Synchronize module-level exports whenever active building model changes
subscribeBuildingChange((b) => {
  FLOOR_AREA_M2 = getFloorAreaM2(b);
  ZONE_MIX = getZoneMix(b);
  IS_IT_DOMINATED = getIsItDominated(b);
});

// Office EUI cohort, Vietnam. Survey of 57 commercial + government office buildings
// (Vietnam Clean Energy Program, 2015; Proc. ICEC 2021, doi:10.55066/proc-icec.2021.19).
// Applies to offices only — see IS_IT_DOMINATED.
export const EUI_BENCHMARK = {
  hanoi: 105.9,   // kWh/m²·year
  hcmc: 116.4,
  both: 109.6,
};

// Vietnam grid emission factor (kgCO₂e per kWh). Vietnam's grid is >60% coal and gas, so the
// carbon intensity of a saved kWh is high relative to temperate markets. Override per site or
// reporting year with VITE_GRID_EF_KG_KWH as MONRE republishes it.
export const GRID_EF_KG_PER_KWH = num(import.meta.env?.VITE_GRID_EF_KG_KWH, 0.6766);

// Instantaneous run-rate: what the annual intensity WOULD be if the building held its
// current load every hour of the year.
export function euiRunRateFromLoadMw(loadMw, building = getBuilding()) {
  const area = getFloorAreaM2(building);
  if (!(area > 0)) return 0;
  return (Math.max(0, loadMw) * 1000 * 8760) / area; // kWh/m²·year
}

// Annualised EUI from the MEAN load actually observed.
export function euiFromMeanLoadMw(meanLoadMw, building = getBuilding()) {
  const area = getFloorAreaM2(building);
  if (!(area > 0)) return 0;
  return (Math.max(0, meanLoadMw) * 1000 * 8760) / area; // kWh/m²·year
}

// Hours of observation before a mean is worth comparing to an annual figure.
export const EUI_MIN_WINDOW_H = 24;

// How this building sits against the office cohort, as a ratio (1.0 = on the benchmark).
export function euiVsBenchmark(meanLoadMw, benchmark = EUI_BENCHMARK.hcmc, building = getBuilding()) {
  return benchmark > 0 ? euiFromMeanLoadMw(meanLoadMw, building) / benchmark : 0;
}

// Operational carbon from grid electricity (Scope 2), at the live load.
export function carbonKgPerHour(loadMw) {
  return Math.max(0, loadMw) * 1000 * GRID_EF_KG_PER_KWH;
}
export function carbonTonnesPerDay(loadMw) {
  return (carbonKgPerHour(loadMw) * 24) / 1000;
}
export function carbonTonnesPerYear(loadMw) {
  return (carbonKgPerHour(loadMw) * 8760) / 1000;
}

// Carbon avoided by whatever the optimizer is currently saving — the ESG-reportable number.
export function carbonAvoidedTonnesPerYear(savedMw) {
  return carbonTonnesPerYear(savedMw);
}

// Compact formatter for tonnes of CO₂e.
export function tonnesStr(t) {
  const n = Number(t) || 0;
  if (Math.abs(n) >= 1000) return (n / 1000).toFixed(1) + 'k t';
  if (Math.abs(n) >= 10) return n.toFixed(0) + ' t';
  return n.toFixed(1) + ' t';
}
