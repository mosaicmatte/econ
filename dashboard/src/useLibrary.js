// The programme library, as the dashboard sees it (GET /api/library).
//
// This hook exists to end a specific class of bug. Panels need a handful of numbers that
// describe the BUILDING — the design supply-air temperature that turns airflow into
// delivered cooling, the fraction of plant load a degree of setpoint is worth, and which
// zone types must never be counted as waste because they are meant to run around the
// clock. Every one of those had been retyped as a JavaScript literal, and they had drifted:
// the supply temperature was pinned at 12 °C after the engine learned to read a real probe,
// the 5%-per-°C rule of thumb appeared three times with three different justifications, and
// the critical-type list still named `server-room` and `mechanical` — types this
// digitizer has not minted since it started emitting `comms-room` and `plant-room`. That
// last one was not cosmetic: it meant the "unoccupied zones still being cooled" card was
// counting the comms room as waste and pricing it as a saving opportunity.
//
// Now there is one answer, read from the same file the engine evaluates its physics with.
// Nothing here falls back to a plausible number: when the engine is unreachable the hook
// reports `loaded: false` and callers are expected to withhold the figure rather than show
// a guess, exactly as they already do for an unobserved EUI.

import { useEffect, useMemo, useState } from 'react';
import { API_BASE } from './api';

// Module-scope cache: the library changes only when an operator recalibrates the site, so
// every panel that mounts should not re-fetch it. Shared promise so concurrent mounts
// issue one request.
let cached = null;
let inflight = null;

function fetchLibrary() {
  if (cached) return Promise.resolve(cached);
  if (!inflight) {
    inflight = fetch(`${API_BASE}/api/library`)
      .then((r) => (r.ok ? r.json() : null))
      .then((j) => {
        if (j && j.physics) cached = j;
        inflight = null;
        return cached;
      })
      .catch(() => {
        inflight = null;
        return null;
      });
  }
  return inflight;
}

export function useLibrary() {
  const [lib, setLib] = useState(cached);
  useEffect(() => {
    if (cached) return;
    let alive = true;
    fetchLibrary().then((j) => { if (alive && j) setLib(j); });
    return () => { alive = false; };
  }, []);

  const physics = lib?.physics || null;
  const critical = lib?.critical || null;

  // Memoized on the library object so the returned helpers are referentially stable —
  // callers put them in useMemo/useEffect dependency lists.
  return useMemo(() => ({
    lib,
    physics,
    // loaded distinguishes "the engine served its library" from "we have nothing". A
    // panel that cannot tell the difference is the one that invents a default.
    loaded: !!lib,
    // calibrated is stricter: the engine served a library that actually came from the
    // JSON rather than the built-in fallback physics.
    calibrated: !!lib?.loaded,
    critical,
    // isCritical answers the one question the panels kept getting wrong. It returns null
    // — not false — when the library has not arrived, so a caller can choose to withhold
    // a judgement instead of silently treating a comms room as an ordinary office.
    isCritical: (zoneType) => {
      if (!critical) return null;
      return critical.includes(zoneType);
    },
    // Design occupant density for a programme, m² per person. Null when the library has
    // not arrived, and null when the programme is not occupied on a density basis (a
    // plant room, a store) — both cases mean "do not draw a capacity bar", which is a
    // different instruction from "capacity is zero".
    areaPerOccupant: (zoneType) => {
      const p = lib?.programmes?.[zoneType];
      if (!p) return null;
      return typeof p.areaPerOccupantM2 === 'number' && p.areaPerOccupantM2 > 0
        ? p.areaPerOccupantM2
        : null;
    },
    // seriesId identifies the load series the engine is currently producing: which
    // building, under which occupancy model. Any window the browser accumulates across
    // reloads — an observed peak, a mean load — belongs to one series and must be
    // discarded when the engine starts producing a different one, exactly as the engine
    // discards its own persisted state. Null until the library arrives, which means "do
    // not start accumulating yet" rather than "any window will do".
    seriesId: lib
      ? `${lib.buildingId || 'unknown'}@v${lib.occupancyModelVersion ?? 0}`
      : null,
    // Fresh-air rate per person. Backs the MODELLED ventilation shown where no VAV
    // reports, so the figure on screen is the one the engine's own heat balance uses.
    outdoorAirLPerSPerPerson: physics?.outdoorAirLPerSPerPerson ?? null,
    // modelledVentilationLPerS is the outdoor air an occupancy implies, from the same
    // coefficient the ventilation load is computed with. Null when the library has not
    // arrived: a surface that cannot compute this shows "—", never a plausible number.
    modelledVentilationLPerS: (occupancy) => {
      const per = physics?.outdoorAirLPerSPerPerson;
      if (per == null) return null;
      return per * (occupancy || 0);
    },
    // The coefficients the panels used to carry as literals. Null until the library
    // arrives so a caller withholds the derived figure rather than pricing a saving
    // against a number nobody calibrated.
    hvacPerDegC: physics?.hvacLoadPerDegCFraction ?? null,
    precoolShift: physics?.precoolShiftFraction ?? null,
    supplyDesignC: physics?.supplyAirDesignC ?? null,
    designCop: physics?.designCop ?? null,
    nonHvacBaseWPerM2: physics?.nonHvacBaseWPerM2 ?? null,
    outdoorCo2Ppm: physics?.outdoorCo2Ppm ?? null,
    co2PerOccupant: physics?.co2PpmPerOccupantSteady ?? null,
    // Modelled zone CO2, from the library's coefficients. Null when they have not
    // arrived: a surface that cannot compute this must show "—" and say no sensor is
    // reporting, never substitute a plausible ppm.
    modelledCo2: (occupancy) => {
      const base = physics?.outdoorCo2Ppm;
      const per = physics?.co2PpmPerOccupantSteady;
      if (base == null || per == null) return null;
      return base + per * (occupancy || 0);
    },
  }), [lib, physics, critical]);
}

export default useLibrary;
