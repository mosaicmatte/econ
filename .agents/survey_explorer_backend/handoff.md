# Handoff Report: Go Backend & Physics Simulation Survey

## 1. Observation

Direct code inspection of `/Users/nguyenhoangkhoi/Documents/econ/server/` and `server/simulation/` revealed the following exact implementations, files, line numbers, and behaviors:

1. **Sensor Ingestion & Freshness Discipline**:
   - `server/mqtt.go` (lines 24–43): `telemetryMsg` struct defines pointer fields (`Occupancy *int`, `Temperature *float64`, `Humidity *float64`, `Co2 *float64`, `PlugW *float64`, `SupplyC *float64`, `AcW *float64`, `Lux *float64`, `AcReal *bool`, `CfgRev *uint32`) and boolean `TempReal`. This strictly distinguishes "unreported/missing" (`nil`) from physical zero.
   - `server/simulation/engine.go` (lines 923–928, 989–1012): Per-field staleness bounds (`hwStaleAfter = 20 * time.Second`). Functions `hwFresh()`, `humFresh()`, `co2Fresh()`, `supplyFresh()`, `acFresh()`, `luxFresh()` gate sensor usage per field.
   - `server/devices.go` (lines 36–44, 134–168): Tracks `fieldStat.Omitted` counter for each metric whenever a telemetry message arrives with a nil pointer for that sensor.

2. **Smart Physics Fallbacks for Missing Sensors**:
   - **Temperature**: `server/simulation/engine.go` (lines 1868–1873): When `!z.hwFresh()`, integrates 2R1C thermal model differential equations:
     - `dTAirDt := ((z.WallTemp-z.Temp)/(z.RIn*z.CAir) + (qInternal-qCooling)/z.CAir)`
     - `dTWallDt := ((tOutside-z.WallTemp)/(z.ROut*z.CWall) - (z.WallTemp-z.Temp)/(z.RIn*z.CWall))`
     - `ShadowTemp` (lines 1885–1893) integrates pure sensor-free 2R1C to compute `ResidualEma` for AFDD.
   - **Occupancy**: `server/simulation/engine.go` (lines 1668–1719, 1829): When `!z.Live`, computes `scheduledOccupancy(z.Type, z.AreaM2, now)` derived from `DesignOccupancy(z.Type, z.AreaM2)` and 24-hour diurnal curve `OccupancyFractionAt(z.Type, hour)` with Gaussian jitter `0.15`.
   - **Solar Gain**: `server/simulation/engine.go` (lines 1036–1045): When BH1750 lux is missing or lights are ON (`z.LightsOn == true`), solar gain falls back to façade aperture exposure `z.SolarGainMult * ph.SolarGainReferenceW`.
   - **HVAC Power / Chiller COP**: `server/simulation/engine.go` (lines 2220–2254): When SCT-013 AC clamp is missing, chiller COP is dynamically computed from building strain: `plantCop := math.Max(ph.CopMin, math.Min(ph.CopMax, ph.DesignCop-ph.CopStrainSlope*avgStrain))`. Cooling electricity is computed as `(unmeteredCoolingW/plantCop + meteredAcW) / 1e6`.
   - **Plug Loads**: `server/simulation/plugs.go` (lines 108–123): When SCT-013 plug clamp is missing, plug draw is derived from polygon area standby (`z.PlugStandbyW = areaM2 * 1.2`) + occupant active load (`float64(z.Occupancy) * 65.0`).
   - **Supply Temperature**: `server/simulation/engine.go` (lines 1019–1025): When DS18B20 supply probe is missing or stale, falls back to `Phys().SupplyAirDesignC` (12.0°C).
   - **CO₂ & Airflow**: `server/simulation/engine.go` (lines 1066–1086, 569–583): `avgCo2` is computed via occupant mass balance `OutdoorCo2Ppm + Co2PpmPerOccupantSteady*float64(totalOccupants)/float64(len(e.Zones))`; airflows and AHU pressure are solved via Hardy-Cross iterative loop.

3. **BIM Context Switching**:
   - `server/simulation/engine.go` (lines 434–508) in `ReloadBuilding(data []byte)`: Atomically wipes old building state (`e.Zones`, `e.Vavs`, `e.loadHist`, `e.demoAssign`), re-sizes VAV box resistances from zone volume (`vavResistanceFor`), rescales fan curve to building network (`sizeFanToBuilding`), runs Hardy-Cross solve (`doHardyCross`), and drops cross-building learned global baselines (`e.baselines.DropGlobal()`).

