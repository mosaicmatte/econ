# Explorer 2 Survey Report: Go Backend Server, Forecasting Proxy & MQTT Telemetry

**Date**: 2026-08-29T21:00:00Z  
**Author**: Explorer 2 (Go Server & MQTT Telemetry)  
**Target Scope**: Go backend server (`server/`), MQTT telemetry & broker configuration (`server/mosquitto/`, `server/mqtt.go`, `edge/`), forecasting proxies (`server/forecast.go`), edge services (`edge/esp32`, `edge/pico`, `edge/raspberry_pi`, `ai_modules/branch_a_occupancy`), logging verbosity, and test infrastructure.

---

## 1. Observation

### 1.1 Go Backend Architecture & HTTP Routing Surface
The Go backend server entry point is `server/main.go`. It initializes the physics simulation engine `simulation.NewEngine()`, connects to TimescaleDB (`initDB()`), connects to the Mosquitto MQTT broker (`startMQTT(engine)`), starts background loops (engine tick, precool, weather, plug persist, baseline persist), and registers HTTP endpoints and a WebSocket streaming endpoint (`/ws`).

#### Complete HTTP Routing Inventory (`server/main.go`):
| HTTP Route | Source Handler | Purpose |
|---|---|---|
| `GET /api/building-data` | `server/main.go:20` | Serves static building geometry JSON (`server/data/building-data.json`) |
| `GET /api/ontology` | `server/main.go:32` | Serves Brick schema ontology JSON (`server/data/brick-ontology.json`) |
| `GET /api/library` | `server/main.go:59` | Serves building programme library coefficients (`simulation.Library`) |
| `GET /api/history` | `server/db.go:70` (`historyHandler`) | Serves historical sensor readings from TimescaleDB |
| `GET /api/series` | `server/db.go:88` (`seriesHandler`) | Serves time-series queries by `?zone=&metric=&minutes=` |
| `GET /api/forecast` | `server/forecast.go:528` (`forecastHandler`) | Proxies live telemetry window to Python LSTM forecaster (`POST /predict`) |
| `GET /api/forecast/load` | `server/forecast.go:66` (`loadForecastHandler`) | Proxies 5-min load history to Google TimesFM foundation model (`POST /forecast/load`) |
| `GET /api/forecast/compare` | `server/forecast.go:246` (`compareForecastHandler`) | Queries both LSTM & TimesFM concurrently; checks plausibility against observed range |
| `GET /api/forecast/engines` | `server/forecast.go:491` (`forecastEnginesHandler`) | Proxies forecaster availability to Python (`GET /model/info`) |
| `GET /api/recommendations` | `server/recommendapi.go:120` (`recommendationsHandler`) | Returns ranked learned baseline anomalies & room dynamics predictions (`engine.Recommendations(8)`) |
| `GET /api/rooms/models` | `server/recommendapi.go:135` (`roomModelsHandler`) | Exposes identified room physical parameters (thermal time constant, cooling authority, ACH) |
| `GET /api/hardware` | `server/main.go:84` | Returns live hardware binding status (`engine.HardwareStatus()`) |
| `GET /api/devices` | `server/devices.go:211` (`devicesHandler`) | Hardware inspector view (seen devices, message count, rate, last JSON) |
| `GET /api/devices/series` | `server/devices.go:240` (`deviceSeriesHandler`) | Device metric history from DB |
| `GET /api/devices/events` | `server/devices.go:290` (`deviceEventsHandler`) | Device lifecycle log (connect/disconnect/config-change) |
| `GET /api/devices/quality` | `server/devices.go:333` (`dataQualityHandler`) | Measured vs modelled breakdown |
| `GET /api/precool` | `server/precool.go:41` (`precoolHandler`) | Returns pre-cooling window status (`{active, until}`) |
| `POST /api/precool` | `server/precool.go:41` (`precoolHandler`) | Opens a manual/automated pre-cooling window |
| `GET /api/weather` | `server/weather.go:19` (`weatherHandler`) | Returns live Open-Meteo weather or fallback |
| `POST /api/digitize` | `server/blueprint.go:21` (`digitizeHandler`) | Proxies drawing upload to Python digitizer |
| `POST /api/building` | `server/blueprint.go:46` (`deployBuildingHandler`) | Deploys newly digitized building model |
| `GET /api/building/backups`| `server/blueprint.go:81` (`backupsHandler`) | Lists building fixture backups |
| `POST /api/building/rollback`| `server/blueprint.go:106` (`rollbackHandler`)| Restores previous building model |
| `GET /api/plugs` | `server/plugapi.go:21` (`plugsHandler`) | Plug load snapshot, phantom leaderboard, sweep policy |
| `POST /api/plugs` | `server/plugapi.go:21` (`plugsHandler`) | Dispatches / updates plug sweep policies |
| `GET /api/model` | `server/modelexport.go:22` (`modelInfoHandler`) | Export pack inventory and model metadata |
| `GET /api/model/export` | `server/modelexport.go:44` (`modelExportHandler`) | Packages models, LSTM weights, and offline runtime into `.zip` |
| `GET /api/model/recommend` | `server/modelcatalog.go:22` (`modelRecommendHandler`)| Matches operator client hardware to model pack |
| `GET /ws` | `server/main.go:192` (`handleWebSocket`) | Bidirectional WebSocket (FlatBuffers stream + JSON control actions) |

