## 2026-08-31T04:36:02Z

Milestone 1 Scope & Implementation Tasks:
1. Smart Physics Fallbacks for Missing/Omitted Sensors (Requirement R2):
   - In `server/simulation/engine.go`, `server/weather.go`, `server/simulation/library.go` (and any necessary physics files in `server/simulation/`):
     a. **Solar Geometry**: Implement astronomical solar zenith angle calculation (Spencer / NOAA algorithm with site coordinates `10.8231° N, 106.6297° E`) and clear-sky GHI irradiance modeling. When `HwLux` sensor is omitted, compute dynamic solar heat gain $q_{\text{solar}}(t)$ based on sun position (strictly 0.0 W at solar midnight, peaking at solar noon).
     b. **Diurnal Weather Fallback**: In `server/weather.go` / `engine.go`, replace the static flat 30.0°C fallback with a realistic diurnal temperature curve (e.g. 25.0°C–34.0°C daily swing) and diurnal relative humidity curve when Open-Meteo weather is offline.
     c. **Thermodynamic Chiller COP & Power**: When `HwAcW` current clamp is omitted, calculate chiller electrical power and COP dynamically from thermodynamic lift ($T_{\text{condenser}} - T_{\text{evaporator}}$), Carnot limit, part-load ratio, and thermal strain rather than a flat static constant.
     d. **Dynamic Supply Air Temperature**: When DS18B20 supply probe is omitted, calculate supply air temperature dynamically from mixed-air temperature ($\alpha_{\text{fresh}} T_{\text{outdoor}} + (1-\alpha_{\text{fresh}}) T_{\text{return}}$) and cooling coil heat exchange balance.
     e. **Multi-Zone Coupled 2R1C & Dynamic CO2**: Maintain dynamic heat balance and mass balance estimation across zones.
2. Go Unit & Integration Tests (Acceptance Criterion 1):
   - Implement `server/simulation/sensor_fallback_test.go` and `server/simulation/sensor_fallback_integration_test.go` explicitly asserting that when sensor inputs (temperature, occupancy, solar lux, AC clamp, plug clamp, supply probe, outdoor weather) are omitted, the simulation engine calculates realistic derived values using physics models instead of falling back to static mock data.
   - Specifically assert:
     - Diurnal solar gain is 0 W at midnight and positive at noon.
     - Offline weather dynamically varies over 24 hours.
     - Chiller COP dynamically degrades with higher outdoor ambient / thermal lift.
     - Supply air dynamically reflects coil thermal loading.
     - Zone thermal ODEs evolve dynamically with adjacent zones and outdoor weather.
3. Verification:
   - Run `cd /Users/nguyenhoangkhoi/Documents/econ/server && go test -v -count=1 ./...`
   - Document all commands, file diffs, and test results in `/Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m1/handoff.md`.
   - Send a completion message when finished.
