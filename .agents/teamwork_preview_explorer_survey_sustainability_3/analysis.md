# Mathematical Modeling, Domain Logic, and Engine Integration Analysis: Sustainability & Decarbonization (R1, R2, R4)

## Executive Summary
This document delivers the architectural and mathematical specification for the **Sustainability & Decarbonization** backend module in the `econ` building management system. It details:
1. **Scope 2 Operational Carbon Accounting (R1)**: Mathematical derivation of energy and carbon accounting from live telemetry (`plugW`, `stripW`, `acW` / AC states), configurable emission factors, cumulative vs instantaneous metrics, and exact analytical verification of the acceptance criterion (1000W for 1h @ 0.5 kgCO2e/kWh = 0.5 kg carbon).
2. **Predictive Maintenance & Space Utilization (R2)**: Runtime state tracking in `ZoneSim`/Engine, 4 distinct anomaly detection algorithms (overload/excessive wattage, transient power spikes, baseline drifts via EWMA z-scores, and cumulative runtime service hours), and an occupancy-to-capacity space utilization formula.
3. **Sustainability API & Data Models (R4)**: Complete Go struct models and JSON schema for `/api/sustainability`, unifying carbon accounting, space utilization efficiency, active equipment warnings, and dynamic carbon credit recommendations.

---

## 1. Scope 2 Operational Carbon Accounting (R1)

### 1.1 Physical Principles & GHG Protocol Alignment
Scope 2 operational emissions represent indirect greenhouse gas emissions from the generation of purchased electricity consumed by the facility (GHG Protocol Scope 2 Guidance).

In `econ`, the building electrical load consists of three core end-use categories:
1. **Air Conditioning / HVAC ($P_{\text{hvac}}$)**:
   - Metered zones: $z.\text{HwAcW}$ (measured via SCT-013 current clamp on GPIO / edge node).
   - Unmetered zones: Modeled cooling electrical demand derived from thermal loads $q_i$ and plant COP ($\text{plantCop}$):
     $$q_i = \text{BaseHeatGain} + \text{Occupancy} \times 100\text{ W} + q_{\text{solar}}$$
     $$P_{\text{unmetered, cool}} = \frac{q_{\text{unmetered}}}{\text{plantCop}}$$
2. **Plug Loads & Power Strips ($P_{\text{plug}}$, $P_{\text{strip}}$)**:
   - Branch circuit plug loads: $z.\text{HwPlugW}$ (measured via SCT-013 clamp) or modeled:
     $$P_{\text{plug, modeled}} = z.\text{PlugStandbyW} + z.\text{Occupancy} \times 65.0\text{ W}$$
   - Smart Power Strips: $z.\text{HwStripW}$ (measured via ACS712 True-RMS sensor on GPIO 35).
   - Note on telemetry relationship: In commercial buildings, $P_{\text{strip}}$ is a measured sub-circuit of a workstation cluster inside the zone. If the branch clamp $z.\text{HwPlugW}$ is active, $z.\text{HwStripW}$ is a submetered component of that plug load. When $z.\text{HwPlugW}$ is absent, $z.\text{HwStripW}$ serves as a direct measured floor:
     $$P_{\text{plug, effective}} = \max(P_{\text{plug, modeled}}, z.\text{HwStripW})$$
3. **Base Non-HVAC Building Load ($P_{\text{base}}$)**:
   - Conditioned floor area scaled baseline (lighting, ventilation fans, elevators, service pumps):
     $$P_{\text{base}} = A_{\text{cond}} \times \text{NonHvacBaseWPerM2} \quad (9.0\text{ W/m}^2)$$

Total instantaneous electrical power draw:
$$P_{\text{total}}(t) = P_{\text{hvac}}(t) + P_{\text{plug, effective}}(t) + P_{\text{base}}(t) \quad [\text{Watts}]$$
In `simulation/engine.go`, this is unified as:
$$P_{\text{total}}(t) = \text{buildingLoadMW} \times 10^6 \quad [\text{Watts}]$$

