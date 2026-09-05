# End-to-End Integration, Multi-Model Backend & Test Strategy Survey

> **Target Workspace**: `/Users/nguyenhoangkhoi/Documents/econ`  
> **Survey Scope**: Go Physics Engine (`server/`), Dashboard (`dashboard/`), FlatBuffers Protocol (`server/schema/telemetry.fbs`), REST APIs, Multi-Model Switching, and E2E Test Suites.  
> **Author**: `survey_explorer_test`  
> **Date**: August 2026  

---

## Executive Summary

This report surveys the architecture, interface contracts, multi-model support, physics-based fallback mechanisms, and test strategies across the ECON digital twin platform for the requirements in `ORIGINAL_REQUEST.md` (§R1 Live Data Integration, §R2 Smart Fallbacks for Missing Sensors, §R3 BIM Context Switching).

### Key Survey Discoveries:
1. **End-to-End Telemetry Flow**: Telemetry is streamed from the Go backend (`server/simulation/engine.go`) to the React/Three.js frontend (`dashboard/src/useDigitalTwin.js`) via high-frequency (~30 Hz) binary **FlatBuffers** over WebSockets (`ws://<host>:8080/ws`) based on `server/schema/telemetry.fbs`. Bidirectional control messages (manual setbacks, auto-pilot toggles, pre-cooling) flow over the same WebSocket using JSON. REST endpoints (`/api/building-data`, `/api/ontology`, `/api/library`, `/api/hardware`, `/api/recommendations`, `/api/precool`, `/api/history`) serve static geometry, runtime models, hardware provenance, and historical metrics.
2. **Multi-Model Backend Support**: The Go backend contains an in-memory simulation engine (`Engine`) capable of running any validated building geometry (`Engine.ReloadBuilding(data []byte)`). However, `GET /api/building-data` currently serves only the single file `simulation.DataPath("building-data.json")` without query parameter support. Furthermore, while the frontend repository contains both `building-data.json` (office tower) and `building-data-home.json` (domestic tube house `bldg-econ-house-hcmc`), the backend lacks dedicated query parameters (e.g. `GET /api/building-data?model=domestic-home`) and a lightweight switching endpoint (e.g. `POST /api/building/switch`) to re-bind the active simulation twin without invoking the heavy blueprint import/deploy pipeline.
3. **Smart Fallback Physics**: The Go engine adheres to the core principle of *omission over fabrication*. When hardware sensors (DS18B20 supply probe, BH1750 lux meter, SCT-013 current clamp, NDIR CO₂ probe, or ambient temperature sensor) are omitted or stale (`hwStaleAfter = 30s`), the engine falls back to first-principles physics (2R1C Euler thermal balance, design COP curves, diurnal solar irradiance and climatological curves, metabolic CO₂ generation) rather than static mock values.
4. **Test Suite Architecture**: The project test runner consists of `go test -v ./...` in `server/` (20 existing test files) and `npm test` in `dashboard/` (executing Puppeteer E2E tests). A new Puppeteer test `dashboard/verify_bim_switching.js` and dedicated Go unit/integration tests (`server/simulation/physics_fallback_test.go` and `server/building_switching_test.go`) can be seamlessly integrated into `dashboard/package.json` and the Go test pipeline.

---

## 1. End-to-End Telemetry Flow & Interface Contracts

### 1.1 Architecture & Component Interaction Flow

