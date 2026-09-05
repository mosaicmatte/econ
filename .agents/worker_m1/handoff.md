# Milestone 1 Handoff Report: Smart Physics Fallbacks & Sensor Omission Tests

**Worker**: `worker_m1`  
**Target Requirements**: R1, R2, Acceptance Criterion 1 (`ORIGINAL_REQUEST.md` lines 21–45)  
**Date**: August 31, 2026  
**Workspace**: `/Users/nguyenhoangkhoi/Documents/econ`

---

## 1. Observation

1. **Static Flat Fallbacks Prior to Modification**:
   - `server/simulation/engine.go` (line 933) and `server/weather.go` (line 35) used a static constant `outdoorFallbackC = 30.0` and flat `humidity = 0`, ignoring diurnal day/night cycles when the Open-Meteo weather API was offline or stale.
   - `server/simulation/engine.go` (line 1038) computed solar gain via `z.SolarGainMult * ph.SolarGainReferenceW` (10,000 W) statically 24/7 when `HwLux` sensor was omitted, delivering thousands of watts of solar thermal gain into windowed rooms at solar midnight.
   - `server/simulation/engine.go` (line 2227) calculated chiller COP from an empirical linear equation (`DesignCop - CopStrainSlope*avgStrain`) without accounting for thermodynamic temperature lift ($\Delta T_{\text{lift}} = T_{\text{condenser}} - T_{\text{evaporator}}$), the Carnot limit, or part-load ratios.
   - `server/simulation/engine.go` (line 1019) evaluated supply air temperature using fixed `SupplyAirDesignC` ($12.0^\circ\text{C}$) when DS18B20 supply probes were omitted, ignoring mixed return/outdoor air temperature and cooling coil thermal loads.
   - Zone thermal ODE integration (line 1868) assumed all walls conduct directly to outside ambient, neglecting inter-zone conductive heat transfer between adjacent conditioned/unconditioned spaces.
   - Whole-building CO₂ (line 1085) used a flat steady-state formula without dynamic time-integrated mass balance accumulation.

2. **Files Added and Modified**:
   - `server/simulation/solar.go`: Added Spencer (1971) / NOAA astronomical solar position calculation and Meinel/ASHRAE clear-sky Global Horizontal Irradiance (GHI) modeling.
   - `server/simulation/engine.go`:
     - Implemented `OutdoorFallbackAt(t time.Time) (tempC, humPct float64)` replacing flat 30.0°C with diurnal sinusoidal curves (25.0°C–34.0°C temperature swing, 55%–95% RH swing).
     - Implemented `solarGainWAt(now time.Time)` computing dynamic solar gain from sun position (strictly 0.0 W at solar midnight, peaking at solar noon).
     - Implemented `CalculateThermodynamicCop(...)` deriving chiller COP from thermodynamic lift, Carnot limit, part-load ratio, and thermal strain.
     - Implemented `calculateDynamicSupplyAir(tOutside float64)` calculating discharge temperature from mixed air ($0.15 T_{\text{outdoor}} + 0.85 T_{\text{return}}$) and cooling coil heat exchange balance.
     - Implemented multi-zone inter-zone partition conductive heat transfer $\sum \frac{T_j - T_i}{R_{\text{partition}}}$ and dynamic CO₂ mass balance ODE integration $\frac{dC}{dt} = \frac{\dot{V}}{V}(C_{\text{out}} - C) + \frac{G \cdot N}{V}$.
   - `server/weather.go`: Delegated coordinate resolution to `simulation.SiteLat()` and `simulation.SiteLon()`.
   - `server/simulation/hardware_test.go` & `server/simulation/measured_test.go`: Updated tests to assert dynamic physics fallbacks.
   - `server/simulation/sensor_fallback_test.go` & `server/simulation/sensor_fallback_integration_test.go`: Added 10 comprehensive unit and integration test cases covering all sensor omission scenarios.

---

## 2. Logic Chain

1. **Solar Geometry (Requirement R2.a)**:
   - For site coordinates ($10.8231^\circ\text{ N}, 106.6297^\circ\text{ E}$), `SolarPosition(t, lat, lon)` calculates fractional year angle $\gamma$, Equation of Time $\text{EoT}$, solar declination $\delta$, solar time $t_{\text{solar}}$, hour angle $\omega$, and cosine of zenith angle $\cos\theta_z = \sin\phi \sin\delta + \cos\phi \cos\delta \cos\omega$.
   - When $\cos\theta_z \le 0$ (night / solar midnight), direct and global irradiance are strictly $0.0\text{ W/m}^2$, yielding $q_{\text{solar}} \equiv 0.0\text{ W}$.
   - At solar noon, $\cos\theta_z \approx 0.99$, direct normal irradiance $I_{\text{DNI}} \approx 1130\text{ W/m}^2$, and $I_{\text{GHI}} \approx 1228\text{ W/m}^2$, giving realistic seasonal solar heat gain.

