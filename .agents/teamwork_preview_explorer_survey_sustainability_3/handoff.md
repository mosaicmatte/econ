# Handoff Report: Sustainability & Decarbonization Domain Logic & Modeling

## 1. Observation
1. **Telemetry & Power Models in `simulation/engine.go`**:
   - `HwStripW` (ACS712 True-RMS power strip draw in watts), `HwPlugW` (SCT-013 branch plug draw), and `HwAcW` (SCT-013 AC power draw) are ingested on lines 103, 116, 118, 669-685.
   - Freshness gates (`stripFresh()`, `plugFresh()`, `acFresh()`) enforce sensor liveness on lines 1022-1032 (`time.Since(at) < hwStaleAfter`).
   - Line 2096 computes cooling electrical power:
     ```go
     coolingElectricalMW := (unmeteredCoolingW/plantCop + meteredAcW) / 1e6
     ```
   - Lines 2112-2113 compute base and total electrical building load:
     ```go
     baseElectricalMW := (condFloorM2*ph.NonHvacBaseWPerM2 + plugTotalW) / 1e6
     buildingLoadMW := coolingElectricalMW + baseElectricalMW
     ```
   - Power strip draw `z.HwStripW` represents an instrumented sub-circuit within the zone; where `z.HwPlugW` is absent, `z.HwStripW` provides a physical floor measurement against modeled standby + occupant load (`simulation/plugs.go:111-122`).
2. **Energy Integration Mechanics in `simulation/plugs.go`**:
   - Line 167 integrates energy avoided over wall-clock duration `dt` using exact Watt-second to kWh conversion:
     ```go
     e.plugSavedKwh += z.PlugStandbyW * plugSwitchableFrac * dt / 3.6e6
     ```
   - Wall-clock seconds `dt` divided by `3.6e6` ($3.6 \times 10^6 \text{ W}\cdot\text{s/kWh}$) is the established system pattern for avoiding simulation acceleration distortions.
3. **Calibrated Grid Emission Factors in `simulation/library.go` and `data/programme-library.json`**:
   - `data/programme-library.json:37`: `"gridEmissionFactorTCo2PerMwh": 0.6766` ($1 \text{ tCO}_2\text{/MWh} \equiv 1 \text{ kgCO}_2\text{e/kWh}$).
   - `simulation/library.go:243`: `func GridEmissionFactor() float64 { return Lib().Calibration.GridEmissionFactor }`.
4. **Space Planning & Occupant Density in `data/programme-library.json`**:
   - Lines 157, 167, 176, 185 define `"areaPerOccupantM2"`: `open-office` = 10.0, `meeting-room` = 2.5, `cellular-office` = 10.0, `lobby` = 20.0, while non-occupiable zones (`corridor`, `wet-core`, `plant-room`, `store`) omit the field (represented as `nil` in Go `Programme.AreaPerOccupantM2`).
5. **Operating Baselines & Anomaly Scoring in `simulation/baselines.go` & `recommend.go`**:
   - `simulation/baselines.go:41`: `baselineMature = 20` samples minimum before score is trusted.
   - `simulation/recommend.go:105-177`: Scoring uses online mean/variance EWMA z-scores ($z = (x - \mu)/\sigma$).
6. **Acceptance Criteria in `.agents/ORIGINAL_REQUEST.md`**:
   - Line 72: Programmatic verification that 1000W drawn for 1 hour with a 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon.
   - Line 77: `curl` request to `/api/sustainability` returns valid JSON containing carbon totals, maintenance alerts, and (if over budget) recommended carbon credit offset amount and live cost.

---

## 2. Logic Chain
1. **Mathematical Equivalence & Float Exactness**:
   - From Observation 2, $\Delta E = \frac{P \times \Delta t}{3.6 \times 10^6}$. For $P = 1000.0\text{ W}$ and $\Delta t = 3600.0\text{ s}$ (1 hour):
     $$\Delta E = \frac{1000.0 \times 3600.0}{3.6 \times 10^6} = \frac{3,600,000.0}{3,600,000.0} = 1.0\text{ kWh}$$
   - Multiplying by $F_{\text{grid}} = 0.5\text{ kgCO}_2\text{e/kWh}$:
     $$\text{Emissions} = 1.0 \times 0.5 = 0.5\text{ kgCO}_2\text{e}$$
   - In IEEE 754 float64, both `1.0` and `0.5` are exact powers of 2, guaranteeing exact assertion matching `emissions == 0.5` without epsilon drift.
2. **Cumulative vs Instantaneous Carbon Tracking**:
   - Instantaneous carbon emission rate is evaluated as:
     $$\dot{C}(t) = \frac{P_{\text{total}}(t)}{1000.0} \times F_{\text{grid}} \quad [\text{kgCO}_2\text{e/hour}]$$
   - Cumulative carbon emissions are integrated on wall-clock intervals $\Delta t$ alongside `plugSavedKwh` in `Engine`:
     $$C_{\text{cum}} \leftarrow C_{\text{cum}} + \frac{P_{\text{total}}(t) \times \Delta t}{3.6 \times 10^6} \times F_{\text{grid}}$$
   - This state should be checkpointed to disk (`data/sustainability-state.json`) so it survives server restarts, matching the design in `recommendapi.go:23-26`.