---

### 1.2 `GET /api/recommendations` & Recommendation Structures
Located in:
- `server/recommendapi.go:120-128`:
  ```go
  func recommendationsHandler(engine *simulation.Engine) http.HandlerFunc {
      return func(w http.ResponseWriter, r *http.Request) {
          if corsPreflight(w, r) {
              return
          }
          w.Header().Set("Content-Type", "application/json")
          json.NewEncoder(w).Encode(engine.Recommendations(8))
      }
  }
  ```
- `server/simulation/engine.go:1159-1193`:
  `engine.Recommendations(topN int) RecommendationReport` extracts zone readings and conditions under `e.mu`, runs `e.baselines.Recommend(...)` to identify present anomalies, runs `e.dynamics.PredictiveRecommendations(...)` to forecast thermal/CO2 breaches, and combines them via `mergeRecommendations(...)`.
- `server/simulation/recommend.go:69-85`:
  ```go
  type RecommendationReport struct {
      Recommendations []Recommendation `json:"recommendations"`
      Model           struct {
          Established      int      `json:"established"`
          Learning         int      `json:"learning"`
          MatureAfter      int      `json:"matureAfter"`
          SampleCadenceSec int      `json:"sampleCadenceSec"`
          Metrics          []string `json:"metrics"`
          RoomsIdentified  int      `json:"roomsIdentified"`
          RoomsLearning    int      `json:"roomsLearning"`
          HorizonMin       int      `json:"horizonMin"`
      } `json:"model"`
  }
  ```
- `server/simulation/recommend.go:39-65`:
  `Recommendation` struct contains:
  `Id`, `Zone`, `Label`, `Metric`, `Severity`, `Basis`, `Title`, `Message`, `Value`, `Unit`, `Baseline`, `Sigma`, `Deviation`, `Samples`, `Hour`, `Action`, `Kind` ("anomaly"|"prediction"|"capability"), `EtaSec`, `Predicted`, `Equilibrium`.

**Current Gap**: `RecommendationReport` does NOT include forecast graph series data (TimesFM zero-shot load horizon or LSTM prediction series).

---

### 1.3 Forecasting Backend & Go Proxy Connection
Located in:
- Python service: `backend/forecasting/main.py`
  - `POST /predict`: Ingests `ForecastRequest{sensor_sequence, outdoor_temp, outdoor_humidity}`, returns `ForecastResponse{predicted_peak_load, outdoor_temp_used, outdoor_humidity_used, weather_source}` via PyTorch LSTM (`model.PeakLoadLSTM`).
  - `POST /forecast/load`: Ingests `LoadForecastRequest{history, horizon, context_len}`, calls `TIMESFM.forecast(history, horizon, context_len)` in `backend/forecasting/timesfm_forecaster.py`.
    Returns `{forecast: [float, ...], quantiles: {q1: [...], ...}, engine: "timesfm", variant: "...", repo: "...", device: "...", zero_shot: true, context_used: N}`.
  - `GET /model/info`: Returns availability for both LSTM and TimesFM.
  - `GET /model/artifacts`: Base64 encoded weights + scaler for offline bundle export.
