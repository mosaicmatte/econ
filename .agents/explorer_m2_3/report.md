# Technical Investigation & Integration Report: Dynamic BIM Model Switching, Live Telemetry & Smart Fallback Physics

**Author**: `explorer_bim_backend_integration` (`explorer_m2_3`)  
**Workspace Root**: `/Users/nguyenhoangkhoi/Documents/econ`  
**Target Requirements**: R1 (Live Data Integration), R2 (Smart Fallbacks for Missing Sensors), R3 (BIM Context Switching) from `ORIGINAL_REQUEST.md` (lines 21–45)  
**Date**: August 31, 2026  

---

## Executive Summary

This report delivers a comprehensive architectural investigation into the ECON Digital Twin platform's Go backend (`server/`) and React/Three.js frontend (`dashboard/`). The investigation focuses on three interconnected operational capabilities:
1. **Dynamic Building Information Model (BIM) Serving & Engine Topology Switching**: Transitioning the active digital twin between a 15-floor, 735-zone commercial office tower (`bldg-econ-digitized`, ~39,776 m²) and a 1-level, 5-zone domestic tube house (`bldg-econ-house-hcmc`, ~72 m²).
2. **Mock Data Audit & Live Telemetry Replacement**: Replacing residual static/mock data across frontend and backend with real-time physical sensor feeds (MQTT/WebSocket FlatBuffers) and external live APIs (Open-Meteo weather).
3. **Smart Fallback Physics Engine**: Deriving realistic physical states when individual hardware sensors are absent, disconnected, or stale—leveraging the 2R1C sensible heat model, steady-state occupant mass balance, Hardy-Cross fluid network solvers, and adaptive fan sizing.

---

## 1. Go Backend Model Serving Investigation

### 1.1 Current Model Serving Endpoints in `server/`

| Endpoint | Method | Implementation File & Line | Current Behavior | Model Switching Support |
|---|---|---|---|---|
| `/api/building-data` | `GET` | `server/main.go:20–29` | Reads static JSON from `simulation.DataPath(simulation.BuildingDataFile)` (`data/building-data.local.json` or `data/building-data.json`). | ❌ **No**. Ignores query params (`?model=home`). Only serves the single active file on disk. |
| `/api/zones` | `GET` | *Not Defined* | Endpoint does not exist as an independent route in `server/`. Zone data is served inside `/api/building-data` (`.floors[].zones[]`), `/api/rooms/models`, and binary `/ws`. | ❌ **No**. |
| `/api/ontology` | `GET` | `server/main.go:32–41` | Reads Brick Ontology JSON from `simulation.DataPath(simulation.OntologyFile)` (`data/brick-ontology.local.json` or `data/brick-ontology.json`). | ❌ **No**. |
| `/api/library` | `GET` | `server/main.go:59–65`, `server/simulation/library.go:433–456` | Serializes `simulation.Library(engine.BuildingId())` containing space programmes (`kitchen`, `living`, `home-office`, `open-office`, etc.), physics constants, and critical zones. | ⚠️ **Partial**. Carries `BuildingId`, but requires engine reload to reflect switched building context. |
| `/api/building` | `POST` | `server/blueprint.go:176–221` | Accepts `{ buildingData, ontology, source }`, validates with `engine.ReloadBuilding()`, backs up previous state, and writes `.local.json` overrides. | ⚠️ **Partial**. Designed for blueprint deployment rather than rapid runtime asset toggling. |
| `/ws` | `GET` (Upgrade) | `server/main.go:174–285`, `server/simulation/engine.go:2404–2543` | Upgrades to WebSocket and streams binary FlatBuffers `SimState` at 10–30 Hz containing `ZoneData`, `VavData`, and `GlobalData`. | ❌ **No**. Streams only the zones in the engine's active `e.Zones` and `e.Vavs` maps. |

### 1.2 Simulation Engine Topology Architecture (`server/simulation/engine.go`)

The core physics engine maintains in-memory state for all active zones and VAV boxes:
- `e.Zones`: `map[string]*ZoneSim` (keyed by `zoneId`, e.g., `zone-corridor-1-lvl1` or `zone-office-lvl1`).
- `e.Vavs`: `map[string]*VavSim` (keyed by `vavId`, e.g., `vav-corridor-1-lvl1` or `vav-office-lvl1`).
- `e.buildingId`: `string` (e.g. `"bldg-econ-digitized"` or `"bldg-econ-house-hcmc"`).

