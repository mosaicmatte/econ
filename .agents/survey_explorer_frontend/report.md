# Comprehensive Frontend Dashboard & BIM Context Switching Survey Report

> **Workspace**: `/Users/nguyenhoangkhoi/Documents/econ/dashboard`  
> **Author**: `survey_explorer_frontend`  
> **Date**: August 2026  
> **Target Requirements**: ORIGINAL_REQUEST.md (lines 21–45) — R1 (Live Data Integration), R3 (BIM Context Switching), and Acceptance Criterion 2 (`verify_bim_switching.js`).

---

## Executive Summary

A comprehensive architectural and code-level survey of the ECON frontend dashboard (`dashboard/`) was conducted. The dashboard is a high-performance digital twin visualization console built with **React 18**, **Three.js / React Three Fiber**, **Three-BVH-CSG**, **React Flow (@xyflow/react)**, and **Recharts**, streaming real-time building telemetry over binary FlatBuffers WebSockets.

### Key Survey Findings:
1. **Live Data Integration (R1)**:
   - Live telemetry ingestion is fully wired via `useDigitalTwin.js` over a binary FlatBuffers WebSocket stream (`SimState`), consuming live per-zone thermal, electrical, airflow, and occupancy states, as well as global chiller COP, BESS SoC, and building load.
   - Initial state seeding in `getInitialSimData()` provides structured fallbacks prior to first packet receipt across all known models (`getAllKnownBuildings()`).
   - Domain constants (e.g. EVN Time-of-Use tariff rates in `tariff.js` and ICEC 2021 cohort benchmarks in `sustainability.js`) are fixed regulatory/literature baselines rather than mock sensor data.
   - Identified area for enhancement: In `sustainability.js`, module-level constants `FLOOR_AREA_M2`, `ZONE_MIX`, and `IS_IT_DOMINATED` evaluate once at boot time against `getBuilding()`; when dynamically switching BIM models at runtime, these should evaluate dynamically against the active building.

2. **BIM Context Switching (R3)**:
   - Centralized in `buildingStore.js`, managing two distinct Building Information Models:
     - **Multi-Level Commercial Office Building** (`building-data.json`): 15 levels, 90+ zones per typical floor, central AHU/VAV network, ~39,776 m² conditioned floor area.
     - **1-Level Domestic House** (`building-data-home.json`): 1 floor (Level 1), 5 residential zones (Kitchen/service, Office, Living room, Passage, Bathroom), ~72 m² floor area.
   - The UI includes floating toggle controls (`data-testid="toggle-multilevel"` and `data-testid="toggle-domestic-home"`) in `App.jsx`.
   - Dynamic synchronization is implemented across:
     - **3D Canvas & Camera (`BuildingModel.jsx`)**: Subscribes to model changes, recalculates bounding sphere and camera framing (`towerFraming`), and reconstructs CSG walls and zone floorplates.
     - **Floor Level Navigation (`GlobalMetricsPanel.jsx`, desktop/mobile steppers)**: Adjusts floor buttons from 15 floors (L1–L15) down to 1 floor (L1) and clamps navigation.
     - **Zone Telemetry Aggregation (`GlobalMetricsPanel.jsx`)**: Dynamically filters `levelZones` and aggregates kW load, occupancy, average temperature, and alarms.
     - **Topology & P&ID Schematic (`App.jsx` `buildTopologyFromSim`)**: Riser grid and VAV terminal units dynamically rebuild for the active floor's zones.
     - **Airflow Volumetric Simulation (`AirflowWindow.jsx`, `ConstrainedAirflow3D.jsx`)**: Bounds, exterior polygon, and origin dynamically adapt.
     - **Telemetry Profiler (`TelemetryPanel.jsx`)**: Automatically transitions between population-level 2D ScatterChart (for >24 zones) and per-room bullet rows (`ZoneProfileRows.jsx` for ≤24 zones).

