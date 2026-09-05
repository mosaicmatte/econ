# Server Architecture & Sustainability Integration Analysis

**Author**: `explorer_survey_server_3`  
**Date**: 2026-09-05  
**Target Repository**: `/Users/nguyenhoangkhoi/Documents/econ`  
**Working Directory**: `/Users/nguyenhoangkhoi/Documents/econ/server`  

---

## Executive Summary

This investigation surveys the Go backend server in `server/` to provide an exact technical blueprint for implementing the **Sustainability & Decarbonization** module (`server/carbon.go` / `server/sustainability.go`) and its endpoint (`/api/sustainability`).

The backend is built in Go 1.22 with standard library `net/http`, Gorilla WebSocket, Eclipse Paho MQTT, Google FlatBuffers, and PostgreSQL/TimescaleDB drivers. It operates a 30 Hz thermal simulation engine (`server/simulation/engine.go`) coupled to physical edge nodes (ESP32 / Pico / CV occupancy) via MQTT. All telemetry fields required by the user prompt (`plugW`, `stripW`, AC states, `occupancy`) already exist in the codebase, with established freshness semantics, TimescaleDB persistence, and FlatBuffers streaming.

---

## 1. Server Entry Points, Routing, Middleware & Ports

### 1.1 Entry Point (`server/main.go`)
- **File**: `server/main.go`, function `main()` at line 18.
- **Port**: Configured via environment variable `PORT` (defaults to `"8080"` at line 215-218).
- **HTTP Server**: Uses Go standard library default multiplexer `http.DefaultServeMux` via `http.HandleFunc(...)` and `http.ListenAndServe(":"+port, nil)` (line 221).

### 1.2 Registered Handlers & Routes
| Route | Method | Handler / Source | Purpose |
|---|---|---|---|
| `/api/building-data` | GET | `main.go:20` inline | Serves static JSON building blueprint |
| `/api/ontology` | GET | `main.go:32` inline | Serves Brick ontology JSON |
| `/api/library` | GET | `main.go:52` inline (`simulation.Library()`) | Engineering coefficients & space programmes |
| `/api/history` | GET | `server/db.go:214` (`historyHandler`) | Historical sensor/metric query from TimescaleDB |
| `/api/series` | GET | `server/db.go:300` (`seriesHandler`) | Generic per-zone/per-metric time-series data |
| `/api/forecast` | GET/POST | `server/forecast.go:68` (`forecastHandler`) | Proxy telemetry window to Python LSTM forecaster |
| `/api/hardware` | GET | `main.go:80` inline (`engine.HardwareStatus()`) | Physical edge node status & binding list |
| `/api/vision/detection`| POST| `main.go:87` inline | Sets zone IR protocol dynamically from CV |
| `/api/command` | POST | `main.go:109` inline | Direct manual command overrides |
| `/api/devices` | GET | `server/devices.go:377` (`registerDeviceRoutes`) | Raw MQTT telemetry inspector & dropout stats |
| `/api/precool` | GET/POST | `server/precool.go:19` (`precoolHandler`) | Peak-load pre-cooling window control |
| `/api/weather` | GET | `server/weather.go:94` (`weatherHandler`) | Live outdoor ambient conditions (Open-Meteo) |
| `/api/digitize` | POST | `server/blueprint.go:147` (`digitizeHandler`) | Digitizes blueprint drawings |
| `/api/building` | POST | `server/blueprint.go:181` (`deployBuildingHandler`) | Deploys active building twin (admin-gated) |
| `/api/building/backups`| GET| `server/blueprint.go:223` (`backupsHandler`) | Lists building backups |
| `/api/building/rollback`|POST| `server/blueprint.go:254` (`rollbackHandler`) | Rolls back building twin (admin-gated) |
| `/api/plugs` | GET/POST | `server/plugapi.go:114` (`plugsHandler`) | Plug load management & APLC sweep policy |
| `/api/recommendations` | GET | `server/recommendapi.go:120` (`recommendationsHandler`) | AI Operations ranked anomaly report |
| `/api/rooms/models` | GET | `server/recommendapi.go:135` (`roomModelsHandler`) | Identified thermal/air dynamics per room |
| `/api/model` | GET | `server/modelexport.go:87` (`modelInfoHandler`) | Exported model manifest & metadata |
| `/api/model/export` | GET | `server/modelexport.go:162` (`modelExportHandler`) | Zip package of offline models & runtimes |
| `/api/model/recommend`| GET | `server/modelcatalog.go:336` (`modelRecommendHandler`)| Model sizing recommendations |
| `/api/forecast/load` | POST | `server/forecast.go:219` (`loadForecastHandler`) | Zero-shot foundation model load forecast |
| `/api/forecast/engines`| GET| `server/forecast.go:420` (`forecastEnginesHandler`)| Forecaster capabilities |
| `/api/forecast/compare`| POST| `server/forecast.go:437` (`compareForecastHandler`)| LSTM vs TimesFM comparison |
| `/ws` | GET/Upgrade | `main.go:208` (`handleWebSocket`) | 30 Hz FlatBuffers telemetry stream & commands |