#### The `ReloadBuilding` Routine (`server/simulation/engine.go:434–508`)
The Go backend engine already contains a robust building reload mechanism:
```go
func (e *Engine) ReloadBuilding(data []byte) error {
    // 1. Scratch validation
    scratch := &Engine{Zones: map[string]*ZoneSim{}, Vavs: map[string]*VavSim{}, PMax: 600.0, KFan: 0.01}
    if err := scratch.buildFromJSON(data); err != nil {
        return err
    }
    if len(scratch.Zones) == 0 {
        return fmt.Errorf("blueprint produced zero zones")
    }

    // 2. Atomic state swap under mutex
    e.mu.Lock()
    defer e.mu.Unlock()
    e.Zones = map[string]*ZoneSim{}
    e.Vavs = map[string]*VavSim{}
    e.lastCmd = map[string]string{}
    e.demoAssign = map[string]string{}
    e.FaultTarget = ""
    e.Scenario = "peak"
    e.PreCoolUntil = time.Time{}

    // 3. Purge previous building load history & global baselines
    if n := len(e.loadHist); n > 0 {
        e.loadHist = e.loadHist[:0] // Reset zero-shot forecaster window
    }
    e.loadMinMw, e.loadMaxMw, e.loadSeen = 0, 0, 0
    if e.baselines != nil {
        e.baselines.DropGlobal() // Drop GLOBAL baseline buckets
    }

    // 4. Rebuild zones, VAV resistances, fan sizing, and Hardy-Cross network
    if err := e.buildFromJSON(data); err != nil {
        return err
    }
    return nil
}
```

### 1.3 The Disconnect: Frontend vs Backend Model State

Currently, model switching is only implemented on the client side:
1. When the user clicks `🏢 Multi-Level Building` or `🏠 1-Level Domestic Home` in `dashboard/src/App.jsx:1158–1198`:
   - `handleToggleBuildingModel(type)` calls `setBuildingModelType(type)` in `dashboard/src/buildingStore.js`.
   - `buildingStore.js` updates `activeModelType = type` in memory and broadcasts `homeData` (`building-data-home.json`) to frontend subscribers.
2. **The Go backend is never notified**:
   - The Go engine remains running the 15-floor, 735-zone office tower model.
   - The `/ws` FlatBuffers stream continues broadcasting tower zones (`zone-corridor-1-lvl1`, etc.) and a 1.5–2.5 MW load.
   - None of the 5 domestic home zones (`zone-kitchen-rear-service-lvl1`, `zone-office-lvl1`, `zone-living-room-lvl1`, `zone-passage-lvl1`, `zone-bathroom-lvl1`) receive telemetry updates over WebSocket.
   - The domestic house zones remain permanently frozen at their initial seeded values (`getInitialSimData()`), and global metrics on screen display the office tower's multi-megawatt power draw.

---

## 2. Audit of Mock Data & Live Telemetry Replacement Plan

### 2.1 Audit Findings Summary (Cross-Referenced with `mock_data_report.md`)

| Location | Component / File | Nature of Mock / Static Data | Live Telemetry / API Replacement Strategy |
|---|---|---|---|
| **Frontend** | `dashboard/src/useDigitalTwin.js:27–68` (`getInitialSimData()`) | Static seeding: `plantCop=3.2`, `co2=450`, `humidity=50`, `temp=24.0`, `ahuPressure=0`. | Transient seeds replaced on the very first binary FlatBuffers frame from `/ws`. Dynamic zone table populated from `getBuilding().floors[].zones`. |
| **Frontend** | `dashboard/src/sustainability.js:29–60` | Module-load static evaluation: `FLOOR_AREA_M2`, `ZONE_MIX`, `IS_IT_DOMINATED` calculated once at import from initial `getBuilding()`. | Make `FLOOR_AREA_M2` and `ZONE_MIX` reactive or accessor functions of the active building (`getBuilding()`), recalculating dynamically on model switch (39,776 m² ↔ 72 m²). |
| **Frontend** | `dashboard/src/GlobalMetricsPanel.jsx:184–220` | Level filtering for domestic house: Level 1 aggregation must filter active zones against active building floor zones. | Implemented via `currentBld.floors.find(f => f.level === activeFloor).zones` matching `simData.zones`. |
| **Backend** | `server/weather.go:35–65` | Sinusoidal climatological model (30.0°C–34.0°C) used when Open-Meteo is offline. | Retained strictly as a high-availability fallback when external API fails or is rate-limited; live Open-Meteo weather poller updates `engine.SetOutdoor(temp, hum)` every 10 min. |
| **Backend** | `server/simulation/engine.go:1020–1086` | Assumptions for unmetered quantities (supply air temp, COP, solar, CO2). | **Physics-based Smart Fallbacks (Requirement R2)**: Replace hardcoded static values with dynamic physical equations. |
| **Edge** | `edge/esp32/src/camera_driver.cpp:25–65` | Synthetic test patterns (`PATTERN_PERSON_SILHOUETTE`, `PATTERN_EMPTY_SCENE`). | Active only in `#ifdef HOST_TEST` or when OV7670 hardware is absent; real DMA frame capture active on hardware. |

