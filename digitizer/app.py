"""
ECON digitizer service — real-world blueprint in, building-data.json out.

Accepts the formats a building actually arrives in:
  .dxf            AutoCAD exchange. Vector path first: closed polylines are rooms and
                  text labels are type hints, with the drawing's own units respected.
                  A DXF with no closed room polylines is rasterized and handed to the
                  CV pipeline instead, so every DXF produces *something*.
  .pdf            First page rendered at 200 dpi (poppler), then the CV pipeline.
  .png/.jpg/...   Scans and phone photos, straight into the CV pipeline.

The CV pipeline is ai_modules/branch_b_digitization (DeepFloorplan-style room
segmentation -> metric polygons -> zones/VAVs/windows), imported, not reimplemented:
one pipeline, whichever door the blueprint came in through.
"""

import io
import json
import math
import os
import re
import tempfile

import cv2
import numpy as np
from fastapi import FastAPI, File, Form, HTTPException, UploadFile

from floorplan_to_buildingdata import build_building, px_to_metric, segment_rooms

app = FastAPI(title="econ-digitizer")

RASTER_EXTS = {".png", ".jpg", ".jpeg", ".bmp", ".webp", ".tif", ".tiff"}

# $INSUNITS -> meters per drawing unit. Unitless drawings fall back to the caller's
# footprint, exactly like the image path where pixels have no physical size either.
DXF_UNIT_M = {1: 0.0254, 2: 0.3048, 4: 0.001, 5: 0.01, 6: 1.0, 7: 1000.0}

TYPE_KEYWORDS = [
    ("server", "server-room"), ("data", "server-room"), ("it ", "server-room"),
    ("conference", "conference"), ("meeting", "conference"), ("họp", "conference"),
    ("lobby", "lobby"), ("sảnh", "lobby"), ("reception", "lobby"),
    ("corridor", "corridor"), ("hall", "corridor"), ("hành lang", "corridor"),
    ("mech", "mechanical"), ("electrical", "mechanical"), ("kỹ thuật", "mechanical"),
    ("wc", "mechanical"), ("toilet", "mechanical"),
    ("office", "office"), ("văn phòng", "office"),
]


def hint_from_text(s):
    low = (s or "").lower()
    for kw, t in TYPE_KEYWORDS:
        if kw in low:
            return t
    return None


def point_in_poly(x, y, poly):
    inside = False
    j = len(poly) - 1
    for i in range(len(poly)):
        xi, yi = poly[i]
        xj, yj = poly[j]
        if (yi > y) != (yj > y) and x < (xj - xi) * (y - yi) / (yj - yi + 1e-12) + xi:
            inside = not inside
        j = i
    return inside


def poly_area(poly):
    s = 0.0
    for i in range(len(poly)):
        x1, y1 = poly[i]
        x2, y2 = poly[(i + 1) % len(poly)]
        s += x1 * y2 - x2 * y1
    return abs(s) / 2.0


def rooms_from_dxf_vectors(doc, fallback_fw, fallback_fd):
    """Closed polylines -> rooms in meters. Returns (rooms_m, fw, fd, unit_note) or None."""
    msp = doc.modelspace()
    polys = []
    for e in msp.query("LWPOLYLINE POLYLINE"):
        try:
            if not e.is_closed:
                continue
            if e.dxftype() == "LWPOLYLINE":
                pts = [(float(p[0]), float(p[1])) for p in e.get_points()]
            else:
                pts = [(float(v.dxf.location.x), float(v.dxf.location.y)) for v in e.vertices]
        except Exception:
            continue
        if len(pts) >= 3:
            polys.append(pts)
    if len(polys) < 2:
        return None  # not drawn as room polylines; caller rasterizes instead

    unit = DXF_UNIT_M.get(int(doc.header.get("$INSUNITS", 0)), None)

    # Drop the outline: a polyline that contains nearly the full extent is the building
    # shell, not a room.
    total = max(poly_area(p) for p in polys)
    rooms = [p for p in polys if poly_area(p) < total * 0.9] or polys

    xs = [x for p in rooms for x, _ in p]
    ys = [y for p in rooms for _, y in p]
    minx, maxx, miny, maxy = min(xs), max(xs), min(ys), max(ys)
    spanx, spany = max(maxx - minx, 1e-9), max(maxy - miny, 1e-9)

    if unit:  # real units in the file win over the caller's guess
        fw, fd = spanx * unit, spany * unit
        sx = sy = unit
        unit_note = f"units from DXF ($INSUNITS): {unit} m/unit"
    else:
        fw, fd = fallback_fw, fallback_fd
        sx, sy = fw / spanx, fd / spany
        unit_note = "DXF is unitless; scaled to the requested footprint"

    # Text labels become type hints for the room that contains them.
    labels = []
    for e in msp.query("TEXT MTEXT"):
        try:
            ins = e.dxf.insert
            txt = e.text if e.dxftype() == "MTEXT" else e.dxf.text
        except Exception:
            continue
        hint = hint_from_text(txt)
        if hint:
            labels.append((float(ins.x), float(ins.y), hint))

    rooms_m = []
    for p in rooms:
        hint = next((h for lx, ly, h in labels if point_in_poly(lx, ly, p)), None)
        # DXF y grows up; the pipeline's metric space grows down — flip y.
        poly_m = [[round((x - minx) * sx, 2), round((maxy - y) * sy, 2)] for x, y in p]
        rooms_m.append({"poly_m": poly_m, "type_hint": hint, "adjacent_to": []})
    return rooms_m, fw, fd, unit_note