3. **Acceptance Criterion 2 Design (`dashboard/verify_bim_switching.js`)**:
   - Designed a comprehensive end-to-end Puppeteer/Node test script structured identically to existing project verification harnesses (`verify_level_toggle.js`, `verify_ai_actions.js`).
   - Defined 5 test suites covering store invariants, DOM toggle interaction, live telemetry aggregation, model reversion & rapid switching stress testing, and mobile responsiveness.

---

## 1. R1: Live Data Integration Audit

### 1.1 Live Ingestion Architecture
- **WebSocket Binary Stream (`useDigitalTwin.js`)**:
  - Connects to `WS_URL` with binary FlatBuffers protocol (`flatbuffers.ByteBuffer`).
  - Unpacks root `SimState` containing:
    - `zones(i)`: `temp`, `load`, `occupants`, `lightsOn`, `humidity`, `co2`, `plugW`, `plugShed`, `supplyC`, `supplyReal`.
    - `vavs(i)`: `airflow` flow rate per terminal unit.
    - `global()`: `buildingLoadMw`, `systemHealth`, `totalOccupants`, `coolingOutputMw`, `plantCop`, `energySavedMw`, `bessDischargeMw`, `bessSocPct`, `avgCo2`, `plugKw`, `plugStandbyKw`, `plugShedKw`, `plugSavedKwh`, `zonesInSetback`, `autoPilot`, `ahuPressurePa`.
  - Reconnection resilience: automatically retries with 3-second backoff; exposes `streamOpen` and `streamAgeMs` to detect stale data.

- **REST API Integration Hooks**:
  - `useOpsStatus.js` → polls `/api/ops/precool` and `/api/weather` every 30s.
  - `usePlugs.js` → polls `/api/plugs` for socket sweep and plug policy.
  - `useRecommendations.js` → polls `/api/recommendations` for baseline model σ-score anomaly alerts.
  - `useForecastCompare.js` → polls `/api/forecast/compare` for TimesFM zero-shot and LSTM comparison.
  - `useLibrary.js` → fetches `/api/library` for calibrated plant coefficients, critical zone types, and design supply temperatures.
  - `useRoomModels.js` → fetches `/api/room-models` for identified 2R1C room thermal capacitance ($C_{air}$) and resistance ($R_{wall}$).

### 1.2 Audit of Static Constants vs Mock Data
| File | Values / Logic | Classification | Rationale & Status |
|---|---|---|---|
| `src/tariff.js` | EVN TOU Rates (`NORMAL=2887`, `OFF_PEAK=1609`, `PEAK=5025` VND/kWh) | **Regulatory Standard** | Static by design (Decision 1279/QĐ-BCT & 963/QĐ-BCT). |
| `src/sustainability.js` | Grid EF `0.6766` kg/kWh; EUI Benchmarks (`hcmc=116.4`, `hanoi=105.9`) | **Literature Benchmark** | Published ICEC 2021 cohort survey standards. |
| `src/useDigitalTwin.js` | `getInitialSimData()` (`plantCop=3.2`, `temp=24.0`, `co2=450`) | **Initial State Seeding** | Seeded prior to first WebSocket packet; overwritten upon packet arrival. |
| `src/floorGeometry.js` | `FOOTPRINT` and `ORIGIN` getters | **Dynamic Getter** | Reads dynamically from `getBuilding()`, centering any geometry on `(0,0)`. |
| `src/TelemetryPanel.jsx` | `AIR_RHO = 1.2`, `AIR_CP = 1.005` | **Physical Constant** | Thermodynamic constants for air density and specific heat. |
| `src/LiveWeatherBackground.jsx` | Fallback coordinates (`lat=10.8231, lon=106.6297`) | **Resilience Fallback** | Fallback for HCMC when `/api/weather` is unavailable. |

---

## 2. R3: BIM Context Switching Analysis

### 2.1 Model Store (`buildingStore.js`)
The building store maintains two primary models:
1. `bundledTower` (`src/building-data.json`): Commercial Multi-Level Office Tower.
2. `bundledHome` (`src/building-data-home.json`): 1-Level Domestic Tube House.

