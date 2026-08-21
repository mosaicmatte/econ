#!/usr/bin/env python3
"""Build a 3D CAD model of the pilot house and its electrical system.

Geometry comes from the same Polycam LiDAR export that feeds `housify_fixture.py`, so the
CAD model and the twin's thermal fixture describe the same building and cannot drift apart.

    walls, floor, rooms   MEASURED  -- Poly-Walls / Poly-Rooms footprints, extruded
    doors, windows        MEASURED  -- Poly-Doors / Poly-Windows positions, cut as openings
    electrical components APPROXIMATE -- placed from the photographic survey, see below

What "approximate" means for the electrical layer
-------------------------------------------------
The survey photographs establish, beyond doubt, WHAT is installed and roughly at what
HEIGHT (main board above a doorway at ladder height; local breakers above sockets at
working height). They do not establish plan coordinates to better than about half a metre,
because nothing in the survey was dimensioned against the scan. Every electrical solid is
therefore placed against a room it was photographed in, at its surveyed height, and is
tagged APPROX in the assembly labels. Treat the electrical layer as a schematic in 3D, not
as a setting-out drawing.

Outputs (STEP is primary, per the CAD skill's STEP-first workflow):
    house.step   full assembly, labelled
    house.stl    mesh, for quick viewing or printing
    house-electrical.step   the electrical layer alone

Usage:
    python tools/house_cad.py --scan "<Polycam export dir>" --out build/cad
"""

import argparse
import os
import sys

from build123d import (
    Align, Axis, Box, BuildPart, Color, Compound, Location, Mode, Plane,
    Pos, add, export_step, export_stl, extrude, make_face, Polyline,
)

# ---------------------------------------------------------------- survey constants
# Heights come from the photographs (a doorway is ~2.1 m, a socket above a worktop is
# ~1.1 m); the scan gives 2.8 m ceilings. Millimetres, matching the CAD skill's default.
CEILING_MM = 2800
MAIN_BOARD_H = 2200      # above a doorway, reached by ladder -- photos IMG_9221..9223
SUB_BOARD_H = 1400       # local breaker above a socket, working height -- IMG_9233/9240
SOCKET_H = 1100          # wall sockets -- IMG_9226/9227/9238
AC_INDOOR_H = 2300       # split-unit indoor head, high on the wall -- IMG_9231
ROUTER_H = 2600          # VNPT router, ceiling corner -- IMG_9237

# Component footprints, from the photographed enclosures (mm).
PARTS = {
    "main-board":   (300, 120, 400),   # perf board carrying MCB + fuse + knife switch
    "sub-board":    (110, 70, 150),    # local breaker in its enclosure
    "socket":       (120, 45, 80),     # Vanlock / Vidaco 2-gang
    "ac-indoor":    (800, 200, 280),   # split-unit indoor head
    "router":       (220, 40, 160),    # VNPT ONT
    "meter":        (180, 110, 260),   # utility kWh meter
    "svp912":       (80, 70, 130),     # SINOTIMER voltage protector
}


# ---------------------------------------------------------------- DXF (shared with housify)
def parse_dxf(path):
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
    out = {}
    for e in ents:
        if e["t"] == "LWPOLYLINE" and len(e["x"]) >= 3:
            out.setdefault(e["layer"], []).append(e)
    return out


def poly_area(xs, ys):
    a = 0.0
    for j in range(len(xs)):
        k = (j + 1) % len(xs)
        a += xs[j] * ys[k] - xs[k] * ys[j]
    return abs(a) / 2.0


def prism(poly_mm, height_mm, base_z=0.0):
    """Extrude a closed 2D polygon (list of (x, y) in mm) into a solid."""
    pts = [(round(x, 3), round(y, 3)) for x, y in poly_mm]
    if pts[0] != pts[-1]:
        pts.append(pts[0])
    # Collapse consecutive duplicates; Polyline rejects zero-length segments.
    clean = [pts[0]]
    for p in pts[1:]:
        if abs(p[0] - clean[-1][0]) > 1e-6 or abs(p[1] - clean[-1][1]) > 1e-6:
            clean.append(p)
    if len(clean) < 4:
        return None
    with BuildPart() as bp:
        with BuildSketchContext(base_z):
            pass
    # Built directly rather than through BuildPart, so a bad polygon fails loudly here.
    face = make_face(Polyline(*clean))
    solid = extrude(face, amount=height_mm)
    return Pos(0, 0, base_z) * solid


