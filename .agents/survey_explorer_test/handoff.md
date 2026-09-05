# Handoff Report — End-to-End Integration, Multi-Model Backend & Test Strategy Survey

## 1. Observation

1. **Telemetry Streaming Architecture**:
   - In `server/simulation/engine.go` (lines 2121–2543), `Engine.broadcast()` serializes live simulation metrics into binary FlatBuffers frames using the schema defined in `server/schema/telemetry.fbs` (root type `SimState`, with tables `ZoneData`, `VavData`, `AhuData`, `GlobalData`).
   - In `server/main.go` (lines 174–285), the WebSocket handler upgrades connections at `/ws` using `upgrader.Upgrade(w, r, nil)`. Incoming messages from the frontend dashboard are parsed as JSON:
     - Auth: `{"action": "auth", "token": "..."}` (`server/main.go` lines 220–228)
     - Auto-pilot toggle: `{"action": "autopilot", "value": bool}` (`server/main.go` lines 256–263)
     - Pre-cool window: `{"action": "precool"}` (`server/main.go` lines 267–273)
     - Manual zone override: `{"action": "<verb>", "zone": "<zoneId>"}` calling `engine.PublishCommand(action, zone)` (`server/main.go` lines 274–277)
   - In `dashboard/src/useDigitalTwin.js` (lines 201–383), the client maintains an active WebSocket to `WS_URL`, decodes binary ArrayBuffers via `SimState.getRootAsSimState(new flatbuffers.ByteBuffer(new Uint8Array(event.data)))`, and mutates React state (`simData.zones`, `simData.vavs`, global metrics).

2. **REST Endpoints & Data Delivery**:
   - `server/main.go` (lines 20–29) serves `GET /api/building-data`, which calls `os.ReadFile(simulation.DataPath(simulation.BuildingDataFile))`. It currently does not inspect query parameters (e.g. `?model=...`).
   - `server/blueprint.go` (lines 176–221) exposes `POST /api/building`, which invokes `engine.ReloadBuilding(p.BuildingData)`.
   - `dashboard/src/buildingStore.js` (lines 13–78) bundles `building-data.json` and `building-data-home.json`. On initial load, `bootBuilding()` issues `GET ${API_BASE}/api/building-data`. `setBuildingModelType(type)` updates `activeModelType` locally but does not send an HTTP request or WebSocket command to switch the running backend engine.

3. **Smart Fallback Physics in Go Engine**:
   - In `server/simulation/engine.go` (lines 345–356, 359–365, 375–377, 2151–2218, 2220–2238) and `server/simulation/measured_test.go` (lines 19–51, 131–171, 185–209):
     - Missing supply probe: `z.supplyC(sp)` falls back to `Phys().SupplyAirDesignC` (12.0°C).
     - Missing daylight sensor: solar gain falls back to `SolarGainMult * Phys().SolarGainReferenceW`.
     - Missing AC current clamp: cooling electrical load falls back to $Q_{cool} / COP(strain)$ where $COP \in [2.2, 4.5]$.
     - Missing physical temperature sensor: zone air temperature evolves under 2R1C explicit Euler thermal integration ($C_{air} \frac{dT}{dt} = \frac{T_{wall}-T}{R_{in}} + Q_i + Q_{solar} - Q_{hvac}$).
     - Missing occupancy sensor: zone occupancy evolves under `scheduledOccupancy(zoneType, areaM2, time)` scaled by programme library occupant density.
     - Missing outdoor weather: `server/weather.go` (lines 35–65) falls back to diurnal sinusoidal climatological curve (30°C–34°C).

4. **Test Infrastructure & Execution**:
   - Running `npm test` in `dashboard/` executes `node verify_ai_actions.js`, which passes 20 tests in ~7 seconds (`Test Summary: 20 Total | 20 Passed | 0 Failed`).
   - Running `node verify_level_toggle.js` in `dashboard/` passes 13 tests in ~19 seconds (`Test Summary: 13 Total | 13 Passed | 0 Failed`).
   - `server/` contains 20 existing `*_test.go` suites tested with `go test ./...`.

---

## 2. Logic Chain

1. **Telemetry Streaming**: Observations 1.1–1.3 establish that the communication pipeline is already unified on WebSocket FlatBuffers (for high-frequency telemetry) and WebSocket JSON (for control actions). The FlatBuffers schema is comprehensive and carries live zone, VAV, and global telemetry.
2. **Multi-Model Support**:
   - Observation 2.1 shows that `Engine.ReloadBuilding(data)` already provides safe, atomic in-memory building switching in Go (resizing fan curves, solving Hardy-Cross network, dropping stale baselines).
   - Observation 2.2 shows that `GET /api/building-data` only returns the default office fixture, ignoring query parameters, and no dedicated lightweight endpoint or WebSocket action exists for switching between office and house models.
   - Observation 2.3 shows that frontend `setBuildingModelType` changes local geometry without notifying the Go engine, causing a telemetry mismatch where the backend streams office telemetry while the UI displays the house model.
   - Therefore, multi-model support requires: (a) placing `building-data-home.json` in `server/data/`, (b) adding `?model=` query param support to `GET /api/building-data`, (c) adding a lightweight switch endpoint (e.g. `POST /api/building/switch` or WS action `{"action":"switch_model", "model":"..."}`), and (d) wiring `buildingStore.js` to dispatch the switch request.
3. **Smart Fallback Verification**: Observation 3 shows that first-principles physics equations already exist in Go for temperature, occupancy, CO₂, solar gain, and chiller electrical draw when hardware sensors are omitted. Explicit unit/integration tests in `server/simulation/physics_fallback_test.go` can directly assert these calculations against omissions.
4. **Test Strategy**: Observations 4.1–4.3 demonstrate that the existing Puppeteer test harness pattern (`TestHarness` with headless Chrome in `verify_ai_actions.js` and `verify_level_toggle.js`) provides reliable opaque-box E2E testing. Constructing `dashboard/verify_bim_switching.js` using this established pattern and adding `physics_fallback_test.go` to the Go suite provides complete, automated verification cleanly integrated into `npm test` and `go test ./...`.

---

## 3. Caveats

- **External Python Services**: The Python forecasting service (`backend/forecasting/`) and YOLO CV occupancy modules (`ai_modules/`) run as optional companion processes; when offline, the Go engine gracefully falls back to TimesFM zero-shot heuristics and scheduled occupancy models without crashing.
- **Database Presence**: TimescaleDB is optional; when `DB_URL` is unset, `server/db.go` degrades gracefully to in-memory live streaming without affecting the 30 Hz FlatBuffers pipeline.

---

## 4. Conclusion

The end-to-end telemetry flow is fully operational over WebSocket FlatBuffers. Multi-model backend support requires adding query parameter support to `GET /api/building-data?model=...`, saving `server/data/building-data-home.json`, exposing a lightweight `POST /api/building/switch` endpoint or WebSocket switch action, and wiring `buildingStore.js` to synchronize model switches. Smart physics fallbacks are already grounded in first-principles equations in Go and can be asserted via `server/simulation/physics_fallback_test.go`. The test strategy should comprise Go physics tests and a Puppeteer script `dashboard/verify_bim_switching.js` integrated into `npm test`.

---

## 5. Verification Method

1. **Frontend E2E Verification**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard
   npm test
   node verify_level_toggle.js
   ```
2. **Backend Unit & Integration Verification**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v ./...
   ```
3. **Artifact Inspection**:
   - Inspect `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_test/report.md` for the full comprehensive survey report.
   - Inspect `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_test/handoff.md` for this 5-component handoff report.
