# Backend and Sensor Integration Architecture Survey & Handoff Report

## 1. Observation

### 1.1 Backend Location & Tech Stack
- **Primary Core Backend Server**: Written in **Go 1.22** located at `/Users/nguyenhoangkhoi/Documents/econ/server`.
  - Main entry point: `server/main.go:18` (`func main()`).
  - Simulation & Physics Engine: `server/simulation/engine.go:57` (`simulation.NewEngine()`).
  - MQTT Client & Telemetry Ingestion: `server/mqtt.go:50` (`func startMQTT(engine *simulation.Engine)`).
  - Learned Recommendation Engine: `server/recommendapi.go:120` (`recommendationsHandler`), `server/simulation/recommend.go:105` (`Baselines.Recommend`), `server/simulation/baselines.go:67` (`metricSpecs`), `server/simulation/dynamics.go:241` (`Dynamics.PredictiveRecommendations`).
  - Edge Node Inspector: `server/devices.go:392` (`registerDeviceRoutes`).
  - TimescaleDB Integration: `server/db.go:41` (`initDB`), `server/db.go:135` (`historyHandler`).
  - Pre-cooling & Load Forecaster Proxy: `server/precool.go:65` (`precoolLoop`), `server/forecast.go:66` (`loadForecastHandler`).
- **Forecasting Microservice**: Written in **Python (FastAPI + PyTorch/TimesFM)** located at `/Users/nguyenhoangkhoi/Documents/econ/backend/forecasting` (`main.py`, `model.py`, `timesfm_forecaster.py`). Listens on port `8000`.
- **Digitizer Microservice**: Written in **Python (FastAPI)** located at `/Users/nguyenhoangkhoi/Documents/econ/digitizer` (`main.py`, `app.py`). Listens on port `8000` (mapped to `8090` in docker-compose).
- **Superseded / Legacy Components**:
  - `backend/core_engine/`: Early Python FastAPI + SQLite prototype. Documented in `CLAUDE.md:73-74` as having *"zero references anywhere and was superseded by the Go engine."*
  - `edge/raspberry_pi/server.py`: Legacy Python MQTT-to-WS bridge, now redundant.

---

### 1.2 Existing Endpoints for AI Recommendations & Actions Execution

#### A. AI Recommendations Endpoints
1. `GET /api/recommendations` (in `server/recommendapi.go:120`, calls `engine.Recommendations(8)`):
   - **Purpose**: Generates ranked, σ-scored anomaly and predictive recommendations from the online baseline model and room dynamics.
   - **Response Format**:
     ```json
     {
       "recommendations": [
         {
           "id": "temp:zone-open-a-lvl4",
           "zone": "zone-open-a-lvl4",
           "label": "open-a-lvl4",
           "metric": "temp",
           "severity": "critical",
           "basis": "learned",
           "title": "Zone Running Hot vs Its Learned Normal",
           "message": "open-a-lvl4 is at 27.5°C — 3.8σ above its own typical 14:00 temperature of 24.1±0.9°C...",
           "value": 27.5,
           "unit": "°C",
           "baseline": 24.1,
           "sigma": 0.9,
           "deviation": 3.8,
           "samples": 45,
           "hour": 14,
           "action": "cool",
           "kind": "anomaly",
           "etaSec": 0,
           "predicted": 0,
           "equilibrium": 0
         }
       ],
       "model": {
         "established": 12,
         "learning": 3,
         "matureAfter": 36,
         "sampleCadenceSec": 300,
         "metrics": ["temp", "co2", "buildingLoadMw", "plugKw", "occupancy"],
         "roomsIdentified": 5,
         "roomsLearning": 2,
         "horizonMin": 30
       }
     }
     ```
   - **Action types emitted**: `"cool"`, `"purge"`, `"precool"`, `""` (advisory).
   - **Kind types**: `"anomaly"`, `"prediction"`, `"capability"`.

2. `GET /api/rooms/models` (in `server/recommendapi.go:135`):
   - Returns identified physical constants per room: time constant $\tau$, cooling authority $\kappa$, air-change rate $ACH$.

3. `GET /api/model`, `GET /api/model/export`, `POST /api/model/recommend` (in `server/modelexport.go` and `server/modelcatalog.go`):
   - Metadata, export bundle, and offline bundle sizing.