---

### 2.2 Smart Fallbacks for Missing Sensors (Requirement R2 Physics Breakdown)

When physical IoT hardware sensors are omitted or disconnect, the system must **never fabricate static data**. Instead, the Go engine evaluates physics-derived estimates based on available inputs and architectural priors from `programme-library.json`.

```
                  ┌────────────────────────────────────────────────────────┐
                  │                 MQTT Telemetry Stream                  │
                  │   (Temp, Humidity, CO2, SupplyC, AcW, Lux, PlugW, Occ) │
                  └──────────────────────────┬─────────────────────────────┘
                                             │
                       ┌─────────────────────┴─────────────────────┐
                       ▼                                           ▼
            [ Sensor Present & Fresh ]                  [ Sensor Omitted / Stale ]
            ──────────────────────────                  ──────────────────────────
            • Exponential blend to HW:                  • 2R1C Sensible Heat Model:
              T += (HwTemp - T) * 0.1                     dT/dt = (Tout-T)/Rout + (Twall-T)/Rin
            • Measured COP from SCT-013:                          + Qint + Qsol - Qcool
              COP = Q_cool / P_elec                     • Dynamic COP Degradation:
            • Measured Lux scales Solar:                  COP = DesignCop - Slope * Strain
              Qsol *= HwLux / DaylightRef               • Steady-State Occupant CO2:
            • Measured Supply Probe:                      AvgCO2 = OutdoorCO2 + 15 * Pax / Nzones
              Qcool = m_dot * cp * (T - HwSupplyC)      • Design Supply Air Fallback:
            • Real Camera/PIR Occupancy:                  Qcool = m_dot * cp * (T - 12.0°C)
              Ingest exact integer count                • Scheduled Diurnal Occupancy:
                                                          Pax = (Area / AreaPerPax) * Profile[hr]
```

#### Detailed Physical Mathematical Models:

1. **Air Temperature ($T_{\text{air}}$) Fallback — 2R1C Envelope Model**:
   When no physical temperature sensor is bound to zone $z$ (`!z.hwFresh()`):
   $$\frac{dT_{\text{air}}}{dt} = \frac{T_{\text{out}} - T_{\text{air}}}{R_{\text{out}}} + \frac{T_{\text{wall}} - T_{\text{air}}}{R_{\text{in}}} + \frac{\dot{Q}_{\text{int}} + \dot{Q}_{\text{solar}} - \dot{Q}_{\text{cooling}}}{C_{\text{air}}}$$
   $$\frac{dT_{\text{wall}}}{dt} = \frac{T_{\text{air}} - T_{\text{wall}}}{R_{\text{in}}} + \frac{T_{\text{out}} - T_{\text{wall}}}{R_{\text{out}}}$$
   Where:
   - $\dot{Q}_{\text{int}} = \text{BaseHeatLoad} + \text{Occupants} \times 100\text{ W} + \text{PlugW}$
   - $\dot{Q}_{\text{solar}} = \text{SolarGainMult} \times \text{SolarGainReferenceW}$ (scaled by BH1750 lux if fresh and lights are off)
   - $\dot{Q}_{\text{cooling}} = \dot{m}_{\text{air}} c_p (T_{\text{air}} - T_{\text{supply}})$

2. **Supply Air Temperature ($T_{\text{supply}}$) Fallback**:
   - **With DS18B20 louvre probe**: $T_{\text{supply}} = HwSupplyC$ (`supplyReal = true`).
   - **Without probe**: $T_{\text{supply}} = \min(\text{SupplyAirDesignC}, \text{Setpoint} - 1.0^\circ\text{C}) = 12.0^\circ\text{C}$ (`supplyReal = false`).

