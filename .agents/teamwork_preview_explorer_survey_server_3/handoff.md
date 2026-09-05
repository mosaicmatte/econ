# Handoff Report — Server Architecture Survey & Sustainability Integration

**Agent**: `explorer_survey_server_3`  
**Date**: 2026-09-05  
**Handoff Type**: Hard (Task complete)  
**Target Path**: `/Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_explorer_survey_server_3/handoff.md`  

---

## 1. Observation

1. **Server Entry Point & Routing**:
   - `server/main.go:18`: `func main()` binds HTTP handlers to Go's `http.DefaultServeMux` and runs `http.ListenAndServe(":"+port, nil)` (lines 221-223).
   - Port is retrieved from `os.Getenv("PORT")` (default `"8080"` at line 217).
   - CORS is standardized in `server/blueprint.go:35-44` via `corsPreflight(w, r) bool`, which emits `Access-Control-Allow-Origin: *` and terminates OPTIONS preflights with `204 No Content`.
   - Admin authorization is gated via `requireAdmin(w, r)` in `server/blueprint.go:52` and `auth.go:43-62` using `ECON_ADMIN_TOKEN` and `X-Admin-Token`.

2. **Telemetry Ingestion & Fields**:
   - `server/mqtt.go:24-44`: `telemetryMsg` struct contains fields `Zone`, `Occupancy` (`*int`), `PlugW` (`*float64`), `StripW` (`*float64`), `AcW` (`*float64`), `SupplyC` (`*float64`), `TempReal` (`bool`), `AcReal` (`*bool`).
   - `server/mqtt.go:112-150`: `handleTelemetry` parses MQTT topics `econ/telemetry/+` and calls `engine.IngestTelemetry(...)` with `simulation.Measurement`.
   - `server/simulation/engine.go:616-692`: Ingests telemetry into `ZoneSim`:
     - `PlugW`: stored in `z.HwPlugW` with arrival timestamp `z.HwPlugAt`.
     - `StripW`: stored in `z.HwStripW` with arrival timestamp `z.HwStripAt`.
     - `AcW`: stored in `z.HwAcW` with arrival timestamp `z.HwAcAt`.
     - `Occupancy`: stored in `z.Occupancy`, sets `z.Live = true`.
   - Freshness threshold: `hwStaleAfter = 20 * time.Second` (`engine.go:939`).
   - FlatBuffers schema `server/schema/telemetry.fbs` and broadcast (`engine.go:2250-2350`) streams `plugW`, `stripW`, `supplyC`, `occupants` over WebSocket `/ws`.

3. **Simulation Engine & Physics Loop**:
   - `server/simulation/engine.go:275-400`: `NewEngine()` calls `buildFromJSON` loading `data/building-data.json`.
   - Zones are instantiated as `ZoneSim` with area `AreaM2` from geometry polygons, plug standby load `PlugStandbyW = AreaM2 * 1.2 W/m²`, and thermal parameters `CAir`, `CWall`, `RIn`, `ROut`.
   - `engine.Start()` (`engine.go:1625`) loops on a 33ms ticker (~30 Hz), executing `e.tick(dt)`, AFDD shadow integration, `e.actuate()`, `e.applyHardware()`, `e.plugTick(now)`, BESS battery dispatch, and `e.broadcast()`.

4. **Build & Test Baseline**:
   - Command `go build .` in `/Users/nguyenhoangkhoi/Documents/econ/server`: Exit code 0, 0 warnings.
   - Command `go test ./...` in `/Users/nguyenhoangkhoi/Documents/econ/server`: Exit code 0, all tests pass across `econ`, `econ/cli`, `econ/schema/Telemetry`, `econ/simulation`.

5. **Existing Constants & Benchmark References**:
   - `server/simulation/library.go:243`: `GridEmissionFactor()` returns `Lib().Calibration.GridEmissionFactor`, defaulting to `0.6766 tCO2/MWh` (`kgCO2e/kWh`).
   - `server/simulation/library.go:29`: `Programme` defines `AreaPerOccupantM2 *float64` for space sizing (e.g. 10 m²/person for office, 2.5 m²/person for meeting rooms).

---

## 2. Logic Chain