```
+-------------------------------------------------------------------------------+
|                             REACT FRONTEND (dashboard/)                       |
|                                                                               |
|  +-------------------+    +----------------------+    +--------------------+  |
|  |   BuildingStore   |    |    useDigitalTwin    |    | GlobalMetricsPanel |  |
|  | (buildingStore.js)|    |  (useDigitalTwin.js) |    |  & 3D Canvas / BIM |  |
|  +--------+----------+    +----------+-----------+    +---------+----------+  |
+-----------|--------------------------|--------------------------|-------------+
            |                          |                          |
   REST:    | GET /api/building-data   | WS: FlatBuffers Binary   | WS: JSON Veto/
            | GET /api/ontology        |     (~30 Hz SimState)    |     AutoPilot
            | GET /api/library         |                          |     Commands
            v                          v                          v
+-------------------------------------------------------------------------------+
|                             GO ENGINE SERVER (server/)                        |
|                                                                               |
|  +--------------------+   +-----------------------+   +--------------------+  |
|  |   HTTP REST API    |   |     WebSocket Hub     |   |   Physics Engine   |  |
|  | (main.go, auth.go) |   |  (main.go:handleWS)   |   | (simulation/engine)|  |
|  +--------------------+   +-----------------------+   +---------+----------+  |
|                                                                 |             |
|                                                    2R1C Integration,          |
|                                                    Hardy-Cross VAV solve,     |
|                                                    Sensor Pinning & Fallbacks |
+-------------------------------------------------------------------------------+
```

### 1.2 Bootstrapping & Model Initialization
1. **Frontend Boot Phase (`main.jsx`, `buildingStore.js`)**:
   - `bootBuilding()` in `dashboard/src/buildingStore.js` executes before React mounts.
   - It issues `GET ${API_BASE}/api/building-data` (timeout 5s).
   - If successful, `towerData` is updated and `live = true`. If unreachable, bundled fixtures (`dashboard/src/building-data.json`, `dashboard/src/building-data-home.json`) act as offline fallback.
   - `useDigitalTwin.js` initializes `getInitialSimData()`, iterating through `getAllKnownBuildings()` to seed `data.zones` and `data.vavs` with geometry centroids, design setpoints, and default properties.

2. **WebSocket Telemetry Stream (`/ws`)**:
   - Client connects to `ws://${backendHost}:${BACKEND_PORT}/ws` with `binaryType = 'arraybuffer'`.
   - Client sends auth token `{"action": "auth", "token": "..."}` if `ECON_ADMIN_TOKEN` is configured.
   - Backend streams binary `SimState` FlatBuffers frames generated in `Engine.broadcast()` (`server/simulation/engine.go` lines 2121–2543).

### 1.3 FlatBuffers Schema (`server/schema/telemetry.fbs`)
The binary schema defines structured data tables:
- **`ZoneData`**:
  - `id: string` (opaque zone identifier, e.g. `zone-north-west-office-lvl4`)
  - `temp: float` (air temperature in °C, noise-dithered 0.08°C)
  - `occupants: int` (live or scheduled occupant count)
  - `load: float` (base heat load in kW)
  - `lightsOn: bool` (actuated lighting state)
  - `humidity: float` (measured %RH from bound sensor, 0 if unmeasured)
  - `co2: float` (measured ppm from NDIR sensor, 0 if unmeasured)
  - `plugW: float` (plug load draw in W)
  - `plugShed: bool` (true if sockets are swept off by APLC)
  - `supplyC: float` (actual supply air discharge temperature used in cooling law)
  - `supplyReal: bool` (true only if measured by DS18B20 probe)
- **`VavData`**:
  - `id: string`, `airflow: float` (m³/s), `damper: float` (relative restriction)
- **`AhuData`**:
  - `id: string`, `supplyTemp: float`, `pressure: float`, `status: string`, `mode: string`
- **`GlobalData`**:
  - `buildingLoadMw: float` (total electrical load in MW)
  - `systemHealth: float` (discomfort-weighted health percentage 0–100%)
  - `totalOccupants: int` (sum of occupants across all active zones)
  - `coolingOutputMw: float` (thermal cooling delivered in MW)
  - `plantCop: float` (live chiller plant coefficient of performance)
  - `energySavedMw: float` (avoided power from setback and lighting cut)
  - `bessDischargeMw: float`, `bessSocPct: float` (BESS battery metrics)
  - `avgCo2: float` (building-wide CO₂ ppm, NDIR prioritized)
  - `plugKw: float`, `plugStandbyKw: float`, `plugShedKw: float`, `plugSavedKwh: float`
  - `autoPilot: bool` (autonomous optimizer state)
  - `zonesInSetback: int` (number of zones currently in energy setback)
  - `ahuPressurePa: float` (Hardy-Cross static duct pressure in Pa)

