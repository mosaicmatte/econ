"""
DeepFloorplan-style multi-task floorplan segmenter — runnable port + real neural path.

DeepFloorplan's idea is two heads — a room-boundary head and a room-type head. This module
offers both a dependency-light classical port and a *real* neural segmenter, behind one
interface that `floorplan_to_buildingdata.py` consumes:

    rooms(img) -> [{ "polygon_px": [(x,y), ...],
                     "type_hint": "office|conference|corridor|server-room|lobby|mechanical" }]

Two engines, selectable via `engine=` or the `ECON_DF_ENGINE` env var (auto|neural|classical):

  • classical (default) — no model needed, works anywhere:
      1. wall/boundary head  -> wall mask from dark thick strokes (adaptive threshold + morphology)
      2. room segmentation   -> watershed on the non-wall interior, seeded by distance-transform
                                peaks, so adjacent rooms split even across doorways
      3. room-type head       -> geometry/topology classifier per region

  • neural — a learned room-interior segmenter (SegFormer fine-tuned on floorplans,
      `Patnev71/segformer-b0-finetuned-floorplan`, 3.7M params, MPS/CPU). It replaces the
      fragile dark-pixel wall threshold with a learned Room/Background mask; the SAME
      watershed + polygon + type stage then runs on top. Weights are pulled once and cached
      under `models/` (offline thereafter). Falls back to classical if torch/transformers or
      the download is unavailable. Room TYPE is still geometric — no off-the-shelf model emits
      commercial archetypes (office/server/…); the door/window/wall Mask2Former
      (`Hyunwoo1605/mask2former-floorplan-instance-segmentation`) is the path for door->adjacency.

  • custom weights override — drop a `deepfloorplan_weights.py` exposing `infer(img)` next to
      this file and the neural path will prefer it (e.g. a real CubiCasa5K / TF2DeepFloorplan port).
"""

import os

import cv2
import numpy as np

ROOM_TYPES = ("office", "conference", "corridor", "server-room", "lobby", "mechanical")

# SegFormer floorplan room-segmenter: binary {0: Background, 1: Room}.
NEURAL_MODEL_ID = os.environ.get("ECON_DF_MODEL", "Patnev71/segformer-b0-finetuned-floorplan")
_MODELS_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "models")
_NEURAL = {"loaded": False, "model": None, "processor": None, "device": None}


def rooms(img, engine=None):
    """Segment rooms. engine: 'auto' | 'neural' | 'classical' (default from ECON_DF_ENGINE,
    else 'classical' to preserve the verified classical pipeline)."""
    engine = (engine or os.environ.get("ECON_DF_ENGINE", "classical")).lower()
    if engine in ("neural", "auto"):
        neural = _rooms_neural(img)
        if neural:
            return neural
        if engine == "neural":
            return []  # explicit neural request failed — surface it, don't silently differ
    return _rooms_classical(img)