1. **Clean Integration Without Breakage**:
   - Because all HTTP routes in `server/main.go` are independently registered on `http.DefaultServeMux` (e.g. `/api/plugs`, `/api/forecast`, `/api/recommendations`), registering a new handler `http.HandleFunc("/api/sustainability", sustainabilityHandler(engine))` adds a self-contained endpoint without modifying or colliding with existing endpoints.
   - Using package `main` in `server/carbon.go` mirrors `plugapi.go` and `weather.go`, allowing direct access to `engine` methods and helper functions (`corsPreflight`) without circular package dependencies.

2. **Accurate Scope 2 Operational Carbon (R1)**:
   - Total power drawn is already computed in `engine.broadcast()`: `buildingLoadMW = coolingElectricalMW + baseElectricalMW` (in MW).
   - In addition, granular components are available:
     - HVAC power: `coolingElectricalMW * 1000` (kW)
     - Plug power: `plugTotalW / 1000` (kW)
     - Strip power: `sum(HwStripW) / 1000` (kW)
   - Scope 2 operational emissions rate is mathematically: $\text{Power (kW)} \times \text{GridFactor (kgCO}_2\text{e/kWh)}$.
   - Continuous accumulation integrates: $\Delta \text{Energy (kWh)} \times \text{GridFactor}$ over elapsed wall-clock seconds.

3. **Predictive Maintenance & Space Utilization (R2)**:
   - `ZoneSim` already retains `AreaM2` and `Occupancy`. Sizing space efficiency against design capacity ($\sum \text{AreaM2}_i / \text{AreaPerOccupantM2}_i$) produces an exact, defensible space utilization percentage.
   - Predictive maintenance alerts can inspect active sensors: abnormal ACS712 strip draws ($>2000\text{ W}$), abnormal SCT-013 AC draws ($>3500\text{ W}$), continuous runtime exceeding service thresholds (e.g. 500h), and AFDD residual divergences (`ResidualEma > 2.0 °C`).

4. **Carbon Credit Offsetting & Live Market Pull (R3)**:
   - Comparing calculated emissions rate or cumulative emissions against a configurable `carbonBudget` identifies excess emissions $\Delta E$.
   - Outbound HTTP requests can query public market pricing feeds via standard `http.Client` with timeout and fallback to current market spot rates ($8.50 - $12.00 / tCO2) to guarantee resiliency in offline or sandboxed environments.
   - Recommended purchase is $\text{OffsetTonnes} = \Delta E / 1000$, with estimated cost $\text{OffsetTonnes} \times \text{SpotPrice}$.

---

## 3. Caveats

- **Network Sandboxing**: Outbound HTTP requests to external domains in the local sandbox environment may be blocked by network policy (e.g., as observed with Open-Meteo / external APIs). Live market fetchers must implement timeout handling (`5s`) and an immediate graceful fallback to spot price defaults so tests and offline operations never fail.
- **Sim Time vs Wall Clock**: Simulation physics `dt` accelerates during faults (up to 2.0s per tick); energy and runtime integration must anchor to real wall-clock time (`time.Now().Sub(lastAt).Seconds()`), following the pattern in `plugs.go`.

---

## 4. Conclusion

The Go backend server is cleanly architected, builds and tests with 0 errors, and provides all foundational telemetry streams (`plugW`, `stripW`, AC electrical/thermal states, `occupancy`) required for the Sustainability & Decarbonization module.

Implementing `server/carbon.go` with endpoint `/api/sustainability` and unit tests in `server/carbon_test.go` can be executed seamlessly in package `main` without altering existing simulation mechanics or breaking existing endpoints.

---

## 5. Verification Method

To verify these findings independently:

1. **Verify Build**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go build .
   ```
   *Expected*: Zero errors, clean binary generation.

2. **Verify Tests**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test ./...
   ```
   *Expected*: All packages pass (`econ`, `econ/simulation`).

3. **Verify Telemetry Pipeline Schema**:
   Inspect `server/schema/telemetry.fbs` lines 11-20 and `server/simulation/engine.go:1496-1498` to confirm `PlugW`, `StripW`, `Occupancy`, and `AcReal` bindings.

4. **Inspect Analysis Report**:
   Review `/Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_explorer_survey_server_3/analysis.md` for complete architectural and mathematical specifications.
