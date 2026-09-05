# Technical Report: Sensor Ingestion, Physics-Based Estimation & Sensor Fallbacks

> **Target**: Requirement R2 & Acceptance Criterion 1 (Authoritative user request: `ORIGINAL_REQUEST.md`)  
> **Subagent**: `explorer_physics_engine` (`.agents/explorer_m2_1/`)  
> **Date**: August 2026  
> **Workspace**: `/Users/nguyenhoangkhoi/Documents/econ`

---

## 1. Executive Summary

Requirement **R2** mandates:
> *"When a specific physical sensor is unavailable, do not use static mock data. Instead, leverage the Go backend's physics and simulation engine to estimate and derive realistic values based on the data from the sensors that are available."*

Acceptance Criterion **AC1** requires:
> *"Go unit/integration tests are written and pass, explicitly asserting that when specific sensor inputs are omitted, the simulation engine calculates realistic derived values using physics models instead of falling back to static mock data."*

An exhaustive audit of the Go backend (`server/` and `server/simulation/`) was performed. The engine currently implements per-field sensor freshness tracking, but relies on several **static constants, empirical linear heuristics, or flat fallback values** when physical sensors are missing or offline.

This report:
1. Details how sensor telemetry is ingested, validated, and timestamped across the backend.
2. Identifies all static mock fallbacks, hardcoded defaults, and empirical constants.
3. Formulates exact, first-principles **physics-based estimation equations** (solar geometry, thermodynamic Carnot/Gordon-Ng chiller COP, multi-zone 2R1C inter-zone coupling, dynamic supply air enthalpy, and CO₂/moisture mass balances).
4. Delivers a complete **Go unit/integration test suite design** that verifies dynamic physics calculations when sensor inputs are omitted.

---

## 2. Sensor Ingestion & Omission Handling Architecture

### 2.1 Ingestion Paths & Data Flow

Sensor telemetry enters the backend through two distinct ingress pathways:

```
[Physical Edge Nodes (ESP32 / Pico / CV)]
       │ (JSON via MQTT: econ/telemetry/+, econ/status/+)
       ▼
  server/mqtt.go ─── handleTelemetry() ───► telemetryMsg (pointers for nil detection)
       │
       ▼
  server/simulation/engine.go ─── IngestTelemetry(zoneRef, topicSuffix, Measurement)
       │
       ├──► per-field freshness timestamps (HwTempAt, HwHumAt, HwCo2At, HwPlugAt, HwSupplyAt, HwAcAt, HwLuxAt)
       ├──► provenance flags (TempReal, AcReal, CfgRev)
       └──► LWT broker status (econ/status/<topic> -> SetNodeStatus)

[External Open-Meteo REST API]
       │ (HTTP GET every 10 min)
       ▼
  server/weather.go ─── weatherLoop() ───► engine.SetOutdoor(temp, hum)
       │
       ▼
  engine.outdoorNow() ───► outdoorStaleAfter (3 hours) ───► outdoorFallbackC (30.0°C)
```

### 2.2 Freshness and Omission Discipline

In `server/simulation/engine.go`, sensor presence is tracked per-field rather than per-node:
- `hwStaleAfter = 20 * time.Second`: Nodes publish at 2–5 s intervals. If an individual sensor on a shared I2C bus fails (e.g. SHT30 humidity reports while SCD30 CO₂ fails CRC), only the failing field becomes stale.
- Pointer fields (`*float64`, `*int`, `*bool`) in `Measurement` differentiate between **omitted (`nil`)** vs **zero (`0.0`)**.
- `tempReal` ensures firmware simulated placeholders never pin the 2R1C thermal model.
- LWT (`econ/status/<topic>`) triggers `SetNodeStatus(topicSuffix, false)` upon disconnect, instantly resetting sensor arrival timestamps to `time.Time{}`.

---

## 3. Audit of Static Mocks, Hardcoded Defaults & Heuristics

