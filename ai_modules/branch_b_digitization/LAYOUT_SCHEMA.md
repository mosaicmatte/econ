# Detailed Floor/Room Layout — schema & DeepFloorplan ingestion

This is the **target layout** the whole ECON system is built on: the Go physics engine and the
React 3D twin both consume `building-data.json` in exactly this shape. Branch B (digitization)
turns a real 2D floorplan into this schema so the building is driven by a true blueprint instead
of the procedural `generate_bim.js`.

Pipeline: **floorplan image → DeepFloorplan/OpenCV room segmentation → this schema.**
Bridge script: `floorplan_to_buildingdata.py` (verified: a real floorplan → 15 floors / 210
zones, all fields valid, drop-in for `econ/server/data/building-data.json`).

---

## 1. Coordinate convention
- A floor is a flat plan in **metres**. Building footprint is `W × D` (default **60 × 40 m**).
- Polygon points are `[x, y]`, `x ∈ [0, W]`, `y ∈ [0, D]`, origin top-left (matches image pixels;
  the digitizer scales `px → metric` by `x = col/imgW·W`, `y = row/imgH·D`).
- The 3D model extrudes each floor on `+elevation` (metres); `elevation = (level-1)·height`.

## 2. Schema
```jsonc
{
  "buildingId": "bldg-econ-digitized",
  "floors": [
    {
      "level": 1,                          // 1-based
      "elevation": 0,                      // metres from ground
      "height": 4.0,                       // floor-to-floor (m)
      "name": "Level 1",
      "geometry": {
        "exteriorPolygon": [[0,0],[60,0],[60,40],[0,40]],   // floor-plate outline
        "corePolygon":     [[..]],          // central core (elevator/stairs) hole; optional
        "wallThickness": 0.3
      },
      "zones": [
        {
          "zoneId": "zone-office-2-lvl1",   // UNIQUE across the whole building
          "name": "Office 2 Level 1",
          "zoneType": "office",             // see archetypes below
          "bim_asset_id": "<uuid v4>",      // stable per-asset id (BIM linkage)
          "polygon": [[x,y],[x,y],...],     // room outline, metric, CW or CCW
          "centroid": { "x": .., "y": .. },
          "thermalProperties": {            // feeds the Go 2R1C physics
            "setpoint": 22.0, "deadband": 2.0,
            "baseHeatLoad": 8000,           // W of internal gain (equipment/lighting)
            "solarGainMultiplier": 1.0,     // perimeter rooms > 0, interior = 0
            "rWall": 0.2,                   // envelope thermal resistance
            "cAir": 968000                  // air thermal capacitance = area·height·1210
          },
          "hvacMapping": { "vavId": "vav-office-2-lvl1" }   // the VAV that serves this zone
        }
      ]
    }
  ]
}
```
**Invariants** (don't break — the engine/frontend rely on them):
- `zoneId` and `vavId` are unique building-wide.
- Every zone has a full `thermalProperties` block and an `hvacMapping.vavId`.
- `polygon` has ≥3 points; `centroid` is inside it.
- `corePolygon` (if present) is what the 3D floor-plate is cut around.

## 3. Zone archetypes (room type → thermal personality)
The digitizer attaches these from the detected room type (table in `floorplan_to_buildingdata.py`):

| zoneType | setpoint | deadband | baseHeatLoad (W) | notes |
|---|---|---|---|---|
| office | 22 | 2 | 8 000 | solar=1 if perimeter |
| conference | 22 | 2 | 6 000 | |
| corridor | 24 | 3 | 2 000 | also used as the building core |
| lobby | 24 | 2 | 5 000 | |
| server-room | 18 | 1 | 85 000 | high load, no solar — the fault hotspot |
| mechanical | 26 | 3 | 12 000 | |
`cAir` is computed per room: `area_m² · height · 1210`. `solarGainMultiplier` is 1.0 only when the
room touches the building perimeter (and isn't a corridor/server room).

## 4. Using DeepFloorplan to fill the layout
`floorplan_to_buildingdata.py` calls `segment_rooms()`:
1. **DeepFloorplan** (`segment_rooms_deepfloorplan`) — preferred. It's an adapter stub: drop in a
   `deepfloorplan_infer.rooms(img)` that runs the model (use the TF2 port
   [`zcemycl/TF2DeepFloorplan`](https://github.com/zcemycl/TF2DeepFloorplan)) and returns
   `[{"polygon_px": [(x,y)...], "type_hint": "<room class>"}]`. The `type_hint` is mapped to a
   `zoneType`, giving far better classification than geometry alone.
2. **OpenCV fallback** (`segment_rooms_opencv`) — works with no model: threshold walls → close door
   gaps → interior contours → bounding-rectangle room polygons. This is what runs today.

Coordinates are normalized to the footprint, rooms are classified (DeepFloorplan label, else
geometry heuristics: aspect→corridor, small+central→server, central→core, small→conference,
else office), thermalProperties + hvacMapping + centroid + `bim_asset_id` are attached, and the
floor is stamped across `--floors` levels.

### Run it
```bash
cd econ/ai_modules/branch_b_digitization
python floorplan_to_buildingdata.py \
  --image deepfloorplan/real_floorplan.png \
  --out  /tmp/building-data.json \
  --floors 15 --footprint 60x40 --debug /tmp/annotated.png
```

### Deploy a digitized building
```bash
cp /tmp/building-data.json econ/server/data/building-data.json
cp /tmp/building-data.json econ/dashboard/src/building-data.json   # dashboard reads its own copy
cd econ/server && docker compose up -d --build server              # image bakes data at build time
```
Then confirm in the preview that the 3D model, topology, and physics render (same schema → no code
changes). For the React-Flow **systems map**, also regenerate `data/brick-ontology.json` from the
digitized adjacency graph (door detection → `brick:feeds`) — see `geometry_merge.py` /
`topologic_graph.json`; this is the remaining Branch B step (tracked in `econ/CONTINUE_HERE.md`).
