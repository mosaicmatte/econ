# Attention-floor camera + layout-constrained airflow

This documents the two features added on top of the 3D twin, the data contract that ties
them to the DeepFloorplan pipeline, and a prioritized list of how the next agent should
extend them. Read `econ/CONTINUE_HERE.md` first for whole-project context.

## TL;DR of what changed
1. **Camera** — the building sits at a **fixed, locked** front-left 3/4 hero angle (rotation
   disabled, zoom kept), framed so the *whole exploded tower* fits **top-to-bottom at any
   viewport shape** (desktop wide + mobile portrait), centred on the floor that "needs
   attention". Injecting a fault flies the camera to that floor. Same framing is used by the
   mobile hero (`MobileApp` already mounts `BuildingModel`).
2. **Airflow** — the airflow window is now a real, layout-constrained flow: a masked
   potential-flow solve over the floor so air streams out of diffusers, bends **through
   doorways**, converges on the **returns**, and leaks at **windows** — never crossing a wall.
   It draws the layout it respects (walls, windows, HVAC diffusers/returns, occupants,
   electrical bus) in the existing Manim arrow-field + tracer-stream style.
3. **DeepFloorplan bridge** now emits an `airflowDomain` (real doors + windows) so a digitized
   plan drives the airflow directly; the frontend derives the same domain geometrically when
   it's absent (e.g. the procedural testbed).

## Files
| File | Role |
|---|---|
| `src/flowfield.js` | Pure **2D** solver: floor + live sim → walls/doors/windows/diffusers/returns/occupants/electrical, then a masked **potential-flow** solve → velocity field + sampler + render primitives. No React/three. Still the source of feature POSITIONS for the 3D solve and the in-model infrastructure. |
| `src/flowfield3d.js` | Pure **3D (volumetric)** solver. Reuses `flowfield.js` for layout-derived feature positions, then builds + solves an anisotropic 7-point potential flow over a voxel grid: ceiling supply, **low** returns, mid-height window relief, full-height walls. Exposes a 3D `sample(x,y,z)` + 3D arrows. |
| `src/ConstrainedAirflow3D.jsx` | R3F rendering of the **3D** field: full-height translucent walls + edges, window panes, ceiling diffusers with throw cones, low return grilles, standing occupants, electrical bus, 3D heat-coloured arrows + tracer particles advecting through the volume. |
| `src/ConstrainedAirflow.jsx` | The original **2D** plan renderer. Superseded by the 3D one in the airflow window; kept as a flat-view fallback (not currently mounted). |
| `src/AirflowWindow.jsx` | Hosts `ConstrainedAirflow3D` with a full-orbit perspective camera, layer-toggle chips, speed legend. |
| `src/FloorInfrastructure.jsx` | The active floor's physical services **in the main 3D model**: AHU + supply ducts + diffusers + returns (HVAC), panel + ceiling cable trays + junction boxes (electrical grid), per-zone camera/thermostat/CO₂ (sensors), and live occupants (people). Driven by the same `flowfield.js` domain so it matches the airflow window. |
| `src/BuildingModel.jsx` | `towerFraming(activeFloor)` + `DynamicControls` — the whole-tower overview camera centred on the attention floor. Mounts `FloorInfrastructure` on the active floor (replaced the crude `InfrastructureLayer`). |
| `src/App.jsx` | `ATTENTION_FLOOR` (data-driven initial floor) + fault-inject camera jump + airflow window sizing. |
| `ai_modules/branch_b_digitization/floorplan_to_buildingdata.py` | Emits `floor.airflowDomain = { doors, windows }` from detected adjacency + the envelope. |

## 3D airflow (flowfield3d.js)
The airflow window is now volumetric. Over a voxel grid (≈60×40×8, anisotropic `hx=hz`, finer
`hy`), it solves the same masked potential flow but in 3D with **HVAC manipulating it**:
- **Supply** injected at the **ceiling** (`k = nk-1`) per diffuser, strength ∝ live VAV flow (×2.6 on alarm).
- **Returns** pulled **low** (`k = 0`) across the corridor/core → air drops from the ceiling
  diffuser, spreads across the room, and is drawn back down to the returns (verified: vy is
  negative below a diffuser, tapering toward the floor).
- **Windows** relieve at mid-height; **walls** are full-height no-flux; doors are full-height openings.
- Solve is ~90 ms, memoized on `flowKey`. `sample(x,y,z)` is trilinear; tracer particles advect in 3D.