### 1.3 Middleware, Security & CORS
- **CORS Handling (`corsPreflight` in `server/blueprint.go:35`)**:
  ```go
  func corsPreflight(w http.ResponseWriter, r *http.Request) bool {
      w.Header().Set("Access-Control-Allow-Origin", "*")
      if r.Method == http.MethodOptions {
          w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
          w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Admin-Token")
          w.WriteHeader(http.StatusNoContent)
          return true
      }
      return false
  }
  ```
- **Access Control (`server/auth.go`)**:
  - `ECON_ADMIN_TOKEN`: When unset, operates in demo mode; when set, write operations require authentication.
  - `requireAdmin(w, r)`: Inspects `X-Admin-Token` header using `subtle.ConstantTimeCompare`.
  - `checkOrigin(r)`: Validates WebSocket Origin against host, loopback, or `ECON_ALLOWED_ORIGINS`.

---

## 2. Telemetry Ingestion & Processing Pipeline

### 2.1 Ingestion Surface (`server/mqtt.go`)
- **Transport**: MQTT client subscribing to `econ/telemetry/+` and `econ/status/+`.
- **Broker**: `MQTT_BROKER` env var (default `tcp://localhost:1883`), credentials `MQTT_USERNAME` / `MQTT_PASSWORD`.
- **Payload Schema (`telemetryMsg` at `mqtt.go:24-44`)**:
  ```go
  type telemetryMsg struct {
      Zone        string   `json:"zone"`
      Occupancy   *int     `json:"occupancy"`
      Temperature *float64 `json:"temperature"`
      Humidity    *float64 `json:"humidity"`
      Co2         *float64 `json:"co2"`
      PlugW       *float64 `json:"plugW"`   // measured plug-circuit watts (SCT-013 clamp)
      SupplyC     *float64 `json:"supplyC"` // measured AC supply-air temperature (DS18B20)
      AcW         *float64 `json:"acW"`     // measured air-conditioner power (2nd SCT-013)
      Lux         *float64 `json:"lux"`     // measured ambient illuminance (BH1750)
      StripW      *float64 `json:"stripW"`  // measured power-strip draw in watts (ACS712)
      Source      string   `json:"source"`
      TempReal    bool     `json:"tempReal"`
      AcReal      *bool    `json:"acReal"`
      CfgRev      *uint32  `json:"cfgRev"`
  }
  ```
- **Dispatch**: `handleTelemetry(engine, topic, payload)` creates a `simulation.Measurement` and calls `engine.IngestTelemetry(ref, suffix, measurement)`.

### 2.2 Storage & Freshness Discipline (`server/simulation/engine.go`)
- **Freshness Window**: `const hwStaleAfter = 20 * time.Second` (`engine.go:939`). If telemetry stops for >20s, sensors are marked stale rather than retaining ghost data.
- **Fields in `ZoneSim`**:
  - `PlugW`: stored in `HwPlugW` with timestamp `HwPlugAt`. Checked via `z.plugFresh()`.
  - `StripW`: stored in `HwStripW` with timestamp `HwStripAt`. Checked via `z.stripFresh()`.
  - `AcW` (AC states/power): stored in `HwAcW` with timestamp `HwAcAt`. Checked via `z.acFresh()`.
  - `Occupancy`: stored in `Occupancy` (`int`), marks `Live = true`.
  - `AcReal`: stored in `HwAcReal` (verifies physical IR actuation vs dummy serial logging).