def rooms_from_image(img, fw, fd):
    rooms, method = segment_rooms(img)
    if not rooms:
        raise HTTPException(422, "no rooms detected in the drawing — check contrast/threshold, "
                                 "or export closed room polylines if this came from CAD")
    h, w = img.shape[:2]
    rooms_m = [{"poly_m": px_to_metric(r["polygon_px"], w, h, fw, fd),
                "type_hint": r.get("type_hint"),
                "adjacent_to": r.get("adjacent_to", [])} for r in rooms]
    return rooms_m, method


def ontology_for(building):
    """Same brick triples main() emits, so the deployed twin keeps its topology view."""
    triples = []
    for fl in building["floors"]:
        for z in fl["zones"]:
            triples.append({"subject": z["hvacMapping"]["vavId"],
                            "predicate": "brick:feeds", "object": z["zoneId"]})
            for adj_idx in z.get("adjacent_to_idx", []):
                if adj_idx < len(fl["zones"]):
                    triples.append({"subject": z["zoneId"], "predicate": "brick:adjacentTo",
                                    "object": fl["zones"][adj_idx]["zoneId"]})
            z.pop("adjacent_to_idx", None)
    return triples


@app.get("/health")
def health():
    return {"ok": True}


@app.post("/digitize")
async def digitize(file: UploadFile = File(...), floors: int = Form(1), footprint: str = Form("60x40")):
    try:
        fw, fd = (float(v) for v in footprint.lower().split("x"))
    except Exception:
        raise HTTPException(400, "footprint must look like 60x40 (meters, WxD)")
    if not (1 <= floors <= 200):
        raise HTTPException(400, "floors must be 1..200")

    raw = await file.read()
    if len(raw) > 40 * 1024 * 1024:
        raise HTTPException(413, "blueprint larger than 40 MB")
    ext = os.path.splitext(file.filename or "")[1].lower()

    method, unit_note = None, None

    if ext == ".dxf":
        import ezdxf
        with tempfile.NamedTemporaryFile(suffix=".dxf", delete=False) as f:
            f.write(raw)
            path = f.name
        try:
            doc = ezdxf.readfile(path)
        except Exception as e:
            raise HTTPException(422, f"could not parse DXF: {e} — DWG must be exported "
                                     "to DXF first (SAVEAS -> DXF in AutoCAD)")
        finally:
            os.unlink(path)

        vec = rooms_from_dxf_vectors(doc, fw, fd)
        if vec:
            rooms_m, fw, fd, unit_note = vec
            method = "dxf-vector"
        else:
            # No closed room polylines: render the drawing and treat it like a scan.
            from ezdxf.addons.drawing import Frontend, RenderContext
            from ezdxf.addons.drawing.matplotlib import MatplotlibBackend
            import matplotlib
            matplotlib.use("Agg")
            import matplotlib.pyplot as plt
            fig = plt.figure(figsize=(16, 12), dpi=150)
            ax = fig.add_axes([0, 0, 1, 1])
            Frontend(RenderContext(doc), MatplotlibBackend(ax)).draw_layout(doc.modelspace())
            buf = io.BytesIO()
            fig.savefig(buf, format="png", facecolor="white")
            plt.close(fig)
            img = cv2.imdecode(np.frombuffer(buf.getvalue(), np.uint8), cv2.IMREAD_COLOR)
            rooms_m, method = rooms_from_image(img, fw, fd)
            method = f"dxf-rasterized+{method}"

    elif ext == ".pdf":
        from pdf2image import convert_from_bytes
        pages = convert_from_bytes(raw, dpi=200, first_page=1, last_page=1)
        if not pages:
            raise HTTPException(422, "PDF has no renderable pages")
        img = cv2.cvtColor(np.array(pages[0]), cv2.COLOR_RGB2BGR)
        rooms_m, method = rooms_from_image(img, fw, fd)
        method = f"pdf+{method}"

    elif ext in RASTER_EXTS:
        img = cv2.imdecode(np.frombuffer(raw, np.uint8), cv2.IMREAD_COLOR)
        if img is None:
            raise HTTPException(422, "could not decode the image")
        rooms_m, method = rooms_from_image(img, fw, fd)

    else:
        raise HTTPException(415, f"unsupported blueprint type '{ext}' — send DXF, PDF, or an image")

    building = build_building(rooms_m, floors, fw, fd)
    ontology = ontology_for(building)

    per_floor = len(building["floors"][0]["zones"]) if building["floors"] else 0
    types = {}
    for fl in building["floors"]:
        for z in fl["zones"]:
            types[z["zoneType"]] = types.get(z["zoneType"], 0) + 1

    return {
        "buildingData": building,
        "ontology": ontology,
        "stats": {
            "method": method,
            "unitNote": unit_note,
            "floors": floors,
            "zonesPerFloor": per_floor,
            "totalZones": sum(len(f["zones"]) for f in building["floors"]),
            "zoneTypes": types,
            "footprintM": [fw, fd],
            "source": file.filename,
        },
    }