## Fixed camera angle (BuildingModel.jsx)
`towerFraming(activeFloor, aspect)` builds the shot from a fixed `VIEW_AZ=45°` / `VIEW_EL=26°`
direction and an **aspect-aware** distance (fits the exploded tower's bounding sphere into
whichever of the vertical/horizontal fov is tighter), so portrait/mobile and wide/desktop both
show the whole building. `DynamicControls` recomputes it on `activeFloor` **and** viewport-shape
change, and the `OrbitControls` have `enableRotate={false}` + `enablePan={false}` (zoom kept) so
the angle stays locked. Drill-down on zone-select still works because the camera position/target
are driven directly.

## Performance (no elements removed)
The dashboard runs two `frameloop="always"` WebGL canvases (building + 3D airflow) plus a dense
services layer, so the heavy items were optimised in place:
- **DPR capped to `[1, 1.5]`** on both canvases — the biggest win on retina/mobile (≈2× → 1.5×
  fewer fragments) for the transparent, overdraw-heavy scene.
- **`FloorInfrastructure` rewritten**: ~70 lit `meshStandardMaterial` meshes → ~8 **unlit**
  `meshBasicMaterial` meshes by **merging** the static duct/conduit/fixture runs per colour and
  **instancing** sensors + people. Same elements, far fewer draw calls + no per-fragment lighting.
- **Inactive-zone per-frame gating**: only the active floor + alarmed zones run the smooth
  temperature lerp / alert pulse each frame; the ~110 faded inactive zones snap their uniform
  only when the live temp actually moves (≥0.05 °C). Cuts the 126-zone/60 fps loop to ~16 active.
- (Headless preview throttles `requestAnimationFrame` offscreen, so FPS can't be numerically
  sampled here — these are structural reductions in fragment load, draw calls, and CPU/frame.)

## In-model infrastructure (FloorInfrastructure.jsx)
Rendered on the **active floor** of the main building, in the floor-local frame `(px-20, y, 20-py)`:
HVAC duct star from the core AHU to each ceiling diffuser + low return grilles; an electrical
panel with ceiling cable trays + junction boxes to each zone; per-zone ceiling camera + wall
thermostat + CO₂ monitor; and instanced occupant capsules sized to live per-zone occupancy.
Opacity follows the view mode (PHYSICAL = opaque/clearest, HYBRID = blended, LOGICAL = faint/wireframe).

## Coordinate frame (must stay consistent)
A floorplan point `(px, py)` in `building-data.json` (metres, origin top-left, `x∈[0,W]`,
`y∈[0,D]`) renders in world/solver-local space as **`(x, z) = (px - 20, 20 - py)`** — the same
transform the 3D zones use. `airflowDomain.doors`/`windows` are stored in the **px/py metric
frame** (same as zone `polygon`s); `flowfield.js` converts them on ingest. Tangent `(tx, tz)`
for a door is in the *local* frame: a vertical wall (constant x) → `(0,1)`, horizontal → `(1,0)`.

## `airflowDomain` data contract
Optional per-floor block. When present the solver uses it; when absent it derives the
equivalent from polygons (shared-edge doors, envelope windows).
```jsonc
"airflowDomain": {
  "doors":   [ { "x": 20.0, "y": 7.5, "tx": 0, "tz": 1 } ],  // opening centre (metric) + tangent
  "windows": [ { "x": 0.0,  "y": 8.0 } ]                      // perimeter relief centre (metric)
}
```
- A door carves a ~1.8 m OPEN slit in the wall along its tangent → air may pass there.
- A window becomes a small relief sink on the nearest interior cell (≈30 % of supply; returns
  take ≈70 %). Supply ⇄ sink totals are balanced so the pure-Neumann Poisson solve is solvable.

## How the solve works (`buildFlowField`)
1. Rasterize the footprint into a grid (~96 cells on the long axis). Cells inside a room =
   `OPEN`+zone; outside the envelope / unmodelled gaps = `SOLID`.
2. **Walls**: a cell flips to `WALL` where it borders a different zone or the exterior (works
   for tiled testbed plans *and* gappy real plans).