### 1.4 REST API Surface Summary

| Endpoint | Method | Source File | Description |
|---|---|---|---|
| `/api/building-data` | GET | `server/main.go:20` | Returns active building geometry JSON. |
| `/api/ontology` | GET | `server/main.go:32` | Returns Brick ontology JSON fixture. |
| `/api/library` | GET | `server/main.go:59` | Returns programme library constants and setpoints. |
| `/api/hardware` | GET | `server/main.go:84` | Returns bound hardware nodes, sensor freshness & AFDD alerts. |
| `/api/recommendations`| GET | `server/recommendapi.go:35` | Returns ranked anomaly/predictive recommendations & load forecast. |
| `/api/precool` | GET, POST| `server/precool.go:40` | Pre-cooling status inspection and manual trigger. |
| `/api/weather` | GET | `server/weather.go:70` | Live outdoor weather conditions (Open-Meteo or climatological). |
| `/api/history` | GET | `server/db.go:85` | TimescaleDB historical telemetry series. |
| `/api/series` | GET | `server/db.go:120` | Generic per-zone per-metric historical queries. |
| `/api/plugs` | GET, POST| `server/plugapi.go:20` | Automated plug-load policy status and configuration. |
| `/api/building` | POST | `server/blueprint.go:176` | Deploys a new building fixture into Go engine. |
| `/api/building/backups`| GET | `server/blueprint.go:223` | Lists timestamped building geometry backups. |
| `/api/building/rollback`| POST| `server/blueprint.go:252` | Rolls back engine to a previous backup. |

---

## 2. Multi-Model Backend Support Analysis

### 2.1 Current Implementation State
- **Backend Model Representation**: The backend models buildings via `BuildingData` (`BuildingId string`, `Floors []FloorData`).
  - Office Tower: `bldg-econ-digitized` (4 floors, 735 zones, multi-megawatt commercial load).
  - Domestic House: `bldg-econ-house-hcmc` (1 floor, 5 zones: kitchen, office, living room, passage, bathroom, ~10 kW load).
- **Backend Engine Reloading**: `Engine.ReloadBuilding(data []byte)` (`server/simulation/engine.go` line 434) is fully implemented:
  - Re-sizes fan parameters to building volume (`sizeFanToBuilding()`).
  - Re-solves Hardy-Cross airflow network (`doHardyCross()`).
  - Drops previous building load history (`loadHist`) and whole-building baseline buckets (`baselines.DropGlobal()`).
  - Re-initializes `e.Zones` and `e.Vavs` in a single critical section (`e.mu.Lock()`).
- **Current Limitation**:
  - `GET /api/building-data` only reads `simulation.DataPath("building-data.json")` and ignores query parameters.
  - The house fixture is present on the frontend (`dashboard/src/building-data-home.json`) and generated by `tools/housify_fixture.py`, but is not stored under `server/data/building-data-home.json`.
  - When the frontend toggles the domestic home model in `App.jsx`, `buildingStore.js` switches local geometry, but the backend is not switched. As a result:
    1. The Go engine continues streaming FlatBuffers for office zones.
    2. Domestic home zones never receive live telemetry.
    3. Global metrics on the dashboard (load, occupants, plant COP) reflect the commercial tower instead of the domestic home.

### 2.2 Proposed Multi-Model Backend Architecture & Contract

#### 1. Fixture Storage & Resolution
Store both fixtures in `server/data/`:
- `server/data/building-data.json` (Multi-Level Commercial Tower)
- `server/data/building-data-home.json` (1-Level Domestic House)