### 2.3 Streaming via FlatBuffers (`server/simulation/engine.go:broadcast()`)
- Ticked at ~30 FPS (33 ms ticker).
- Serializes `Telemetry.ZoneData` and `Telemetry.GlobalData` into binary FlatBuffers (`server/schema/telemetry.fbs`).
- Transmitted over `/ws` to all active WebSocket clients.
- `ZoneData` contains: `id`, `temp`, `occupants`, `load`, `lightsOn`, `humidity`, `co2`, `plugW`, `plugShed`, `supplyC`, `supplyReal`, `stripW`.
- `GlobalData` contains: `buildingLoadMw`, `systemHealth`, `totalOccupants`, `coolingOutputMw`, `plantCop`, `energySavedMw`, `bessDischargeMw`, `bessSocPct`, `avgCo2`, `plugKw`, `plugStandbyKw`, `plugShedKw`, `plugSavedKwh`, `autoPilot`.

### 2.4 Database Persistence (`server/db.go`)
- Connects to TimescaleDB at `DB_URL` (default `postgres://econ:econ@localhost:5432/econ?sslmode=disable`).
- Decoupled from physics thread via buffered channel `writeCh := make(chan reading, 8192)` and background `writeLoop()`.
- Persisted metrics at 1 Hz (`engine.go:2137`):
  - Global: `buildingLoadMw`, `coolingOutputMw`, `systemHealth`, `avgCo2`, `totalOccupants`, `plugKw`, `plantCop`, `measuredCop`, `meteredAcKw`, `avgTemp`, `avgAirflow`, `outdoorTemp`.
  - Per Zone: `temp`, `occupancy`, `humidity`, `co2`, `afddResidual`, `stripW`.

---

## 3. Simulation Engine (`server/simulation/`)

### 3.1 Initialization
- `NewEngine()` in `server/simulation/engine.go:275`:
  - Initializes maps `Zones`, `Vavs`, `Clients`, internal BESS battery, baseline model (`baselines.go`), and dynamics model (`dynamics.go`).
  - Calls `buildFromJSON(data)` loading `./data/building-data.json`.
  - For each floor and zone in `BuildingData`:
    - Computes polygon area `AreaM2` via Shoelace formula.
    - Sizes plug standby power: `PlugStandbyW = areaM2 * plugStandbyWPerM2` (1.2 W/m²).
    - Sets thermal capacitance `CAir` (floored at `Phys().MinZoneCapacitanceJPerK = 50000 J/K`), `CWall`, `RIn`, `ROut`.
    - Creates corresponding `VavSim` with physical resistance `vavResistanceFor(...)` based on room volume and design air changes (`supplyAirDesignAch = 6.0`).
  - Solves AHU airflow network using Hardy-Cross (`doHardyCross()`).

### 3.2 Physics & Telemetry Stepping (`engine.Start()` at `engine.go:1625`)
- Ticker: `33 * time.Millisecond` (~30 Hz).
- Timestep `dt`: 0.033s (normal), dynamically accelerated to 0.3s - 2.0s during fault injection or recovery.
- Critical section loop steps under `e.mu`:
  1. `e.tick(dt)`: Evaluates 2R1C explicit-Euler integration for zone air temperature and wall temperature. Sizes cooling against design AHU discharge (`supplyAirDesignC = 12.0 °C` or measured `HwSupplyC`).
  2. Pure physics AFDD shadow model: `z.ShadowTemp` integrated without sensor pinning. Residual `z.ResidualEma = ema(|z.HwTemp - z.ShadowTemp|)`.
  3. `e.actuate()`: Evaluates occupancy-driven setbacks (lights extinguished and setpoint relaxed after `vacantDwellTicks`).
  4. `e.applyHardware()`: Overrides simulated `z.Temp` with physical `z.HwTemp` when `z.hwFresh()` is valid.
  5. `e.plugTick(now)`: Automated Plug Load Control sweep logic; sheds switchable plug loads after hours when vacant for `GraceMinutes`; integrates `plugSavedKwh`.
  6. `e.Bess.Dispatch(...)`: Dispatches BESS battery based on Time-Of-Use (TOU) tariff bands.
  7. `e.broadcast()`: Calculates total building heat load, total electrical power (`coolingElectricalMW + baseElectricalMW`), global metrics, triggers 1 Hz DB persist, and broadcasts FlatBuffers over WebSocket.

---

## 4. Current Build and Test Configuration

### 4.1 Dependency Manifest (`server/go.mod`)
```go
module econ

go 1.22.12

require (
	github.com/eclipse/paho.mqtt.golang v1.4.3
	github.com/google/flatbuffers v25.12.19+incompatible
	github.com/gorilla/websocket v1.5.3
	github.com/lib/pq v1.12.3
)

require (
	golang.org/x/net v0.8.0 // indirect
	golang.org/x/sync v0.1.0 // indirect
)
```