```javascript
let activeModelType = 'multi-level'; // 'multi-level' or 'domestic-home'
let towerData = bundledTower;
let homeData = bundledHome;
let live = false;
const listeners = new Set();

export function getBuilding() {
  return activeModelType === 'domestic-home' ? homeData : towerData;
}
export function getBuildingModelType() {
  return activeModelType;
}
export function setBuildingModelType(type) {
  if (type !== 'multi-level' && type !== 'domestic-home') return;
  if (activeModelType !== type) {
    activeModelType = type;
    listeners.forEach((fn) => {
      try { fn(getBuilding(), activeModelType); } catch (e) { console.error(e); }
    });
  }
}
```

### 2.2 Model Switching UI Controls
In `App.jsx` (lines 1137–1198):
- **Container**: `data-testid="building-model-toggle"` floating above the bottom HUD.
- **Buttons**:
  - `data-testid="toggle-multilevel"`: calls `handleToggleBuildingModel('multi-level')`.
  - `data-testid="toggle-domestic-home"`: calls `handleToggleBuildingModel('domestic-home')`.
- **Level Controls**:
  - `data-testid="desktop-level-toggle"` with steppers `data-testid="level-step-prev"` and `data-testid="level-step-next"`.
  - `data-testid="level-toggle"` in `GlobalMetricsPanel` with individual level buttons `data-testid="level-btn-${lvl}"`.

### 2.3 Subsystem Response to Model Toggle

```
                       [User Clicks BIM Toggle]
                                   │
                                   ▼
                   setBuildingModelType(nextType)
                                   │
          ┌────────────────────────┼────────────────────────┐
          ▼                        ▼                        ▼
  BuildingModel.jsx       GlobalMetricsPanel.jsx       App.jsx (Topology)
  - Camera re-framing     - Filters active zones       - Rebuilds riser grid
  - Bounding sphere calc  - Aggregates kW, Pax, °C     - Resets active floor
  - CSG wall generation   - Re-renders L1..Ln buttons  - Terminal unit cards
          │                        │                        │
          ▼                        ▼                        ▼
  AirflowWindow.jsx       TelemetryPanel.jsx           MobileApp.jsx
  - Bounding polygon      - ≤24 zones: ZoneProfileRows - Clamps mobile stepper
  - Centering on ORIGIN   - >24 zones: ScatterChart    - Re-frames 3D viewport
```

#### Detailed Subsystem Interactions:
1. **3D Viewport & Camera Framing (`BuildingModel.jsx`)**:
   - Calls `towerFraming(activeFloor, aspect, safeArea, currentBld)`.
   - Evaluates footprint bounds via `getFootprint(building)`.
   - Computes vertical span: Office tower span ~60m vs House span ~2.8m.
   - Automatically repositions camera distance $dist = \max(R / \sin(fov/2) \cdot 1.45, 14)$ and target vector $(0, y_{mid}, 0)$, ensuring the domestic house is framed without camera clipping.
2. **Zone Hierarchy & Level Buttons (`GlobalMetricsPanel.jsx`)**:
   - `availableFloors` maps over `currentBld.floors`. Office renders 15 buttons (`L1`–`L15`), House renders 1 button (`L1`).
   - `levelZones` and `levelMetrics` calculate load, occupancy, and average temperature filtered strictly by `floor.zones`.
3. **P&ID Schematic & Equipment Schedule (`App.jsx` `buildTopologyFromSim`)**:
   - Filters active zones by active floor and reads Brick ontology relationships (`brick:feeds`).
   - Positions `ahu-main` at top and generates 1:1 VAV terminal unit cards in a sorted grid beneath it.
   - Office Level 1 produces 90 terminal unit nodes; Domestic House produces 5 terminal unit nodes (`Kitchen & rear service`, `Office`, `Living room`, `Passage`, `Bathroom`).