The table below catalogs every static mock fallback or constant identified across `server/` and `server/simulation/`:

| Subsystem / Quantity | Code Location | Current Fallback / Mock Behavior | Physics Deficiency / Impact |
|---|---|---|---|
| **Outdoor Weather** | `engine.go:933`<br>`weather.go:35-65` | `outdoorFallbackC = 30.0` (static flat 30.0°C). Flat `humidity = 0`. | When Open-Meteo is offline, outdoor air temperature is a static flat line, ignoring day/night diurnal cycles, geographic solar angle, and local diurnal thermal swings (24°C–35°C). |
| **Solar Irradiance / Gain** | `engine.go:1036-1044`<br>`programme-library.json:68-80` | `SolarGainMult * SolarGainReferenceW` (`SolarGainReferenceW = 10000.0 W`). | When `HwLux` sensor is omitted, solar gain is a **static constant multiplier** ($10,000\text{ W} \times \text{mult}$) 24 hours a day. At midnight, rooms with windows still receive thousands of watts of solar heat gain! |
| **Chiller COP & AC Power** | `engine.go:2227`<br>`programme-library.json:83-88` | `plantCop = DesignCop - CopStrainSlope * avgStrain` (`DesignCop = 3.6`, `CopStrainSlope = 0.35`). | When `HwAcW` clamp is omitted, AC electrical draw uses an empirical linear heuristic based on room comfort strain instead of thermodynamic lift ($\Delta T_{\text{lift}} = T_{\text{condenser}} - T_{\text{evaporator}}$), part-load ratio (PLR), and compressor isentropic efficiency. |
| **Supply Air Temp ($T_{\text{supply}}$)** | `engine.go:1019-1025`<br>`programme-library.json:91` | `Phys().SupplyAirDesignC` (fixed default `12.0°C`). | When DS18B20 supply probe is missing, returns static 12.0°C regardless of cooling coil thermal load, chilled water temperature, mixed air temperature, or fan airflow rate. |
| **Zone Indoor Temp ($T_{\text{zone}}$)** | `engine.go:1868-1873` | Single-envelope 2R1C: $\frac{dT_{\text{air}}}{dt} = \frac{T_{\text{wall}}-T_{\text{air}}}{R_{\text{in}} C_{\text{air}}} + \frac{q_i - q_c}{C_{\text{air}}}$. | Assumes all partition walls conduct directly to $T_{\text{outside}}$ via $R_{\text{out}}$. Completely ignores **inter-zone conductive heat exchange** between adjacent conditioned and unconditioned zones ($T_{\text{adj}}$). |
| **Plug Power & Standby** | `plugs.go:111-123` | $\text{total} = \text{standby} + \text{Occ} \times 65\text{ W}$, where $\text{standby} = \text{Area} \times 1.2\text{ W/m}^2$. | Fixed static 65 W/occupant and static 1.2 W/m² standby. Does not account for equipment operating states, workstation power management, or time-of-day diversity profiles. |
| **Zone & Global CO₂** | `engine.go:1084-1086` | $\text{avgCo2} = 400 + 15 \times \frac{\text{totalOccupants}}{\text{len(Zones)}}$. Zone `HwCo2` sets to 0.0 when offline. | Static steady-state formula. Ignores dynamic mass-balance accumulation ($\frac{dC}{dt}$), room volume, and mechanical ventilation air changes (ACH). |
| **Zone Humidity** | `engine.go:2430-2436` | `HwHum` sets to 0.0 (omitted from telemetry). | Sensible-only thermal model; latent occupant moisture generation ($\approx 50\text{ g/h/person}$) and ventilation humidity balance are completely unmodeled when sensor is offline. |

---

## 4. Smart Physics-Based Estimation Formulations

To replace static mocks with dynamic physics, the following formulations must be implemented in the Go simulation engine:

### 4.1 Solar Geometry & Diurnal Irradiance Estimation
When the `HwLux` ambient illuminance sensor is offline or omitted, solar radiation must be derived from solar position geometry (Spencer / NOAA algorithm) and clear-sky irradiance models.

1. **Fractional Year Angle ($\gamma$)**:
   $$\gamma = \frac{2\pi}{365} \left(d_n - 1 + \frac{t_{\text{UTC}}}{24}\right) \quad \text{rad}$$
   where $d_n \in [1, 365]$ is day of year, $t_{\text{UTC}}$ is UTC decimal hour.

2. **Equation of Time ($\text{EoT}$) & Solar Declination ($\delta$)**:
   $$\text{EoT} = 229.18 \times (0.000075 + 0.001868 \cos\gamma - 0.032077 \sin\gamma - 0.014615 \cos(2\gamma) - 0.040849 \sin(2\gamma)) \quad \text{min}$$
   $$\delta = 0.006918 - 0.399912 \cos\gamma + 0.070257 \sin\gamma - 0.006758 \cos(2\gamma) + 0.000907 \sin(2\gamma) - 0.002697 \cos(3\gamma) + 0.00148 \sin(3\gamma) \quad \text{rad}$$

3. **Solar Time ($t_{\text{solar}}$) & Hour Angle ($\omega$)**:
   $$t_{\text{solar}} = t_{\text{local}} + \frac{4(\text{Lon} - \text{Lon}_{\text{tz}}) + \text{EoT}}{60} \quad \text{hours}$$
   $$\omega = 15^\circ \times (t_{\text{solar}} - 12) \quad \text{deg}$$

4. **Solar Zenith Angle ($\theta_z$)**:
   $$\cos\theta_z = \sin\phi \sin\delta + \cos\phi \cos\delta \cos\omega$$
   where $\phi = \text{SiteLat()}$ ($10.8231^\circ\text{ N}$ for HCMC).

5. **Clear-Sky Direct & Global Horizontal Irradiance ($I_{\text{GHI}}$)**:
   $$I_{\text{sc}} = 1361.0\text{ W/m}^2$$
   $$I_{\text{DNI}} = \begin{cases} I_{\text{sc}} \left(1 + 0.033 \cos\frac{2\pi d_n}{365}\right) \exp\left(-\frac{0.18}{\max(0.01, \cos\theta_z)}\right), & \text{if } \cos\theta_z > 0 \\ 0, & \text{if } \cos\theta_z \le 0 \end{cases}$$
   $$I_{\text{GHI}} = I_{\text{DNI}} \cos\theta_z + 0.1 \times I_{\text{DNI}} \cos\theta_z$$

6. **Dynamic Zone Solar Heat Gain ($q_{\text{solar}}$)**:
   $$q_{\text{solar}}(t) = \text{SolarGainMult} \times A_{\text{facade}} \times \text{SHGC} \times I_{\text{GHI}}(t)$$
   - At solar midnight ($\cos\theta_z \le 0$): $q_{\text{solar}} \equiv 0.0\text{ W}$.
   - At solar noon: peaks dynamically based on seasonal sun angle.

---

### 4.2 Multi-Zone Coupled 2R1C Thermal Network
When zone temperature sensors are offline, the zone thermal state integrates against adjacent room temperatures as well as the exterior envelope:

1. **Inter-Zone Partition Heat Flux**:
   $$q_{\text{interzone}, i} = \sum_{j \in \text{Adj}(i)} \frac{T_{\text{air}, j} - T_{\text{air}, i}}{R_{\text{partition}, ij}}$$
   where $R_{\text{partition}, ij} = \frac{R_{\text{wall, int}}}{A_{\text{shared}, ij}}$.

2. **Infiltration & Fresh Air Conductive Load**:
   $$q_{\text{infil}, i} = \dot{m}_{\text{infil}, i} c_p (T_{\text{outdoor}} - T_{\text{air}, i})$$