### 4.2 Build & Test Verification
- **Build Command**: `go build .` in `/Users/nguyenhoangkhoi/Documents/econ/server`
  - Result: **Exit Code 0** (compiles cleanly).
- **Test Command**: `go test ./...` in `/Users/nguyenhoangkhoi/Documents/econ/server`
  - Result: **Exit Code 0** (all unit tests pass across all packages):
    - `econ`: PASS (cached)
    - `econ/cli`: no test files
    - `econ/schema/Telemetry`: no test files
    - `econ/simulation`: PASS (cached)

---

## 5. Clean Integration Architecture for `server/carbon.go` & `/api/sustainability`

### 5.1 Package & File Placement
- **File**: `server/carbon.go` (or `server/sustainability.go`), package `main`.
- **Test File**: `server/carbon_test.go`, package `main`.
- Placing the file in package `main` follows the exact architectural pattern of `plugapi.go`, `weather.go`, `forecast.go`, and `recommendapi.go`. It has direct access to `simulation.Engine`, `corsPreflight`, `requireAdmin`, and `simulation.GridEmissionFactor()`.

### 5.2 Mathematical Specifications & Accounting

#### A. Scope 2 Operational Carbon (R1)
- **Grid Emission Factor ($EF$)**:
  - Default: `simulation.GridEmissionFactor() = 0.6766 kgCO2e/kWh` (or `tCO2/MWh`), matching Vietnam national grid factor documented in `docs/EVIDENCE.md` and `server/data/programme-library.json`.
  - Configurable via environment variable `GRID_EMISSION_FACTOR_KG_KWH`.
- **Power Breakdown (Instantaneous)**:
  - Plug Loads ($P_{\text{plug}}$): $\sum z.\text{plugNowW}()$ in kW.
  - Power Strips ($P_{\text{strip}}$): $\sum z.\text{HwStripW}$ (for fresh sensors) in kW.
  - HVAC/AC Power ($P_{\text{ac}}$): $\text{coolingElectricalMW} \times 1000$ in kW.
  - Non-HVAC Base ($P_{\text{base}}$): $(\text{condFloorM2} \times 9.0\text{ W/m}^2) / 1000$ in kW.
  - Total Building Power ($P_{\text{total}}$): $P_{\text{ac}} + P_{\text{base}} + P_{\text{plug}} + P_{\text{strip}}$ (or `buildingLoadMW * 1000`) in kW.
- **Emissions Rate & Accumulator**:
  $$\text{Emissions Rate (kgCO}_2\text{e/hour)} = P_{\text{total}} (\text{kW}) \times EF (\text{kgCO}_2\text{e/kWh})$$
  $$\Delta \text{Emissions (kgCO}_2\text{e)} = P_{\text{total}} (\text{kW}) \times \frac{\Delta t_{\text{sec}}}{3600} \times EF (\text{kgCO}_2\text{e/kWh})$$
- **Programmatic Test Verification**:
  For $1000\text{ W} = 1\text{ kW}$, over $\Delta t = 1\text{ hour} = 3600\text{ s}$, with $EF = 0.5\text{ kgCO}_2\text{e/kWh}$:
  $$\text{Emitted Carbon} = 1\text{ kW} \times 1\text{ h} \times 0.5\text{ kgCO}_2\text{e/kWh} = 0.5000\text{ kgCO}_2\text{e}$$

#### B. Space Utilization Efficiency (R2)
- **Zone Design Capacity**:
  $$\text{Capacity}_i = \frac{\text{AreaM2}_i}{\text{AreaPerOccupantM2}_i}$$
  where $\text{AreaPerOccupantM2}_i$ comes from `simulation.ProgrammeFor(z.Type).AreaPerOccupantM2` (e.g. 10.0 m² for open-office, 2.5 m² for meeting-room; default 10.0 m²).
- **Building Capacity**: $\text{TotalCapacity} = \sum_{i} \text{Capacity}_i$.
- **Utilization Ratio**:
  $$\text{Efficiency} (\%) = \frac{\text{Total Occupants}}{\text{Total Capacity}} \times 100\%$$
- **Active Zone Ratio**: $\frac{\text{Occupied Zones (Occupancy > 0)}}{\text{Total Conditioned Zones}} \times 100\%$.

#### C. Predictive Maintenance & Equipment Health (R2)
Track equipment health per zone using existing physical indicators:
1. **Abnormal Power Draws**:
   - Power Strip (`stripW` via ACS712): Trigger alert if $P_{\text{strip}} > 2000\text{ W}$ or sudden uncharacteristic spikes when zone is vacant.
   - AC Power (`acW` via SCT-013): Flag if $P_{\text{ac}} > 3500\text{ W}$ or high continuous draw when zone is within deadband.