- Go proxy layer: `server/forecast.go`
  - `FORECAST_URL` environment variable configured in `server/docker-compose.yml` (`http://forecasting:8000`), defaulting to `http://localhost:8000`.
  - `buildForecastRequest(engine)` builds the 12-step sequence `[room_temp, airflow]` + live Open-Meteo outdoor weather.
  - `loadForecastHandler` (`GET /api/forecast/load`): Passes `engine.LoadHistory()` to `POST /forecast/load`.
  - `compareForecastHandler` (`GET /api/forecast/compare`): Calls `queryLSTM` and `queryTimesFM` concurrently, annotates with `checkPlausible` against `engine.ObservedLoadRange()`, and calculates `forecastAgreement`.
  - `forecastHandler` (`GET /api/forecast`): Calls `POST /predict` and annotates with window provenance and plausibility.

---

### 1.4 MQTT Telemetry Handlers, Broker Setup & Edge Services
Located in:
- Broker Configuration:
  - `server/docker-compose.yml:16-29`: `eclipse-mosquitto:2` exposing port `1883`.
  - `server/mosquitto/mosquitto.conf`: Production config with authentication (`password_file /mosquitto/config/passwd`, `acl_file /mosquitto/config/acl`).
  - `server/mosquitto/mosquitto.dev.conf`: Anonymous access config for local dev.
- Go MQTT Client (`server/mqtt.go`):
  - `startMQTT(engine)` connects to `MQTT_BROKER` (default `tcp://localhost:1883`), subscribes to `econ/telemetry/+` and `econ/status/+`.
  - Defines `engine.Publish = func(topic, payload string) { client.Publish(topic, 0, false, payload) }`.
  - `handleTelemetry`:
    ```go
    type telemetryMsg struct {
        Zone        string   `json:"zone"`
        Occupancy   *int     `json:"occupancy"`
        Temperature *float64 `json:"temperature"`
        Humidity    *float64 `json:"humidity"`
        Co2         *float64 `json:"co2"`
        PlugW       *float64 `json:"plugW"`
        SupplyC     *float64 `json:"supplyC"`
        AcW         *float64 `json:"acW"`
        Lux         *float64 `json:"lux"`
        Source      string   `json:"source"`
        TempReal    bool     `json:"tempReal"`
        AcReal      *bool    `json:"acReal"`
        CfgRev      *uint32  `json:"cfgRev"`
    }
    ```
    Parses JSON into `telemetryMsg`, calls `registry.observe(suffix, msg, payload)` (`server/devices.go`), and passes measurements to `engine.IngestTelemetry`.
  - `handleStatus`: Receives retained liveness payloads (`"online"` / `"offline"`) on `econ/status/<topic>` and calls `engine.SetNodeStatus`.
- Outbound Actuation:
  - `engine.PublishCommand(action, zone)` (`server/simulation/engine.go:1936`) translates actions (`purge` -> `LIGHTS_OFF;SETPOINT=18.0`, `cool` -> `LIGHTS_ON;SETPOINT=20.0`), updates `ZoneSim`, latches 15-min override, and calls `e.Publish("econ/commands/"+topic, cmd)`.
- Edge Nodes & Publishers:
  1. `edge/esp32/src/main.cpp`: ESP32 firmware publishing telemetry to `econ/telemetry/<ZONE_TOPIC>`, subscribing to `econ/commands/<ZONE_TOPIC>`, publishing retained status to `econ/status/<ZONE_TOPIC>`, and handling `econ/config/<ZONE_TOPIC>`.
  2. `edge/esp32/esp32_emulator.py`: Python emulator mirroring ESP32 wire contract.
  3. `edge/pico/main.py` + `edge/pico/bridge.py`: MicroPython firmware on RP2040 and serial-to-MQTT bridge.
  4. `edge/raspberry_pi/gateway.py`: Autonomous failsafe gateway subscribing to `econ/telemetry/+` and `econ/commands/+`, emitting setback commands `LIGHTS_OFF;SETPOINT=28.0;SRC=FAILSAFE` if the Go engine goes silent for >10s.
  5. `ai_modules/branch_a_occupancy/yolo_bytetrack/yolo_tracker.py`: YOLOv8 + ByteTrack publishing `{"zone": ..., "occupancy": count, "source": "cv"}` to `econ/telemetry/<topic>`.
  6. `bridge.py`: USB Serial to MQTT bridge for offline mode.