3. **Governing ODE System**:
   $$\frac{dT_{\text{air}, i}}{dt} = \frac{T_{\text{wall}, i} - T_{\text{air}, i}}{R_{\text{in}, i} C_{\text{air}, i}} + \frac{q_{\text{interzone}, i} + q_{\text{infil}, i} + q_{\text{internal}, i} - q_{\text{cooling}, i}}{C_{\text{air}, i}}$$
   $$\frac{dT_{\text{wall}, i}}{dt} = \frac{T_{\text{outside}} - T_{\text{wall}, i}}{R_{\text{out}, i} C_{\text{wall}, i}} - \frac{T_{\text{wall}, i} - T_{\text{air}, i}}{R_{\text{in}, i} C_{\text{wall}, i}} + \frac{q_{\text{solar, abs}, i}}{C_{\text{wall}, i}}$$

---

### 4.3 Thermodynamic Chiller Plant & AC Power Model (Carnot / Gordon-Ng)
When AC power current clamps (`HwAcW`) are omitted:

1. **Thermodynamic Temperature Lift**:
   - Condenser saturation temperature: $T_{\text{cond}} = T_{\text{outdoor}} + \Delta T_{\text{cond, approach}}$ ($\Delta T_{\text{cond, approach}} \approx 5.0\text{ K}$).
   - Evaporator saturation temperature: $T_{\text{evap}} = T_{\text{supply}} - \Delta T_{\text{evap, approach}}$ ($\Delta T_{\text{evap, approach}} \approx 3.0\text{ K}$).
   - Kelvin temperatures: $T_{\text{cond, K}} = T_{\text{cond}} + 273.15$, $T_{\text{evap, K}} = T_{\text{evap}} + 273.15$.

2. **Carnot Thermodynamic Limit & Actual COP**:
   $$\text{COP}_{\text{Carnot}} = \frac{T_{\text{evap, K}}}{T_{\text{cond, K}} - T_{\text{evap, K}}}$$
   $$\text{COP}_{\text{actual}} = \eta_{\text{chiller}} \times \text{COP}_{\text{Carnot}} \times f(\text{PLR})$$
   where:
   - Second-law efficiency $\eta_{\text{chiller}} \approx 0.55$.
   - Part-Load Ratio $\text{PLR} = \frac{\dot{Q}_{\text{thermal}}}{\dot{Q}_{\text{rated}}}$.
   - Part-load curve: $f(\text{PLR}) = 0.15 + 1.25\,\text{PLR} - 0.40\,\text{PLR}^2$.

3. **Derived AC Electrical Power**:
   $$\hat{P}_{\text{electrical, AC}} = \frac{\dot{Q}_{\text{thermal, delivered}}}{\text{COP}_{\text{actual}}}$$

---

### 4.4 Dynamic Supply Air Temperature ($T_{\text{supply}}$)
When the DS18B20 discharge probe is omitted:

1. **Mixed Air Enthalpy / Temperature**:
   $$T_{\text{mixed}} = \alpha_{\text{fresh}} T_{\text{outdoor}} + (1 - \alpha_{\text{fresh}}) T_{\text{return}}$$
   where $\alpha_{\text{fresh}} = \frac{\dot{V}_{\text{outdoorAir}}}{\dot{V}_{\text{supplyTotal}}}$, and $T_{\text{return}} = \frac{\sum (VAV_{\text{flow}, i} \cdot T_{\text{air}, i})}{\sum VAV_{\text{flow}, i}}$.

2. **Cooling Coil Heat Exchanger Effectiveness ($\epsilon_{\text{coil}}$)**:
   $$\hat{T}_{\text{supply}} = T_{\text{mixed}} - \epsilon_{\text{coil}} (T_{\text{mixed}} - T_{\text{cw, in}})$$
   with chilled water supply $T_{\text{cw, in}} \approx 7.0^\circ\text{C}$ and $\epsilon_{\text{coil}} \approx 0.75 - 0.85$.