#### 2. Enhanced `GET /api/building-data` Query Parameter Contract
Update `server/main.go` so `/api/building-data` inspects the `model` query parameter:
- `GET /api/building-data?model=domestic-home` (or `?model=house` / `?model=home`): returns `building-data-home.json`.
- `GET /api/building-data?model=multi-level` (or `?model=office` / `?model=tower`): returns `building-data.json`.
- `GET /api/building-data` (no param): returns the currently active building model JSON.

#### 3. Active Model Switching Endpoint
Provide a dedicated model switching endpoint:
- `POST /api/building/switch` with payload `{"model": "domestic-home" | "multi-level"}` or query param `?model=...`.
- Handler loads the corresponding fixture JSON, invokes `engine.ReloadBuilding(data)`, updates the active model state, and immediately broadcasts the updated building FlatBuffers stream.
- Also support WebSocket action: `{"action": "switch_model", "model": "domestic-home"}` in `handleWebSocket`.

#### 4. Frontend Integration (`buildingStore.js`, `App.jsx`, `useDigitalTwin.js`)
- `buildingStore.js:setBuildingModelType(type)`:
  - Dispatches `POST /api/building/switch?model=${type}` or WebSocket message `{"action":"switch_model", "model": type}`.
  - Updates `activeModelType` and fetches live geometry via `GET /api/building-data?model=${type}`.
- `useDigitalTwin.js`:
  - When new FlatBuffers frame arrives after model swap, dynamically updates `simData.zones` and `simData.vavs` matching the active model.

---

## 3. Smart Fallbacks & Physics-Based Estimation Engine

### 3.1 Physics vs Mock Data Audit Matrix

| Sensor Type | Physical Hardware | Live Behavior (Sensor Present & Fresh) | Smart Physics Fallback (Sensor Omitted / Stale) | Verification Method |
|---|---|---|---|---|
| **Zone Temperature** | DS18B20 / SHT30 | Air temperature pinned to `HwTemp` with exponential tracking ($\alpha = 0.1$). Shadow model integrates pure physics for AFDD. | Explicit Euler 2R1C integration: $C_{air} \frac{dT}{dt} = \frac{T_{wall}-T}{R_{in}} + Q_i + Q_{solar} + Q_{hvac}$. | `hardware_test.go:TestTempRealPinning`, `hardware_test.go:TestStalenessAndOfflineRelease` |
| **Zone Occupancy** | YOLO Head Count / PIR | Occupancy reflects detected heads or PIR count; feeds dynamic fresh air & internal gains. | Diurnal schedule curve `scheduledOccupancy(zoneType, areaM2, time)` scaled by programme library design density ($\text{pax}/\text{m}^2$). | `occupancy_test.go:TestScheduledOccupancyScalesWithArea` |
| **Zone CO₂ Concentration** | NDIR SCD30 / SCD40 | True ppm value reported; feeds RLS air-change estimation and baseline anomaly detector. | Dynamic metabolic balance: $\dot{V}_{CO2} = \text{pax} \times 0.005 \text{ L/s} + \text{ventilation exchange}$; global `avgCo2` computed from total occupants. | `engine.go:avgCo2`, `telemetry_schema_test.go` |
| **Supply Discharge Air** | DS18B20 Probe in Louvre | `HwSupplyC` directly replaces design value in zone cooling law $Q_{cool} = \dot{m} C_p (T_{room} - T_{supply})$. | Falls back to library design constant `Phys().SupplyAirDesignC` (12.0°C). Rejects readings within 1.0°C of setpoint. | `measured_test.go:TestSupplyProbeSupersedesDesignValue` |
| **Daylight Illuminance** | BH1750 Lux Sensor | Scales zone solar irradiance $Q_{solar} = \text{SolarGainMult} \times \frac{Lux}{Lux_{ref}} \times Q_{ref}$ (only while lights are OFF). | Evaluates baseline solar multiplier $Q_{solar} = \text{SolarGainMult} \times Q_{ref}$ from façade orientation and time of day. | `measured_test.go:TestDaylightScalesSolarGainOnlyWhenUncontaminated` |
| **AC Power Draw** | SCT-013 Current Clamp | Measured electrical draw (`HwAcW`) directly accounts for that zone's cooling power; achieves measured COP. | Electrical power computed from thermodynamic heat load divided by dynamic plant COP curve: $P_{elec} = \frac{Q_{thermal}}{COP(strain)}$. | `measured_test.go:TestMeasuredAcPowerReplacesModelledCop` |
| **Plug Loads** | SCT-013 Clamp on Sockets | `HwPlugW` replaces active plug draw; non-switchable standby tracked. | Sized by digitized floor area ($\text{Area}_{m2} \times 3.0 \text{ W/m}^2$ standby + active occupant load); APLC sweeps switchable portion. | `plugs_test.go:TestPlugStandbyScalesWithArea` |
| **Outdoor Weather** | Open-Meteo API | Live ambient temperature & humidity drive outer envelope boundary condition $T_{out}$. | Diurnal sinusoidal climatological curve (30.0°C–34.0°C peak at 14:00) derived from Ho Chi Minh City climate normals. | `weather.go:climatologicalOutdoorC` |