#### B. Actions & Override Execution Channels
1. **Primary Control Channel — WebSocket `/ws` (Interactive Overrides & Actions)**:
   - Location: `server/main.go:250-281` (`handleWebSocket`).
   - Wire payload schemas:
     - Zone Overrides: `{"action": "cool" | "purge" | "reset" | "LIGHTS_ON;SETPOINT=20.0", "zone": "<zoneId_or_topic>"}`
     - Pre-cool Trigger: `{"action": "precool", "zone": "GLOBAL"}`
     - Auto-Pilot Toggle: `{"action": "autopilot", "value": true | false}`
     - Scenario injection: raw string text (e.g. `"fault"`, `"remediating"`, `"nominal"`)
   - Execution Logic (`simulation/engine.go:2545` `PublishCommand`):
     - Resolves `zoneRef` to `ZoneSim` and its associated `MqttTopic`.
     - Normalizes high-level actions (`"purge"` $\rightarrow$ `"LIGHTS_OFF;SETPOINT=18.0"`, `"cool"` $\rightarrow$ `"LIGHTS_ON;SETPOINT=20.0"`, `"reset"` $\rightarrow$ `"LIGHTS_ON;SETPOINT=<BaseSetpoint>"`).
     - Sets a 15-minute latch (`z.OverrideUntil = time.Now().Add(15 * time.Minute)`), preventing autonomous optimizer overwrite.
     - Immediately applies command to in-memory zone state (`applyCommandToZone`).
     - Dispatches command via MQTT topic `econ/commands/<MqttTopic>`.

2. **HTTP Control Endpoints**:
   - `POST /api/precool?minutes=20` (in `server/precool.go:197`): Starts building-wide pre-cooling. (Guarded by `X-Admin-Token` if `ECON_ADMIN_TOKEN` is set).
   - `POST /api/plugs` (in `server/plugapi.go:127`): Updates plug sweep configuration. (Guarded by `X-Admin-Token`).
   - `POST /api/building` & `POST /api/building/rollback` (in `server/blueprint.go`): Topology deployment / rollback. (Guarded by `X-Admin-Token`).

---

### 1.3 Sensor States, Storage, Updating, and Dispatch Mechanism

1. **In-Memory Twin State (`simulation.Engine`)**:
   - Holds `e.Zones map[string]*ZoneSim`, `e.Vavs map[string]*VavSim`, `e.Ahus map[string]*AhuSim`.
   - Runs a 30 fps ticker (`engine.Start()` in `server/simulation/engine.go`):
     - `tick(dt)`: 2R1C lumped capacitance thermal balance & Hardy Cross airflow balance.
     - `applyHardware()`: Pinned zones with `tempReal: true` pull zone air temperature toward physical sensor (`HwTemp`) using exponential filter ($\alpha = 0.1$, 20s freshness window). Tracks shadow twin (`ShadowTemp`) and residual EMA for AFDD.
     - `actuate()`: Autonomous optimizer evaluates occupancy; if vacant for $\ge 90$ ticks (3s), sets setback (`BaseSetpoint + 4°C`, `LIGHTS_OFF`). If pre-cooling, sets `BaseSetpoint - 1.5°C`. If manual override active (`time.Now().Before(z.OverrideUntil)`), optimizer yields to manual veto.
     - `broadcast()`: Serializes delta-compressed physics and telemetry state into FlatBuffers (`SimState`) and pushes to `/ws` clients at 30 fps.

2. **Edge Telemetry Ingestion (MQTT)**:
   - Subscribes to `econ/telemetry/+` and `econ/status/+` in `server/mqtt.go:79-90`.
   - Ingests telemetry payload (`server/mqtt.go:24` `telemetryMsg`):
     ```json
     {
       "zone": "Level 4",
       "occupancy": 3,
       "temperature": 24.5,
       "humidity": 55.0,
       "co2": 650.0,
       "plugW": 120.5,
       "supplyC": 18.2,
       "acW": 850.0,
       "lux": 350.0,
       "source": "esp32",
       "tempReal": true,
       "acReal": true
     }
     ```
   - Automatically binds node topics (`zone_1`, `pico_1`) to building zones (`server/simulation/engine.go:1330` `assignDemoZone`).