3. **Chiller Plant COP Fallback**:
   - **With SCT-013 AC power clamp**: $\text{COP}_{\text{measured}} = \frac{\dot{Q}_{\text{cooling, total}}}{P_{\text{electrical, measured}}}$.
   - **Without clamp**: Dynamic strain degradation curve evaluated from building thermal stress:
     $$\text{Strain} = \max\left(0, \frac{1}{N}\sum_{i=1}^N (T_i - \text{Setpoint}_i)\right)$$
     $$\text{COP} = \text{clamp}\left(\text{DesignCop} - \text{CopStrainSlope} \times \text{Strain}, \; \text{CopMin}, \; \text{CopMax}\right)$$
     $$\text{COP} = \text{clamp}(3.6 - 0.35 \times \text{Strain}, \; 2.2, \; 3.8)$$

4. **CO₂ Concentration Fallback**:
   - **With NDIR SCD30 sensor**: Stream zone's measured ppm; non-zero value rendered with `(M)` badge.
   - **Without sensor**: Zone stream carries `0.0` (indicating unmetered). Building average evaluates steady-state mass balance:
     $$\text{AvgCO}_2 = \text{OutdoorCO}_2 (400\text{ ppm}) + \text{Co2PpmPerOccupantSteady} (15\text{ ppm}) \times \frac{\text{TotalOccupants}}{N_{\text{zones}}}$$

5. **Occupancy Fallback**:
   - **With CV Camera / Edge AI**: Real integer count ingested directly.
   - **Without camera**: Diurnal schedule based on room programme and digitized area:
     $$\text{DesignOccupants} = \frac{\text{AreaM}^2}{\text{AreaPerOccupantM}^2}$$
     $$\text{Occupants}(t) = \text{round}\Big(\text{DesignOccupants} \times \text{Profile}[\text{hour}] \times (1 + \mathcal{N}(0, \sigma_{\text{jitter}}))\Big)$$

---

## 3. End-to-End Dynamic Model Switching Architecture

To achieve 100% synchronized state, telemetry, and rendering when switching between the Commercial Office Tower and Domestic House, the following end-to-end integration contracts are established:

```
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                 FRONTEND (Dashboard)                                   │
│                                                                                        │
│  [ UI Model Toggle: Office 🏢 <-> Home 🏠 ]                                           │
│       │                                                                                │
│       ▼                                                                                │
│  buildingStore.setBuildingModelType(type)                                              │
│       ├── (1) POST /api/model/switch { model: "domestic-home" | "multi-level" } ──────┐│
│       ├── (2) Update activeModelType -> notify subscribers                            ││
│       └── (3) getBuilding() -> returns building-data-home.json                        ││
│                                                                                        ││
│  React Components Reactive Update:                                                     ││
│  ├── useDigitalTwin: resets simData.zones/vavs for new zoneIds                         ││
│  ├── sustainability.js: recomputes FLOOR_AREA_M2 (72 m²), ZONE_MIX                     ││
│  ├── GlobalMetricsPanel: adapts level filtering to Level 1, aggregates 5 zones        ││
│  ├── TopologyPanel: renders AHU-MAIN + 5 residential VAV units                        ││
│  └── BuildingModel: recalibrates getFootprint(), re-centers origin, frames 3D model    ││
└────────────────────────────────────────┬───────────────────────────────────────────────┘
                                         │ (1) HTTP POST /api/model/switch
                                         ▼
┌────────────────────────────────────────────────────────────────────────────────────────┐
│                                  BACKEND (Go Engine)                                   │
│                                                                                        │
│  POST /api/model/switch (or GET /api/building-data?model=home)                        │
│       │                                                                                │
│       ▼                                                                                │
│  engine.ReloadBuilding(homeDataBytes)                                                  │
│       ├── [e.mu.Lock()]                                                                │
│       ├── Drop previous zones (735 -> 5) and VAVs (735 -> 5)                           │
│       ├── Discard old building load history (e.loadHist[:0])                           │
│       ├── Drop previous GLOBAL learned baselines                                       │
│       ├── Size fan curve & VAV resistances to 72 m² (PMax ~12 kW)                      │
│       ├── Solve Hardy-Cross static pressure for 5-zone residential layout              │
│       └── [e.mu.Unlock()]                                                              │
│                                                                                        │
│  Telemtry Stream (/ws):                                                                │
│  └── Binary FlatBuffers SimState now streams:                                          │
│       • ZoneData: zone-kitchen-rear-service-lvl1, zone-office-lvl1, etc.               │
│       • VavData: vav-kitchen-rear-service-lvl1, etc.                                   │
│       • GlobalData: buildingLoadMw ~0.008 MW (8 kW), totalOccupants ~2-4               │
└────────────────────────────────────────────────────────────────────────────────────────┘
```