---

### 4.5 Dynamic CO₂ & Moisture Mass Balance (with Virtual Occupancy Inversion)
1. **Dynamic CO₂ Mass Balance**:
   $$\frac{dC_{\text{zone}}(t)}{dt} = \frac{\dot{V}_{\text{vent}}}{V_{\text{zone}}} (C_{\text{outdoor}} - C_{\text{zone}}(t)) + \frac{G_{\text{occ}} \cdot N_{\text{occ}}}{V_{\text{zone}}}$$
   where $G_{\text{occ}} = 0.005\text{ L/s/person} = 18\text{ L/h/person}$.
   Analytic integration over time step $\Delta t$:
   $$C(t + \Delta t) = C_{\text{ss}} + (C(t) - C_{\text{ss}}) \exp\left(-\frac{\dot{V}_{\text{vent}}}{V_{\text{zone}}} \Delta t\right), \quad C_{\text{ss}} = C_{\text{outdoor}} + \frac{G_{\text{occ}} \cdot N_{\text{occ}}}{\dot{V}_{\text{vent}}}$$

2. **Virtual Occupancy Estimation (Inverse Physics)**:
   When PIR/CV occupancy sensor is missing, but an NDIR CO₂ sensor $C(t)$ is reporting:
   $$\hat{N}_{\text{occ}} = \text{round}\left(\frac{V_{\text{zone}} \frac{\Delta C}{\Delta t} + \dot{V}_{\text{vent}} (C(t) - C_{\text{outdoor}})}{G_{\text{occ}}}\right)$$
   bounded to $[0, N_{\text{design}}]$.

3. **Latent Moisture & Relative Humidity Balance**:
   $$\rho_{\text{air}} V_{\text{zone}} \frac{dw_{\text{zone}}}{dt} = \dot{m}_{\text{vent}} (w_{\text{supply}} - w_{\text{zone}}) + \dot{m}_{\text{infil}} (w_{\text{outdoor}} - w_{\text{zone}}) + \dot{m}_{\text{vap, occ}} \cdot N_{\text{occ}}$$
   $$\text{RH}_{\text{zone}} = \frac{p_v(w_{\text{zone}}, P_{\text{atm}})}{p_{\text{sat}}(T_{\text{air}})}$$

---

### 4.6 Climatological Diurnal Weather Fallback
When external Open-Meteo weather feed is stale (>3 hours):
$$T_{\text{outdoor}}(t) = T_{\text{mean}} + \Delta T_{\text{diurnal}} \cos\left(\frac{2\pi (t_{\text{hour}} - 15)}{24}\right)$$
$$\text{RH}_{\text{outdoor}}(t) = \text{RH}_{\text{mean}} - \Delta \text{RH}_{\text{diurnal}} \cos\left(\frac{2\pi (t_{\text{hour}} - 15)}{24}\right)$$
For Ho Chi Minh City: $T_{\text{mean}} = 29.5^\circ\text{C}$, $\Delta T_{\text{diurnal}} = 4.5^\circ\text{C}$ (min 25.0°C at 03:00, max 34.0°C at 15:00); $\text{RH}_{\text{mean}} = 75\%$, $\Delta \text{RH}_{\text{diurnal}} = 20\%$.

---

## 5. Go Unit & Integration Test Suite Design (Acceptance Criterion 1)

To strictly fulfill Acceptance Criterion 1, a dedicated Go test suite is designed for `server/simulation/`. The test suite covers both fine-grained unit physics calculations and macro end-to-end telemetry omission scenarios.

### 5.1 Test Suite Structure

File: `server/simulation/sensor_fallback_test.go` (and `server/simulation/sensor_fallback_integration_test.go`)

```
server/simulation/
├── sensor_fallback_test.go             # Unit tests for physical models upon sensor omission
└── sensor_fallback_integration_test.go # End-to-end engine integration tests with partial telemetry
```