4. **Airflow Domain & Visualizer (`AirflowWindow.jsx`, `ConstrainedAirflow3D.jsx`)**:
   - Reads `floor.geometry.exteriorPolygon` and `floor.geometry.airflowDomain` (doors, windows).
   - Centers coordinates using `toWorld([x, y])` relative to `ORIGIN.x` and `ORIGIN.y`.
5. **Telemetry Profiler Dynamic Mode (`TelemetryPanel.jsx`)**:
   - If active zones count $\le 24$ (Domestic House has 5 zones): renders `ZoneProfileRows.jsx`, displaying each room as a row with individual setpoint, deadband, current temperature, and status bullet.
   - If active zones count $> 24$ (Office Tower has hundreds of zones): renders the 2D Quadrant `ScatterChart` (delivered cooling kW vs temperature deviation).

### 2.4 Identified Improvement for Implementation
- **Sustainability Metrics Re-computation**:
  In `src/sustainability.js`, `FLOOR_AREA_M2`, `ZONE_MIX`, and `IS_IT_DOMINATED` are declared as static module-scope variables evaluated once against `getBuilding()` on initial import:
  ```javascript
  const buildingData = getBuilding();
  const ZONES = (buildingData.floors || []).flatMap((f) => f.zones || []);
  export const FLOOR_AREA_M2 = ZONES.reduce((s, z) => s + polygonArea(z.polygon), 0);
  ```
  When the BIM model is toggled to Domestic Home at runtime, `FLOOR_AREA_M2` and `ZONE_MIX` in `GlobalMetricsPanel` still reflect the tower's ~39,776 m² unless parameterized or exported as dynamic getters/functions (e.g. `getFloorAreaM2(building)`, `getZoneMix(building)`).
  *Recommendation for Implementer*: Update `sustainability.js` and `GlobalMetricsPanel.jsx` so floor area and EUI run-rate evaluate dynamically against `currentBld`.

---

## 3. Acceptance Criterion 2: Design of `dashboard/verify_bim_switching.js`

### 3.1 Test Harness & Execution Architecture
- **Language & Runtime**: Node.js ES Modules (`node verify_bim_switching.js`).
- **Dependencies**: `puppeteer` (already in `package.json` dependencies: `^25.1.0`), built-in `http`, `fs`, `path`, `url`.
- **Serving Mechanism**: Embedded static HTTP server hosting `dist/` (compiled via `npm run build`), or live Vite dev server with request interception.
- **Harness Structure**: Class-based `TestHarness` with ANSI color output, assertion helpers (`assert`, `assertEqual`, `assertDeepEqual`), elapsed timers, and exit code reporting.

### 3.2 Test Suites Specification

#### Suite 1: Pure Store & Mathematical Invariants
- `Assert default model is 'multi-level' and contains 15 floors (buildingId: 'bldg-econ-digitized')`.
- `Assert setBuildingModelType('domestic-home') updates model to 1 floor and 5 zones (buildingId: 'bldg-econ-house-hcmc')`.
- `Assert subscribeBuildingChange listener triggers accurately on model switch`.
- `Assert getFootprint() bounds contract from (60m x 40m) to (13.56m x 5.51m)`.
- `Assert setBuildingModelType('multi-level') cleanly restores tower geometry`.

#### Suite 2: UI DOM & Model Toggle Interaction
- `Assert DOM contains [data-testid="building-model-toggle"] with [data-testid="toggle-multilevel"] initially active`.
- `Assert Office model renders >= 10 floor buttons in [data-testid="level-toggle"]`.
- `Click [data-testid="toggle-domestic-home"] and assert:`
  - `Button styling reflects active state (background: var(--accent-blue))`.
  - `Level button count reduces to exactly 1 button ([data-testid="level-btn-1"])`.
  - `Selected level display ([data-testid="selected-level-display"]) and desktop active level display ([data-testid="desktop-active-level"]) show 'L1'`.
  - `Level stepper prev/next are clamped at L1`.
  - `Topology header updates to 'MAP LEVEL 1 TOPOLOGY'`.

