# Handoff Report: Dynamic BIM Model Switching, Live Telemetry & Smart Fallback Physics

**Agent**: `explorer_bim_backend_integration` (`explorer_m2_3`)  
**Working Directory**: `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_3`  
**Handoff Type**: Hard (Task Complete)  
**Date**: August 31, 2026  

---

## 1. Observation

1. **Backend Model Serving & Route Limitations**:
   - `server/main.go:20–29`: The `/api/building-data` handler reads exclusively from `simulation.DataPath(simulation.BuildingDataFile)` (`data/building-data.json` or `data/building-data.local.json`). It accepts no query parameters (e.g. `?model=home`), and there is no standalone `/api/zones` endpoint.
   - `server/main.go:59–65`: The `/api/library` endpoint returns `simulation.Library(engine.BuildingId())`. Space programmes (`kitchen`, `living`, `home-office`, `open-office`, etc.) are defined in `server/data/programme-library.json:388–551`.
   - `server/main.go:174–285` & `server/simulation/engine.go:2404–2543`: Binary FlatBuffers telemetry streamed over `/ws` serializes zones and VAVs directly from `e.Zones` and `e.Vavs`.
   - `server/simulation/engine.go:434–508`: `engine.ReloadBuilding(data []byte)` validates scratch state, acquires `e.mu.Lock()`, resets `e.Zones`, `e.Vavs`, `loadHist`, and global baselines, sizes the fan curve (`sizeFanToBuilding()`), and solves Hardy-Cross duct pressures. However, no public REST endpoint or WebSocket message currently invokes `ReloadBuilding` for runtime model toggling.

2. **Frontend Model Switching Asymmetry**:
   - `dashboard/src/buildingStore.js:13–76`: `setBuildingModelType(type)` toggles `activeModelType` between `'multi-level'` (`building-data.json`) and `'domestic-home'` (`building-data-home.json`) in local JavaScript memory only.
   - `dashboard/src/App.jsx:1158–1198`: Buttons `toggle-multilevel` and `toggle-domestic-home` call `handleToggleBuildingModel(type)` which triggers `buildingStore`.
   - **Critical Gap**: When the user switches to `domestic-home`, the Go backend is not notified. The backend continues simulating 735 office zones, while the frontend renders 5 domestic zones whose telemetry IDs never match the incoming FlatBuffers stream, leaving them frozen on initial seeds (`getInitialSimData()`).

3. **Mock Data & Static Constants**:
   - `dashboard/src/useDigitalTwin.js:27–68`: `getInitialSimData()` seeds `plantCop = 3.2`, `co2 = 450`, `humidity = 50`, `temp = 24.0`, `ahuPressure = 0`.
   - `dashboard/src/sustainability.js:29–60`: `FLOOR_AREA_M2` (~39,776 m²) and `ZONE_MIX` are evaluated once at module import from the initial `getBuilding()` result, preventing dynamic recalculation on model switch.
   - `server/data/`: `building-data-home.json` is missing from `server/data/`, existing only in `dashboard/src/building-data-home.json`.

4. **Smart Fallback Physics (Requirement R2)**:
   - `server/simulation/engine.go:920–1100`:
     - Missing temperature sensor: 2R1C envelope differential equations integrate thermal mass ($C_{\text{air}}, C_{\text{wall}}$), internal loads ($\dot{Q}_{\text{int}}$), solar gains ($\dot{Q}_{\text{solar}}$), and cooling ($\dot{Q}_{\text{cooling}}$).
     - Missing louvre probe: uses library design value ($12.0^\circ\text{C}$) with `supplyReal = false`.
     - Missing AC current clamp: calculates COP via strain degradation ($3.6 - 0.35 \times \text{Strain}$).
     - Missing NDIR sensor: zone telemetry reports `0.0`; building average evaluates steady-state occupant mass balance ($400 + 15 \times \text{Pax} / N$).
     - Missing CV camera: evaluates diurnal programme schedule from `programme-library.json`.

---

## 2. Logic Chain

1. **From Observation 1 & 2**: Because `dashboard/src/buildingStore.js` modifies only local JavaScript state and never issues an HTTP/WebSocket notification to `server/main.go`, the Go simulation engine remains bound to the 15-floor tower topology.
2. **From Observation 1 & 3**: Because `server/data/building-data-home.json` is absent and `ReloadBuilding` is not called, the `/ws` FlatBuffers stream outputs zone IDs belonging to `bldg-econ-digitized`. When the frontend is in `domestic-home` mode, none of the 5 house zone IDs match, causing zone telemetry to stall and global power to report 2 MW instead of ~8 kW.
3. **From Observation 3**: Because `dashboard/src/sustainability.js` computes `FLOOR_AREA_M2` at module load time, switching to the domestic house leaves EUI normalized against 39,776 m² instead of 72 m², corrupting sustainability calculations.
4. **From Observation 4**: Because the Go backend already possesses mathematical models for 2R1C thermal dynamics, fan sizing, Hardy-Cross network solving, and occupant schedule evaluation, smart fallback physics (R2) can be fully validated via targeted unit tests without introducing synthetic mock data.
5. **Conclusion Formulation**: Bridging the model switch requires:
   - Placed asset: `server/data/building-data-home.json` and `brick-ontology.home.json`.
   - Endpoint: `POST /api/model/switch` calling `engine.ReloadBuilding()`.
   - Client sync: `buildingStore.js` dispatching `POST /api/model/switch` and `sustainability.js` / `useDigitalTwin.js` dynamically adapting state.

---

## 3. Caveats

1. **External Weather Network Dependency**: `server/weather.go` falls back to a deterministic 30.0°C–34.0°C sinusoidal model if Open-Meteo API is unreachable. This is an intentional resilience fallback, not a mock.
2. **YOLO Machine Learning Pipeline**: AI digitization pipelines (`ai_modules/`) are separate from runtime BIM model switching and were not modified.
3. **No Code Implementation in Read-Only Investigation**: All concrete code changes, tests, and file creations for production implementation must be executed by the implementing agent.

---

## 4. Conclusion

The ECON digital twin architecture possesses the underlying physics engine, Hardy-Cross network solver, and FlatBuffers streaming capacity to support dynamic BIM model switching and live sensor fallback. The current limitation is an integration gap: the Go backend lacks a REST/WebSocket switch route and server-side domestic home asset files, while frontend sustainability and telemetry stores require dynamic re-binding upon model change. Implementing the 5-step integration action plan detailed in `report.md` will achieve 100% synchronized, live, and physics-driven digital twin switching.

---

## 5. Verification Method

To independently verify these findings:

1. **Verify Backend Model Reload & Smart Fallback Unit Tests**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v ./simulation/...
   ```
   *Expected*: Passes existing hardware, measured, and dynamics tests.

2. **Verify Frontend Build & Syntax**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard
   npm run build
   ```
   *Expected*: Vite bundle builds cleanly with zero errors.

3. **Verify Full Technical Report**:
   Inspect `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_3/report.md` for complete mathematical equations, line-by-line file audits, and endpoint contract designs.