2. **Cumulative Runtime Hours**:
   - Continuous AC runtime tracking: Accumulate seconds AC is actively cooling. If runtime exceeds maintenance inspection threshold (e.g. 500 hours), flag filter/compressor service alert.
3. **Thermal AFDD Drift**:
   - Link `z.ResidualEma > afddThreshold` ($2.0\ ^\circ\text{C}$) to alert list for degraded heat exchanger coils.

#### D. Carbon Credit Recommendations & Live Market Data (R3)
- **Target Carbon Budget**:
  - Configurable target emission threshold $B_{\text{target}}$ (e.g. `CARBON_BUDGET_KG_PER_HOUR`, default e.g. 350.0 kgCO2e/h, or monthly budget).
- **Offset Calculation**:
  - If live emissions $E > B_{\text{target}}$, excess is $\Delta E = E - B_{\text{target}}$ (kgCO2e/h).
  - Offset required in metric tonnes: $\text{Offset Tonnes} = \frac{\Delta E}{1000}$.
- **Outbound HTTP Fetching**:
  - Outbound HTTP client (`http.Client{Timeout: 5 * time.Second}`) querying live carbon market feeds (e.g., Toucan, Carbonmark, or public market APIs with fallback spot price $8.50 - $12.00 / tCO2).
  - Estimated offset purchase cost:
    $$\text{Cost (USD)} = \text{Offset Tonnes} \times \text{SpotPriceUsdPerTonne}$$

#### E. REST Endpoint Specification (`/api/sustainability`) (R4)
- **Route**: `GET /api/sustainability`
- **CORS Support**: Integrates `corsPreflight(w, r)`.
- **Payload Structure**:
  ```json
  {
    "timestamp": "2026-09-05T04:25:00Z",
    "scope2Carbon": {
      "instantaneousKgCo2ePerHour": 541.28,
      "cumulativeKgCo2e": 1284.50,
      "gridEmissionFactorKgPerKwh": 0.6766,
      "powerBreakdownKw": {
        "total": 800.0,
        "hvac": 450.0,
        "plugs": 150.0,
        "strips": 25.0,
        "base": 175.0
      },
      "carbonBudgetKgPerHour": 400.0,
      "isOverBudget": true,
      "excessKgPerHour": 141.28
    },
    "spaceUtilization": {
      "totalOccupants": 350,
      "designCapacity": 700,
      "efficiencyPct": 50.0,
      "occupiedZonesCount": 45,
      "totalZonesCount": 60,
      "zoneUtilizationPct": 75.0
    },
    "predictiveMaintenance": {
      "activeWarningsCount": 2,
      "alerts": [
        {
          "zoneId": "zone-north-west-office-lvl4",
          "severity": "warning",
          "equipment": "power_strip",
          "metric": "stripW",
          "message": "Power strip drawing 2250W (exceeds 2000W safety threshold)",
          "currentValue": 2250.0,
          "threshold": 2000.0,
          "unit": "W"
        }
      ]
    },
    "carbonCreditRecommendation": {
      "overBudget": true,
      "recommendedPurchaseTCo2": 0.1413,
      "marketPriceUsdPerTonne": 9.50,
      "estimatedCostUsd": 1.34,
      "currency": "USD",
      "marketSource": "live_feed",
      "liveMarketFetched": true
    }
  }
  ```

---

## 6. Integration Checklist for Implementer
1. Add `server/carbon.go` containing:
   - Carbon accounting state & math (`CalculateScope2Carbon`).
   - Space utilization calculator (`CalculateSpaceUtilization`).
   - Predictive maintenance evaluator (`EvaluatePredictiveMaintenance`).
   - Live carbon market fetcher (`FetchLiveCarbonCreditPrice`).
   - HTTP handler `sustainabilityHandler(engine)`.
2. Register route in `server/main.go`:
   - `http.HandleFunc("/api/sustainability", sustainabilityHandler(engine))`
3. Add `server/carbon_test.go`:
   - Unit test verifying $1000\text{ W} \times 1\text{ h}$ at $0.5\text{ kgCO}_2\text{e/kWh} = 0.5\text{ kgCO}_2\text{e}$.
   - Unit test verifying HTTP endpoint returns valid JSON with all required fields.
   - Test verifying carbon offset pricing math and fallback resiliency.
4. Verify with `go build .` and `go test ./...`.
