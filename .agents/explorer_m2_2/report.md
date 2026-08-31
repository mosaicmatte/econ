# BIM Model Management, Telemetry Binding & Verification Architecture Report

**Agent**: `explorer_bim_frontend` (`explorer_m2_2`)  
**Workspace**: `/Users/nguyenhoangkhoi/Documents/econ/dashboard`  
**Reference Mandate**: `ORIGINAL_REQUEST.md` (Requirement R3 & Acceptance Criterion 2)  
**Date**: 2026-08-31  

---

## 1. Executive Summary

Requirement R3 mandates complete, dynamic Building Information Model (BIM) context switching between the multi-level commercial office building model and the single-level domestic house model. Acceptance Criterion 2 requires creating a robust Puppeteer/Node end-to-end verification script (`dashboard/verify_bim_switching.js`) that programmatically interacts with the BIM toggle in the UI and asserts that the underlying telemetry context, DOM elements, and 3D rendering pipeline accurately reflect the newly chosen model.

This investigation conducted an exhaustive architectural audit of the frontend BIM management store (`src/buildingStore.js`), data models (`src/building-data.json` and `src/building-data-home.json`), application shell (`src/App.jsx`), simulation telemetry hook (`src/useDigitalTwin.js`), 3D WebGL renderer (`src/BuildingModel.jsx`), geometry calculus (`src/floorGeometry.js`), metrics panels (`src/GlobalMetricsPanel.jsx`), and mobile companion app (`src/MobileApp.jsx`).

---

## 2. BIM Model Management Architecture

### 2.1 The `buildingStore.js` Subsystem
In the earlier dashboard iterations, geometry was compiled as a static JSON import at bundle time. The modern architecture utilizes a two-stage boot sequence and a reactive store (`src/buildingStore.js`):
- **State Management**:
  - `activeModelType`: String tracking active BIM model (`'multi-level'` or `'domestic-home'`).
  - `towerData`: In-memory fixture of the 15-floor commercial office tower (`src/building-data.json`).
  - `homeData`: In-memory fixture of the 1-floor domestic house (`src/building-data-home.json`).
  - `listeners`: Set of callback functions subscribed to model switching events.
- **Exported API**:
  - `getBuilding()`: Returns `homeData` if `activeModelType === 'domestic-home'`, otherwise `towerData`.
  - `getAllKnownBuildings()`: Returns `[towerData, homeData]` enabling global initialization of telemetry nodes.
  - `getBuildingModelType()`: Returns active type string.
  - `setBuildingModelType(type)`: Mutates `activeModelType` and dispatches notifications to all subscribers `(getBuilding(), activeModelType)`.
  - `subscribeBuildingChange(fn)`: Registers listener and returns cleanup unsubscription closure.
  - `bootBuilding()`: Pre-fetches live geometry from `GET /api/building-data` before mounting.

### 2.2 Telemetry Integration (`useDigitalTwin.js`)
- `getInitialSimData()` loops over `getAllKnownBuildings()`:
  - Every zone across both the multi-level tower (1,350 zones) and domestic house (5 zones) is instantiated into `simData.zones` and `simData.vavs`.
  - Data attributes initialized include `temp`, `setpoint`, `deadband`, `load`, `occupancy`, `baseHeatGain`, `areaM2`, `centroid`, `co2`, and `humidity`.
- Real-time FlatBuffers stream updates match zone IDs (`z.id()`) dynamically without hardcoded index assumptions.
- `FAULT_ZONES` dynamically indexes fault-capable zones across all known buildings.

---

## 3. Comparative Analysis: Office Building vs. Domestic House

