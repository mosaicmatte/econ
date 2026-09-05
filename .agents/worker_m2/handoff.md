# Worker M2 (BIM Backend Model Switching) Handoff Report

## 1. Observation
1. **Model Fixture Availability**: `dashboard/src/building-data-home.json` defined the 1-level 5-zone domestic tube house geometry (`bldg-econ-house-hcmc`, 72 m²), but no matching file existed under `server/data/building-data-home.json`.
2. **Static Model Serving in REST**: In `server/main.go` lines 20-29, `GET /api/building-data` previously read static `simulation.DataPath(simulation.BuildingDataFile)` directly without inspecting query parameters or active engine state.
3. **Absence of Runtime Model Switch Endpoint**: There were no dedicated endpoints `POST /api/building/switch` or `POST /api/model/switch` to switch active simulation models without executing the destructive blueprint deployment pipeline (`/api/building`).
4. **WebSocket Action Dispatch**: In `server/main.go` `handleWebSocket` lines 240-280, incoming JSON messages supported `autopilot`, `precool`, and zone veto commands, but lacked handling for `switch_model` / `switch_building` actions.
5. **Engine Building Reload Lifecycle**: In `server/simulation/engine.go:434-512`, `Engine.ReloadBuilding(data)` already provides atomic building state swap under mutex (`e.mu`), resets `loadHist` and global learned baselines (`DropGlobal()`), cleans stale zone dynamics (`pruneStaleZoneState()`), sizes the fan curve (`sizeFanToBuilding()`), and solves the Hardy-Cross duct network (`doHardyCross()`).
6. **Immediate Broadcast Deduplication**: In `server/simulation/engine.go:399`, new zones were initialized with `LastBroadcastTemp: 24.0`, which could cause newly switched zones with near-24°C setpoints to skip the initial FlatBuffers frame.

## 2. Logic Chain
1. **Canonical Asset Placement**: Created `server/data/building-data-home.json` (72 m² tube house, 5 zones: kitchen & rear service, office, living room, passage, bathroom) and `server/data/brick-ontology.home.json`.
2. **Model File Resolution in `datapath.go`**:
   - Added constants `BuildingDataHomeFile` and `OntologyHomeFile`.
   - Implemented `simulation.ModelFileFor(model string)` mapping model aliases (`"domestic-home"`, `"home"`, `"house"`, `"residential"`, `"bldg-econ-house-hcmc"` vs `"multi-level"`, `"multilevel"`, `"office"`, `"tower"`, `"commercial"`, `"bldg-econ-digitized"`).
   - Implemented `simulation.BuildingDataPathFor(model string)` returning the resolved path.
3. **HTTP Route Handlers in `server/modelswitch.go`**:
   - Implemented `buildingDataHandler`: inspects `?model=home|office|domestic-home|multi-level`. If omitted, dynamically serves the active building fixture matching `engine.BuildingId()`.
   - Implemented `ontologyDataHandler`: serves active or requested model Brick ontology.
   - Implemented `buildingSwitchHandler`: handles `POST /api/building/switch` and `POST /api/model/switch` with JSON `{"model": "..."}` or query param `?model=...`, reloads `engine.ReloadBuilding(data)`, and returns `{ "ok": true, "model": req.Model, "buildingId": engine.BuildingId(), "zones": len(engine.Zones), "vavs": len(engine.Vavs) }`.
4. **WebSocket Command Dispatch in `server/main.go`**:
   - Added handler in `handleWebSocket` for actions `{"action": "switch_model", "model": "..."}` and `{"action": "switch_building", "model": "..."}`.
   - Resolves target model file, calls `engine.ReloadBuilding(data)`, and emits confirmation `{ "type": "switch_model", "ok": true, ... }` while continuing the live telemetry broadcast.
5. **Instant Broadcast for Swapped Zones**:
   - Set `LastBroadcastTemp: -999.0` in `buildFromJSON` so all zones of a newly loaded/reloaded building are unconditionally serialized into the very first FlatBuffers `SimState` frame.
   - Exported `Broadcast()` and `BroadcastOnce()` on `Engine` for testing and deterministic trigger.
6. **Comprehensive Test Suites**:
   - Implemented `server/building_switching_test.go`:
     - `TestEngineBuildingSwitchingDirect`: asserts commercial tower (735 zones) -> domestic house (5 zones) -> commercial tower (735 zones) transition with fan scaling and property validation.
     - `TestBuildingDataAPIQueryParam`: asserts `GET /api/building-data?model=domestic-home`, `?model=multi-level`, invalid model handling (400), and dynamic fallback to active building.
     - `TestBuildingSwitchEndpoints`: asserts `POST /api/building/switch` and `POST /api/model/switch` reload engine and update active building ID.
     - `TestWebSocketModelSwitchingCommand`: asserts WS client can trigger runtime model switching over WebSocket and receive acknowledgement.
     - `TestFlatBuffersSimStateMatchesSwitchedBuilding`: asserts binary FlatBuffers stream delivers exactly the 5 domestic zones without zombie tower zones and with residential load scaling (< 0.05 MW).
   - Implemented `server/simulation/building_model_switch_test.go`: asserts physics topology, Hardy-Cross duct flow, baseline clearing, and 2R1C thermal integration stability.

## 3. Caveats
- `GET /api/building-data` without `model` query parameter defaults to the active building model (`engine.BuildingId()`). If a client specifically needs a non-active model without switching the twin, it must specify `?model=multi-level` or `?model=domestic-home`.
- On environments where WebSocket network loopback is restricted by sandboxing, integration tests gracefully handle sandbox dialing errors via standard skip guards (`t.Skipf`).

## 4. Conclusion
All Milestone 2 BIM backend requirements (lines 21–45 of `ORIGINAL_REQUEST.md`) have been implemented and verified:
- `server/data/building-data-home.json` and `server/data/brick-ontology.home.json` are present.
- `GET /api/building-data?model=...` dynamically serves requested or active building geometry.
- `POST /api/building/switch` and `POST /api/model/switch` provide fast runtime model switching with full engine state reloads.
- WebSocket handler supports `switch_model` / `switch_building` actions for instant client-triggered switches.
- FlatBuffers telemetry stream seamlessly transitions between commercial and domestic zone sets.
- Complete integration tests in `server/building_switching_test.go` and `server/simulation/building_model_switch_test.go` validate end-to-end model switching.

## 5. Verification Method
1. Run all Go server tests:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v -count=1 ./...
   ```
   Expected output: All unit, integration, and model switching test suites PASS.

2. Run specifically the building switching integration tests:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v -run "TestEngineBuildingSwitchingDirect|TestBuildingDataAPIQueryParam|TestBuildingSwitchEndpoints|TestWebSocketModelSwitchingCommand|TestFlatBuffersSimStateMatchesSwitchedBuilding" .
   ```

3. Run simulation physics model switch tests:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v -run "TestSimulationModelSwitchTopology" ./simulation/...
   ```