### 1.2 Mathematical Translation to Energy (kWh) and Carbon (kgCO2e)
Over a continuous time interval $[0, T]$:
$$E = \int_0^T P_{\text{total}}(t) \, dt \quad [\text{Joules or Watt-seconds}]$$
In kilowatt-hours (kWh):
$$E_{\text{kWh}} = \frac{E}{3.6 \times 10^6} = \int_0^T \frac{P_{\text{total}}(t)}{3.6 \times 10^6} \, dt$$

Given a Grid Emission Factor $F_{\text{grid}}$ in $\text{kgCO}_2\text{e/kWh}$ (or equivalently $\text{tCO}_2\text{/MWh}$):
$$\text{Emissions} (t) = E_{\text{kWh}}(t) \times F_{\text{grid}} \quad [\text{kgCO}_2\text{e}]$$

#### Discrete Engine Integration Formula:
On each simulation engine tick with wall-clock elapsed time $\Delta t = t_k - t_{k-1}$ (seconds):
$$\Delta E_k = \frac{P_{\text{total}}(t_k) \times \Delta t}{3.6 \times 10^6} \quad [\text{kWh}]$$
$$\Delta C_k = \Delta E_k \times F_{\text{grid}} \quad [\text{kgCO}_2\text{e}]$$

Accumulators:
$$E_{\text{cum}} \leftarrow E_{\text{cum}} + \Delta E_k$$
$$C_{\text{cum}} \leftarrow C_{\text{cum}} + \Delta C_k$$

#### Instantaneous Emission Rate:
$$\dot{C}(t) = \frac{P_{\text{total}}(t)}{1000.0} \times F_{\text{grid}} \quad [\text{kgCO}_2\text{e/hour}]$$
$$\dot{C}_{\text{sec}}(t) = \frac{P_{\text{total}}(t) \times F_{\text{grid}}}{3.6 \times 10^6} \quad [\text{kgCO}_2\text{e/second}]$$

### 1.3 Analytical Verification of Acceptance Criterion
The acceptance criterion states:
> *"1000W drawn for 1 hour with a 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon."*

**Proof**:
- Given:
  - $P = 1000.0\text{ W}$
  - $\Delta t = 1.0\text{ hour} = 3600.0\text{ seconds}$
  - $F_{\text{grid}} = 0.5\text{ kgCO}_2\text{e/kWh}$
- Step 1: Energy in kWh:
  $$E_{\text{kWh}} = \frac{P \times \Delta t}{3.6 \times 10^6} = \frac{1000.0 \times 3600.0}{3,600,000.0} = \frac{3,600,000.0}{3,600,000.0} = 1.0000000000\text{ kWh}$$
- Step 2: Emitted carbon in kgCO2e:
  $$\text{Emissions} = E_{\text{kWh}} \times F_{\text{grid}} = 1.0 \times 0.5 = 0.5000000000\text{ kgCO}_2\text{e}$$
- IEEE 754 float64 representation:
  $1000 \times 3600 = 3.6 \times 10^6$, division by $3.6 \times 10^6$ yields exactly `1.0` (representable without rounding error). Multiplication by `0.5` ($2^{-1}$) produces exact binary float `0.5`.
  Assertion `emissions == 0.5` in Go will evaluate to `true` with zero floating point epsilon error.

### 1.4 Configurable Grid Emission Factors
The grid factor must be configurable across environments and regional grids:
| Grid Profile | Emission Factor ($F_{\text{grid}}$) | Source / Notes |
|---|---|---|
| **Acceptance / Benchmark Standard** | `0.5000 kgCO2e/kWh` | Required by acceptance criteria test harness |
| **Vietnam National Grid (EVN)** | `0.6766 kgCO2e/kWh` | Existing calibrated value in `server/data/programme-library.json` |
| **US National Average (EPA eGRID)** | `0.3860 kgCO2e/kWh` | EPA eGRID 2022 summary table |
| **US California (CAMX)** | `0.2200 kgCO2e/kWh` | Low-carbon renewable heavy subregion |
| **US Midwest (MROW)** | `0.6000 kgCO2e/kWh` | Coal-dominant subregion |
| **EU-27 Average** | `0.2300 kgCO2e/kWh` | European Environment Agency (EEA) |