### 5.2 Concrete Test Cases & Specification

#### 1. `TestOutdoorWeatherFallbackDynamicVsStatic(t *testing.T)`
- **Objective**: Assert that when outdoor weather API is omitted or offline, outdoor temperature and humidity dynamically follow diurnal physics rather than a static 30.0°C flatline.
- **Method**:
  1. Call `e.SetOutdoorTemp` with stale timestamp (>3 hours) to trigger fallback mode.
  2. Sample `outdoorNow()` at simulated times 03:00 (dawn minimum) and 15:00 (afternoon peak).
  3. Assert $T_{\text{outdoor}}(15:00) > T_{\text{outdoor}}(03:00)$ with $\Delta T \ge 6.0^\circ\text{C}$.
  4. Assert that $T_{\text{outdoor}}$ never equals a frozen static value across the 24-hour cycle.

#### 2. `TestSolarGainDynamicPhysicsWhenLuxSensorOmitted(t *testing.T)`
- **Objective**: Assert that when `HwLux` sensor is omitted, solar gain is dynamically calculated from solar geometry (zenith angle $\theta_z$, time of day, day of year).
- **Method**:
  1. Initialize zone with aperture (`SolarGainMult = 1.0`, `HwLuxAt` zero).
  2. Evaluate `solarGainW()` at midnight ($t_{\text{local}} = 00:00$). Assert $q_{\text{solar}} == 0.0\text{ W}$ (rejecting the old 10,000 W static fallback).
  3. Evaluate `solarGainW()` at solar noon ($t_{\text{local}} = 12:00$). Assert $q_{\text{solar}} > 500.0\text{ W}$.
  4. Compare noon solar gain on summer solstice ($d_n = 172$) vs winter solstice ($d_n = 355$) to verify seasonal solar geometry.

#### 3. `TestCopAndCoolingPowerPhysicsWhenAcClampOmitted(t *testing.T)`
- **Objective**: Assert that when $HwAcW$ (AC clamp) is omitted, chiller electrical power and COP are computed dynamically from thermodynamic lift ($T_{\text{condenser}} - T_{\text{evaporator}}$) and thermal load.
- **Method**:
  1. Set identical thermal cooling load $Q_{\text{thermal}} = 100\text{ kW}$ across two scenarios.
  2. Scenario A: Mild ambient $T_{\text{outdoor}} = 25^\circ\text{C}$.
  3. Scenario B: Extreme ambient $T_{\text{outdoor}} = 38^\circ\text{C}$.
  4. Compute `plantCop` and derived `coolingElectricalMW` for both scenarios.
  5. Assert $\text{COP}(25^\circ\text{C}) > \text{COP}(38^\circ\text{C})$ by at least $25\%$.
  6. Assert $P_{\text{electrical}}(38^\circ\text{C}) > P_{\text{electrical}}(25^\circ\text{C})$, proving thermodynamic Carnot lift coupling.

#### 4. `TestSupplyAirTemperaturePhysicsWhenProbeOmitted(t *testing.T)`
- **Objective**: Assert that when DS18B20 supply probe is omitted, supply air temperature is calculated from mixed air enthalpy and cooling coil heat exchange balance rather than returning a flat static 12.0°C.
- **Method**:
  1. Omit $HwSupplyC$ probe (`HwSupplyAt` zero).
  2. Vary mixed air temperature by changing outdoor temperature from 26°C to 38°C and zone return temperatures.
  3. Assert derived supply air temperature dynamically rises with high coil thermal loading and falls under light load, remaining within physical limits ($10^\circ\text{C} \le T_{\text{supply}} \le 16^\circ\text{C}$).

