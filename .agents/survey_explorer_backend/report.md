# Comprehensive Survey Report: Go Backend & Physics Simulation Engine

**Date**: August 31, 2026  
**Auditor**: `survey_explorer_backend`  
**Scope**: `server/` and `server/simulation/`  
**Target Requirements**: `ORIGINAL_REQUEST.md` (lines 21–45): R1 (Live Data Integration), R2 (Smart Fallbacks for Missing Sensors), R3 / AC1 (Physics-Derived Values Test Coverage & BIM Switching).

---

## Executive Summary

The ECON Go backend (`server/`) and physics simulation engine (`server/simulation/`) implement an advanced, physics-grounded digital twin architecture. The codebase is designed around two strict principles:
1. **Omission Over Fabrication**: Missing or disconnected sensors never emit fabricated or static mock data. Instead, the system tracks per-sensor and per-field freshness (`hwFresh`, `humFresh`, `co2Fresh`, `supplyFresh`, `acFresh`, `luxFresh`), marks live vs. unmetered state explicitly, and calculates realistic derived quantities using rigorous physical models.
2. **Data-Driven Engineering Coefficients**: Building-specific coefficients reside in data (`programme-library.json`) rather than compiled Go constants, ensuring adaptability across diverse Building Information Models (e.g., commercial office towers vs. domestic residences).

This report details the architectural audit of live data integration (R1), analyzes the smart fallback mechanics for missing physical sensors (R2), evaluates existing test coverage and defines test matrices for Acceptance Criterion 1 (R3/AC1), and provides actionable implementation recommendations.

---

## 1. R1: Live Data Integration & Codebase Audit

### 1.1 Ingestion Pipeline (`server/mqtt.go`, `server/devices.go`)

- **MQTT Broker Connectivity** (`server/mqtt.go`, lines 45–109):
  - Subscribes to `econ/telemetry/+` and `econ/status/+`.
  - Connects asynchronously with auto-reconnect and LWT (Last Will and Testament) tracking.
  - Supports authenticated connections (`MQTT_USERNAME`, `MQTT_PASSWORD`) and dev brokers.
- **Telemetry Message Protocol** (`server/mqtt.go`, lines 24–43):
  - `telemetryMsg` uses pointer fields (`*int`, `*float64`, `*bool`) to strictly distinguish between a reported zero value and an omitted/missing sensor field.
  - `TempReal` boolean flag distinguishes physical hardware thermistors/RTDs from firmware simulated placeholders (`#ifdef HOST_TEST`). Simulated temperatures never pin the physics engine.
  - `AcReal` boolean pointer explicitly tracks whether commanded setpoints physically reach an air conditioner or are merely logged.
  - `CfgRev` uint32 pointer tracks hardware calibration revisions to detect sensor recalibration events.
- **Sensor Omission Tracking** (`server/devices.go`, lines 33–44, 130–169):
  - Per-field health (`fieldStat`) records `Last`, `At`, `Count`, `Min`, `Max`, and `Omitted` counters.
  - When an edge sensor fails (e.g., I2C bus error or CRC checksum failure on SHT30/NDIR), firmware omits the field. The server tracks `Omitted++` rather than holding stale data.

### 1.2 External Live Weather Poller (`server/weather.go`)

- **Live Ingestion** (`server/weather.go`, lines 42–91):
  - Polls Open-Meteo REST API every 10 minutes (`weatherPoll = 10 * time.Minute`) at building GPS coordinates (`WEATHER_LAT`, `WEATHER_LON`, defaulting to HCMC: 10.8231, 106.6297).
  - Fetches 2-meter air temperature (`temperature_2m`) and relative humidity (`relative_humidity_2m`).
  - Plausibility gate rejects values outside $[-40^\circ\text{C}, 55^\circ\text{C}]$ or $[0\%, 100\%]$ RH.
- **Resilience Fallback**:
  - If network or API fails, the envelope uses the climatological fallback ($30.0^\circ\text{C}$) via `outdoorNow()`.
  - Stale threshold: 3 hours (`outdoorStaleAfter = 3 * time.Hour`).

### 1.3 Audit of Constants & Engineering Library (`server/simulation/library.go`)

The engine separates physics laws from building parameters. Parameters are loaded from `data/programme-library.json` (or `ECON_PROGRAMME_LIBRARY` / `programme-library.local.json`):