---

## 4. Test Suite Architecture & Integration Plan

### 4.1 Test Architecture Hierarchy

```
+-------------------------------------------------------------------------------+
|                             E2E TEST RUNNER ARCHITECTURE                      |
+-------------------------------------------------------------------------------+
| Tier 1: Go Unit & Physics Invariants (`cd server && go test -v ./...`)         |
|   - `physics_fallback_test.go`: Asserts physics derivation on omitted sensors |
|   - `building_switching_test.go`: Asserts engine reloads office vs house      |
|   - `measured_test.go`, `hardware_test.go`, `plugs_test.go` (existing 20 suites)|
+-------------------------------------------------------------------------------+
| Tier 2: Go Integration & API Contracts (`cd server && go test -v ./...`)     |
|   - `building_api_test.go`: Asserts GET /api/building-data?model=...          |
|   - `recommendapi_test.go`, `mqtt_test.go`, `auth_test.go`                    |
+-------------------------------------------------------------------------------+
| Tier 3: Frontend Mathematical Invariants (`dashboard/verify_bim_switching.js`)|
|   - Multi-model geometry assertions, zone filtering, area shoelace            |
|   - Per-level telemetry aggregation across office vs house scale              |
+-------------------------------------------------------------------------------+
| Tier 4: Headless Chrome / Puppeteer E2E (`npm test` in dashboard/)            |
|   - `verify_bim_switching.js`: Programmatic toggle of Office <-> House BIM    |
|   - Asserts DOM mutations: floor buttons (L1..L4 -> L1), topology header,      |
|     zone profile rows, 3D model footprint, metrics cards                      |
|   - `verify_ai_actions.js`, `verify_level_toggle.js` (existing E2E runners)   |
+-------------------------------------------------------------------------------+
```

### 4.2 Structuring the New Go Tests (`server/simulation/physics_fallback_test.go`)
The Go physics test must assert that when sensors are omitted, the simulation engine calculates realistic derived values rather than returning static mocks:
1. **Omitted Temperature Sensor**: Assert $T_{zone}(t)$ integrates differential heat balance:
   $$\Delta T = \frac{\Delta t}{C_{air}} \left( \frac{T_{wall}-T}{R_{in}} + Q_i + Q_{solar} - Q_{cooling} \right)$$
   and converges dynamically toward thermodynamic equilibrium ($T > 24.0^\circ\text{C}$ under internal heat load).
2. **Omitted Occupancy Sensor**: Assert occupancy is dynamically computed from `scheduledOccupancy` ($\text{density} \times \text{area} \times \text{hourProfile}$), varying realistically across 24 hours.
3. **Omitted CO₂ Sensor**: Assert `avgCo2` is calculated from total occupant respiration and fresh-air ventilation rather than static 450 ppm.
4. **Omitted AC Electrical Clamp**: Assert cooling electrical draw scales with thermodynamic cooling output and dynamic plant COP curve ($COP \in [2.2, 4.5]$).

