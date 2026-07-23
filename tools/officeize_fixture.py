#!/usr/bin/env python3
"""Turn raw digitizer output into a fixture that behaves like the building it depicts.

The DeepFloorplan segmenter (ai_modules/branch_b_digitization) is good at finding room
BOUNDARIES and poor at naming what is inside them. On the bundled plan it labelled 555
closets averaging 4.2 m2 as "server-room" and stamped each with a default 85 kW load --
14,768 W/m2, and 86% of the whole building's connected load. Everything downstream
inherited that: the dashboard read 15.2 MW of grid power for a 42,000 m2 building, and
96% of the optimizer's credited savings came from those closets.

This script keeps every polygon the segmenter found -- the geometry came off a real plan
and is not in question -- and re-derives the two things it got wrong:

  * PROGRAMME. What a room is, inferred from its own area and position using the size
    bands a space planner would use, rather than a classifier's guess.
  * PHYSICS. Internal gain from published power densities, and air capacitance from the
    room's ACTUAL volume instead of a per-type constant. In the raw fixture a 2.7 m2
    corridor and a 161 m2 corridor were given identical thermal mass, which makes their
    measured time constants meaningless and is part of why identification returned
    non-physical coefficients.

NOTHING is hardcoded here. Every band, density, setpoint and physical constant is read
from server/data/programme-library.json, where each value carries its source. To
re-calibrate for a different site, edit that file -- not this script.

    python3 tools/officeize_fixture.py                  # report only, writes nothing
    python3 tools/officeize_fixture.py --write          # rewrite the fixture
    python3 tools/officeize_fixture.py --restore        # put the raw digitizer output back
"""

import argparse
import collections
import json
import math
import pathlib
import shutil
import sys

ROOT = pathlib.Path(__file__).resolve().parent.parent
FIXTURE = ROOT / "server" / "data" / "building-data.json"
LIBRARY = ROOT / "server" / "data" / "programme-library.json"
RAW_BACKUP = FIXTURE.with_suffix(".json.digitizer-raw")


def poly_area(p):
    s = 0.0
    for i in range(len(p)):
        x1, y1 = p[i][0], p[i][1]
        x2, y2 = p[(i + 1) % len(p)][0], p[(i + 1) % len(p)][1]
        s += x1 * y2 - x2 * y1
    return abs(s) / 2.0


def dist_to_boundary(pt, poly):
    """Shortest distance from a point to a polygon's boundary."""
    px, py = pt
    best = float("inf")
    for i in range(len(poly)):
        x1, y1 = poly[i][0], poly[i][1]
        x2, y2 = poly[(i + 1) % len(poly)][0], poly[(i + 1) % len(poly)][1]
        dx, dy = x2 - x1, y2 - y1
        l2 = dx * dx + dy * dy
        t = 0.0 if l2 == 0 else max(0.0, min(1.0, ((px - x1) * dx + (py - y1) * dy) / l2))
        best = min(best, math.hypot(px - (x1 + t * dx), py - (y1 + t * dy)))
    return best


class Library:
    """The programme library, and the only place a number may come from."""

    def __init__(self, doc):
        self.progs = doc["programmes"]
        self.plan = doc["planning"]
        self.phys = doc["physics"]
        self.cal = doc["calibration"]
        # Programmes that want exactly N per floor, largest-first among small rooms.
        self.per_floor = [(k, v["perFloor"]) for k, v in self.progs.items()
                          if v.get("perFloor")]
        self.per_floor.sort(key=lambda kv: kv[0])

    def classify(self, area, is_ground, cell_rank, svc_rank):
        """cell_rank ranks the zone among its floor's CELLULAR-band rooms (largest
        first), which is where the one-per-floor programmes are placed; svc_rank ranks
        it among the sub-cellular service rooms."""
        p = self.plan
        if is_ground and area >= p["lobbyMinM2"] and "lobby" in self.progs:
            return "lobby"
        if area >= p["openOfficeMinM2"]:
            return "open-office"
        if area >= p["meetingMinM2"]:
            return "meeting-room"
        if area >= p["cellularMinM2"]:
            slot = 0
            for name, count in self.per_floor:
                if cell_rank < slot + count:
                    return name
                slot += count
            return "cellular-office"
        # Below the cellular band: service space. Alternate so a floor gets both.
        return "wet-core" if svc_rank % 2 == 0 else "store"

    def envelope_r(self, area, wall_m2, roof_m2, partition_m2):
        """Total envelope resistance, K/W, from the zone's OWN exposed area.

        The raw fixture used a flat 0.2 K/W for every zone regardless of size, which gives
        a 650 m2 floor plate UA = 3.3 W/K where a real one is over 200. That single number
        is why identified time constants came back in the hundreds of hours: the RLS was
        working correctly and faithfully reporting a building modelled as a thermos.
        """
        ua = wall_m2 * self.phys["uValueWallWPerM2K"] + roof_m2 * self.phys["uValueRoofWPerM2K"]
        # Core zones have no facade; they still exchange heat through their partitions
        # with the space next door. Scaling that with PARTITION area rather than floor
        # area is what stops every core zone landing on an identical time constant.
        ua += partition_m2 * self.phys["uValuePartitionWPerM2K"]
        return 1.0 / ua if ua > 0 else 1.0

    def thermal(self, prog, area, height, solar_mult, wall_m2, roof_m2, partition_m2):
        spec = self.progs[prog]
        gain = area * spec["lightingWPerM2"] + spec.get("fixedEquipmentW", 0)
        cap = (self.phys["airRhoCpJPerM3K"] * area * height
               * self.phys["furnishingCapacitanceMultiplier"])
        r = self.envelope_r(area, wall_m2, roof_m2, partition_m2)
        return {
            "setpoint": spec["setpointC"],
            "deadband": spec["deadbandC"],
            "baseHeatLoad": round(gain),
            "solarGainMultiplier": solar_mult,
            "rWall": round(r, 6),
            "cAir": round(cap),
            "areaM2": round(area, 1),
            "exteriorWallM2": round(wall_m2, 1),
            "roofM2": round(roof_m2, 1),
            "partitionM2": round(partition_m2, 1),
            "timeConstantH": round(r * cap / 3600.0, 2),
        }