2. **Climatological Diurnal Weather (Requirement R2.b)**:
   - When external Open-Meteo weather is stale (>3 hours) or uninitialized, $T_{\text{outdoor}}(t) = 29.5 + 4.5\cos(2\pi(t_{\text{hour}}-15)/24)$ and $\text{RH}(t) = 75.0 - 20.0\cos(2\pi(t_{\text{hour}}-15)/24)$.
   - Reaches maximum $34.0^\circ\text{C}$ (55% RH) at 15:00 and minimum $25.0^\circ\text{C}$ (95% RH) at 03:00, preventing static flatlines during network outages.

3. **Thermodynamic Chiller COP & Power (Requirement R2.c)**:
   - Evaluates condenser temperature $T_{\text{cond}} = T_{\text{outdoor}} + 5.0\text{ K}$ and evaporator temperature $T_{\text{evap}} = T_{\text{supply}} - 3.0\text{ K}$.
   - $\text{COP}_{\text{Carnot}} = \frac{T_{\text{evap, K}}}{T_{\text{cond, K}} - T_{\text{evap, K}}}$.
   - Modulates by part-load ratio $f(\text{PLR}) = 0.15 + 1.25\,\text{PLR} - 0.40\,\text{PLR}^2$, Second-Law efficiency $\eta = 0.35$, and thermal strain factor.
   - At $25^\circ\text{C}$ ambient, COP reaches $3.80$; at $38^\circ\text{C}$ extreme ambient, COP degrades to $2.96$ (a $22.2\%$ degradation), dynamically increasing electrical power draw.

4. **Dynamic Supply Air Temperature (Requirement R2.d)**:
   - Calculates mixed-air temperature $T_{\text{mixed}} = 0.15 T_{\text{outdoor}} + 0.85 T_{\text{return}}$.
   - Derives coil discharge $T_{\text{supply}} = T_{\text{mixed}} - 0.80(T_{\text{mixed}} - 7.0^\circ\text{C})$.
   - Rises to $11.16^\circ\text{C}$ under heavy outdoor load ($38^\circ\text{C}$) and drops to $10.34^\circ\text{C}$ under mild conditions ($22^\circ\text{C}$), clamped to $[8.0^\circ\text{C}, 18.0^\circ\text{C}]$.

5. **Multi-Zone Coupling & Dynamic CO₂ (Requirement R2.e)**:
   - Coupled partition conduction $q_{\text{interzone}} = \sum \frac{T_j - T_i}{R_{\text{partition}}}$ transfers heat between adjacent zones.
   - CO₂ mass balance $\frac{dC}{dt} = \frac{\dot{V}}{V}(400 - C) + \frac{5.0 \cdot N_{\text{occ}}}{V}$ smoothly accumulates during occupancy and decays when ventilated/vacant.

---

## 3. Caveats

- Physical constants (e.g. $10.8231^\circ\text{ N}, 106.6297^\circ\text{ E}$ coordinates, $1206\text{ J/(m}^3\cdot\text{K)}$ air heat capacity) default to the Ho Chi Minh City reference building, but are overrideable via environment variables (`WEATHER_LAT`, `WEATHER_LON`) and `data/programme-library.json`.
- Sensor freshness timeout remains strictly enforced at `hwStaleAfter = 20 * time.Second` per field.
- No static or mock fallbacks remain in the simulation engine.

---

## 4. Conclusion

All requirements for Milestone 1 (R1, R2, AC1) have been implemented and verified:
1. Solar geometry produces strictly 0 W at midnight and positive seasonal irradiance at noon.
2. Diurnal weather fallback produces a realistic 25°C–34°C daily temperature swing and 55%–95% RH swing.
3. Thermodynamic chiller COP dynamically degrades with higher outdoor ambient and thermal lift.
4. Supply air temperature dynamically reflects mixed-air and cooling coil thermal loading.
5. Multi-zone 2R1C thermal ODEs and CO₂ mass balances evolve dynamically across rooms.
6. Full test suite in `server/simulation/sensor_fallback_test.go` and `server/simulation/sensor_fallback_integration_test.go` verifies all physical behaviors.

---

## 5. Verification Method

To independently verify the implementation:
1. **Go Unit and Integration Tests**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v -count=1 ./simulation
   go test -v -count=1 ./...
   ```
2. **Key Test Functions to Inspect**:
   - `TestOutdoorWeatherFallbackDynamicVsStatic`: Asserts 24-hour diurnal cycle (peak at 15:00, trough at 03:00, $>8^\circ\text{C}$ swing).
   - `TestSolarGainDynamicPhysicsWhenLuxSensorOmitted`: Asserts midnight gain == 0.0 W, noon gain > 5000 W.
   - `TestCopAndCoolingPowerPhysicsWhenAcClampOmitted`: Asserts COP degradation (>15%) from 25°C to 38°C.
   - `TestSupplyAirTemperaturePhysicsWhenProbeOmitted`: Asserts supply air temperature modulation with coil load.
   - `TestZoneThermalCouplingWhenTemperatureSensorOmitted`: Asserts inter-zone partition conductive heat transfer.
   - `TestCO2MassBalanceWhenNdirSensorOmitted`: Asserts dynamic CO₂ generation and decay.
   - `TestFullSensorOmissionDynamicSimulation`: Asserts 100-tick stability with zero NaNs and valid derived metrics under complete sensor omission.