# --------------------------------------------------------------------------- neural path
def _rooms_neural(img, max_dim=2200, infer_long=1024, min_area_frac=0.0006):
    """Learned room-interior segmentation -> shared watershed/polygon/type extraction.

    Returns the same list shape as _rooms_classical, or None to fall back to classical."""
    # 1) custom drop-in weights win if provided (e.g. a real CubiCasa5K port)
    try:
        import deepfloorplan_weights  # optional, user-provided
        return deepfloorplan_weights.infer(img)
    except Exception:
        pass

    model, processor, device = _load_segformer()
    if model is None:
        return None

    h0, w0 = img.shape[:2]
    scale = min(1.0, max_dim / max(h0, w0))
    work = cv2.resize(img, None, fx=scale, fy=scale, interpolation=cv2.INTER_AREA) if scale < 1 else img.copy()
    if work.ndim == 2:
        work = cv2.cvtColor(work, cv2.COLOR_GRAY2BGR)
    Hh, Ww = work.shape[:2]

    try:
        import torch
        from PIL import Image
        rgb = cv2.cvtColor(work, cv2.COLOR_BGR2RGB)
        # keep aspect; long side -> infer_long, rounded to a multiple of 32 for the encoder
        s = infer_long / max(Hh, Ww)
        ih = max(32, (int(Hh * s) // 32) * 32)
        iw = max(32, (int(Ww * s) // 32) * 32)
        processor.size = {"height": ih, "width": iw}
        inputs = processor(images=Image.fromarray(rgb), return_tensors="pt").to(device)
        with torch.no_grad():
            logits = model(**inputs).logits
        seg = processor.post_process_semantic_segmentation(
            type("O", (), {"logits": logits}), target_sizes=[(Hh, Ww)])[0]
        room = (seg.cpu().numpy().astype(np.uint8) == 1).astype(np.uint8) * 255
    except Exception as e:
        print(f"[neural] inference failed ({e}); falling back to classical")
        return None

    # clean speckle, then feed the SAME extractor the classical path uses
    room = cv2.morphologyEx(room, cv2.MORPH_OPEN, np.ones((3, 3), np.uint8))
    room = cv2.morphologyEx(room, cv2.MORPH_CLOSE, np.ones((3, 3), np.uint8))
    if np.count_nonzero(room) == 0:
        return None
    walls = cv2.bitwise_not(room)
    out = _extract_rooms(work, scale, interior=room, walls=walls, min_area_frac=min_area_frac, doorways=None)
    return out or None


def _load_segformer():
    """Lazy-load + cache the SegFormer floorplan model. Returns (model, processor, device)
    or (None, None, None) if torch/transformers/weights are unavailable."""
    if _NEURAL["loaded"]:
        return _NEURAL["model"], _NEURAL["processor"], _NEURAL["device"]
    _NEURAL["loaded"] = True
    try:
        import torch
        from transformers import SegformerForSemanticSegmentation, SegformerImageProcessor
        os.makedirs(_MODELS_DIR, exist_ok=True)
        device = "mps" if torch.backends.mps.is_available() else ("cuda" if torch.cuda.is_available() else "cpu")
        processor = SegformerImageProcessor.from_pretrained(NEURAL_MODEL_ID, cache_dir=_MODELS_DIR)
        model = SegformerForSemanticSegmentation.from_pretrained(NEURAL_MODEL_ID, cache_dir=_MODELS_DIR)
        model.to(device).eval()
        _NEURAL.update(model=model, processor=processor, device=device)
        print(f"[neural] loaded {NEURAL_MODEL_ID} on {device}")
    except Exception as e:
        print(f"[neural] unavailable ({e}); using classical engine")
        _NEURAL.update(model=None, processor=None, device=None)
    return _NEURAL["model"], _NEURAL["processor"], _NEURAL["device"]


# --------------------------------------------------------------------------- classical path
def _rooms_classical(img, max_dim=2200, min_area_frac=0.0006):
    h0, w0 = img.shape[:2]
    scale = min(1.0, max_dim / max(h0, w0))
    work = cv2.resize(img, None, fx=scale, fy=scale, interpolation=cv2.INTER_AREA) if scale < 1 else img.copy()
    if work.ndim == 2:
        work = cv2.cvtColor(work, cv2.COLOR_GRAY2BGR)
    gray = cv2.cvtColor(work, cv2.COLOR_BGR2GRAY)
    H, W = gray.shape

    # 1) WALL / BOUNDARY HEAD --------------------------------------------------
    # Combine an adaptive pass (catches thin walls on varying paper) with a global dark pass
    # (walls are the darkest ink) so the structure survives downscaling.
    blk = max(11, (min(H, W) // 60) | 1)  # odd block size that scales with the image
    adapt = cv2.adaptiveThreshold(gray, 255, cv2.ADAPTIVE_THRESH_MEAN_C,
                                  cv2.THRESH_BINARY_INV, blk, 12)
    _, dark = cv2.threshold(gray, 110, 255, cv2.THRESH_BINARY_INV)
    walls = cv2.bitwise_or(adapt, dark)
    walls = cv2.morphologyEx(walls, cv2.MORPH_OPEN, np.ones((2, 2), np.uint8))   # drop text/specks

    # STRIP SMALL CCs (furniture, symbols)
    n_labels, labels, stats, _ = cv2.connectedComponentsWithStats(walls, connectivity=8)
    min_cc_area = max(20, min(H, W) * 0.5) # increased threshold
    clean_walls = np.zeros_like(walls)
    for i in range(1, n_labels):
        if stats[i, cv2.CC_STAT_AREA] >= min_cc_area:
            clean_walls[labels == i] = 255
    walls = clean_walls

    walls = cv2.dilate(walls, np.ones((3, 3), np.uint8))                          # solidify thin walls

    # Seal doorway gaps so each room is an ENCLOSED interior blob before watershed — otherwise
    # rooms connected through open doors merge into one region and seeds land in the slivers
    # beside walls (every region then looks like a thin "corridor"). Mirrors the bridge's OpenCV
    # path. The sealed mask is the watershed boundary too, so rooms split at the doorway line.
    seal = max(3, int(min(H, W) * 0.012)) | 1
    sealed_walls = cv2.morphologyEx(walls, cv2.MORPH_CLOSE, np.ones((seal, seal), np.uint8))

    # DOORWAYS are the gaps that got filled
    doorways = cv2.bitwise_and(cv2.bitwise_not(walls), sealed_walls)
    doorways = cv2.morphologyEx(doorways, cv2.MORPH_OPEN, np.ones((3, 3), np.uint8))

    interior = cv2.bitwise_not(sealed_walls)
    interior = cv2.morphologyEx(interior, cv2.MORPH_OPEN, np.ones((3, 3), np.uint8))

    cv2.imwrite("debug_walls.png", walls)
    cv2.imwrite("debug_sealed.png", sealed_walls)
    cv2.imwrite("debug_doorways.png", doorways)

    return _extract_rooms(work, scale, interior=interior, walls=sealed_walls, min_area_frac=min_area_frac, doorways=doorways)


# --------------------------------------------------------------------------- shared extractor
def _extract_rooms(work, scale, interior, walls, min_area_frac, doorways=None):
    """Watershed room segmentation + polygon extraction + type classification, shared by both
    engines. `interior` = room-interior mask (255 inside rooms), `walls` = its complement used
    as the watershed boundary. Coordinates are de-scaled by `scale` back to original pixels."""
    H, W = work.shape[:2]

    # 2) ROOM SEGMENTATION (watershed bounded by walls) -----------------------
    # Seed with LOCAL maxima of the distance transform, not a single global threshold: a large
    # open area (atrium/courtyard) makes dist.max() huge, and a global `frac*dist.max()` cut then
    # exceeds every small room's peak so they get no seed and are swallowed. Local maxima place one
    # marker in every enclosed region regardless of size; an absolute floor (a few px above wall
    # thickness) rejects noise without scaling to the biggest room.
    dist = cv2.distanceTransform(interior, cv2.DIST_L2, 5)
    if dist.max() <= 0:
        return []
    k = (max(3, int(min(H, W) * 0.012)) | 1)
    pk = cv2.getStructuringElement(cv2.MORPH_ELLIPSE, (k, k))
    floor = max(3.0, 0.03 * dist.max())
    localmax = ((dist >= cv2.dilate(dist, pk)) & (dist > floor)).astype(np.uint8) * 255
    peaks = cv2.dilate(localmax, pk)       # coalesce maxima that belong to the same room

    markers = np.zeros(work.shape[:2], dtype=np.int32)
    n_seeds, peak_labels = cv2.connectedComponents(peaks)
    markers[peaks > 0] = peak_labels[peaks > 0] + 1
    
    markers[0, :] = 1
    markers[-1, :] = 1
    markers[:, 0] = 1
    markers[:, -1] = 1
    
    work_ws = work.copy()
    work_ws[walls > 0] = (0, 0, 0)
    markers = cv2.watershed(work_ws, markers)

    # 3) ROOM-TYPE HEAD + polygon extraction ----------------------------------
    min_area = min_area_frac * H * W
    out = []
    lab_to_idx = {}
    for lab in range(2, markers.max() + 1):
        mask = np.uint8(markers == lab) * 255
        area = int(np.count_nonzero(mask))
        if area < min_area:
            continue
        cnts, _ = cv2.findContours(mask, cv2.RETR_EXTERNAL, cv2.CHAIN_APPROX_SIMPLE)
        if not cnts:
            continue
        c = max(cnts, key=cv2.contourArea)
        x, y, bw, bh = cv2.boundingRect(c)
        if bw > 0.97 * W and bh > 0.97 * H:
            continue                       # the whole floor plate, not a room
        # Check if this region touches the image border (likely exterior bleeding)
        mask_border = np.zeros_like(mask)
        mask_border[0,:]=255; mask_border[-1,:]=255; mask_border[:,0]=255; mask_border[:,-1]=255
        if np.any(mask & mask_border):
            continue

        eps = 0.012 * cv2.arcLength(c, True)
        approx = cv2.approxPolyDP(c, eps, True).reshape(-1, 2)
        if len(approx) >= 3:
            poly = approx
        else:
            poly = np.array([[x, y], [x + bw, y], [x + bw, y + bh], [x, y + bh]])
        poly_full = [(int(round(px / scale)), int(round(py / scale))) for px, py in poly]
        
        idx = len(out)
        lab_to_idx[lab] = idx
        out.append({"polygon_px": poly_full,
                    "type_hint": _classify(x, y, bw, bh, area, W, H),
                    "adjacent_to": []})

    if doorways is not None:
        n_doors, door_labels = cv2.connectedComponents(doorways)
        # Dilate slightly enough to cross the watershed boundary (which is 1px wide usually, but walls might be thicker)
        k = max(5, int(min(H, W) * 0.015)) | 1
        for i in range(1, n_doors):
            door_mask = np.uint8(door_labels == i) * 255
            door_mask = cv2.dilate(door_mask, np.ones((k, k), np.uint8))
            touched = np.unique(markers[door_mask > 0])
            valid_touched = [t for t in touched if t in lab_to_idx]
            for r1 in valid_touched:
                for r2 in valid_touched:
                    if r1 != r2:
                        idx1 = lab_to_idx[r1]
                        idx2 = lab_to_idx[r2]
                        if idx2 not in out[idx1]["adjacent_to"]:
                            out[idx1]["adjacent_to"].append(idx2)
    return out


def _classify(x, y, bw, bh, area, W, H):
    cx, cy = x + bw / 2, y + bh / 2
    aspect = max(bw, bh) / max(1.0, min(bw, bh))
    frac = area / float(W * H)
    perimeter = x < 0.04 * W or y < 0.04 * H or x + bw > 0.96 * W or y + bh > 0.96 * H
    central = 0.30 * W < cx < 0.70 * W and 0.30 * H < cy < 0.70 * H
    if aspect > 3.5:
        return "corridor"
    if frac < 0.012 and central:
        return "server-room"          # small, deep-interior, no facade -> IT/server
    if frac < 0.010 and not perimeter:
        return "mechanical"
    if frac < 0.020:
        return "conference"
    if frac > 0.10 and central:
        return "lobby"
    return "office"


# --------------------------------------------------------------------------- CLI smoke test
if __name__ == "__main__":
    import sys
    from collections import Counter
    args = [a for a in sys.argv[1:] if not a.startswith("--")]
    flags = [a for a in sys.argv[1:] if a.startswith("--")]
    path = args[0] if args else "deepfloorplan/real_floorplan.png"
    engine = next((f.split("=", 1)[1] for f in flags if f.startswith("--engine=")), None)
    im = cv2.imread(path)
    if im is None:
        raise SystemExit(f"could not read image: {path}")
    rs = rooms(im, engine=engine)
    print(f"{path}: engine={engine or os.environ.get('ECON_DF_ENGINE', 'classical')}  "
          f"{len(rs)} rooms  types={dict(Counter(r['type_hint'] for r in rs))}")
    for r in rs[:8]:
        print("  ", r["type_hint"], "poly_pts", len(r["polygon_px"]))