Configuration Priority in Go:
1. HTTP Query parameter or POST override: `?gridFactor=0.5`
2. Environment variable: `ECON_GRID_EMISSION_FACTOR`
3. Shipped calibration file: `Lib().Calibration.GridEmissionFactor` (`data/programme-library.json`)
4. Hardcoded benchmark fallback: `0.50 kgCO2e/kWh`

---

## 2. Predictive Maintenance & Equipment Health (R2)

### 2.1 Zone Runtime State Tracking
Equipment health in `ZoneSim` covers three monitored classes:
1. `ac`: Air conditioning compressor & fan coil unit (monitored by `HwAcW`, `HwSupplyC`, and damper modulation).
2. `power_strip`: ACS712 True-RMS smart power strip (monitored by `HwStripW`).
3. `plug_circuit`: SCT-013 circuit branch meter (monitored by `HwPlugW`).

To track equipment health over time without memory leaks or unconstrained growth, each zone maintains an `EquipmentHealthState` struct in memory, updated on each engine tick:

```go
type EquipmentHealthState struct {
    EquipmentType      string    `json:"equipmentType"`      // "ac" | "power_strip" | "plug_circuit"
    RuntimeHours       float64   `json:"runtimeHours"`       // Cumulative hours operating above idle threshold
    LastPowerW         float64   `json:"lastPowerW"`         // Last observed power for derivative/spike checks
    PeakPowerW         float64   `json:"peakPowerW"`         // Max power observed
    RatedCapacityW     float64   `json:"ratedCapacityW"`     // Rated nameplate capacity (e.g. 2000W strip, 3500W AC)
    ServiceThresholdH  float64   `json:"serviceThresholdH"`  // Threshold for maintenance warning (e.g. 2000h)
    OverloadCount      int       `json:"overloadCount"`      // Count of over-capacity events
    SpikeCount         int       `json:"spikeCount"`         // Count of transient power surges
    LastMaintenanceAt  time.Time `json:"lastMaintenanceAt"`  // Timestamp of last reset/service
    HealthScore        float64   `json:"healthScore"`        // 0.0 - 100.0%
}
```

### 2.2 Anomaly Detection Algorithms

#### Algorithm 1: Over-Capacity / Excessive Wattage
- **Objective**: Prevent electrical hazards, equipment burning, and circuit breaker tripping.
- **Rule**:
  $$\text{If } P_{\text{equip}}(t) > P_{\text{rated}} \implies \text{CRITICAL ALERT}$$
- **Defaults**:
  - Power strip (ACS712): $P_{\text{rated}} = 2000.0\text{ W}$ (typical 10A @ 220V rating).
  - AC Unit: $P_{\text{rated}} = 3500.0\text{ W}$ (1.5 HP - 2.5 HP VRF/split unit limit).
- **Severity**: `"critical"`.
- **Payload Alert**: `"Excessive wattage (2250.0 W) exceeds rated capacity of 2000.0 W. Immediate overload hazard."`

#### Algorithm 2: Transient Power Spike (Derivative Surge)
- **Objective**: Detect motor stalling, compressor short cycling, failing start capacitors, or arcing appliances.
- **Rule**:
  $$\Delta P = P(t) - P(t - \Delta t)$$
  $$\text{Condition: } \Delta P \ge \Delta P_{\text{spike\_thresh}} \quad \text{and} \quad P(t) \ge 2.0 \times \bar{P}_{\text{normal}}$$
- **Defaults**:
  - $\Delta P_{\text{spike\_thresh}} = 1000.0\text{ W}$ (or $> 3\times$ baseline).
- **Severity**: `"warning"`.
- **Action**: Increment zone `SpikeCount`; trigger warning if multiple spikes occur within 1 hour.