#### Suite 3: Telemetry Context & Component Adaptation
- `Assert Topology nodes count updates to 6 (1 AHU-MAIN + 5 Domestic Home VAV/Zone nodes)`.
- `Assert Topology node labels contain domestic zones ('Kitchen & rear service', 'Office', 'Living room', 'Passage', 'Bathroom')`.
- `Assert GlobalMetricsPanel level telemetry reflects 5 domestic zones`.
- `Assert TelemetryPanel switches view mode to ZoneProfileRows (asserting row elements for 5 domestic zones)`.

#### Suite 4: Model Reversion & Rapid Toggle Stress Testing
- `Click [data-testid="toggle-multilevel"] and assert restoration of 15 floor buttons, multi-level topology nodes, and ScatterChart view`.
- `Perform rapid toggle stress test (4 alternating switches between Office and Home in 800ms) and assert zero DOM crashes or unhandled exceptions`.

#### Suite 5: Mobile Viewport Responsiveness
- `Set viewport to 390x844 (iPhone 14 / Mobile standard)`.
- `Assert mobile 3D canvas and HUD render without overflow errors`.
- `Assert mobile level display ([data-testid="mobile-level-display"]) updates and clamps correctly between models`.

---

## 4. Subsystem Comparison & Invariant Matrix

| Subsystem / Feature | Multi-Level Commercial Tower (`multi-level`) | 1-Level Domestic House (`domestic-home`) | Invariant / Validation Target |
|---|---|---|---|
| **Building ID** | `bldg-econ-digitized` | `bldg-econ-house-hcmc` | Exact string match on `getBuilding().buildingId` |
| **Floor Count** | 15 Levels (L1–L15) | 1 Level (L1) | `building.floors.length === 15` vs `1` |
| **Footprint Dimensions** | ~60.0m × 40.0m | ~13.56m × 5.51m | `FOOTPRINT.width` and `FOOTPRINT.depth` |
| **Conditioned Area** | ~39,776 m² | ~72.0 m² | Sum of polygon shoelace areas |
| **Typical Level Zones** | 90+ zones per floor | 5 zones total | `levelZones.length` |
| **Level Buttons Bar** | 15 buttons (`data-testid="level-btn-1"`..`15`) | 1 button (`data-testid="level-btn-1"`) | DOM element count in `[data-testid="level-toggle"]` |
| **Topology Node Count** | 91 nodes on Level 1 (1 AHU + 90 zones) | 6 nodes on Level 1 (1 AHU + 5 zones) | DOM node count in ReactFlow canvas |
| **Telemetry Profiler** | 2D ScatterChart (Quadrants: $\Delta T$ vs kW) | `ZoneProfileRows` (5 room bullet cards) | Auto-switch threshold: $\le 24$ zones |
| **3D Camera Framing** | Distance $\approx 60\text{m}$, Aim Height $\approx 30\text{m}$ | Distance $\approx 18\text{m}$, Aim Height $\approx 1.4\text{m}$ | `towerFraming` calculations |
| **Stepper Bounds** | Min: 1, Max: 15 | Min: 1, Max: 1 | Boundary clamping on prev/next clicks |

---

## 5. Verification & Test Plan

To ensure end-to-end verification during implementation:
1. **Frontend Bundle Build**:
   ```bash
   cd dashboard && npm run build
   ```
2. **Run Existing Test Scripts**:
   ```bash
   cd dashboard
   node verify_level_toggle.js
   node verify_ai_actions.js
   ```
3. **Execute New BIM Switching Verification Script**:
   ```bash
   cd dashboard
   node verify_bim_switching.js
   ```
4. **Validation Criteria**:
   - All 5 test suites pass with 0 failures.
   - DOM state, Three.js canvas, and React Flow topology cleanly switch without memory leaks or console exceptions.

---
*Report prepared by `survey_explorer_frontend` for the ECON Digital Twin Platform.*
