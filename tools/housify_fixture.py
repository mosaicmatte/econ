#!/usr/bin/env python3
"""Build a building fixture for a REAL Vietnamese house from a Polycam room scan.

Why this exists
---------------
`officeize_fixture.py` generates the synthetic 735-zone office tower the engine has always
run on. That building does not exist. The hardware is being commissioned in an actual
74 m2 tube house (nha ong) in Ho Chi Minh City, and every number the twin reports — learned
baselines, identified room models, savings — describes the fiction until the fixture is the
real place.

This reads a Polycam LiDAR room scan and emits that house instead. Unlike the office
generator, almost nothing here is invented: room polygons, floor areas, wall areas, volumes,
ceiling heights, door and window positions are all MEASURED by the scan.

What is measured vs inferred
----------------------------
MEASURED (from the scan, do not second-guess):
    room polygons, floor area, wall area, volume, ceiling height, door and window positions
INFERRED (stated here so it can be corrected):
    which programme each room is (a scan cannot tell a bathroom from a store cupboard —
    Polycam itself only labels two of the five), and how glazing splits between rooms

Output goes to `building-data.local.json`, the gitignored per-deployment override
(server/simulation/datapath.go), so it never fights the repository's default fixture on a
pull and a collaborator without the scan still boots.

Usage:
    python3 tools/housify_fixture.py --scan "<Polycam export dir>"          # dry run
    python3 tools/housify_fixture.py --scan "<Polycam export dir>" --write
"""

import argparse
import csv
import json
import math
import os
import re
import sys
import uuid

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(HERE)
LIB = os.path.join(REPO, "server", "data", "programme-library.json")
OUT = os.path.join(REPO, "server", "data", "building-data.local.json")

# Which programme each scanned room is. Polycam names only "Office" and "Living Room"; the
# rest come back as "Other N" and are matched by AREA RANK, which is stable for this scan.
# This is the one genuinely subjective mapping in the file — override it here, not in the
# emitted JSON, which is regenerated.
ROOM_PROGRAMME = {
    "Other 1": ("kitchen", "Kitchen & rear service"),
    "Office": ("home-office", "Office"),
    "Living Room": ("living", "Living room"),
    "Other 2": ("circulation", "Passage"),
    "Other 3": ("bathroom", "Bathroom"),
}


# --------------------------------------------------------------------------- DXF
def parse_dxf(path):
    """Pull LWPOLYLINEs out of a DXF by layer. Polycam writes clean per-feature layers
    (Poly-Rooms, Poly-Doors, Poly-Windows), so no CAD library is needed."""
    lines = [l.rstrip("\r\n") for l in open(path, errors="ignore")]
    ents, cur, i = [], None, 0
    while i < len(lines) - 1:
        code, val = lines[i].strip(), lines[i + 1]
        if code == "0":
            if cur:
                ents.append(cur)
            cur = {"t": val.strip(), "layer": None, "x": [], "y": []}
        elif cur is not None:
            if code == "8" and cur["layer"] is None:
                cur["layer"] = val.strip()
            elif code in ("10", "20"):
                try:
                    cur["x" if code == "10" else "y"].append(float(val))
                except ValueError:
                    pass
        i += 2
    if cur:
        ents.append(cur)
    by_layer = {}
    for e in ents:
        if e["t"] == "LWPOLYLINE" and len(e["x"]) >= 2:
            by_layer.setdefault(e["layer"], []).append(e)
    return by_layer


def poly_area(xs, ys):
    a = 0.0
    for j in range(len(xs)):
        k = (j + 1) % len(xs)
        a += xs[j] * ys[k] - xs[k] * ys[j]
    return abs(a) / 2.0


def point_in_poly(px, py, poly):
    inside = False
    n = len(poly)
    for j in range(n):
        k = (j + 1) % n
        x1, y1 = poly[j]
        x2, y2 = poly[k]
        if (y1 > py) != (y2 > py):
            xint = (x2 - x1) * (py - y1) / (y2 - y1 + 1e-12) + x1
            if px < xint:
                inside = not inside
    return inside