#### Algorithm 3: Baseline Drift (Mechanical Wear & Phantom Creep)
- **Objective**: Detect bearing degradation in fans, dirty coils, refrigerant leaks causing higher continuous draw, or unauthorized phantom loads.
- **Rule**: Leverage existing online learned baseline engine (`simulation/baselines.go`):
  $$z = \frac{P(t) - \mu_{\text{baseline}}(h)}{\sigma_{\text{baseline}}(h)}$$
  $$\text{Condition: } z \ge 3.0\sigma \quad \text{and bucket is mature } (N \ge 20)$$
- **Severity**: `"warning"`.
- **Insight**: Equipment is consuming significantly more power than historical normal for this specific hour of the day.

#### Algorithm 4: Cumulative Runtime Hours Tracking
- **Objective**: Condition-based maintenance alert before catastrophic failure.
- **Integration**:
  On each tick $\Delta t$:
  $$\text{If } P(t) > P_{\text{idle\_threshold}} \implies \text{RuntimeHours} \leftarrow \text{RuntimeHours} + \frac{\Delta t}{3600.0}$$
  - $P_{\text{idle\_threshold}} = 50.0\text{ W}$ for AC, $20.0\text{ W}$ for Power Strip.
- **Alert Levels**:
  - `RuntimeHours >= 2000.0`: `"warning"` — Filter cleaning and lubrication inspection due.
  - `RuntimeHours >= 5000.0`: `"critical"` — Major overhaul / compressor service due.
- **Health Degradation Formula**:
  $$\text{HealthScore} = \max\left(0.0, 100.0 \times \left(1.0 - \frac{\text{RuntimeHours}}{T_{\text{max}}}\right) - 10 \times N_{\text{spikes}} - 25 \times [\text{OverCapacity}]\right)$$
  where $T_{\text{max}} = 8000.0\text{ hours}$.

---

## 3. Space Utilization Efficiency (R2)

### 3.1 Telemetry and Building Geometry Inputs
- **Current Occupancy ($O_i$)**: Received via MQTT from CV/ByteTrack or mmWave sensors into `z.Occupancy`.
- **Zone Area ($A_i$)**: Digitized floor area in $m^2$ from `z.AreaM2` (`BuildingData`).
- **Design Area Per Occupant ($a_i$)**: Provided by `Programme` in `simulation/library.go` (`data/programme-library.json`):
  - `open-office`: $10.0\text{ m}^2/\text{person}$
  - `meeting-room`: $2.5\text{ m}^2/\text{person}$
  - `cellular-office`: $10.0\text{ m}^2/\text{person}$
  - `lobby`: $20.0\text{ m}^2/\text{person}$
  - Non-occupiable (`corridor`, `wet-core`, `store`, `mechanical`, `plant-room`, `comms-room`): $a_i = \text{nil}$

### 3.2 Designed Zone Capacity Derivation
For each occupiable zone $i$:
$$\text{Capacity}_i = \begin{cases}
\max\left(1, \left\lfloor \frac{A_i}{a_i} \right\rfloor\right) & \text{if } a_i > 0 \\
0 & \text{if non-occupiable}
\end{cases}$$

### 3.3 Space Utilization Efficiency Formulas

#### 1. Per-Zone Space Utilization Efficiency:
$$U_i (\%) = \begin{cases}
\min\left(100.0, \frac{O_i}{\text{Capacity}_i} \times 100.0\right) & \text{if } \text{Capacity}_i > 0 \\
0.0\% & \text{otherwise}
\end{cases}$$
*(Note: If $O_i > \text{Capacity}_i$, overcrowding warning is flagged, while utilization is capped at 100% or tracked as load factor).*

#### 2. Building-Wide Space Utilization Efficiency:
$$\text{Efficiency}_{\text{building}} (\%) = \frac{\sum_{i \in \text{OccZones}} O_i}{\sum_{i \in \text{OccZones}} \text{Capacity}_i} \times 100.0$$
Boundary condition:
$$\text{If } \sum \text{Capacity}_i == 0 \implies \text{Efficiency}_{\text{building}} = 0.0\%$$