| Attribute / Domain | Multi-Level Commercial Tower (`building-data.json`) | Domestic House Model (`building-data-home.json`) |
| :--- | :--- | :--- |
| **Building ID** | `bldg-econ-digitized` | `bldg-econ-house-hcmc` |
| **Scale & Footprint** | 60.0 m × 40.0 m (2,400.0 m² per floor) | 13.56 m × 5.51 m (74.7 m² footprint) |
| **Total Conditioned Area** | ~36,000.0 m² across 15 floors | ~67.7 m² across 5 zones |
| **Levels / Floors** | 15 Floors (Level 1 through Level 15) | 1 Floor (Level 1 "Ground floor") |
| **Floor Height / Elevation** | 4.0 m floor height; elevation 0.0 m to 56.0 m | 2.8 m floor height; elevation 0.0 m |
| **Total Zones** | 1,350 zones (90 zones per floor) | 5 zones total on Level 1 |
| **Zone Classifications** | `corridor`, `conference`, `office`, `mechanical`, `server-room`, `lobby` | `kitchen`, `home-office`, `living`, `circulation`, `bathroom` |
| **Specific Zone IDs** | `zone-corridor-1-lvl1`, `zone-conference-2-lvl1`, `zone-office-3-lvl1`, etc. | `zone-kitchen-rear-service-lvl1`, `zone-office-lvl1`, `zone-living-room-lvl1`, `zone-passage-lvl1`, `zone-bathroom-lvl1` |
| **Base Heat Loads (Design)** | 200 W to 18,000 W (Server rooms / IT loads) | 20 W to 283 W (Domestic appliances & lighting) |
| **Aggregate Floor 1 Base Load**| > 20.0 kW | 0.644 kW (0.6 kW displayed) |
| **Service Core Polygon** | Non-empty core polygon (`[0.03, 11.06]...`) | Empty `corePolygon: []` (no central riser/core) |
| **Wall Thickness** | 0.3 m | 0.2 m |
| **HVAC / VAV Mapping** | 1,350 VAV units (`vav-...`) | 5 VAV units (`vav-kitchen-...`, etc.) |
| **Airflow Domain** | Multi-door/window riser network per floor | 5 interior doors, 0 exterior windows |
| **Origin & Center (`(cx, cy)`)**| `cx = 30.0, cy = 20.0` | `cx = 6.78, cy = 2.755` |

---

## 4. UI Model Switching Mechanism & Rebinding Flow

### 4.1 UI Controls Location & DOM Attributes
The 3D model switcher is located on the desktop HUD floating directly above the bottom command bar:
- **Container Selector**: `[data-testid="building-model-toggle"]`
  - Position: `bottom: 5.2rem`, `left: 50%`, `transform: translateX(-50%)`, `zIndex: 15`.
- **Interactive Buttons**:
  - `[data-testid="toggle-multilevel"]`: "🏢 Multi-Level Building"
  - `[data-testid="toggle-domestic-home"]`: "🏠 1-Level Domestic Home"
- **Visual State**:
  - Active button: `background: 'var(--accent-blue)'`, `color: '#ffffff'`.
  - Inactive button: `background: 'transparent'`, `color: 'var(--text-secondary)'`.

### 4.2 Component Rebinding & Reset Pipeline
When `handleToggleBuildingModel(type)` is triggered:
1. **Store Notification**: `setBuildingModelType(type)` fires `subscribeBuildingChange` listeners across components.
2. **Floor Clamping & Zone Selection Reset**:
   - `selectedZone` is unconditionally set to `null`, clearing any stale node diagnostics.
   - For `domestic-home`, `activeFloor` resets to `1`.
   - For `multi-level`, `activeFloor` resets to `defaultFloor(newBld)`.
3. **Geometry & 3D Canvas Rebinding (`BuildingModel.jsx` & `floorGeometry.js`)**:
   - `getFootprint(building)` dynamically recalculates `minX, maxX, minY, maxY, cx, cy, width, depth`.
   - `toWorld(p)` maps fixture coordinates to the newly computed origin.
   - `towerFraming` recalculates the bounding sphere, camera distance, and perspective target.
   - CSG wall generation and floor plate meshes re-render for the active building structure.
4. **Level Selector Buttons & Steppers (`GlobalMetricsPanel.jsx` & `App.jsx`)**:
   - `availableFloors` recomputes from `(currentBld?.floors || []).map(f => f.level)`.
   - For Domestic Home: exactly 1 level button (`data-testid="level-btn-1"`).
   - For Multi-Level: 15 level buttons (`data-testid="level-btn-1"` through `data-testid="level-btn-15"`).
   - Floating level stepper (`data-testid="desktop-level-toggle"`) clamps navigation at `[minLevel, maxLevel]`.
5. **Topology / P&ID Schematic (`App.jsx` React Flow)**:
   - `buildTopologyFromSim` filters active floor zones by `floorZoneIds`.
   - Header updates to `MAP LEVEL {activeFloor} TOPOLOGY`.
   - Domestic Home renders 5 terminal unit nodes + 1 AHU node (`ahu-main`) with 5 connecting edges.
   - Office Tower renders up to 90 terminal unit nodes per floor with a 9-column grid layout.