class BuildSketchContext:
    """Placeholder so the helper above reads linearly; build123d needs no real context."""

    def __init__(self, _z):
        pass

    def __enter__(self):
        return self

    def __exit__(self, *a):
        return False


# ---------------------------------------------------------------- build
def build(scan_dir, out_dir):
    dxf_path = next((os.path.join(scan_dir, f) for f in os.listdir(scan_dir)
                     if f.lower().endswith(".dxf")), None)
    if not dxf_path:
        sys.exit("no .dxf in %s" % scan_dir)
    layers = parse_dxf(dxf_path)

    rooms = layers.get("Poly-Rooms", [])
    walls = layers.get("Poly-Walls", [])
    doors = layers.get("Poly-Doors", [])
    windows = layers.get("Poly-Windows", [])
    if not rooms or not walls:
        sys.exit("DXF is missing Poly-Rooms or Poly-Walls")

    allx = [v for e in rooms + walls for v in e["x"]]
    ally = [v for e in rooms + walls for v in e["y"]]
    ox, oy = min(allx), min(ally)
    M = 1000.0  # scan is in metres, CAD in mm

    def to_mm(e):
        return [((x - ox) * M, (y - oy) * M) for x, y in zip(e["x"], e["y"])]

    def centre_mm(e):
        p = to_mm(e)
        return (sum(a for a, _ in p) / len(p), sum(b for _, b in p) / len(p))

    print("Scan     : %s" % os.path.basename(scan_dir))
    print("Envelope : %.2f x %.2f m, ceiling %.2f m"
          % ((max(allx) - ox), (max(ally) - oy), CEILING_MM / M))
    print("Layers   : %d rooms, %d wall segments, %d doors, %d windows"
          % (len(rooms), len(walls), len(doors), len(windows)))

    # ---- structure -------------------------------------------------------
    parts, labels = [], []

    slab = prism([(0, 0), ((max(allx) - ox) * M, 0),
                  ((max(allx) - ox) * M, (max(ally) - oy) * M),
                  (0, (max(ally) - oy) * M)], 120, base_z=-120)
    parts.append(slab)
    labels.append("floor-slab")

    wall_solids = []
    for w in walls:
        s = prism(to_mm(w), CEILING_MM)
        if s is not None:
            wall_solids.append(s)
    print("Walls    : %d extruded to %d mm" % (len(wall_solids), CEILING_MM))

    # ---- openings --------------------------------------------------------
    # Doors and windows are cut from the wall mass rather than modelled as frames: the scan
    # gives their footprint but not their reveal detail, and a cut opening is the honest
    # representation of "we know a hole is here, not what is in it".
    cutters = []
    for d in doors:
        cx, cy = centre_mm(d)
        cutters.append(Pos(cx, cy, 1050) * Box(1000, 400, 2100))
    for w in windows:
        cx, cy = centre_mm(w)
        cutters.append(Pos(cx, cy, 1800) * Box(1100, 400, 1200))

    wall_mass = wall_solids[0]
    for s in wall_solids[1:]:
        wall_mass = wall_mass + s
    for c in cutters:
        wall_mass = wall_mass - c
    parts.append(wall_mass)
    labels.append("walls (%d openings cut)" % len(cutters))

    # ---- electrical layer ------------------------------------------------
    # Rooms sorted largest-first, matching housify_fixture.py's area-rank convention:
    #   0 kitchen/rear  1 office  2 living  3 passage  4 bathroom
    rs = sorted(rooms, key=lambda e: -poly_area(e["x"], e["y"]))
    ctr = [centre_mm(r) for r in rs]

    def wall_anchor(room_idx, frac=0.5, inset=90.0):
        """A point ON the room's longest wall, pushed `inset` mm into the room.

        Electrical gear is fixed to walls. Placing it at a room centroid leaves every
        component floating in mid-air, which both looks wrong and would mislead anyone
        reading the model for where to put a clamp. This walks the room polygon, takes its
        longest edge as the most likely mounting wall, and returns a point along it offset
        along the inward normal.
        """
        poly = to_mm(rs[room_idx])
        best, blen = None, -1.0
        for j in range(len(poly)):
            (x1, y1), (x2, y2) = poly[j], poly[(j + 1) % len(poly)]
            L = ((x2 - x1) ** 2 + (y2 - y1) ** 2) ** 0.5
            if L > blen:
                best, blen = ((x1, y1), (x2, y2)), L
        (x1, y1), (x2, y2) = best
        px, py = x1 + (x2 - x1) * frac, y1 + (y2 - y1) * frac
        # Inward normal: rotate the edge 90 deg and pick the sense pointing at the centroid.
        nx, ny = -(y2 - y1) / blen, (x2 - x1) / blen
        cx, cy = ctr[room_idx]
        if (cx - px) * nx + (cy - py) * ny < 0:
            nx, ny = -nx, -ny
        return (px + nx * inset, py + ny * inset)

    elec = []

    def place(kind, name, xy, z):
        w, d, h = PARTS[kind]
        s = Pos(xy[0], xy[1], z) * Box(w, d, h)
        elec.append(s)
        labels.append("APPROX %s" % name)
        return s

    # Main board: above a doorway on the kitchen/rear side, ladder height (IMG_9221-9223).
    place("main-board", "main board (C50 MCB + 30A fuse + knife switch)",
          wall_anchor(0, 0.45), MAIN_BOARD_H)
    place("meter", "utility kWh meter", wall_anchor(0, 0.35), MAIN_BOARD_H)
    place("svp912", "SVP-912 voltage protector 40A", wall_anchor(0, 0.55), MAIN_BOARD_H)

    # Local breakers above sockets, at working height -- the reachable metering points, and
    # the reason this floor was chosen as the pilot at all.
    for nm, idx, frac in [("kitchen", 0, 0.75), ("office", 1, 0.30),
                          ("living", 2, 0.50), ("passage", 3, 0.50)]:
        a = wall_anchor(idx, frac)
        place("sub-board", "local breaker (%s)" % nm, a, SUB_BOARD_H)
        place("socket", "socket outlet (%s)" % nm, a, SOCKET_H)

    # The AC indoor head and the router, both photographed high on a wall.
    place("ac-indoor", "AC indoor unit (split)", wall_anchor(1, 0.65), AC_INDOOR_H)
    place("router", "VNPT router", wall_anchor(1, 0.90), ROUTER_H)

    elec_mass = elec[0]
    for s in elec[1:]:
        elec_mass = elec_mass + s
    print("Electrical: %d components placed (APPROX)" % len(elec))

    # ---- export ----------------------------------------------------------
    os.makedirs(out_dir, exist_ok=True)
    step_path = os.path.join(out_dir, "house.step")
    stl_path = os.path.join(out_dir, "house.stl")
    elec_path = os.path.join(out_dir, "house-electrical.step")

    # Export the electrical layer on its own FIRST. Compound() reparents its children, so a
    # solid that has already been folded into the assembly can no longer be exported alone.
    export_step(elec_mass, elec_path)
    export_stl(elec_mass, os.path.join(out_dir, "house-electrical.stl"))

    house = Compound(children=[slab, wall_mass, elec_mass])
    house.label = "econ-house-hcmc"
    export_step(house, step_path)
    export_stl(house, stl_path)

    for p in (step_path, stl_path, elec_path):
        print("wrote %s  (%.1f KB)" % (p, os.path.getsize(p) / 1024))
    return house


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--scan", required=True)
    ap.add_argument("--out", default="build/cad")
    ap.add_argument("--render", action="store_true", help="also write house_views.png (needs matplotlib)")
    a = ap.parse_args()
    out = os.path.expanduser(a.out)
    build(os.path.expanduser(a.scan), out)
    if a.render:
        render(out)