---

### 1.5 Logging Analysis & Payload Truncation Verification
Direct inspection of logging in all services reveals:

1. **Go Server Telemetry Logging (`server/mqtt.go:146-148`)**:
   ```go
   log.Printf("[mqtt] telemetry %s occ=%d src=%q real_temp=%v (zone=%q)",
       suffix, occ, msg.Source, msg.TempReal && msg.Temperature != nil, msg.Zone)
   ```
   **Observation**: The log statement completely omits the full incoming JSON payload (`string(payload)`). Specific sensor values (`temperature`, `humidity`, `co2`, `plugW`, `supplyC`, `acW`, `lux`, `cfgRev`) are excluded from standard logging.
   
2. **Go Server Log Level Support (`server/main.go`, `server/mqtt.go`, etc.)**:
   Go backend uses `log.Printf` directly without log level filtering (e.g. `DEBUG`, `INFO`) or environment variable controls (`LOG_LEVEL` or `ECON_LOG_LEVEL`).

3. **Python Forecasting Service (`backend/forecasting/main.py`)**:
   Uses standard `print()` calls without structured loggers or debug level configuration. Uvicorn default level is info.

4. **Edge Services Logging**:
   - `edge/raspberry_pi/gateway.py:42`: `logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")` is hardcoded to INFO.
   - `ai_modules/branch_a_occupancy/yolo_bytetrack/yolo_tracker.py:28`: `logging.basicConfig(level=logging.INFO)` is hardcoded to INFO.

---

### 1.6 Existing Test Suites & Runners
1. **Go Backend Tests (`server/`)**:
   - Command: `go test -v ./...`
   - Status: **17 test files, 100% PASS** (e.g., `auth_test.go`, `forecast_plausibility_test.go`, `modelcatalog_test.go`, `server_protocol_stress_test.go`, `simulation/*_test.go`).
2. **Dashboard E2E Test Suite (`dashboard/`)**:
   - Command: `npm test` (runs `node verify_ai_actions.js`)
   - Status: **18 test cases across 5 suites, 100% PASS** (Puppeteer headless + mock simulation engine).
3. **ESP32 Edge Tests (`edge/esp32/`)**:
   - Command: `./test/run_all_e2e_tests.sh`
   - Status: **93 test cases across 4 tiers, 100% PASS** (`host_config_test` + `test_e2e_opaque_box`).

---

## 2. Logic Chain

```
[Observation 1.2: GET /api/recommendations returns RecommendationReport without forecast series]
  + [Observation 1.3: GET /api/forecast/load & compare expose TimesFM and LSTM forecast data separately]
  + [User Request R1/R2 & Acceptance Criteria: Wire forecast graph directly into recommendations and AI panel]
  ──> Logic Step 1: RecommendationReport in server/simulation/recommend.go should be extended with a forecast graph structure, or recommendationsHandler in server/recommendapi.go should attach the forecast graph (from queryTimesFM / queryLSTM / engine load history) to the API response.
  ──> Logic Step 2: Integration tests must be updated to verify that GET /api/recommendations delivers the forecast graph payload alongside recommendations.

[Observation 1.5: server/mqtt.go logs only summary "[mqtt] telemetry %s occ=%d src=%q real_temp=%v (zone=%q)"]
  + [Observation 1.5: Raw payload []byte is dropped from server log output]
  + [User Request R3 & Acceptance Criteria: Output full MQTT telemetry JSON payloads in backend logs]
  ──> Logic Step 3: server/mqtt.go handleTelemetry must format and print the full JSON payload (e.g. log.Printf("[mqtt] telemetry %s payload=%s ...", suffix, string(payload), ...)).
  ──> Logic Step 4: A test (e.g. in server/mqtt_test.go or integration tests) must capture and assert that backend logs emit the full raw JSON telemetry string.

[Observation 1.5: Logging levels across Go server, Python forecasting, and edge gateway are hardcoded to INFO/standard print]
  + [User Request R3: Increase logging verbosity to debug level across forecasting, server, and edge]
  ──> Logic Step 5: Implement debug logging / configurable log levels (via LOG_LEVEL / DEBUG env vars) across Go server, Python forecasting (backend/forecasting/main.py), edge gateway (edge/raspberry_pi/gateway.py), and CV tracker (yolo_tracker.py).
```