3. **Actuation Command Dispatch (MQTT)**:
   - Commands published to `econ/commands/<topic_suffix>`:
     - Format: `LIGHTS_ON;SETPOINT=20.0`, `LIGHTS_OFF;SETPOINT=26.0`, etc.
   - Handled by physical ESP32 (`edge/esp32/src/main.cpp`), Pico (`edge/pico/main.py`), or emulator (`edge/esp32/esp32_emulator.py`).

4. **Time-Series Persistence (TimescaleDB)**:
   - Stored in PostgreSQL/TimescaleDB (`server/db.go`).
   - Tables: `sensor_readings(time, zone_id, sensor_type, value, quality, device_id)` and `device_events(time, device_id, event, detail)`.
   - Flushed in asynchronous batches every 1 second (`persistReading`, `persistMeasured`).

5. **Durable Model State Checkpointing**:
   - `server/data/baseline-model.json`: learned mean & σ per (zone, metric, hour).
   - `server/data/room-dynamics.json`: online RLS parameter identification per room.
   - `server/data/load-history.json`: 5-minute electrical load series.
   - `server/data/plug-state.json`: APLC plug sweep policy and cumulative saved kWh.
   - Checkpointed every 60 seconds (`baselinePersistLoop`, `plugPersistLoop`).

---

### 1.4 Endpoints for Fetching Sensor States & Verifying State Changes

| Endpoint | Method | Path in Code | Description / Response Schema |
|---|---|---|---|
| `/ws` | WS | `server/main.go:174` | Binary FlatBuffers 30 fps stream: `ZoneData` (temp, occ, load), `VavData` (airflow, damper), `AhuData` (supplyTemp, pressure), `GlobalData` (load MW, health %, occupants, saved MW, COP). |
| `/api/hardware` | GET | `server/main.go:84` | Returns array of bound edge hardware nodes with live sensor values, online state, pin state, and AFDD residuals. |
| `/api/devices` | GET | `server/devices.go:393` | Device registry view: packet rates, field omissions, age, online status. |
| `/api/devices/series` | GET | `server/devices.go:394` | `?device=<id>&metric=<m>&minutes=<N>`: Raw historical sensor series from TimescaleDB with quality tags (`measured` vs `modelled`). |
| `/api/history` | GET | `server/main.go:70` | `?zone=<id>&minutes=<N>`: Zone temperature and occupancy history. |
| `/api/series` | GET | `server/main.go:77` | `?zone=<id>&metric=<m>&minutes=<N>`: Generic time-series query from TimescaleDB. |
| `/api/precool` | GET | `server/precool.go:197` | Pre-cooling state: `{"active": bool, "until": timestamp}`. |
| `/api/weather` | GET | `server/main.go:108` | Outdoor weather: `{"temp": float, "source": "open-meteo" | "fallback"}`. |
| `/api/plugs` | GET | `server/plugapi.go:127` | Live plug power, sweep status, cumulative saved kWh, phantom leaderboard. |
| `/api/building-data` | GET | `server/main.go:20` | Full building geometry, floor mapping, and VAV-to-zone topological mappings. |
| `/api/library` | GET | `server/main.go:59` | Programme library: design temperatures, critical zone types, plant coefficients. |

---

### 1.5 Startup, Port Configurations, and Dependencies

- **Docker Compose Stack** (`server/docker-compose.yml`):
  - `server` (Go backend): `8080:8080` (HTTP & WS)
  - `db` (TimescaleDB PostgreSQL 14): `5432:5432`
  - `mqtt` (Mosquitto 2): `1883:1883`
  - `forecasting` (Python LSTM/TimesFM): `8000:8000`
  - `digitizer` (Python FastAPI): `8090:8000`
- **Host Native Run**:
  - `cd server && go run .` (listens on port 8080 or `PORT` env var).
  - Python forecaster: `cd backend/forecasting && uvicorn main:app --port 8000`.
  - ESP32 Node Emulator: `cd edge/esp32 && python3 esp32_emulator.py --zone-topic zone_1`.
- **Dependencies**:
  - Go: `github.com/eclipse/paho.mqtt.golang v1.4.3`, `github.com/gorilla/websocket v1.5.3`, `github.com/google/flatbuffers v24.3.25`, `github.com/lib/pq v1.10.9`.

---

## 2. Logic Chain