6. **Per-Level Telemetry Context**:
   - `levelMetrics` in `GlobalMetricsPanel` filters live telemetry by zone IDs for the active floor.
   - Computes `loadKw`, `occupancy`, `avgTemp`, and `count` (5Z vs 90Z) accurately reflecting the active model.

---

## 5. Specification of Verification Test Script (`dashboard/verify_bim_switching.js`)

In accordance with Acceptance Criterion 2, `dashboard/verify_bim_switching.js` must be implemented with two comprehensive verification suites:

### Suite 1: Mathematical & Structural Model Invariants (Unit Level)
1. **Office Tower Structural Invariants**:
   - Asserts 15 floors, 1350 total zones, 60x40m footprint, non-empty core polygon, and 0.3m wall thickness.
2. **Domestic Home Structural Invariants**:
   - Asserts 1 floor, 5 zones, 13.56x5.51m footprint, empty core polygon, and 0.2m wall thickness.
3. **Dynamic Footprint & Origin Transformation**:
   - Asserts `getFootprint()` accurately shifts center coordinates (`cx=30, cy=20` vs `cx=6.78, cy=2.755`).
4. **Dynamic Telemetry Aggregation Variance**:
   - Asserts Domestic Home Level 1 load is 0.6 kW with 5 zones, whereas Office Tower Level 1 load is > 20.0 kW with 90 zones.
5. **Building Store Subscription & Type Invariants**:
   - Asserts `setBuildingModelType` updates `getBuildingModelType()`, triggers registered listeners, and rejects invalid inputs.

### Suite 2: Real Built App Puppeteer DOM Verification (Vite Production Bundle)
1. **Initial Mount Verification (Multi-Level Office Default)**:
   - Renders built bundle in headless browser.
   - Asserts `[data-testid="building-model-toggle"]` is mounted.
   - Asserts `toggle-multilevel` is styled active and `toggle-domestic-home` is inactive.
   - Asserts `>= 10` floor buttons (`[data-testid^="level-btn-"]`) are present.
   - Asserts Topology header displays `MAP LEVEL 1 TOPOLOGY`.
2. **BIM Model Switch to 1-Level Domestic Home**:
   - Clicks `[data-testid="toggle-domestic-home"]`.
   - Asserts `toggle-domestic-home` becomes active.
   - Asserts level toggle shrinks to exactly 1 button (`data-testid="level-btn-1"`).
   - Asserts `[data-testid="selected-level-display"]` and `[data-testid="desktop-active-level"]` read `L1`.
   - Asserts Topology header displays `MAP LEVEL 1 TOPOLOGY`.
   - Asserts level zone metric displays `5 Z`.
3. **Level Boundary Clamping under Domestic Home Model**:
   - Clicks `[data-testid="level-step-next"]` and `[data-testid="level-step-prev"]`.
   - Asserts active level remains pinned to `L1` without exceptions.
4. **BIM Model Switch Back to Multi-Level Office Tower**:
   - Clicks `[data-testid="toggle-multilevel"]`.
   - Asserts `toggle-multilevel` becomes active.
   - Asserts available floor buttons expand back to 15 buttons.
   - Clicks `[data-testid="level-btn-4"]` -> asserts `selected-level-display` and Topology header update to `L4` and `MAP LEVEL 4 TOPOLOGY`.
5. **Zone Selection Reset Across BIM Model Switch**:
   - Clicks a node to open Node Diagnostics.
   - Switches BIM model to Domestic Home.
   - Asserts `selectedZone` is reset to null and right dock reverts to Enterprise Overview / Global Metrics.
6. **Rapid BIM Switching Stress Test**:
   - Sequentially toggles between models 6 times in rapid succession.
   - Asserts DOM consistency, active state synchronization, and zero uncaught runtime errors.
7. **Mobile Viewport Compatibility (390x844)**:
   - Mounts under mobile viewport.
   - Asserts mobile floor stepper reflects the active building's floor boundaries.

---

## 6. Recommendations for Implementation & Execution

1. **Test Script Location**: Place the script at `dashboard/verify_bim_switching.js`.
2. **Server & Request Interception**: Follow the proven static server architecture using Node `http` and Puppeteer `setRequestInterception` on `http://dashboard.local/` serving `dashboard/dist/`.
3. **Exit Code Discipline**: Return exit code 0 on complete pass, exit code 1 on any assertion failure.
4. **Package Script Integration**: Add `"test:bim": "node verify_bim_switching.js"` to `dashboard/package.json` for CI/CD parity.