### 3.1 Backend Implementation Requirements

1. **Storage of Canonical Assets in `server/data/`**:
   - `server/data/building-data.json`: Canonical 15-story office tower fixture (`bldg-econ-digitized`, 735 zones).
   - `server/data/building-data-home.json`: Canonical 1-level domestic house fixture (`bldg-econ-house-hcmc`, 5 zones).
   - `server/data/brick-ontology.json`: Tower Brick schema.
   - `server/data/brick-ontology.home.json`: Domestic home Brick schema.

2. **Model Switching REST Endpoints in `server/main.go`**:
   - Support `GET /api/building-data?model=home` and `GET /api/building-data?model=office`.
   - Implement `POST /api/model/switch` (or `POST /api/building/switch`):
     ```go
     http.HandleFunc("/api/model/switch", func(w http.ResponseWriter, r *http.Request) {
         if corsPreflight(w, r) { return }
         if r.Method != http.MethodPost {
             http.Error(w, "POST required", http.StatusMethodNotAllowed)
             return
         }
         var req struct {
             Model string `json:"model"` // "domestic-home" | "multi-level" | "home" | "office"
         }
         if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
             http.Error(w, "invalid json payload", http.StatusBadRequest)
             return
         }
         
         var targetFile string
         if req.Model == "domestic-home" || req.Model == "home" {
             targetFile = "building-data-home.json"
         } else {
             targetFile = "building-data.json"
         }
         
         data, err := os.ReadFile(filepath.Join(simulation.DataDir(), targetFile))
         if err != nil {
             http.Error(w, "failed to read model file: "+err.Error(), http.StatusInternalServerError)
             return
         }
         
         if err := engine.ReloadBuilding(data); err != nil {
             http.Error(w, "engine failed to reload model: "+err.Error(), http.StatusUnprocessableEntity)
             return
         }
         
         w.Header().Set("Content-Type", "application/json")
         json.NewEncoder(w).Encode(map[string]interface{}{
             "ok": true,
             "buildingId": engine.BuildingId(),
             "zones": len(engine.Zones),
             "vavs": len(engine.Vavs),
         })
     })
     ```
   - WebSocket Command Support: Parse `{"action":"switch_model","model":"domestic-home"}` in `handleWebSocket` (`server/main.go:250–280`).

3. **Multi-Model Support in `/api/building-data` & `/api/library`**:
   - `GET /api/building-data` serves the currently active engine building data, or query param override `?model=home`.
   - `GET /api/library` returns the library view corresponding to `engine.BuildingId()`.

### 3.2 Frontend Implementation Requirements

1. **`dashboard/src/buildingStore.js`**:
   - In `setBuildingModelType(type)`, asynchronously notify the Go backend:
     ```javascript
     export async function setBuildingModelType(type) {
       if (type !== 'multi-level' && type !== 'domestic-home') return;
       if (activeModelType !== type) {
         activeModelType = type;
         try {
           await fetch(`${API_BASE}/api/model/switch`, {
             method: 'POST',
             headers: { 'Content-Type': 'application/json' },
             body: JSON.stringify({ model: type }),
           });
         } catch (e) {
           console.warn('[buildingStore] Failed to notify backend of model switch:', e);
         }
         listeners.forEach((fn) => {
           try { fn(getBuilding(), activeModelType); } catch (e) { console.error(e); }
         });
       }
     }
     ```

2. **`dashboard/src/useDigitalTwin.js`**:
   - Subscribe to `subscribeBuildingChange`.
   - When building changes, purge `simData.zones` and `simData.vavs` and re-seed with the new building's zones from `getInitialSimData()` so stale tower zones are cleared and domestic house zones immediately start accepting WebSocket telemetry.

3. **`dashboard/src/sustainability.js`**:
   - Convert `FLOOR_AREA_M2` and `ZONE_MIX` into dynamic getters:
     ```javascript
     export function getFloorAreaM2(b = getBuilding()) {
       const zones = (b?.floors || []).flatMap((f) => f.zones || []);
       return zones.reduce((s, z) => s + polygonArea(z.polygon), 0);
     }
     export const FLOOR_AREA_M2 = {
       valueOf() { return getFloorAreaM2(); },
       toString() { return String(getFloorAreaM2()); },
       toLocaleString(...args) { return getFloorAreaM2().toLocaleString(...args); }
     };
     ```