# --------------------------------------------------------------------------- CSV
def parse_csv(path):
    """Polycam's metrics CSV: Room,Description,Value. Values carry units and stray spaces."""
    rooms = {}
    meta = {}
    with open(path, newline="", encoding="utf-8-sig") as fh:
        for row in csv.reader(fh):
            if len(row) < 3:
                continue
            room, desc, val = row[0].strip(), row[1].strip(), row[2].strip()
            num = None
            m = re.match(r"^-?[\d.]+", val)
            if m:
                try:
                    num = float(m.group(0))
                except ValueError:
                    num = None
            if room in ("Settings", "") or room.startswith("$"):
                continue  # Polycam writes capture settings into the same table
            target = meta if room == "Entire Roomplan" else rooms.setdefault(room, {})
            key = desc.lower()
            if "floor area" in key:
                target["area"] = num
            elif "wall area" in key:
                target["wall"] = num
            elif "volume" in key:
                target["volume"] = num
            elif "ceiling height" in key:
                target["height"] = num
            elif "total window area" in key:
                target["windowArea"] = num
    return rooms, meta


# --------------------------------------------------------------------------- physics
class Library:
    """Coefficients come from programme-library.json, never from literals here (rule 2)."""

    def __init__(self):
        doc = json.load(open(LIB))
        self.phys = doc["physics"]
        self.progs = doc["programmes"]

    def require(self, names):
        missing = [n for n in names if n not in self.progs]
        if missing:
            sys.exit(
                "programme-library.json is missing residential programmes: %s\n"
                "Add them before generating a house fixture — the office programmes do not "
                "describe a kitchen or a bathroom." % ", ".join(sorted(missing))
            )

    def thermal(self, prog, area, height, solar_mult, wall_m2, roof_m2, partition_m2):
        """Identical derivation to officeize_fixture.py, so the two fixtures are comparable
        and the engine's physics sees the same shape of input."""
        spec = self.progs[prog]
        gain = area * spec["lightingWPerM2"] + spec.get("fixedEquipmentW", 0)
        cap = (self.phys["airRhoCpJPerM3K"] * area * height
               * self.phys["furnishingCapacitanceMultiplier"])
        ua = (wall_m2 * self.phys["uValueWallWPerM2K"]
              + roof_m2 * self.phys["uValueRoofWPerM2K"]
              + partition_m2 * self.phys["uValuePartitionWPerM2K"])
        r = 1.0 / ua if ua > 0 else 1.0
        return {
            "setpoint": spec["setpointC"],
            "deadband": spec["deadbandC"],
            "baseHeatLoad": round(gain),
            "solarGainMultiplier": round(solar_mult, 3),
            "rWall": round(r, 6),
            "cAir": round(cap),
            "areaM2": round(area, 1),
            "exteriorWallM2": round(wall_m2, 1),
            "roofM2": round(roof_m2, 1),
            "partitionM2": round(partition_m2, 1),
            "timeConstantH": round(r * cap / 3600.0, 2),
        }


def slug(name):
    return re.sub(r"[^a-z0-9]+", "-", name.lower()).strip("-")


