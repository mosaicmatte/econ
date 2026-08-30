# Round 2 Adversarial Reviewer Report

**Reviewer Role:** reviewer@swe_light & qa@swe_light  
**Working Directory:** `/Users/nguyenhoangkhoi/Documents/econ/.agents/reviewer_r2`  
**Target:** Round 1 Implementation, UI Rendering Bugs, Domestic Home Toggle & Multi-Model Lifecycle  
**Status:** Verification Complete & Defects Resolved  

---

## 1. What the Prior Attempt Got Wrong

### 1.1 P&ID Engineering Topology Cross-Building Zone Leakage on Level 1
- **Input:** User switched building model to "1-Level Domestic Home" or viewed Tower on Level 1, then viewed the 2D P&ID Topology panel (`ReactFlow`).
- **Expected:** When viewing Domestic Home (Level 1), exactly 5 terminal unit cards (`zone-kitchen-rear-service-lvl1`, `zone-office-lvl1`, `zone-living-room-lvl1`, `zone-passage-lvl1`, `zone-bathroom-lvl1`) and 1 AHU node render. When viewing Tower on Level 1, exactly 90 tower terminal units render.
- **Actual:** Both views rendered 95 terminal units (merging all 90 tower Level 1 zones and all 5 domestic home Level 1 zones together).
- **Root Cause:** `buildTopologyFromSim(simState, activeFloor, ontology)` in `dashboard/src/App.jsx` filtered `simState.zones` solely by `z.level === activeFloor`. Since `simState.zones` contains all 131 zones across all known buildings, all Level 1 zones from both fixtures were included unconditionally.

### 1.2 Duplicate Load Forecast Chart on Mobile Screen (`MobileAIScreen.jsx`)
- **Input:** User opened the mobile screen view (`MobileAIScreen.jsx`).
- **Expected:** Dedicated top visual forecast card displays the primary interactive sparkline chart and uncertainty band, while the "AI Load Forecast" recommendation card below provides complementary model architecture diagnostics without duplicating the identical chart.
- **Actual:** The recommendation card rendered `<ForecastChart>` a second time directly below the dedicated top chart.
- **Root Cause:** In `MobileAIScreen.jsx` line 532, `RecCard` rendered `<ForecastChart ... />` whenever `fc` existed, mirroring the original desktop bug where duplicate charts cluttered the view.

### 1.3 FloorPlate Geometry Cache Collisions across Dynamic Model Switches
- **Input:** User toggled repeatedly between Multi-Level Building and 1-Level Domestic Home models.
- **Expected:** CSG and extruded plate geometries re-evaluate relative to each building's active origin and footprint.
- **Actual:** Geometry cache signatures in `BuildingModel.jsx` did not include `ORIGIN.x` and `ORIGIN.y`, creating potential cache key collisions if footprint polygon coordinates had matching relative coordinates.
- **Root Cause:** Cache signature in `FloorPlate` was keyed only on `ext`, `core`, and `thick`, omitting dynamic origin offsets.

---

## 2. What I Changed

1. **`dashboard/src/App.jsx`**:
   - Updated `buildTopologyFromSim(simState, activeFloor, ontology, building = getBuilding())` to derive `floorZoneIds` from the active building fixture's floor.
   - Filtered `activeZones` by `floorZoneIds.has(z.id)`, guaranteeing strictly 5 terminal units for Domestic Home and 90 terminal units for Tower Level 1.
   - Updated `useEffect` to pass `currentBuilding` and depend on `[activeFloor, ontology, currentBuilding]`.

2. **`dashboard/src/MobileAIScreen.jsx`**:
   - Replaced the duplicate `<ForecastChart>` inside `RecCard` with a clean, compact model diagnostics breakdown (active forecaster engine, forecast horizon, and peak load projection), matching the desktop deduplication strategy.

3. **`dashboard/src/BuildingModel.jsx`**:
   - Updated `FloorPlate` cached geometry signature to include dynamic `ORIGIN.x` and `ORIGIN.y`, ensuring clean geometry cache separation across model switches.

4. **`dashboard/verify_ui_rendering.js`**:
   - Added automated tests verifying:
     - P&ID Topology zone set isolation and mutual exclusivity (5 zones for Home, 90 zones for Tower, 0 collisions).
     - FloorPlate dynamic origin and geometry cache separation.
     - MobileAIScreen forecast chart deduplication.

---

## 3. Verification Record

- **Deep Verification (Ran Actual Tests):**
  - `node verify_ui_rendering.js`: **12 / 12 passed** (100% pass rate in 4.11s).
  - `node verify_adversarial_ui.js`: **7 / 7 passed** (100% pass rate in 5.05s).
  - `npm test` (`node verify_ai_actions.js`): **20 / 20 passed** (100% pass rate in 6.35s).
  - `npm run build`: Production bundle compiled cleanly (Vite v5.4.21, 0 errors, 4.34s).
  - `go test -v -count=1 ./...` (in `server/`): All Go tests passed (simulation, MQTT, forecast, and recommend APIs).
- **Shallow Verification (Manual / DOM Inspection):**
  - Inspected left dock tab bar rendering across 380px, 300px, and 260px widths (zero truncation of "PLUGS").
  - Inspected 3D model toggle (`data-testid="building-model-toggle"`) state sync.
  - Inspected orthogonal duct routing and ambient lighting parameters.
- **Unverified Aspects:**
  - Real hardware Float32 WebGL extensions on legacy mobile Safari browsers without modern ES2022 support.

---

## 4. Known Issues

- `Minor Robustness Risk`: Extremely narrow viewports (<240px) could show horizontal scrolling if user custom OS font has an unusually large character width.
- `Minor Robustness Risk`: Live WebSocket connections delivering unknown dynamic zone IDs outside known fixtures will fall back to nominal telemetry defaults.

---

## 5. Remaining Risk & Next Step

All functional requirements from `<original_task>` are fully implemented, bug-fixed, deduplicated, and validated across 3 test suites totaling 39 automated tests (100% pass rate). All tasks are complete.
