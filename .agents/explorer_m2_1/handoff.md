# Handoff Report: Physics-Based Estimation & Sensor Fallbacks

## 1. Observation

1. **Sensor Ingestion Pipeline & Freshness Tracking**:
   - `server/mqtt.go` (lines 24–43, 111–151): Ingests MQTT JSON telemetry into `telemetryMsg` with pointer fields (`*float64`, `*int`, `*bool`) to distinguish omitted fields (`nil`) from zero values. Passes data to `engine.IngestTelemetry()`.
   - `server/simulation/engine.go` (lines 618–690): Updates per-field arrival timestamps (`HwTempAt`, `HwHumAt`, `HwCo2At`, `HwSupplyAt`, `HwAcAt`, `HwLuxAt`, `HwPlugAt`) and provenance flags (`TempReal`, `AcReal`).
   - `server/simulation/engine.go` (lines 923–928, 989–1012): Defines `hwStaleAfter = 20 * time.Second` and per-field freshness checkers (`hwFresh()`, `humFresh()`, `co2Fresh()`, `supplyFresh()`, `acFresh()`, `luxFresh()`, `plugFresh()`).

2. **Static Mocks & Constants Used During Sensor Omission**:
   - **Outdoor Temperature Fallback**: `server/simulation/engine.go` (lines 933, 966–971): `outdoorFallbackC = 30.0`. When `outdoorAt` is stale (>3 hours), returns static flat 30.0°C.
   - **Solar Gain Fallback**: `server/simulation/engine.go` (lines 1036–1044): `w := z.SolarGainMult * ph.SolarGainReferenceW`. When `luxFresh()` is false, evaluates static $10,000\text{ W} \times \text{mult}$ 24/7 without solar zenith/azimuth geometry. Confirmed in `server/data/programme-library.json` (lines 69–80).
   - **Chiller Plant COP Fallback**: `server/simulation/engine.go` (lines 2226–2228): `plantCop := math.Max(ph.CopMin, math.Min(ph.CopMax, ph.DesignCop - ph.CopStrainSlope*avgStrain))`. When `HwAcW` clamp is omitted, COP is evaluated via empirical linear room strain slope ($\text{slope}=0.35$) rather than thermodynamic condenser-evaporator lift and Carnot second-law efficiency.
   - **Supply Air Temperature Fallback**: `server/simulation/engine.go` (lines 1019–1025): `supplyC()` returns static `Phys().SupplyAirDesignC` (12.0°C) when DS18B20 probe is missing.
   - **Zone Temperature Model**: `server/simulation/engine.go` (lines 1868–1873): Single-envelope 2R1C model conducts purely against $T_{\text{outside}}$, omitting inter-zone partition wall heat conduction ($T_{\text{adj}}$).
   - **CO₂ Fallback**: `server/simulation/engine.go` (lines 1084–1086): `ph.OutdoorCo2Ppm + ph.Co2PpmPerOccupantSteady*float64(totalOccupants)/float64(len(e.Zones))` (static $400 + 15 \times \text{occ}$).

3. **Existing Test Suite State**:
   - Existing tests (`server/simulation/measured_test.go`, `server/simulation/hardware_test.go`, `server/simulation/state_provenance_test.go`) assert sensor overriding behavior and provenance gating, but no dedicated tests exist asserting dynamic physics derivation (solar geometry, thermodynamic COP, inter-zone conduction, dynamic CO₂ mass balances) when sensor inputs are omitted.

---

## 2. Logic Chain

1. **Step 1 (Ingestion & Detection)**: From Observation 1, the backend already possesses robust pointer-based nil detection and per-field freshness timestamping. Therefore, the engine can accurately detect precisely which sensors are active vs omitted in real time without architectural restructuring.
2. **Step 2 (Mock Deficiencies)**: From Observation 2, when sensors are omitted, the simulation engine relies on static constants (flat 30.0°C weather, static 10,000 W solar multipliers, static 12.0°C supply air, empirical strain-based COP). These violate Requirement R2 ("*When a specific physical sensor is unavailable, do not use static mock data. Instead, leverage the Go backend's physics and simulation engine to estimate and derive realistic values*").
3. **Step 3 (Physics Replacement Formulation)**:
   - Solar gain can be derived from Spencer/NOAA solar position equations ($\theta_z$, $\text{EoT}$, $\delta$) and ASHRAE/Haurwitz clear-sky global horizontal irradiance ($I_{\text{GHI}}$).
   - Chiller COP can be derived from Carnot thermodynamic lift ($T_{\text{condenser}} - T_{\text{evaporator}}$) adjusted by isentropic efficiency ($\eta \approx 0.55$) and part-load curves ($f(\text{PLR})$).
   - Multi-zone 2R1C thermal models can incorporate partition wall conductive terms $\sum \frac{T_j - T_i}{R_{ij}}$.
   - Supply air temperature can be derived from mixed air enthalpy and cooling coil effectiveness.
   - Dynamic CO₂ and latent moisture can be modeled via mass-balance differential equations, enabling inverse virtual occupancy sensing.
4. **Step 4 (Verification & AC1 Compliance)**: From Observation 3 and Steps 1–3, designing a dedicated Go unit and integration test suite (`sensor_fallback_test.go`) with explicit assertions on dynamic diurnal swings, non-zero solar variance, Carnot lift divergence, and mass-balance ODE trajectories satisfies Acceptance Criterion 1.

---

## 3. Caveats

- **No Caveats**: All sensor ingestion pathways, fallback constants, physical formulations, and test requirements have been thoroughly analyzed and documented.
- **Assumptions**: Solar geometry calculations assume Ho Chi Minh City coordinates (`lat = 10.8231, lon = 106.6297`) by default, configurable via `WEATHER_LAT` / `WEATHER_LON`.

---

## 4. Conclusion

The Go backend engine is well-architected for per-field telemetry tracking, but requires updating its sensor fallback mechanisms with first-principles physical models (solar geometry, Carnot chiller lift, multi-zone partition coupling, dynamic coil heat transfer, and mass balances) to eliminate all static mocks. A comprehensive Go unit/integration test suite (`server/simulation/sensor_fallback_test.go`) has been fully designed to verify AC1.

---

## 5. Verification Method

1. **Inspect Report**:
   - Review `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_1/report.md` for complete mathematical equations, audit matrices, and test specifications.
2. **Execute Go Test Suite (upon implementation)**:
   - Run: `go test -v ./simulation -run TestSensorFallback` in `server/`.
   - Invalidation conditions:
     - Any test returning a static constant (e.g. $q_{\text{solar}} > 0$ at midnight or $\text{COP}(25^\circ\text{C}) == \text{COP}(38^\circ\text{C})$).
     - Any NaN or $\pm\infty$ generated during numerical Euler integration under omitted sensor states.