| Parameter | Shipped Value | Physical Description | Location in Code |
|---|---|---|---|
| `AirRhoCpJPerM3K` | $1206.0\text{ J/(m}^3\cdot\text{K)}$ | Volumetric heat capacity of air | `library.go:151` |
| `FurnishingCapMultiplier` | $5.0$ | Thermal mass multiplier for room contents | `library.go:152` |
| `OccupantSensibleW` | $100.0\text{ W}$ | Sensible heat rejection per human occupant | `library.go:153` |
| `OutdoorAirLPerSPerPerson` | $10.0\text{ L/(s}\cdot\text{pers)}$ | Ventilation fresh-air requirement | `library.go:154` |
| `VentilationEnthalpyKjPerKg` | $55.0\text{ kJ/kg}$ | Tropical dehumidification & cooling enthalpy lift | `library.go:155` |
| `AirDensityKgPerM3` | $1.2\text{ kg/m}^3$ | Ambient air density | `library.go:156` |
| `DesignCop` | $3.6$ | Baseline nominal chiller plant COP | `library.go:158` |
| `SupplyAirDesignC` | $12.0^\circ\text{C}$ | Design HVAC supply discharge temperature | `library.go:159` |
| `SolarGainReferenceW` | $10000.0\text{ W}$ | Reference aperture solar thermal gain | `library.go:163` |
| `DaylightReferenceLux` | $1000.0\text{ lux}$ | Indoor daylight illuminance normalization reference | `library.go:164` |
| `CopStrainSlope` | $0.35\text{ /}^\circ\text{C}$ | Chiller COP degradation rate per degree of building strain | `library.go:165` |
| `CopMin`, `CopMax` | $2.2, 3.8$ | Physical bounds on operational chiller COP | `library.go:166` |
| `NonHvacBaseWPerM2` | $9.0\text{ W/m}^2$ | Lighting, pumps, fans, and elevators baseline | `library.go:160` |
| `SupplyAirDesignAch` | $6.0\text{ ACH}$ | Design air change rate for sizing VAV boxes | `library.go:172` |
| `AhuDesignPressurePa` | $480.0\text{ Pa}$ | Design duct static pressure for Hardy-Cross solver | `library.go:173` |
| `OutdoorCo2Ppm` | $400.0\text{ ppm}$ | Ambient atmospheric background CO₂ | `library.go:170` |
| `Co2PpmPerOccupantSteady`| $15.0\text{ ppm/pers}$ | Steady-state CO₂ rise per occupant per zone | `library.go:171` |

---

## 2. R2: Smart Fallbacks for Missing Sensors

When physical sensors are omitted, disconnected, or stale, the Go backend does **not** inject static mock numbers. Instead, it activates physics-derived estimation models:

### 2.1 Zone Air & Wall Temperature (2R1C Dynamic Thermal Model)

- **Source Code**: `server/simulation/engine.go` (lines 1818–1894), `server/simulation/dynamics.go`
- **When Live Sensor Present** (`z.hwFresh() == true` && `TempReal == true`):
  - Zone air temperature $T_{air}$ is exponentially pulled toward $HwTemp$ ($0.1$ blend per tick) to reflect physical reality (`engine.go:1098`).
  - The unpinned shadow twin (`ShadowTemp`) continues integrating pure 2R1C dynamics without sensor pull (`engine.go:1885–1893`).
  - The exponential moving average residual $EMA(|HwTemp - ShadowTemp|)$ computes the physics-grounded AFDD (Automated Fault Detection & Diagnostics) signal (`ResidualEma`).