3. **Doors**: provided `airflowDomain.doors`, else shared-edge detection; each carves a slit.
4. **Sources/sinks**: one supply diffuser per VAV at the zone centroid, strength ∝ live VAV
   flow (×2.6 when the zone is in alarm — the engine ramps it); returns in the corridor/core;
   windows as perimeter relief. Net source ≈ 0.
5. **Solve** `∇²φ = S` with Gauss–Seidel; walls are excluded from each cell's stencil → exact
   no-flux (`∂φ/∂n = 0`) boundary. Velocity `v = ∇φ` on open faces.
6. Expose a bilinear `sample(x,z)` (for tracer advection) + `arrows` (coarse grid) + render
   primitives. The whole thing is memoized on a `flowKey` (rounded VAV flow / bucketed
   occupancy / alarm flips) so the solve only re-runs when the field meaningfully changes.

## Verified
- Headless solve on testbed L6: 96×64 grid, walls + 12 doors carved, 8 diffusers, 6 returns,
  32 windows, sampler returns flow at diffusers and convergence (~0) at the corridor.
- Provided-`airflowDomain` path: 4 doors → 12 door cells, 5 windows consumed, valid field.
- Bridge unit test: adjacent rooms → correct door midpoints/tangents + 32 envelope windows.
- Preview (1440×900): no console errors; arrows heat-coloured by speed; camera frames the full
  exploded tower with the L6 server-room fault as the focal floor.
- Arrow colour gotcha (fixed): per-instance colour MUST be an `instancedBufferAttribute`
  attached at construction, or the material compiles without `USE_INSTANCING_COLOR` and arrows
  render white. Don't regress this.

---

## Further build instructions (for the next agent), in priority order

1. **Thermal ⇄ flow coupling (true convection).** The flow is now volumetric (ceiling supply →
   low return), but it is still potential flow — no buoyancy, no jet momentum. Make occupants +
   equipment **heat sources** that buoy air (warm air rises) and bias the field; drive the
   diffuser throw as a real downward jet. Upgrade the Poisson solve to a small advection–diffusion
   / Stable-Fluids step (Jos Stam) on the 3D masked grid; run it in a Web Worker so larger grids
   stay 60 fps. The 3D grid is coarse (≈60×40×8) for main-thread budget — a worker lets it grow.
2. **Real occupants instead of synthetic dots.** `econ/edge/.../yolo_tracker.py` already counts
   people per zone via ByteTrack. Stream per-zone *positions* (not just counts) over MQTT/WS and
   feed `field.occupants` from them so "humans" are the actual tracked people.
3. **Real HVAC topology.** Pull diffuser/return positions and the AHU→VAV→zone tree from the
   Brick ontology (`/api/ontology`, `brick:feeds`) instead of "diffuser = centroid, return =
   corridor". Multiple diffusers/returns per large zone; return location from the real grille.
4. **Neural doors/windows into `airflowDomain`.** `deepfloorplan_infer.py` references a
   door/window Mask2Former (`Hyunwoo1605/mask2former-floorplan-instance-segmentation`). Wire it
   so detected door + window instances populate `airflowDomain.doors/windows` with true
   positions, replacing the adjacency-midpoint heuristic. (Also fixes the known bad classical
   polygons — see CONTINUE_HERE Branch B warnings.)
5. **Electrical grid from real data.** The `POWER` layer is a placeholder radial bus from a core
   panel. Drive it from real panel/circuit/breaker→zone data (the topology already models
   `panel`/`circuit` nodes) and colour by live per-zone load.
6. **Section / elevation camera mode.** Add a toggle for a true orthographic front elevation
   ("top to bottom" with no perspective) and a cut-away section through the attention floor, in
   addition to the current 3/4 overview. `towerFraming()` is the place to add framing presets.
7. **Reactive attention floor.** Right now the open shot uses `ATTENTION_FLOOR` (the default
   critical asset's floor) and only re-aims on explicit fault inject. Optionally auto-surface a
   floor when a *live* alarm appears on it (debounced, and never while the user is mid-drilldown).
8. **Optional: airflow inside the main building canvas.** A per-floor flow ribbon on the active
   floor in the main 3D view (it was removed earlier for the 0-size-canvas bug; reintroduce
   guarded by `CanvasErrorBoundary` + the resize-pulse fix).
9. **Validation.** Tune door width, supply/return split, and grid resolution against measured
   airflow or a CFD reference before trusting the numbers quantitatively — today it is
   physically *plausible and layout-correct*, not calibrated.
