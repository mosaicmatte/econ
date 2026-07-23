// Energy Use Intensity (EUI) and operational carbon — the two metrics the Vietnamese
// literature this project benchmarks against actually reports, and the two an ESG reviewer
// asks for first.
//
// Both are derived from the live load stream and the building's own geometry: nothing here is
// a constant standing in for data. Floor area comes from the digitized zone polygons (the same
// building-data.json the engine loads), so a regenerated building recomputes everything.

import { getBuilding } from './buildingStore';
const buildingData = getBuilding(); // live geometry — fetched before this module evaluates (see main.jsx)

const num = (v, d) => (v != null && !Number.isNaN(Number(v)) ? Number(v) : d);

// Shoelace formula: signed area of a simple polygon, in m² (zone polygons are metres).
const polygonArea = (p) => {
  if (!Array.isArray(p) || p.length < 3) return 0;
  let a = 0;
  for (let i = 0; i < p.length; i++) {
    const [x1, y1] = p[i];
    const [x2, y2] = p[(i + 1) % p.length];
    a += x1 * y2 - x2 * y1;
  }
  return Math.abs(a) / 2;
};

const ZONES = (buildingData.floors || []).flatMap((f) => f.zones || []);

// Total conditioned floor area (m²), summed once from the real digitized geometry.
export const FLOOR_AREA_M2 = ZONES.reduce((s, z) => s + polygonArea(z.polygon), 0);

// What this building actually is, by connected internal load. It matters: an EUI is only
// comparable against a cohort of the same building type, and this one is dominated by
// server rooms, not desks.
export const ZONE_MIX = (() => {
  const by = {};
  ZONES.forEach((z) => {
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
  return { byType: ranked, dominant: ranked[0], totalW };
})();

// True when connected load is dominated by IT/server space — i.e. when comparing this
// building against an *office* EUI cohort would be meaningless.
export const IS_IT_DOMINATED = (ZONE_MIX.dominant?.loadShare ?? 0) > 0.5
  && /server|data|it/i.test(ZONE_MIX.dominant?.type || '');

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
export const GRID_EF_KG_PER_KWH = num(import.meta.env.VITE_GRID_EF_KG_KWH, 0.6766);

// Instantaneous run-rate: what the annual intensity WOULD be if the building held its
// current load every hour of the year. Useful as a live rate; it is not an EUI, and it must
// never be compared to the cohort — an office at 09:00 with 3,000 people in it is nowhere
// near its own annual average, so the comparison reads ~3x high and means nothing. Sampled
// at a quiet hour the same formula reads far too low. Use euiFromMeanLoadMw for anything
// that sits next to a benchmark.
export function euiRunRateFromLoadMw(loadMw) {
  if (!(FLOOR_AREA_M2 > 0)) return 0;
  return (Math.max(0, loadMw) * 1000 * 8760) / FLOOR_AREA_M2; // kWh/m²·year
}

// Annualised EUI from the MEAN load actually observed. Mean load x 8760 is the building's
// energy over a year by definition, so this is the correct estimator rather than a fudge —
// it simply needs enough of the daily cycle to be representative, which is what
// EUI_MIN_WINDOW_H guards. Short of that window the comparison is withheld instead of
// being shown with a caveat nobody reads.
export function euiFromMeanLoadMw(meanLoadMw) {
  if (!(FLOOR_AREA_M2 > 0)) return 0;
  return (Math.max(0, meanLoadMw) * 1000 * 8760) / FLOOR_AREA_M2; // kWh/m²·year
}

// Hours of observation before a mean is worth comparing to an annual figure. A full day
// covers the occupied peak, the overnight base and both shoulders; less than that and the
// mean is a statement about which hours happened to be sampled.
export const EUI_MIN_WINDOW_H = 24;

// How this building sits against the office cohort, as a ratio (1.0 = on the benchmark).
// Takes a MEAN load — passing an instantaneous one is the error this file exists to prevent.
export function euiVsBenchmark(meanLoadMw, benchmark = EUI_BENCHMARK.hcmc) {
  return benchmark > 0 ? euiFromMeanLoadMw(meanLoadMw) / benchmark : 0;
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