#### 5. `TestZoneThermalCouplingWhenTemperatureSensorOmitted(t *testing.T)`
- **Objective**: Assert that when physical temperature sensors are omitted, zone temperatures evolve via multi-zone 2R1C thermal differential equations, dynamically responding to adjacent zone temperatures, envelope conduction, solar flux, and occupancy heat gains.
- **Method**:
  1. Set Zone A unpinned (`HwTempAt` zero) at 24°C.
  2. Heat adjacent Zone B to 32°C while keeping exterior ambient at 24°C.
  3. Integrate thermal steps for 30 minutes.
  4. Assert Zone A temperature rises due to inter-zone partition conduction $q_{\text{interzone}}$, verifying multi-zone spatial coupling.

#### 6. `TestCO2MassBalanceWhenNdirSensorOmitted(t *testing.T)`
- **Objective**: Assert that when NDIR CO2 sensor is omitted, zone and building CO2 concentrations dynamically integrate generation rate ($G \cdot N_{\text{occ}}$) and fresh air air-changes over time.
- **Method**:
  1. Place 10 occupants in a $100\text{ m}^3$ room with 2.0 ACH ventilation. Omit CO2 sensor.
  2. Integrate mass balance ODE for 1 hour.
  3. Assert CO2 concentration smoothly ascends from 400 ppm toward the analytic equilibrium $C_{\text{ss}} = 400 + \frac{10 \times 18}{200} \times 10^6 \approx 1300\text{ ppm}$.
  4. Empty room to 0 occupants; assert exponential decay back toward 400 ppm.

#### 7. `TestVirtualOccupancyEstimationFromCO2Dynamics(t *testing.T)`
- **Objective**: Assert that when CV/PIR occupancy sensors are offline but CO2 is reporting, the engine can invert the mass-balance differential equation to infer real occupant headcount.
- **Method**:
  1. Feed a known CO2 rising slope $\Delta C / \Delta t$ corresponding to 5 occupants into a known volume and airflow rate.
  2. Run virtual occupancy estimator.
  3. Assert inferred occupancy $\hat{N}_{\text{occ}} == 5$.

#### 8. `TestFullSensorOmissionIntegration(t *testing.T)`
- **Objective**: End-to-end integration test verifying that omitting multiple sensors simultaneously triggers stable, dynamic physics calculations across all global and zone telemetry streams.
- **Method**:
  1. Initialize `Engine` with multi-floor building fixture.
  2. Ingest sparse telemetry (e.g. only 1 of 50 zones reporting hardware temp, no AC clamps, no lux sensors, no weather API).
  3. Advance simulation engine for 100 ticks.
  4. Verify FlatBuffers stream and TimescaleDB persistence:
     - No NaN or $\pm\infty$ values.
     - `buildingLoadMw`, `coolingOutputMw`, `plantCop`, `systemHealth`, `avgCo2` all fluctuate continuously according to diurnal physics and internal thermodynamic equations.

---

## 6. Recommendations for Implementer Agent (M2 Milestone)

1. **File Modifications**:
   - `server/simulation/engine.go`:
     - Implement `solarGeometryGain(z *ZoneSim, now time.Time) float64` in place of static `SolarGainReferenceW * SolarGainMult`.
     - Implement thermodynamic `calculateThermodynamicCop(tOut, tSupply, thermalLoadW float64) float64` for unmetered cooling electrical load.
     - Add inter-zone thermal coupling term $\sum \frac{T_j - T_i}{R_{ij}}$ in `tick()`.
     - Enhance `avgCo2()` and zone CO2 telemetry to integrate dynamic mass-balance differential equations.
   - `server/weather.go`:
     - Update `outdoorNow()` fallback to compute diurnal sinusoidal curve from local time and site coordinates instead of returning flat 30.0°C.
   - `server/simulation/library.go` & `data/programme-library.json`:
     - Deprecate static `solarGainReferenceW` literal; replace with façade area-scaled irradiance coefficient.
2. **New Test Suite**:
   - Implement `server/simulation/sensor_fallback_test.go` and `server/simulation/sensor_fallback_integration_test.go` matching the specification in Section 5.

---