3. **Predictive Maintenance Formulation**:
   - Equipment power telemetry (`HwStripW`, `HwAcW`, `HwPlugW`) maps to 4 anomaly detectors:
     1. *Over-Capacity*: $P(t) > P_{\text{rated}}$ (2000W strip, 3500W AC) $\to$ critical alert.
     2. *Transient Surge*: $\Delta P \ge 1000\text{W}$ and $P(t) \ge 2\bar{P} \to$ warning alert.
     3. *Baseline Drift*: $z \ge 3.0\sigma$ via `Baselines` during idle/off-hours $\to$ warning alert.
     4. *Runtime Hours*: $\text{RuntimeHours} \leftarrow \text{RuntimeHours} + \frac{\Delta t}{3600}$ when $P(t) > P_{\text{idle}}$. When $\ge 2000\text{h} \to$ maintenance warning.
4. **Space Utilization Efficiency Derivation**:
   - For each occupiable zone with area $A_i$ and design area per occupant $a_i$:
     $$\text{Capacity}_i = \max\left(1, \left\lfloor \frac{A_i}{a_i} \right\rfloor\right)$$
   - Building space utilization efficiency is derived as total live occupants over total building capacity:
     $$\text{Efficiency} (\%) = \frac{\sum_{i} O_i}{\sum_{i} \text{Capacity}_i} \times 100.0$$
5. **Unified Endpoint `/api/sustainability` Design**:
   - Synthesizes the 4 requirement pillars into one coherent payload: `carbonAccounting`, `spaceUtilization`, `predictiveMaintenance`, and `carbonCreditRecommendations`.

---

## 3. Caveats
1. **Strip vs Plug Double-Counting**:
   - In zones where both `HwPlugW` (branch SCT-013) and `HwStripW` (ACS712) are active simultaneously, `HwStripW` is physically a subset of `HwPlugW`. The building-wide load $P_{\text{total}}$ in `engine.go` already avoids double-counting by summing `coolingElectricalMW + baseElectricalMW`. In the breakdown fields of `/api/sustainability`, `stripW` is surfaced as an itemized submetered component.
2. **External Market Price Availability**:
   - Outbound HTTP calls to public carbon market APIs can encounter network rate limits or timeouts. The API data model includes `pricingSource`, `lastPriceFetch`, and cached fallback fields designed by `explorer_survey_market_3` to prevent blocking the HTTP handler.
3. **Non-Occupiable Space Handling**:
   - Service corridors, wet cores, and plant rooms have $a_i = \text{nil}$ and must be excluded from the capacity denominator to avoid skewing space utilization downward.

---

## 4. Conclusion
1. **Scope 2 Carbon Accounting**:
   - Implemented via standard formula $E_{\text{kWh}} = \frac{P \times \Delta t}{3.6 \times 10^6}$ and $\text{kgCO}_2\text{e} = E_{\text{kWh}} \times F_{\text{grid}}$.
   - Acceptance criterion test ($1000\text{W} \times 1\text{h} \times 0.5\text{ kg/kWh} = 0.5\text{ kg}$) is analytically exact in float64.
2. **Predictive Maintenance**:
   - Integrated into `ZoneSim`/Engine via `EquipmentHealthState` tracking runtime hours above idle thresholds (alerts at 2000h), power spikes ($\Delta P > 1000\text{W}$), rated overloads ($>2000\text{W}$ strip), and baseline drifts ($z \ge 3.0\sigma$).
3. **Space Utilization**:
   - Computed from live occupancy telemetry and geometric capacity ($A_i / a_i$), producing both building-wide and per-zone utilization efficiency percentages.
4. **API Endpoint Schema**:
   - Standardized in `server/sustainability.go` (or `carbon.go`), returning the structured JSON schema defined in `analysis.md §5`.

---

## 5. Verification Method
1. **Unit Test Execution**:
   - Run `go test -v ./...` in `server/` to execute `carbon_test.go` and verify all tests pass.
   - Specifically verify `TestCarbonCalculationExact1000W1H`:
     ```bash
     cd /Users/nguyenhoangkhoi/Documents/econ/server && go test -v -run TestCarbonCalculationExact1000W1H .
     ```
2. **API Endpoint Verification**:
   - Start server: `go run .` in `server/`.
   - Send HTTP request:
     ```bash
     curl -s http://localhost:8000/api/sustainability | jq .
     ```
   - Verify that all four top-level JSON objects (`carbonAccounting`, `spaceUtilization`, `predictiveMaintenance`, `carbonCreditRecommendations`) are present, non-null, and mathematically consistent.
3. **Invalidation Conditions**:
   - If `carbonAccounting.cumulativeEmissionsKgCO2e` fails to integrate over time when load is active.
   - If space utilization exceeds 100% without clamping or includes non-occupiable zones in capacity.
   - If power strip draws > 2000W fail to generate an active predictive maintenance alert.