- **When Sensor Missing / Stale** (`!z.hwFresh()`):
  - State integrates the coupled differential equations of the 2R1C equivalent circuit:
    $$\frac{dT_{air}}{dt} = \frac{T_{wall} - T_{air}}{R_{in} \cdot C_{air}} + \frac{Q_{internal} - Q_{cooling}}{C_{air}}$$
    $$\frac{dT_{wall}}{dt} = \frac{T_{outside} - T_{wall}}{R_{out} \cdot C_{wall}} - \frac{T_{wall} - T_{air}}{R_{in} \cdot C_{wall}}$$
  - **Dynamic Thermal Resistances**: $R_{in} = R_{wall} \cdot RInFraction$, $R_{out} = R_{wall} \cdot (1 - RInFraction)$.
  - **Dynamic Thermal Capacitances**: $C_{air} = \max(CAir_{digitized}, MinZoneCapacitance)$, $C_{wall} = 4.0 \times 10^6\text{ J/K}$.
  - **Sensible Heat Balance**:
    $$Q_{internal} = Q_{base} + (Occupancy \times 100\text{ W}) + Q_{solar}$$
    $$Q_{cooling} = \frac{\dot{V}_{vav}}{\dot{V}_{nominal}} \cdot Q_{nominalTotal} \cdot \left(\frac{T_{air} - T_{supply}}{T_{setpoint} - T_{supply}}\right)$$
    $$Q_{nominalTotal} = Q_{internalNominal} + \frac{T_{outside} - T_{setpoint}}{R_{in} + R_{out}}$$
  - **Numerical Stability**: Clamped to physically plausible bounds $[5.0^\circ\text{C}, 50.0^\circ\text{C}]$ to protect against degenerate geometries.

### 2.2 Occupancy (Diurnal Schedule & Geometry-Derived Capacity)

- **Source Code**: `server/simulation/engine.go` (lines 1668–1719), `server/simulation/library.go` (lines 331–378)
- **When Live Sensor Present** (`z.Live == true`):
  - Telemetry from computer vision (YOLO/ByteTrack) or edge mmWave/PIR sets `z.Occupancy = *m.Occupancy`.
- **When Sensor Missing / Stale**:
  - Derives dynamic scheduled occupancy based on space type and geometry:
    $$N_{design} = \text{round}\left(\frac{\text{AreaM2}}{\text{AreaPerOccupantM2}}\right)$$
    $$N_{scheduled} = \text{clamp}\left(0, N_{design}, \text{round}\left(N_{design} \cdot f_{profile}(hour) \cdot (1 + \mathcal{N}(0, \sigma_{jitter}))\right)\right)$$
  - Non-occupied space types (circulation, bathrooms, mechanical/plant/comms rooms, corridors) have $AreaPerOccupantM2 = \text{null}$ and evaluate to 0 occupants.
  - Generates realistic diurnal curves (e.g. office peaks 09:00–17:00 and empties overnight; residential peaks morning/evening).

### 2.3 Solar Irradiance / Daylight (Aperture & Illuminance Coupling)

- **Source Code**: `server/simulation/engine.go` (lines 1036–1045)
- **When Live Sensor Present** (`z.luxFresh() == true` && `z.HwLux > 0` && `!z.LightsOn`):
  - Physical lux from BH1750 ambient light sensor scales the solar thermal gain:
    $$Q_{solar} = SolarGainMult \cdot SolarGainReferenceW \cdot \min\left(4.0, \frac{HwLux}{DaylightReferenceLux}\right)$$
  - **Electric Luminaire Gating**: If electric lighting is ON (`z.LightsOn == true`), the lux reading is contaminated by indoor luminaires. The engine automatically ignores the contaminated lux to prevent double-counting lighting heat.
- **When Sensor Missing / Stale**:
  - Solar gain is calculated purely from façade aperture orientation:
    $$Q_{solar} = SolarGainMult \cdot SolarGainReferenceW$$
  - Interior zones without exterior exposure have $SolarGainMult = 0.0$, yielding $Q_{solar} = 0\text{ W}$.

### 2.4 Electrical Power & Chiller Plant COP (Thermodynamic Energy Balance)

- **Source Code**: `server/simulation/engine.go` (lines 2125–2276)
- **When Live AC Clamp Present** (`z.acFresh() == true` && `z.HwAcW > 0`):
  - SCT-013 current clamp measures true electrical draw ($meteredAcW$).
  - Measured COP is computed as real physical evidence:
    $$COP_{measured} = \frac{meteredCoolingW}{meteredAcW}$$
- **When AC Clamp Missing**:
  - Chiller plant COP is derived dynamically from whole-building thermal lift/strain:
    $$avgStrain = \frac{1}{N} \sum_{i=1}^N \max(0, T_{air, i} - T_{setpoint, i})$$
    $$COP_{plant} = \text{clamp}(CopMin, CopMax, DesignCop - CopStrainSlope \cdot avgStrain)$$
  - Total cooling electrical demand is calculated from thermodynamic enthalpy balance:
    $$\dot{Q}_{vent} = N_{totalOccupants} \cdot \left(\frac{10.0\text{ L/s}}{1000}\right) \cdot \rho_{air} \cdot \Delta h_{vent}$$
    $$\dot{W}_{cooling, electrical} = \frac{\dot{Q}_{unmetered}}{COP_{plant}} + \dot{W}_{meteredAc}$$
  - Non-HVAC base electricity scales dynamically with conditioned floor area:
    $$\dot{W}_{base} = \text{Area}_{conditioned} \cdot NonHvacBaseWPerM2 + \dot{W}_{plugTotal}$$