4. **`dashboard/src/App.jsx`**:
   - Dynamic Level Selector: clamps `activeFloor` to valid floors in `currentBuilding` (`activeFloor = 1` for domestic home).
   - Topology View: reconstructs ReactFlow nodes for the 5 domestic zones linked to `AHU-MAIN`.
   - P&ID Schematic & Airflow Window: re-bounds volumetric mesh to $13.56\text{m} \times 5.51\text{m}$.

5. **`dashboard/src/BuildingModel.jsx`**:
   - Automatically adapts camera framing via `towerFraming()` and world offsets `toWorld()` using dynamic `getFootprint(currentBld)`.
   - Zone shaders (`SingleFloorLayout`) map live temperatures and alerts onto the 5 domestic rooms.

---

## 4. Verification & Testing Strategy

### 4.1 Go Physics & Fallback Unit Tests

New test suites in `server/simulation/engine_model_switch_test.go` and `server/simulation/smart_fallback_test.go`:

1. **TestSmartFallbackTemperature**:
   - Assert that omitting temperature inputs runs the 2R1C thermal model.
   - Verify that indoor air temperature moves toward $T_{\text{out}}$ with realistic thermal capacitance time constants ($0.77\text{h}–1.54\text{h}$) rather than jumping or staying static.
2. **TestSmartFallbackSupplyAndCop**:
   - Assert that missing louvre probe evaluates cooling against library design ($12.0^\circ\text{C}$).
   - Assert that missing AC clamp evaluates COP from load strain curve ($3.6 - 0.35 \times \text{Strain}$).
3. **TestSmartFallbackCo2AndOccupancy**:
   - Assert that missing NDIR sensor evaluates steady-state occupant CO2 model ($400 + 15 \times \text{Pax} / N$).
   - Assert that missing camera evaluates diurnal programme schedule based on floor area.
4. **TestModelSwitchingEngineTopology**:
   - Load office tower model (735 zones, 735 VAVs, $P_{\text{max}} = 600\text{ kW}$).
   - Call `engine.ReloadBuilding(homeData)`.
   - Assert `len(engine.Zones) == 5` and `len(engine.Vavs) == 5`.
   - Assert $P_{\text{max}}$ scales down to residential sizing (~$12\text{ kW}$).
   - Assert `loadHist` is emptied and `DropGlobal()` dropped tower baselines.
   - Assert FlatBuffers serialization emits exactly 5 zones and domestic global load (~$0.008\text{ MW}$).

### 4.2 End-to-End Puppeteer Test (`dashboard/verify_bim_switching.js`)

A dedicated Puppeteer verification script verifying:
1. Initial page load on Multi-Level Tower:
   - Asserts 15-level toggle is visible and active.
   - Asserts global building load is in multi-megawatt office range (1.0–3.0 MW).
2. Programmatic click on `data-testid="toggle-domestic-home"`:
   - Asserts HTTP `POST /api/model/switch` with `{"model":"domestic-home"}` succeeds.
   - Asserts DOM level selector switches to Level 1 only.
   - Asserts level metrics display 5 zones and residential kW load (2.0–15.0 kW).
   - Asserts 3D Canvas re-renders the 5-room tube house geometry.
   - Asserts WebSocket stream delivers updates for `zone-kitchen-rear-service-lvl1`, `zone-office-lvl1`, etc.
3. Programmatic click on `data-testid="toggle-multilevel"`:
   - Asserts return to 15-floor tower with full 735-zone context.

---

## 5. Architectural Recommendation & Action Plan

1. **Step 1 — Asset Placement**: Copy `dashboard/src/building-data-home.json` to `server/data/building-data-home.json` and generate `server/data/brick-ontology.home.json`.
2. **Step 2 — Backend Route Implementation**: Add `POST /api/model/switch` and query param support to `GET /api/building-data?model=` in `server/main.go`.
3. **Step 3 — Frontend Store Sync**: Update `dashboard/src/buildingStore.js` to dispatch `POST /api/model/switch` upon model toggle.
4. **Step 4 — State & Sustainability Reactivity**: Update `useDigitalTwin.js` to reset zone maps on switch and update `sustainability.js` to compute floor area and EUI dynamically.
5. **Step 5 — Verification**: Execute Go unit tests (`go test -v ./simulation/...`) and execute Puppeteer E2E test (`node verify_bim_switching.js`).