# ---------------------------------------------------------------- optional render
def render(out_dir):
    """Four review views as one PNG. Optional: needs matplotlib, and the CAD artifacts
    (STEP/STL) are the real deliverable -- this only exists so the model can be eyeballed
    without opening a CAD package."""
    try:
        import numpy as np
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
        from mpl_toolkits.mplot3d.art3d import Poly3DCollection
    except ImportError:
        print("render skipped: matplotlib not installed (pip install matplotlib)")
        return None

    def load_stl(p):
        raw = open(p, "rb").read()
        if raw[:5] == b"solid" and b"facet" in raw[:2000]:
            v = [t[1:4] for t in (l.split() for l in raw.decode(errors="ignore").splitlines())
                 if t[:1] == ["vertex"]]
            return np.array(v, dtype=float).reshape(-1, 3, 3)
        n = int.from_bytes(raw[80:84], "little")
        return np.array([np.frombuffer(raw[84 + i * 50 + 12:84 + i * 50 + 48],
                                       dtype="<f4").reshape(3, 3) for i in range(n)])

    house = load_stl(os.path.join(out_dir, "house.stl")) / 1000.0
    elec = load_stl(os.path.join(out_dir, "house-electrical.stl")) / 1000.0
    hs, ec = house.reshape(-1, 9).round(4), elec.reshape(-1, 9).round(4)
    keep = ~np.isin(hs.view([("", hs.dtype)] * 9).ravel(), ec.view([("", ec.dtype)] * 9).ravel())
    struct = hs[keep].reshape(-1, 3, 3)

    def shade(tri, base):
        n = np.cross(tri[:, 1] - tri[:, 0], tri[:, 2] - tri[:, 0])
        ln = np.linalg.norm(n, axis=1, keepdims=True)
        ln[ln == 0] = 1
        light = np.array([0.4, -0.7, 0.6])
        lam = np.clip((n / ln) @ (light / np.linalg.norm(light)), 0, 1) * 0.55 + 0.45
        return np.clip(np.array(base)[None, :] * lam[:, None], 0, 1)

    def panel(ax, el, az, title, with_struct):
        layers = ([(struct, [0.62, 0.68, 0.75], "#46525e")] if with_struct else []) + \
                 [(elec, [0.91, 0.36, 0.18], "#6d2409")]
        v = np.array([np.cos(np.radians(el)) * np.cos(np.radians(az)),
                      np.cos(np.radians(el)) * np.sin(np.radians(az)), np.sin(np.radians(el))])
        for tri, base, edge in layers:
            idx = np.argsort(tri.mean(axis=1) @ v)   # painter's algorithm; mpl has no z-buffer
            ax.add_collection3d(Poly3DCollection(tri[idx], facecolors=shade(tri[idx], base),
                                                 edgecolor=edge, linewidths=0.12))
        ax.set_xlim(0, 14); ax.set_ylim(0, 5.7); ax.set_zlim(0, 3.0)
        ax.set_box_aspect((14, 5.7, 3.0)); ax.view_init(elev=el, azim=az)
        ax.set_title(title, fontsize=12, pad=2); ax.set_axis_off()

    fig = plt.figure(figsize=(16, 15), facecolor="white")
    for i, (el, az, t, ss) in enumerate([
            (24, -62, "1. Isometric - structure + electrical", True),
            (90, -90, "2. Plan (looking down)", True),
            (10, -90, "3. Long elevation - component heights", True),
            (24, -62, "4. Electrical layer alone (APPROX placement)", False)], 1):
        panel(fig.add_subplot(4, 1, i, projection="3d"), el, az, t, ss)
    plt.tight_layout()
    p = os.path.join(out_dir, "house_views.png")
    plt.savefig(p, dpi=110, bbox_inches="tight", facecolor="white")
    print("wrote %s" % p)
    return p


if __name__ == "__main__":
    main()