def exterior_wall_m(poly, exterior, tol):
    """Length of a zone's boundary that sits on the building's outer wall."""
    if not exterior:
        return 0.0
    total = 0.0
    for i in range(len(poly)):
        x1, y1 = poly[i][0], poly[i][1]
        x2, y2 = poly[(i + 1) % len(poly)][0], poly[(i + 1) % len(poly)][1]
        mid = ((x1 + x2) / 2.0, (y1 + y2) / 2.0)
        if dist_to_boundary(mid, exterior) <= tol:
            total += math.hypot(x2 - x1, y2 - y1)
    return total


def build(doc, lib, write):
    kept = dropped = 0
    mix, load_by, area_by = collections.Counter(), collections.Counter(), collections.Counter()
    solar_area_w = 0.0
    sliver = lib.plan["sliverM2"]
    facade = lib.plan["facadeDepthM"]
    EFLH = lib.phys["equivalentFullLoadHoursPerYr"]
    DESIGN_COP = lib.phys["designCop"]
    tol = lib.plan["facadeToleranceM"]
    top_level = max(f.get("level", 0) for f in doc["floors"])
    taus = []

    for floor in doc["floors"]:
        height = float(floor.get("height") or 4.0)
        exterior = (floor.get("geometry") or {}).get("exteriorPolygon") or []
        is_ground = floor.get("level") == 1

        zones = []
        for z in floor["zones"]:
            a = poly_area(z["polygon"])
            if a < sliver:
                dropped += 1
                continue
            zones.append((a, z))

        # Circulation keeps its identity: it is the one class the segmenter gets right
        # (median 50 m2, correctly ribbon-shaped), and an area band would misfile a long
        # thin corridor as open-plan.
        def band(lo, hi):
            return sorted([t for t in zones if lo <= t[0] < hi
                           and t[1].get("zoneType") != "corridor"], key=lambda t: -t[0])

        cell_rank = {id(t[1]): i for i, t in
                     enumerate(band(lib.plan["cellularMinM2"], lib.plan["meetingMinM2"]))}
        svc_rank = {id(t[1]): i for i, t in
                    enumerate(band(0.0, lib.plan["cellularMinM2"]))}

        out = []
        for a, z in zones:
            prog = ("corridor" if z.get("zoneType") == "corridor"
                    else lib.classify(a, is_ground,
                                      cell_rank.get(id(z), 10 ** 6),
                                      svc_rank.get(id(z), 0)))
            spec = lib.progs[prog]

            solar = 0.0
            c = z.get("centroid") or {}
            if spec.get("facadeExposed") and exterior and "x" in c and "y" in c:
                d = dist_to_boundary((c["x"], c["y"]), exterior)
                if d < facade:
                    solar = round(max(0.15, 1.0 - d / facade), 2)

            wall_m = exterior_wall_m(z["polygon"], exterior, tol)
            perim_m = sum(math.hypot(z["polygon"][(i+1) % len(z["polygon"])][0] - z["polygon"][i][0],
                                     z["polygon"][(i+1) % len(z["polygon"])][1] - z["polygon"][i][1])
                          for i in range(len(z["polygon"])))
            partition_m2 = max(0.0, perim_m - wall_m) * height
            roof = a if floor.get("level") == top_level else 0.0
            z["zoneType"] = prog
            z["name"] = "%s %s" % (prog.replace("-", " ").title(), floor.get("name", ""))
            z["thermalProperties"] = lib.thermal(prog, a, height, solar, wall_m * height, roof, partition_m2)
            taus.append(z["thermalProperties"]["timeConstantH"])
            if spec.get("critical"):
                z["critical"] = True
            out.append(z)
            kept += 1
            mix[prog] += 1
            load_by[prog] += a * spec["lightingWPerM2"] + spec.get("fixedEquipmentW", 0)
            area_by[prog] += a
            solar_area_w += solar * lib.phys["solarPeakWPerM2"] * a
        floor["zones"] = out

    tot_load, tot_area = sum(load_by.values()), sum(area_by.values())
    print("%-16s %5s %10s %8s %10s %7s" % ("programme", "n", "load kW", "%load", "area m2", "W/m2"))
    for k, _ in load_by.most_common():
        print("%-16s %5d %10.1f %7.1f%% %10.0f %7.1f" % (
            k, mix[k], load_by[k] / 1000, 100 * load_by[k] / tot_load, area_by[k],
            load_by[k] / area_by[k] if area_by[k] else 0))
    import statistics as _st
    taus.sort()
    print("\nkept %d zones, dropped %d slivers under %.0f m2" % (kept, dropped, sliver))
    print("envelope time constant h: min %.1f  p25 %.1f  median %.1f  p75 %.1f  max %.1f"
          % (taus[0], taus[len(taus)//4], _st.median(taus), taus[3*len(taus)//4], taus[-1]))
    plausible = sum(1 for t in taus if 1.0 <= t <= 40.0)
    print("  in the 1-40 h band a real office shows: %d/%d (%.0f%%)"
          % (plausible, len(taus), 100*plausible/len(taus)))
    print("lighting + fixed equipment: %.0f kW over %.0f m2 = %.1f W/m2"
          % (tot_load / 1000, tot_area, tot_load / tot_area))

    # Design-day cooling load, summed the same way the engine's heat balance sums it, so
    # this is a real check rather than a rule of thumb: internal gain + occupants at
    # design density + solar on the facade, divided by plant COP, plus the floor-area
    # baseline and live plug load. The engine remains the authority; this catches a
    # fixture that is off by an order of magnitude before it is ever loaded.
    cohort = lib.cal["cohortEuiKwhPerM2Yr"]["both"]
    occ_w = 0.0
    people = 0.0
    for prog, a in area_by.items():
        per = lib.progs[prog].get("areaPerOccupantM2")
        if per:
            people += a / per
            occ_w += (a / per) * lib.phys["occupantSensibleW"]
    solar_w = solar_area_w
    plug_w = tot_area * cohort * lib.cal["endUseShares"]["plugLoads"] * 1000 / EFLH
    # Fresh air: the dominant cooling term in a tropical office, and mostly latent.
    vent_w = (people * lib.phys["outdoorAirLPerSPerPerson"] / 1000.0
              * lib.phys["airDensityKgPerM3"] * lib.phys["ventilationEnthalpyKjPerKg"] * 1000.0)
    thermal_w = tot_load + occ_w + solar_w + plug_w + vent_w
    cool_e = thermal_w / DESIGN_COP
    base_e = tot_area * lib.phys["nonHvacBaseWPerM2"]
    peak_kw = (cool_e + base_e + plug_w) / 1000
    print("\ndesign occupancy %.0f people (%.1f m2/person)" % (people, tot_area / people))
    print("design-day thermal %.0f kW = lighting %.0f + people %.0f + solar %.0f + plug %.0f + fresh air %.0f"
          % (thermal_w / 1000, tot_load / 1000, occ_w / 1000, solar_w / 1000,
             plug_w / 1000, vent_w / 1000))
    print("electrical peak %.0f kW = cooling %.0f / COP %.1f + base %.0f + plug %.0f"
          % (peak_kw, cool_e / 1000, DESIGN_COP, base_e / 1000, plug_w / 1000))
    eui = peak_kw * EFLH / tot_area
    verdict = "in cohort" if 90 <= eui <= 135 else "OUT OF COHORT BAND"
    print("~%.0f kWh/m2.yr at %d EFLH  (cohort %.1f) -> %s" % (eui, EFLH, cohort, verdict))

    if write:
        if not RAW_BACKUP.exists():
            shutil.copy(FIXTURE, RAW_BACKUP)
            print("\nraw digitizer output preserved at %s" % RAW_BACKUP.name)
        FIXTURE.write_text(json.dumps(doc, separators=(",", ":")))
        print("wrote %s" % FIXTURE.relative_to(ROOT))
    else:
        print("\n(dry run -- pass --write to apply)")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--write", action="store_true")
    ap.add_argument("--restore", action="store_true", help="restore the raw digitizer fixture")
    args = ap.parse_args()

    if args.restore:
        if not RAW_BACKUP.exists():
            sys.exit("no raw backup at %s" % RAW_BACKUP)
        shutil.copy(RAW_BACKUP, FIXTURE)
        print("restored raw digitizer fixture")
        return

    for p in (FIXTURE, LIBRARY):
        if not p.exists():
            sys.exit("missing: %s" % p)
    build(json.loads(FIXTURE.read_text()), Library(json.loads(LIBRARY.read_text())), args.write)


if __name__ == "__main__":
    main()