### 4.3 Structuring the Puppeteer E2E Test (`dashboard/verify_bim_switching.js`)
The test script should be structured into 4 cohesive suites:
- **Suite 1: Multi-Model Telemetry & Geometry Invariants**:
  - Validates commercial tower model (`bldg-econ-digitized`): 4 floors, 735 zones, multi-megawatt load.
  - Validates domestic house model (`bldg-econ-house-hcmc`): 1 floor, 5 zones, ~10 kW load.
- **Suite 2: Headless Chrome DOM Verification of BIM Switching**:
  - Mounts dashboard application using Puppeteer.
  - Asserts initial commercial tower state: level buttons `L1` through `L4` present, active level display `L1`, topology header `MAP LEVEL 1 TOPOLOGY`.
  - Clicks `[data-testid="toggle-domestic-home"]`:
    - Asserts level button count becomes exactly 1 (`L1`).
    - Asserts active level display remains `L1`.
    - Asserts topology header updates to reflect domestic home zones (`zone-kitchen-rear-service-lvl1`, etc.).
    - Asserts per-level metrics display realistic domestic scale load and zone count (`5Z`).
  - Clicks `[data-testid="toggle-multilevel"]`:
    - Asserts level buttons restore to all 4 levels (`L1`, `L2`, `L3`, `L4`).
    - Asserts topology header and zone list restore to commercial office.
- **Suite 3: Rapid BIM Model Switching Stress Test**:
  - Alternates between Tower and House 6 times in succession; verifies zero unhandled exceptions or state desynchronization.
- **Suite 4: Mobile Viewport BIM & Stepper Verification**:
  - Emulates mobile screen (390x844); asserts BIM toggling and single-floor stepper behavior on mobile UI.

### 4.4 Clean CI / E2E Test Runner Integration
- **`dashboard/package.json`**: Update `"scripts"`:
  ```json
  "scripts": {
    "test": "node verify_ai_actions.js && node verify_level_toggle.js && node verify_bim_switching.js",
    "test:bim": "node verify_bim_switching.js",
    "test:level": "node verify_level_toggle.js",
    "test:ai": "node verify_ai_actions.js"
  }
  ```
- **Go Test Suite**: Executed via standard `go test -v ./...` in `server/`.

---

## 5. Summary & Actionable Implementation Plan

| Milestone | Target Files | Key Implementation Objectives | Verification |
|---|---|---|---|
| **M1: Backend Multi-Model & Fallbacks** | `server/data/building-data-home.json`, `server/main.go`, `server/simulation/engine.go` | 1. Add `building-data-home.json` to `server/data/`.<br>2. Add `?model=` query param support to `GET /api/building-data`.<br>3. Add `POST /api/building/switch` or WS action to call `engine.ReloadBuilding()`.<br>4. Implement physics derivation assertions in Go tests. | `cd server && go test -v ./...` |
| **M2: Frontend BIM Switching Wiring** | `dashboard/src/buildingStore.js`, `dashboard/src/App.jsx`, `dashboard/src/useDigitalTwin.js`, `dashboard/src/sustainability.js` | 1. Wire `setBuildingModelType` to notify backend of model switch.<br>2. Ensure `useDigitalTwin` resets/subscribes zone telemetry to active model.<br>3. Dynamically recompute `FLOOR_AREA_M2` and footprint on model switch. | `npm run build && npm run test` |
| **M3: E2E Verification Suites** | `server/simulation/physics_fallback_test.go`, `dashboard/verify_bim_switching.js`, `dashboard/package.json` | 1. Implement comprehensive Go unit tests for physics fallbacks.<br>2. Implement Puppeteer `verify_bim_switching.js` testing desktop & mobile DOM mutations.<br>3. Integrate into `npm test` and `go test ./...`. | Full test suite execution: `go test ./...` and `npm test` |

---
*Report prepared by survey_explorer_test for ECON Digital Twin platform.*