# --------------------------------------------------------------------------- build
def build(scan_dir, write, top_floor=False):
    dxf = next((os.path.join(scan_dir, f) for f in os.listdir(scan_dir)
                if f.lower().endswith(".dxf")), None)
    csvf = next((os.path.join(scan_dir, f) for f in os.listdir(scan_dir)
                 if f.lower().endswith(".csv")), None)
    if not dxf or not csvf:
        sys.exit("need both a .dxf and a .csv in %s" % scan_dir)

    layers = parse_dxf(dxf)
    rooms_dxf = layers.get("Poly-Rooms", [])
    if not rooms_dxf:
        sys.exit("no Poly-Rooms layer in the DXF — is this a Polycam floor-plan export?")
    csv_rooms, csv_meta = parse_csv(csvf)

    # Normalise every coordinate so the plan sits in the positive quadrant, which is what
    # the dashboard's polygon renderer and the airflow solver both assume.
    allx = [v for e in rooms_dxf for v in e["x"]]
    ally = [v for e in rooms_dxf for v in e["y"]]
    ox, oy = min(allx), min(ally)

    def norm(e):
        return [[round(x - ox, 2), round(y - oy, 2)] for x, y in zip(e["x"], e["y"])]

    def centre(e):
        return (sum(e["x"]) / len(e["x"]) - ox, sum(e["y"]) / len(e["y"]) - oy)

    # Match DXF polygons to CSV rooms by area rank. The DXF traces wall centrelines and the
    # CSV reports inner faces, so the two differ by a few percent and never by an ordering.
    dxf_sorted = sorted(rooms_dxf, key=lambda e: -poly_area(e["x"], e["y"]))
    csv_sorted = sorted(csv_rooms.items(), key=lambda kv: -(kv[1].get("area") or 0))
    if len(dxf_sorted) != len(csv_sorted):
        print("! %d room polygons but %d rooms in the CSV — matching the first %d by area"
              % (len(dxf_sorted), len(csv_sorted), min(len(dxf_sorted), len(csv_sorted))))

    # Windows are drawn twice (both faces of the casing); collapse pairs within 30 cm, then
    # attribute each to the room it falls in. Polycam's own per-room window attribution puts
    # all 9.5 m2 in one room, which the geometry contradicts, so geometry wins.
    wins = []
    for e in layers.get("Poly-Windows", []):
        cx, cy = centre(e)
        if not any(math.hypot(cx - a, cy - b) < 0.3 for a, b in wins):
            wins.append((cx, cy))

    lib = Library()
    lib.require({p for p, _ in ROOM_PROGRAMME.values()})

    zones, win_counts = [], {}
    for poly_e, (csv_name, m) in zip(dxf_sorted, csv_sorted):
        prog, label = ROOM_PROGRAMME.get(csv_name, ("living", csv_name))
        poly = norm(poly_e)
        area = m.get("area") or poly_area(poly_e["x"], poly_e["y"])
        height = m.get("height") or 2.8
        wall = m.get("wall") or 0.0
        zones.append([csv_name, prog, label, poly, area, height, wall, 0])

    # Attribute each window to the NEAREST room, not the containing one: a window sits in a
    # wall, i.e. exactly on a room polygon's boundary, so a point-in-polygon test finds none
    # of them (it reported 3-in-one-room and zero everywhere else, which the plan contradicts).
    for wx, wy in wins:
        best, bestd = None, 1e18
        for z in zones:
            cx = sum(p[0] for p in z[3]) / len(z[3])
            cy = sum(p[1] for p in z[3]) / len(z[3])
            d = math.hypot(wx - cx, wy - cy)
            if d < bestd:
                best, bestd = z, d
        if best is not None:
            best[7] += 1
    for z in zones:
        win_counts[z[0]] = z[7]

    total_win = sum(n for _, _, _, _, _, _, _, n in zones) or 1
    floors_zones = []
    for csv_name, prog, label, poly, area, height, wall, nwin in zones:
        # Solar aperture scaled by this room's share of the glazing the scan actually found.
        # The most-glazed room lands near 0.8, matching the office fixture's observed maximum,
        # so the engine's solar term stays in a range its coefficients were sized for.
        peak = max(n for *_, n in zones) or 1
        solar = round(0.8 * nwin / peak, 3) if nwin else 0.0
        # A nha ong is built wall-to-wall with its neighbours: the two LONG side walls are
        # party walls shared with the houses either side, and only the short front and back
        # elevations see outdoor air. That is the opposite of a free-standing building and it
        # is why the split is weighted so far toward partition.
        exterior = wall * 0.35
        partition = wall - exterior
        # Roof exposure applies to the TOP floor only. This scan is a ground floor with the
        # house continuing above, so the ceiling faces a conditioned-ish space, not the sky.
        # Assuming an exposed roof here would roughly double every room's UA and halve its
        # time constant — the single biggest error available in this file.
        roof = area if top_floor else 0.0
        zid = "zone-%s-lvl1" % slug(label)
        cx = round(sum(p[0] for p in poly) / len(poly), 2)
        cy = round(sum(p[1] for p in poly) / len(poly), 2)
        floors_zones.append({
            "zoneId": zid,
            "name": label,
            "zoneType": prog,
            "bim_asset_id": str(uuid.uuid5(uuid.NAMESPACE_URL, "econ-house/" + zid)),
            "polygon": poly,
            "centroid": {"x": cx, "y": cy},
            "thermalProperties": lib.thermal(prog, area, height, solar, exterior, roof, partition),
            "hvacMapping": {"vavId": "vav-%s-lvl1" % slug(label)},
        })

    doors = []
    for e in layers.get("Poly-Doors", []):
        cx, cy = centre(e)
        doors.append({"x": round(cx, 2), "y": round(cy, 2), "tx": 0, "tz": 1})

    maxx = round(max(p[0] for z in floors_zones for p in z["polygon"]), 2)
    maxy = round(max(p[1] for z in floors_zones for p in z["polygon"]), 2)

    doc = {
        "buildingId": "bldg-econ-house-hcmc",
        "floors": [{
            "level": 1,
            "elevation": 0.0,
            "height": max((z["thermalProperties"]["areaM2"] and 2.8) for z in floors_zones),
            "name": "Ground floor",
            "geometry": {"outline": [[0, 0], [maxx, 0], [maxx, maxy], [0, maxy]]},
            "airflowDomain": {"doors": doors},
            "zones": floors_zones,
        }],
    }

    # ---- report -----------------------------------------------------------
    print("Scan      : %s" % os.path.basename(scan_dir))
    print("Envelope  : %.2f x %.2f m   livable %.1f m2 (scan) / %.1f m2 (zones)"
          % (maxx, maxy, csv_meta.get("area") or 0,
             sum(z["thermalProperties"]["areaM2"] for z in floors_zones)))
    print("Windows   : %d unique, total glazing %.1f m2 (scan)"
          % (len(wins), csv_meta.get("windowArea") or 0))
    print("Doors     : %d" % len(doors))
    print()
    print("%-22s %-13s %7s %7s %8s %9s %7s" %
          ("zone", "programme", "area", "height", "cAir", "rWall", "tau_h"))
    for z in floors_zones:
        t = z["thermalProperties"]
        print("%-22s %-13s %7.1f %7s %8.2e %9.5f %7.2f" %
              (z["name"], z["zoneType"], t["areaM2"],
               "-", t["cAir"], t["rWall"], t["timeConstantH"]))
    print()
    print("windows per room:", win_counts)

    if write:
        with open(OUT, "w") as fh:
            json.dump(doc, fh, indent=1)
        print("\nwrote %s (%d zones)" % (OUT, len(floors_zones)))
        print("This file is GITIGNORED — it is this deployment's own building and a pull "
              "cannot overwrite it. Delete it to fall back to the repository fixture.")
    else:
        print("\ndry run — pass --write to emit %s" % os.path.basename(OUT))
    return doc


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--scan", required=True, help="Polycam floor-plan export directory")
    ap.add_argument("--write", action="store_true")
    ap.add_argument("--top-floor", action="store_true",
                    help="this storey has an exposed roof (default: a floor sits above it)")
    args = ap.parse_args()
    build(os.path.expanduser(args.scan), args.write, args.top_floor)


if __name__ == "__main__":
    main()