4. **Existing Go Test Suite**:
   - 12 test suites in `server/simulation/` (`measured_test.go`, `hardware_test.go`, `occupancy_test.go`, `plugs_test.go`, `dynamics_test.go`, `state_provenance_test.go`, `bess_sizing_test.go`, `baselines_test.go`, `autopilot_test.go`, `site_test.go`, `forecast_window_test.go`, `protocol_stress_test.go`).
   - `measured_test.go` specifically asserts supply probe override, daylight scaling & light contamination gating, and AC clamp replacing modelled COP.
   - `state_provenance_test.go` specifically asserts BIM model switching context isolation.

---

## 2. Logic Chain

1. *From Observation 1*: The telemetry pipeline in `server/mqtt.go` and `server/devices.go` uses pointer semantics and per-field arrival timestamps (`hwStaleAfter = 20s`). When a sensor is omitted or disconnected, the Go backend knows immediately that the field is absent.
2. *From Observation 2*: When sensor inputs are absent, the engine does not inject static mock constants. Instead, it computes values dynamically using first-principles physics:
   - Missing zone temperature $\to$ 2R1C thermal model differential equations with sensible heat balance and outdoor ambient weather coupling.
   - Missing occupancy $\to$ space-density-derived design capacity and 24-hour diurnal profile.
   - Missing solar lux $\to$ BIM façade aperture multiplier $\times$ reference solar radiation.
   - Missing AC power clamp $\to$ dynamic chiller COP derived from whole-building thermal strain and fresh-air ventilation enthalpy.
   - Missing plug power clamp $\to$ floor area standby ($1.2\text{ W/m}^2$) + occupant active load ($65\text{ W/person}$) with APLC vacancy shedding.
   - Missing supply air temp $\to$ engineering design discharge temperature ($12.0^\circ\text{C}$).
   - Missing CO₂ $\to$ mass-balance steady-state derivation.
   - Airflow and static pressure $\to$ Hardy-Cross duct network solver.
3. *From Observation 3*: On BIM context reload (`ReloadBuilding`), geometry-derived sizing (VAV resistance, fan curve $P_{max}$, BESS capacity) and state isolation (clearing load history and global baselines) ensure seamless switching between Office and Domestic House models.
4. *From Observation 4*: Existing tests cover individual sensor overrides and BIM isolation, but a dedicated integration test suite (`smart_fallback_test.go`) explicitly exercising the full sensor omission matrix (AC1) is needed to prove end-to-end physics calculations under all combinations of sensor omissions.

---

## 3. Caveats

- **External Python Services**: The TimesFM zero-shot forecaster (`backend/forecasting/timesfm_forecaster.py`) and LSTM service (`backend/forecasting/main.py`) run as external services; their internal model weights were surveyed from their Go integration endpoints (`server/forecast.go`).
- **Go Toolchain in Sandbox**: `go` binary was not present in the local shell `$PATH` in the sandbox environment; test execution in CI/containerized environment will verify the test suite via `go test ./...`.
- No other caveats.

---

## 4. Conclusion

The Go backend and physics simulation engine are fully equipped with rigorous, first-principles physical models for live data integration (R1), smart fallback estimation for missing/omitted sensors (R2), and BIM context switching (R3). No static mock values are used in the physics calculation pipeline. To complete Acceptance Criterion 1 (AC1), an explicit sensor omission test suite (`server/simulation/smart_fallback_test.go`) should be added to formalize unit/integration assertions across all sensor omission combinations.

---

## 5. Verification Method

1. **Survey Artifact Verification**:
   - Inspect `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/report.md`
   - Inspect `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/handoff.md`
2. **Codebase Inspection**:
   - Inspect `server/simulation/engine.go` (lines 1818–1894, 2125–2276) for 2R1C thermal model and COP calculations.
   - Inspect `server/simulation/plugs.go` (lines 104–170) for plug load derivations.
   - Inspect `server/simulation/dynamics.go` for RLS system identification.
   - Inspect `server/simulation/state_provenance_test.go` and `measured_test.go` for existing test coverage.
3. **Execution Command (when Go environment is active)**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server && go test -v ./...
   ```
4. **Invalidation Conditions**:
   - Any commit that reintroduces static mock data into `server/simulation/engine.go` or bypasses the 2R1C thermal model.