#### 3. Area-Weighted Active Utilization (Supplementary):
$$\text{Efficiency}_{\text{area}} (\%) = \frac{\sum_{i \in \text{OccZones}} A_i \cdot \frac{\min(O_i, \text{Capacity}_i)}{\text{Capacity}_i}}{\sum_{i \in \text{OccZones}} A_i} \times 100.0$$

---

## 4. Carbon Credit Recommendations Logic (R3)

### 4.1 Carbon Budgeting Logic
The system maintains a configurable target carbon budget:
- `BudgetPeriod`: `"daily"` | `"hourly"` | `"cumulative"`
- `BudgetCgCO2e`: e.g., $500.0\text{ kgCO}_2\text{e}$ per day.

At evaluation instant $t$:
$$\text{Deficit} = \max\left(0.0, C_{\text{emissions}} - C_{\text{budget}}\right) \quad [\text{kgCO}_2\text{e}]$$
- If $\text{Deficit} == 0$: Building is operating within budget (`"under_budget"`). Offset recommendation is $0$.
- If $\text{Deficit} > 0$: Building exceeds budget (`"over_budget"`). Exact offset required:
  $$\text{OffsetTons} = \frac{\text{Deficit}}{1000.0} \quad [\text{metric tons CO}_2\text{e}]$$

### 4.2 Dynamic Pricing Integration
Using live pricing fetched by the market poller (`explorer_survey_market_3`):
$$\text{PricePerTonUSD} = P_{\text{market}} \quad [\text{\$/metric ton}]$$
$$\text{EstimatedCostUSD} = \text{OffsetTons} \times P_{\text{market}}$$

---

## 5. Sustainability API Specification (`/api/sustainability`) (R4)

### 5.1 Route and Handler Design
- **Endpoint**: `GET /api/sustainability`
- **Controller File**: `server/sustainability.go` (or `server/carbon.go`)
- **Headers**:
  - `Content-Type: application/json`
  - `Access-Control-Allow-Origin: *`