1. **Analysis of Requirements**:
   - The user request requires connecting the frontend dashboard's AI panel, recommendations, and actions to real backend sensor APIs and verifying that action executions propagate to sensor states / edge commands.
2. **Identification of Active Backend**:
   - Investigation revealed that `server/` (Go 1.22) is the active core engine running physics, MQTT ingestion, recommendation generation, and command dispatch. `backend/core_engine/` is dead legacy code.
3. **Tracing AI Recommendations Flow**:
   - `server/recommendapi.go` serves `GET /api/recommendations`.
   - `server/simulation/engine.go:1159` aggregates live zone readings and calls `baselines.Recommend()` and `dynamics.PredictiveRecommendations()`.
   - Each recommendation object contains a structured `action` (`"cool"`, `"purge"`, `"precool"`).
   - In the frontend, `dashboard/src/useRecommendations.js` fetches `GET /api/recommendations` and passes items to `dashboard/src/AiInsightsPanel.jsx`.
4. **Tracing Action Execution & State Propagation**:
   - Clicking an action in `AiInsightsPanel.jsx` calls `sendManualOverride(rec.action, rec.zone)`.
   - `useDigitalTwin.js` sends `{"action": action, "zone": zone}` over WebSocket `/ws`.
   - `server/main.go` parses the override and calls `engine.PublishCommand(action, zone)`.
   - `engine.PublishCommand()` updates the in-memory `ZoneSim` setpoint/lights, latches the override for 15 minutes, and publishes `LIGHTS_x;SETPOINT=y` to MQTT `econ/commands/<topic>`.
   - The edge hardware / emulator executes the relay or HVAC command.
   - The engine's 30 fps FlatBuffers loop broadcasts the updated zone and global physics metrics back to the dashboard, completing the closed loop.

---

## 3. Caveats

- **Legacy Python Backend**: `backend/core_engine` exists on disk but has zero active callers; any work targeting backend APIs must be done in `server/`.
- **Authentication**: When `ECON_ADMIN_TOKEN` is set, HTTP control endpoints (`POST /api/precool`, `POST /api/plugs`, `POST /api/building`) require the `X-Admin-Token` header, and WebSocket connections require an initial `{"action":"auth","token":"..."}` handshake before sending commands. In demo mode (`ECON_ADMIN_TOKEN` unset), commands are open.
- **Hardware vs Simulated Pinning**: Only telemetry with `tempReal: true` will pin the thermodynamic simulation temperature. Simulated nodes with `tempReal: false` will update occupancy and telemetry records without forcing the 2R1C thermal model.

---

## 4. Conclusion

- The backend architecture is fully implemented in Go (`server/`) with comprehensive support for:
  1. Real-time recommendation delivery via `GET /api/recommendations`.
  2. Action execution via WebSocket JSON messages (`{"action":"...", "zone":"..."}`) and HTTP endpoints (`POST /api/precool`, `POST /api/plugs`).
  3. Real-time sensor state ingestion and edge command dispatch via MQTT (`econ/telemetry/+`, `econ/commands/+`).
  4. Live state inspection via `GET /api/hardware`, `GET /api/devices`, `GET /api/devices/series`, and the binary FlatBuffers `/ws` stream.
- The dashboard is already wired to `GET /api/recommendations` via `useRecommendations.js` and dispatches actions over WebSocket to `engine.PublishCommand`.

---

## 5. Verification Method

1. **Unit & Integration Test Suite**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test ./...
   ```
   *(Verified: all tests pass).*

2. **Recommendations Endpoint Verification**:
   ```bash
   curl -s http://localhost:8080/api/recommendations | jq .
   ```

3. **Hardware Status Verification**:
   ```bash
   curl -s http://localhost:8080/api/hardware | jq .
   ```

4. **End-to-End Action Execution Loop**:
   - Start Mosquitto broker & server: `cd server && docker compose up -d`
   - Start ESP32 node emulator: `cd edge/esp32 && python3 esp32_emulator.py --zone-topic zone_1`
   - Send action via WebSocket or curl:
     ```bash
     curl -s -X POST "http://localhost:8080/api/precool?minutes=10"
     ```
   - Verify MQTT command output:
     ```bash
     docker exec server-mqtt-1 mosquitto_sub -t 'econ/commands/#' -v
     ```
