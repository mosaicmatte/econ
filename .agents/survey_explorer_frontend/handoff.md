# Handoff Report — Frontend Dashboard & BIM Context Switching Survey

## 1. Observation
- **Live Ingestion Architecture (`dashboard/src/useDigitalTwin.js`)**:
  - Binary FlatBuffers telemetry stream unpacked at lines 249–328, directly populating `newSimData.zones[id]` (`temp`, `load`, `occupants`, `lightsOn`, `humidity`, `co2`, `plugW`, `plugShed`, `supplyC`, `supplyReal`), `vavs[id].flow`, and `global` metrics (`buildingLoadMw`, `systemHealth`, `totalOccupants`, `coolingOutputMw`, `plantCop`, `energySavedMw`, `bessDischargeMw`, `bessSocPct`, `avgCo2`, `plugKw`, `zonesInSetback`, `autoPilot`, `ahuPressurePa`).
  - Liveness detection implemented at lines 119–125 (`streamOpen`, `streamAgeMs`).
  - Pre-stream seeding in `getInitialSimData()` (lines 27–68) iterates over `getAllKnownBuildings()`.
- **BIM Context State (`dashboard/src/buildingStore.js`)**:
  - `activeModelType` toggles between `'multi-level'` (`building-data.json`, 15 floors, ~39,776 m²) and `'domestic-home'` (`building-data-home.json`, 1 floor, 5 zones, ~72 m²) via `setBuildingModelType(type)` (lines 37–45).
  - Subscribers registered via `subscribeBuildingChange(fn)` are notified with `(getBuilding(), activeModelType)`.
- **UI Toggle Elements (`dashboard/src/App.jsx`)**:
  - Lines 1159–1197: `<button data-testid="toggle-multilevel" onClick={() => handleToggleBuildingModel('multi-level')}>` and `<button data-testid="toggle-domestic-home" onClick={() => handleToggleBuildingModel('domestic-home')}>`.
  - Lines 1201–1230: Floating desktop level stepper (`data-testid="desktop-level-toggle"`, `data-testid="level-step-prev"`, `data-testid="level-step-next"`).
  - Lines 216–307: `buildTopologyFromSim(simState, activeFloor, ontology, building = getBuilding())` generates AHU and VAV nodes dynamically based on active building floor zones.
- **3D Framing & Camera (`dashboard/src/BuildingModel.jsx` & `dashboard/src/floorGeometry.js`)**:
  - `BuildingModel.jsx` lines 690–728: Subscribes to building changes and recalculates camera positioning using `towerFraming(activeFloor, aspect, safeArea, currentBld)`.
  - `floorGeometry.js` lines 63–111: `FOOTPRINT` and `ORIGIN` getters dynamically query `getBuilding()`, centering scene coordinates on `(0,0)`.
- **Telemetry & Metrics Panels (`dashboard/src/GlobalMetricsPanel.jsx` & `dashboard/src/TelemetryPanel.jsx`)**:
  - `GlobalMetricsPanel.jsx` lines 181–229: Computes `levelZones`, `levelMetrics`, and `availableFloors` based on `currentBld`.
  - `TelemetryPanel.jsx` lines 205–284: Switches between `ZoneProfileRows` (when $\le 24$ zones) and `ScatterChart` (when $> 24$ zones).
- **Static Scope Evaluation (`dashboard/src/sustainability.js`)**:
  - Lines 10–29: `FLOOR_AREA_M2`, `ZONE_MIX`, and `IS_IT_DOMINATED` evaluate at module import time against initial `getBuilding()`.
- **Existing Test Harnesses (`dashboard/package.json`, `verify_level_toggle.js`, `verify_ai_actions.js`)**:
  - Puppeteer is configured under `"dependencies": { "puppeteer": "^25.1.0" }`.
  - `verify_level_toggle.js` and `verify_ai_actions.js` implement standalone `TestHarness` with embedded HTTP static server and Puppeteer headless browser assertions.

## 2. Logic Chain
1. **Live Data Integration**: Observations in `useDigitalTwin.js` demonstrate that real hardware telemetry and simulation physics are streamed over binary FlatBuffers WebSockets without fabricated mock data. Domain constants in `tariff.js` (EVN TOU rates) and `sustainability.js` (ICEC 2021 benchmarks) reflect standard regulatory/literature figures.
2. **BIM Context Switching**:
   - `buildingStore.js` cleanly isolates `'multi-level'` and `'domestic-home'` models.
   - When `setBuildingModelType` is invoked, `subscribeBuildingChange` propagates the new model object across React components (`App.jsx`, `BuildingModel.jsx`, `GlobalMetricsPanel.jsx`, `MobileApp.jsx`).
   - 3D rendering recalculates bounding boxes and camera framing via `towerFraming` and `getFootprint()`.
   - Level controls clamp between L1–L15 (Office) and L1 (Home).
   - Topology dynamically rebuilds 91 nodes vs 6 nodes.
   - Telemetry profiling dynamically switches between 2D quadrant scatter and per-room bullet rows.
3. **Implementation Refinement Need**: Because `sustainability.js` executes `FLOOR_AREA_M2 = ZONES.reduce(...)` at module evaluation, runtime BIM toggling does not update `FLOOR_AREA_M2` in `sustainability.js` unless wrapped in a dynamic getter or function.
4. **Test Harness Feasibility (`verify_bim_switching.js`)**: Existing test scripts (`verify_level_toggle.js`, `verify_ai_actions.js`) provide a proven blueprint for implementing `verify_bim_switching.js` with Puppeteer. The proposed 5-suite design directly asserts all state and DOM invariants specified in Acceptance Criterion 2.

## 3. Caveats
- The Go backend physics engine state when pointed at the house fixture vs the office fixture was surveyed via existing tests (`forecast_plausibility_test.go`, `bess_sizing_test.go`); backend implementation details will be handled by the backend explorer/implementer.
- No source code modifications were made during this investigation, in strict compliance with the read-only explorer constraint.

## 4. Conclusion
The frontend dashboard codebase has established clean primitives for live data streaming and BIM model toggling. To achieve 100% compliance with R1, R3, and Acceptance Criterion 2:
1. Ensure `sustainability.js` recalculates `FLOOR_AREA_M2` and `ZONE_MIX` dynamically on building change.
2. Implement `dashboard/verify_bim_switching.js` using the 5-suite design specified in `report.md`, asserting store invariants, DOM toggle button interactions, level button list mutation, topology node reconstruction, per-level telemetry aggregation, and mobile stepper clamping.

## 5. Verification Method
1. Inspect the survey report at `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_frontend/report.md`.
2. Inspect `dashboard/src/buildingStore.js`, `dashboard/src/App.jsx`, `dashboard/src/BuildingModel.jsx`, and `dashboard/src/GlobalMetricsPanel.jsx` to verify the observed line numbers and component structure.
3. Build the dashboard bundle:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard && npm run build
   ```
4. Run existing verification harnesses:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard && node verify_level_toggle.js
   ```