### 5.2 Go Struct Data Models
```go
package main

import "time"

// SustainabilityResponse is the top-level payload for GET /api/sustainability
type SustainabilityResponse struct {
    Timestamp                   time.Time                     `json:"timestamp"`
    BuildingId                  string                        `json:"buildingId"`
    CarbonAccounting            CarbonAccountingMetrics       `json:"carbonAccounting"`
    SpaceUtilization            SpaceUtilizationMetrics       `json:"spaceUtilization"`
    PredictiveMaintenance       PredictiveMaintenanceMetrics  `json:"predictiveMaintenance"`
    CarbonCreditRecommendations CarbonCreditRecommendation    `json:"carbonCreditRecommendations"`
}

type CarbonAccountingMetrics struct {
    GridEmissionFactorKgPerKwh     float64            `json:"gridEmissionFactorKgPerKwh"`
    GridFactorSource               string             `json:"gridFactorSource"`
    InstantaneousPowerW            float64            `json:"instantaneousPowerW"`
    InstantaneousEmissionsKgPerHour float64           `json:"instantaneousEmissionsKgPerHour"`
    CumulativeKwh                  float64            `json:"cumulativeKwh"`
    CumulativeEmissionsKgCO2e      float64            `json:"cumulativeEmissionsKgCO2e"`
    BudgetKgCO2e                   float64            `json:"budgetKgCO2e"`
    BudgetStatus                   string             `json:"budgetStatus"` // "under_budget" | "over_budget"
    Breakdown                      CarbonBreakdown    `json:"breakdown"`
}

type CarbonBreakdown struct {
    CoolingW         float64 `json:"coolingW"`
    CoolingKgPerHour float64 `json:"coolingKgPerHour"`
    PlugW            float64 `json:"plugW"`
    PlugKgPerHour    float64 `json:"plugKgPerHour"`
    StripW           float64 `json:"stripW"`
    StripKgPerHour   float64 `json:"stripKgPerHour"`
    BaseW            float64 `json:"baseW"`
    BaseKgPerHour    float64 `json:"baseKgPerHour"`
}

type SpaceUtilizationMetrics struct {
    BuildingEfficiencyPercent float64          `json:"buildingEfficiencyPercent"`
    TotalOccupants            int              `json:"totalOccupants"`
    TotalCapacity             int              `json:"totalCapacity"`
    OccupiableZoneCount       int              `json:"occupiableZoneCount"`
    Zones                     []ZoneEfficiency `json:"zones"`
}

type ZoneEfficiency struct {
    ZoneId            string  `json:"zoneId"`
    Name              string  `json:"name"`
    ZoneType          string  `json:"zoneType"`
    Occupancy         int     `json:"occupancy"`
    Capacity          int     `json:"capacity"`
    EfficiencyPercent float64 `json:"efficiencyPercent"`
}

type PredictiveMaintenanceMetrics struct {
    TotalActiveWarnings int                `json:"totalActiveWarnings"`
    Warnings            []MaintenanceAlert `json:"warnings"`
    EquipmentTracked    int                `json:"equipmentTracked"`
}

type MaintenanceAlert struct {
    Id             string    `json:"id"`
    ZoneId         string    `json:"zoneId"`
    EquipmentType  string    `json:"equipmentType"`  // "ac" | "power_strip" | "plug_circuit"
    WarningType    string    `json:"warningType"`    // "runtime_exceeded" | "power_spike" | "baseline_drift" | "excessive_wattage"
    Severity       string    `json:"severity"`       // "critical" | "warning" | "info"
    Message        string    `json:"message"`
    Value          float64   `json:"value,omitempty"`
    Threshold      float64   `json:"threshold,omitempty"`
    RuntimeHours   float64   `json:"runtimeHours,omitempty"`
    ThresholdHours float64   `json:"thresholdHours,omitempty"`
    Timestamp      time.Time `json:"timestamp"`
}

type CarbonCreditRecommendation struct {
    Required             bool      `json:"required"`
    BudgetDeficitKgCO2e  float64   `json:"budgetDeficitKgCO2e"`
    OffsetTonsNeeded     float64   `json:"offsetTonsNeeded"`
    MarketPriceUsdPerTon float64   `json:"marketPriceUsdPerTon"`
    EstimatedCostUsd     float64   `json:"estimatedCostUsd"`
    PricingSource        string    `json:"pricingSource"`
    LastPriceFetch       time.Time `json:"lastPriceFetch"`
    Recommendation       string    `json:"recommendation"`
}
```

---

## 6. Verification and Unit Testing Plan (`carbon_test.go`)

### 6.1 Test Matrix
To guarantee compliance with all acceptance criteria:
1. `TestCarbonCalculationExact1000W1H`:
   - Inputs: Power = 1000.0 W, Duration = 3600.0 s, GridFactor = 0.5 kgCO2e/kWh.
   - Assert: Energy == 1.0 kWh, Emissions == 0.5 kgCO2e.
2. `TestCarbonCalculationZeroAndNegativeGuards`:
   - Negative Watts or negative duration clamped to 0.
3. `TestSpaceUtilizationEfficiency`:
   - Synthetic building with 3 zones:
     - Zone A: Area 70m2, office (10m2/occ) -> Capacity 7, Occupancy 5 -> 71.4%
     - Zone B: Area 25m2, meeting (2.5m2/occ) -> Capacity 10, Occupancy 10 -> 100.0%
     - Zone C: Corridor (non-occupiable) -> Capacity 0, Occupancy 1 -> Ignored
   - Total capacity = 17, Total occ = 15 -> Overall Efficiency = 88.2%.
4. `TestPredictiveMaintenanceOverload`:
   - Power strip draw = 2200W (rated 2000W) -> Triggers critical overload warning.
5. `TestPredictiveMaintenanceRuntimeThreshold`:
   - AC simulated running for 2100 hours -> Triggers 2000h service inspection warning.
6. `TestCarbonCreditDeficitAndCost`:
   - Emissions = 650 kg, Budget = 500 kg -> Deficit = 150 kg (0.15 tons).
   - Market price = $20.00/ton -> Cost = $3.00 USD.