---

## 3. Caveats

1. **Standalone Running Forecasting Service**: Running `backend/forecasting/test_predict.py` directly on host requires a running Uvicorn server on port 8000 (or docker compose).
2. **TimesFM Checkpoint Availability**: TimesFM is zero-shot and lazy-loaded. In container environments without network access to HuggingFace, it falls back cleanly to the supervised LSTM without crashing.
3. **Plausibility Bounds**: Any forecast graph data returned through the server API must preserve the existing plausibility checks (`checkPlausible`) against `engine.ObservedLoadRange()` to prevent out-of-distribution hallucinations on small buildings.

---

## 4. Conclusion & Actionable Gap Summary

### Gap Matrix with Respect to Requirements:

| Req | Description | Code Location | Exact Gap / Required Modification |
|---|---|---|---|
| **R1 & R2** | Forecast Graph Delivery via Backend API | `server/simulation/recommend.go`, `server/recommendapi.go`, `server/forecast.go` | 1. Extend `RecommendationReport` with `Forecast` / `ForecastGraph` field containing series data, step minutes, horizon, peak, and quantiles.<br>2. Wire `recommendationsHandler` / `engine.Recommendations` to fetch and embed the forecast graph.<br>3. Ensure frontend `useRecommendations.js` and `AiInsightsPanel.jsx` render the forecast graph directly. |
| **R3** | Full MQTT JSON Payload Logging | `server/mqtt.go:146-148` (`handleTelemetry`) | Modify log output in `handleTelemetry` to output full raw JSON payload: `log.Printf("[mqtt] telemetry %s payload=%s occ=%d ...", suffix, string(payload), ...)` or debug logger. |
| **R3** | Debug Log Level Across Services | `server/main.go`, `backend/forecasting/main.py`, `edge/raspberry_pi/gateway.py`, `ai_modules/.../yolo_tracker.py` | Add debug level logging support (configurable via `LOG_LEVEL=DEBUG` or `DEBUG=1` env var) across Go server, forecasting service, gateway, and edge trackers. |
| **AC 1** | Integration Tests for Recommendations Forecast Graph | `server/simulation/baselines_test.go`, `server/recommendapi_test.go`, `dashboard/verify_ai_actions.js` | Add/update tests asserting `GET /api/recommendations` returns non-empty forecast graph data (`series`, `peakMw`, etc.). |
| **AC 2** | Programmatic Verification Script for Forecast Chart | `dashboard/verify_ai_actions.js` | Add Puppeteer test asserting the forecast graph/chart SVG/canvas element is rendered in the AI panel UI. |
| **AC 3** | MQTT Full JSON Backend Log Validation Test | `server/mqtt_test.go` (or integration test) | Add test that feeds an MQTT payload to `handleTelemetry` with log capture and validates that the full JSON payload appears in the log stream. |

---

## 5. Verification Method

To independently verify all findings and test suites:

1. **Verify Go Backend Build & Tests**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go build ./...
   go test -v ./...
   ```
2. **Verify Dashboard E2E Test Suite (Puppeteer & Recommendations)**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard
   npm test
   ```
3. **Verify ESP32 Edge Host Test Suite**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/edge/esp32
   ./test/run_all_e2e_tests.sh
   ```
4. **Inspect MQTT Telemetry Logging Code**:
   Inspect line 146 in `/Users/nguyenhoangkhoi/Documents/econ/server/mqtt.go` to verify current omitted JSON payload logging.
5. **Inspect Recommendations API Structure**:
   Inspect line 69 in `/Users/nguyenhoangkhoi/Documents/econ/server/simulation/recommend.go` to verify absence of forecast graph series in `RecommendationReport`.