### 2.5 Plug Loads & APLC (Area-Scaled Standby + Active Load)

- **Source Code**: `server/simulation/plugs.go` (lines 104–123, 128–170)
- **When Live Plug Clamp Present** (`z.plugFresh() == true`):
  - SCT-013 clamp reading $HwPlugW$ directly provides live circuit power.
- **When Plug Clamp Missing**:
  - Sized from digitized polygon floor area: $P_{standby} = \text{AreaM2} \times 1.2\text{ W/m}^2$.
  - Dynamic active load adds $65\text{ W}$ per present occupant (coincidence-factored).
  - During after-hours vacancy, Automated Plug Load Control (APLC) sheds $70\%$ switchable standby:
    $$P_{total} = P_{standby} \cdot (1 - 0.70 \cdot \mathbf{1}_{shed}) + Occupancy \times 65\text{ W}$$
  - Critical zones (server rooms, comms, plant rooms) are excluded from shedding.

### 2.6 AC Supply Air Discharge Temperature

- **Source Code**: `server/simulation/engine.go` (lines 1019–1025)
- **When Live Supply Probe Present** (`z.supplyFresh() == true` && $HwSupplyC > 0$):
  - Uses DS18B20 louvre probe reading, provided $HwSupplyC < T_{setpoint} - 1.0^\circ\text{C}$.
- **When Probe Missing / Stale / Implausible**:
  - Uses engineering library design discharge temperature $SupplyAirDesignC$ ($12.0^\circ\text{C}$).
  - Safety check prevents division by zero in the cooling law: $\min(12.0, T_{setpoint} - 1.0^\circ\text{C})$.

### 2.7 Indoor Air Quality (CO₂ and Humidity)

- **Source Code**: `server/simulation/engine.go` (lines 1066–1086), `server/simulation/dynamics.go`
- **When Live NDIR CO₂ Sensor Present** (`z.co2Fresh() == true`):
  - Streamed over FlatBuffers and persisted to TimescaleDB.
  - Recursive Least Squares (RLS) identifies room air-change rate ($\phi_1 = ACH$) and occupant generation rate ($\phi_0$).
- **When CO₂ Sensor Missing**:
  - Whole-building average uses mass-balance steady-state estimation:
    $$CO2_{avg} = OutdoorCo2Ppm + Co2PpmPerOccupantSteady \cdot \frac{N_{totalOccupants}}{N_{zones}}$$
  - Per-zone FlatBuffers stream emits 0 (signaling unmetered to UI) to prevent displaying fabricated sensor readings.
- **Humidity**:
  - Live SHT30 sensor streams %RH. When omitted, zone emits 0 (unmetered), while envelope and LSTM forecaster use live outdoor humidity from Open-Meteo.

### 2.8 Ambient Airflow & Static Pressure (Hardy-Cross Duct Network Solver)

- **Source Code**: `server/simulation/engine.go` (lines 528–583)
- Sized physically from digitized zone volume: $V = \text{AreaM2} \cdot \text{Height}$.
- Design resistance: $R_{design} = \frac{AhuDesignPressurePa}{(V \cdot SupplyAirDesignAch / 3600)^2}$.
- Fan curve: $P_{ahu} = P_{max} - K_{fan} \cdot (\sum \dot{V})^2$.
- Hardy-Cross loop iterations solve real airflow $\dot{V} = \sqrt{\frac{P_{ahu}}{R_{vav}}}$ for every branch dynamically.

---

## 3. R3 / Acceptance Criterion 1: Go Test Suite Audit & Gap Analysis

### 3.1 Existing Go Test Coverage in `server/simulation/`

The test suite contains 12 dedicated test suites verifying physics-based logic:

1. `measured_test.go` (274 lines):
   - `TestSupplyProbeSupersedesDesignValue`: Validates fresh DS18B20 probe superseding design 12°C, stale fallback, and rejection of implausible readings ($\ge Setpoint - 1^\circ\text{C}$).
   - `TestRoomConditionCarriesMeasuredSupply`: Validates measured supply reaching RLS dynamics.
   - `TestRoomModelReportsSupplyProvenance`: Asserts provenance tracking of fits.
   - `TestDaylightScalesSolarGainOnlyWhenUncontaminated`: Asserts BH1750 scaling, electric light contamination suppression, stale fallback, and interior zero-aperture enforcement.
   - `TestMeasuredAcPowerReplacesModelledCop`: Asserts SCT-013 AC clamp replacing modelled COP, and stale clamp falling back to exact modelled baseline.
   - `TestNodeBindsToDigitizerZoneTypes`: Tests auto-binding to `cellular-office` and `open-office`.
   - `TestNodeBindsSomewhereRatherThanDroppingData`: Tests non-critical binding fallback.
2. `hardware_test.go` (516 lines):
   - `TestTempRealPinning`: Tests real hardware temperature pulling zone temp vs. fake simulation temp rejection.
   - `TestStalenessAndOfflineRelease`: Tests 20s staleness timeout and immediate LWT offline release back to 2R1C thermal model.
   - `TestShadowModelAfddResidual`: Validates 2R1C shadow model integration and AFDD residual EMA.
   - `TestPublishCommandAppliesState`: Validates manual operator overrides and latching.
3. `occupancy_test.go` (174 lines):
   - `TestDesignOccupancyFollowsAreaAndDensity`: Area-scaled capacity testing.
   - `TestProgrammesWithNoOccupantDensityHoldNobody`: Zero occupancy for non-occupied space types.
   - `TestOccupancyVariesOverTheDayAndReachesZero`: Diurnal profile progression and overnight vacancy.
   - `TestOccupancyScheduleNeverOverwritesAMeasurement`: Asserts `z.Live` immunity from schedule overwrites.
4. `plugs_test.go` (141 lines):
   - `TestPlugConfigArmed`: Work hours arming policy.
   - `TestPlugSweepShedAndRestore`: APLC vacancy shedding and occupancy restoration.
   - `TestPlugMeasurementWinsAndExpires`: SCT-013 clamp override and fallback to area-scaled standby.
5. `dynamics_test.go` (526 lines):
   - `TestDynamicsIdentifiesKnownRoom`: RLS identification of thermal time constant $\tau$ and cooling authority $\theta_1$.
   - `TestDynamicsIdentifiesAirChangeRate`: RLS identification of ventilation air-change rate (ACH).
   - `TestDynamicsPredictsActualCrossing`: Validates closed-form forward prediction of temperature limit breaches.
6. `state_provenance_test.go` (470 lines):
   - `TestLoadHistoryIsNotRestoredIntoAnotherBuilding`: BIM switching context isolation.
   - `TestGlobalBaselinesAreNotRestoredIntoAnotherBuilding`: BIM switching baseline clearing.
   - `TestVavsAreSizedFromTheirZoneNotTheZoneCount`: Sizing VAV resistance from zone volume across building sizes (house vs tower).
7. `bess_sizing_test.go` (92 lines):
   - `TestUndeclaredPackIsSizedToTheBuilding`: Dynamic sizing of battery storage to observed building peak load.
8. `baselines_test.go` (216 lines):
   - `TestBaselineLearnsAndScores`: EWMA baseline learning and standard fallback.
9. `autopilot_test.go` (2078 bytes):
   - AutoPilot toggle suspending and resuming autonomous setpoint actuation.
10. `site_test.go` (4540 bytes):
    - Site fingerprinting via network gateway hardware address.
11. `forecast_window_test.go` (2423 bytes):
    - Real rolling history window sampling for forecaster.
12. `protocol_stress_test.go` (13124 bytes):
    - Concurrent MQTT telemetry ingestion and WebSocket broadcast under load.

### 3.2 Gaps & Proposed Acceptance Criterion 1 Test Cases

To fully satisfy **Acceptance Criterion 1** (*"Go unit/integration tests are written and pass, explicitly asserting that when specific sensor inputs are omitted, the simulation engine calculates realistic derived values using physics models instead of falling back to static mock data"*), the following test cases should be formalized:

```
Proposed Test Suite: server/simulation/smart_fallback_test.go
├── TestOmission_AllSensorsOmitted_PurePhysicsSimulation
│   ├── Assert 2R1C thermal model integrates continuous temperature trajectory
│   ├── Assert scheduled occupancy dynamically follows diurnal profile
│   ├── Assert plant COP dynamically modulates with thermal strain
│   ├── Assert plug load computes from polygon area + scheduled occupants
│   ├── Assert solar gain computes from façade aperture multiplier
│   └── Assert AHU static pressure & VAV airflow solve via Hardy-Cross
├── TestOmission_SelectiveSensorMatrix
│   ├── Case A: Temperature omitted -> derived via 2R1C heat balance & weather
│   ├── Case B: Occupancy omitted -> derived via space density & diurnal curve
│   ├── Case C: Solar lux omitted -> derived via aperture multiplier & exterior area
│   ├── Case D: AC clamp omitted -> derived via strain-coupled COP & vent enthalpy
│   ├── Case E: Plug clamp omitted -> derived via area standby & occupant count
│   ├── Case F: Supply probe omitted -> derived via design supply temperature
│   └── Case G: CO2 omitted -> avgCo2 derived via occupant mass balance
├── TestOmission_BIMSwitching_DynamicPhysicsRecomputation
│   ├── Switch from Office (bldg-office, 735 zones) to House (bldg-house, 2 zones)
│   ├── Assert VAV flows scale down to residential volumes (~0.05 m³/s vs ~0.5 m³/s)
│   ├── Assert AHU fan curve PMax rescales to house duct network
│   ├── Assert total building load scales down (~15 kW vs ~1.5 MW)
│   └── Assert BESS sizes to house load without zero-grid-draw artifact
└── TestOmission_SensorDropoutAndRecoveryDynamics
    ├── Feed live sensor data (T=28°C, Occ=5, AcW=2500W, Lux=800)
    ├── Simulate sensor dropout (>20s silent)
    ├── Assert graceful decay to 2R1C thermal physics and modelled power
    ├── Reconnect sensor
    └── Assert smooth re-pinning without state discontinuity
```

---

## 4. Synthesis of Findings & BIM Context Switching

### 4.1 BIM Model Switching Mechanics

When switching BIM models via `/api/building` (or `ReloadBuilding(data []byte)`):
1. **Critical Section Atomicity**: Swap executes under `e.mu.Lock()`.
2. **State Purging**:
   - `e.Zones` and `e.Vavs` completely rebuilt from new geometry.
   - `e.loadHist` discarded to prevent cross-building foundation model contamination (`engine.go:483–487`).
   - `e.baselines.DropGlobal()` drops whole-building learned baselines (`engine.go:498–501`).
   - `e.demoAssign` and `e.lastCmd` reset so hardware nodes rebind cleanly.
   - `e.dynamics.RetainZones(newZoneSet)` purges orphaned room models (`engine.go:408`).
3. **Hardware & Sizing Recomputation**:
   - `e.sizeFanToBuilding()` recomputes fan curve ($P_{max}$) for new duct network.
   - `e.doHardyCross()` balances loop pressure drops for new VAV box resistances.
   - `e.Bess.SizeToBuilding()` resizes battery capacity to new peak load.

### 4.2 Frontend / Backend Contract Alignment

To ensure seamless end-to-end integration:
- The backend FlatBuffers binary stream (`Telemetry.fbs`) carries:
  - `supplyReal` bool indicating whether `supplyC` was measured or design.
  - `AhuPressurePa` computed from Hardy-Cross solver (replaces frontend 500 Pa placeholder).
  - `plugKw`, `plugStandbyKw`, `plugShedKw`, `plugSavedKwh` live plug telemetry.
  - `hwHum`, `hwCo2` emitting 0 when unmetered so frontend displays unmetered badges rather than mock readings.
- `/api/library` serves building-specific parameters so frontend never relies on hardcoded JavaScript constants.

---

## 5. Conclusion & Recommendations

1. **Backend Readiness**: The Go physics engine already possesses all core equations and logic needed to satisfy R1, R2, and R3. No static mock data is used in the simulation engine; fallbacks are strictly grounded in 2R1C thermal differential equations, mass balances, Hardy-Cross duct solves, and data-driven programme coefficients.
2. **Action Item for Implementation Phase**: Create `server/simulation/smart_fallback_test.go` encapsulating the comprehensive sensor omission test matrix (AC1) to formally verify all sensor dropouts and BIM switching physics under CI.
