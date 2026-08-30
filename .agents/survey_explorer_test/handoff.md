# Investigation & Survey Report: Test Infrastructure, Verification Scripts & E2E Integration Harness

## Executive Summary
This survey investigates existing tests, test infrastructure, dependencies, and execution mechanics across the ECON repository, with specific focus on the integration between the Dashboard AI Insights Panel, recommendations, and backend/sensor actuation as specified in `ORIGINAL_REQUEST.md`.

---

## 1. Inventory of Existing Test Suites & Verification Infrastructure

### 1.1 Backend Test Suites (Go Engine)
- **Location**: `server/` and `server/simulation/`
- **Runner**: `cd server && go test ./...`
- **Toolchain**: Go 1.26.4 (`go test -v ./...` runs all 17 test suites in < 1.0s)
- **Suite Details**:
  - `server/auth_test.go`: Origin check validation (`TestCheckOriginRejectsForeignOrigin`, `TestCheckOriginAcceptsLegitimateClients`, `TestCheckOriginHonoursAllowlist`), token auth parsing (`TestParseAuthMessage`, `TestEmptyTokenNeverAuthorizesWhenEnforced`, `TestDemoModeAcceptsAnyToken`), and REST admin gating (`TestRequireAdminGatesRESTWrites`).
  - `server/forecast_plausibility_test.go`: Plausibility checks between supervised LSTM and zero-shot TimesFM models (`TestForecastPlausibility`, `TestAgreementExcludesAnOutOfDistributionForecast`, `TestAgreementStillComparesTwoPlausibleForecasts`).
  - `server/modelcatalog_test.go`: Offline model package tier sizing and machine profiling (`TestRecommendModelPicksTierForRealMachines`, `TestNonFittingTiersExplainThemselves`, `TestWorkerCountIsSane`, `TestClassifyGPU`, `TestCleanGPUName`).
  - `server/telemetry_schema_test.go`: FlatBuffers binary telemetry schema serialization/deserialization.
  - `server/simulation/hardware_test.go`: Ingests physical/mock telemetry, verifies edge node sticky binding, AFDD residual computation, and includes `TestPublishCommandAppliesState` which tests `e.PublishCommand("LIGHTS_OFF;SETPOINT=27.5", "Pico Lab")` and `e.PublishCommand("reset", "Pico Lab")`.
  - `server/simulation/dynamics_test.go`: Recursive Least Squares (RLS) identification of room thermal dynamics (time constant, cooling authority) and ventilation dynamics (ACH).
  - `server/simulation/baselines_test.go`: Online anomaly detection model (Gaussian mean/std per zone, metric, and hour).
  - `server/simulation/autopilot_test.go`: Autonomous setback optimizer and manual veto override behavior.
  - `server/simulation/plugs_test.go`: Automatic Plug Load Control (APLC) sweeps, standby shedding, and restore.
  - `server/simulation/measured_test.go`: Distinction between measured (`tempReal: true`) and modelled physics values.
  - `server/simulation/bess_sizing_test.go`, `forecast_window_test.go`, `occupancy_test.go`, `state_provenance_test.go`, `site_test.go`.

### 1.2 Edge Firmware & Hardware Test Suites (ESP32)
- **Location**: `edge/esp32/test/`
- **Runner**: `cd edge/esp32 && ./test/run_all_e2e_tests.sh`
- **Harness**: `test_e2e_opaque_box.cpp` + `host_config_test.cpp` with `arduino_shim.h`
- **Coverage**: 93 off-target C++17 tests across 4 tiers (Feature Coverage, Boundary/Corner, Pairwise Integration, and Real-World Workload Scenarios). 100% pass rate.

### 1.3 Machine Learning / Forecaster Test Suites (Python)
- **Location**: `backend/forecasting/`
- **Runner**: `python3 backend/forecasting/test_predict.py`
- **Coverage**: Sanity smoke test verifying `/health`, `/predict`, and monotonicity of predicted peak load against temperature and airflow.

### 1.4 Dashboard UI & Frontend Verification Scripts (JavaScript/Node.js)
- **Location**: `dashboard/`
- **Framework**: React 18 + Vite 5 + Three.js + FlatBuffers + Puppeteer 25.1.0
- **Runner Scripts**:
  - `dashboard/verify_stable.js`: Launches Puppeteer, loads `http://localhost:5173`, captures screenshots at 5s and 15s.
  - `dashboard/verify_stable2.js`: Captures screenshots every 5s up to 30s, counting runtime page/console errors.
  - `dashboard/check_dom.js`: Verifies `.hud-container` rendering and left dock DOM elements.
  - `dashboard/check_errors.js`: Evaluates page load errors, console errors, and HTML body length.
  - `dashboard/check_mobile.js`, `check_mobile_error.js`, `check_dom_mobile.js`: Emulates mobile viewport (390x844) to test mobile Impact/AI screens.
  - `dashboard/take_screenshot.js`, `dashboard/debug.cjs`: Ad-hoc visual debugging.

---

## 2. Test Execution Mechanics & Dependency Environment

| Layer | Runner / Command | Key Dependencies / Tools | Execution Notes / Sandbox Gotchas |
|---|---|---|---|
| **Go Backend** | `cd server && go test ./...` | Go 1.26.4 | Extremely fast (<1s), runs in-memory without requiring TimescaleDB or Mosquitto containers. |
| **ESP32 Edge** | `cd edge/esp32 && ./test/run_all_e2e_tests.sh` | Apple Clang / C++17, ArduinoJson | Off-target hermetic compilation using shims. Exits code 0. |
| **Python Forecaster** | `python3 backend/forecasting/test_predict.py` | Python 3.13.7, requests | Requires forecasting service running on `:8000`. |
| **Dashboard Puppeteer** | `node dashboard/check_errors.js` | Node v24.13.0, Puppeteer v25.1.0, Chrome for Testing v149 | **Sandbox Gotcha**: In sandboxed environments (macOS Mach port rendezvous), launching Chrome requires `--no-sandbox` / `--disable-setuid-sandbox` args and `BypassSandbox: true`. |
| **Full Stack Integration** | Currently manual / partial | Go Server (:8080), Vite (:5173), Mosquitto (:1883) | Backend runs with `cd server && go run .` (or `docker-compose up -d`); Frontend runs with `cd dashboard && npm run dev`. |

---

## 3. Data Flow & Mechanics: AI Panel Actions to Backend & Sensors

### 3.1 AI Panel & Recommendation Ingestion
1. **Source**: `server/simulation/recommend.go` computes `RecommendationReport` containing ranked anomalies, dynamic predictions (time-to-breach `etaSec`), and capabilities.
2. **API Endpoint**: `GET /api/recommendations` serves `{ "recommendations": [...], "model": {...} }`.
3. **Frontend Ingestion**: `dashboard/src/useRecommendations.js` polls `GET /api/recommendations` every 10s and feeds `dashboard/src/AiInsightsPanel.jsx` and `dashboard/src/MobileAIScreen.jsx`.

### 3.2 Action Interactivity & Remediations
Each recommendation or insight card exposes actionable remediation triggers:
- **Purge Zone**: Recommendation with `action: "purge"` -> renders "PURGE ZONE" button -> calls `sendManualOverride('purge', rec.zone)`.
- **Flood Cooling**: Recommendation with `action: "cool"` or Critical Fault -> renders "FLOOD COOLING" button -> calls `sendManualOverride('cool', rec.zone)`.
- **Pre-Cooling**: High Grid Demand card or recommendation with `action: "precool"` -> renders "ACTIVATE PRE-COOLING" button -> calls `sendManualOverride('precool', 'GLOBAL')` (or `POST /api/precool`).
- **Zone Setpoint Vetoes**: In micro-HUD / maintenance drawer -> calls `sendManualOverride('LIGHTS_OFF;SETPOINT=26.0', selectedZone)` or `sendManualOverride('LIGHTS_ON;SETPOINT=20.0', selectedZone)`.
- **Auto-Pilot Toggle**: AI Auto-Pilot toggle in UI -> sends `{ "action": "autopilot", "value": boolean }`.

### 3.3 WebSocket Dispatch to Go Engine
In `dashboard/src/useDigitalTwin.js`:
```javascript
const sendManualOverride = (action, zoneId) => {
  if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
    wsRef.current.send(JSON.stringify({ action, zone: zoneId }));
  }
};
```
In `server/main.go` (`handleWebSocket`):
- For `action == "precool"`: calls `engine.StartPreCool(20 * time.Minute)`.
- For `action == "autopilot"`: calls `engine.SetAutoPilot(*ap.Value)`.
- For `override["zone"]`: calls `engine.PublishCommand(action, zone)`.

### 3.4 Backend Actuation & Sensor State Updates
In `server/simulation/engine.go` (`PublishCommand`):
1. Resolves target zone `z`.
2. Latches human override for 15 minutes: `z.OverrideUntil = time.Now().Add(15 * time.Minute)`.
3. Normalizes verb via `normalizeOverride`:
   - `"purge"` -> `"LIGHTS_OFF;SETPOINT=18.0"`
   - `"cool"` -> `"LIGHTS_ON;SETPOINT=20.0"`
   - `"reset"` -> `"LIGHTS_ON;SETPOINT=<BaseSetpoint>"`
4. Applies command directly to zone state via `applyCommandToZone(z, cmd)`:
   - Updates `z.LightsOn` (true/false)
   - Updates `z.Setpoint` (e.g. 18.0, 20.0, 26.0)
5. Dispatches MQTT payload to edge hardware topic: `econ/commands/<topic>`.

### 3.5 Verifiable Observability Channels
- **HTTP `GET /api/hardware`**: Returns snapshot of all hardware nodes with live `lightsOn`, `setpoint`, `zoneTemp`, `hwTemp`, `residual`, `afddAlert`.
- **HTTP `GET /api/precool`**: Returns `{"active": true, "until": "<timestamp>", "durationMinutes": 20}`.
- **WebSocket Binary Stream**: FlatBuffers stream emitting real-time zone setpoints, temperatures, and load.
- **MQTT Broker**: Messages on `econ/commands/+`.

---

## 4. Features Discovered & Probed

### Features Discovered
| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Dashboard UI | AI Recommendations Polling | Frontend hook fetching live recommendations from engine | Polling interval (10s), `GET /api/recommendations` | Array of recommendations with σ-scores, kind, action | Graceful fallback to empty report | `dashboard/src/useRecommendations.js` |
| 2 | Dashboard UI | AI Insights Action Dispatch | UI button triggering remediation for anomalies / forecasts | Click event on action button | WebSocket JSON payload `{action, zone}` | Disabled when engaged / settled | `dashboard/src/AiInsightsPanel.jsx` |
| 3 | Dashboard UI | Auto-Pilot Toggle | Live control switch to suspend/resume autonomous optimizer | Toggle switch change | WebSocket JSON `{action: "autopilot", value: bool}` | Optimistic update with engine echo | `dashboard/src/useDigitalTwin.js` |
| 4 | Dashboard UI | Manual Veto Overrides | Micro-HUD and drawer buttons for direct zone control | Click FORCE OFF, MAX COOL, PURGE, RESET | WebSocket JSON `{action, zone}` | Ignored if WS not open | `dashboard/src/App.jsx`, `MaintenanceDrawer.jsx` |
| 5 | Backend API | `/api/recommendations` | Generates ranked anomaly and predictive breach recommendations | `GET /api/recommendations` | JSON `RecommendationReport` | Returns empty array if models cold | `server/recommendapi.go`, `recommend.go` |
| 6 | Backend API | `/api/precool` | Status and trigger for 20-minute building-wide pre-cool window | `GET /api/precool`, `POST /api/precool?minutes=` | JSON `{active: bool, until: timestamp}` | POST requires admin token if enforced | `server/precool.go` |
| 7 | Backend API | `/api/hardware` | Live snapshot of edge hardware nodes and bound zone telemetry | `GET /api/hardware` | Array of `HardwareNode` objects with setpoint, lights, sensors | Returns empty array if no hardware bound | `server/main.go`, `engine.go` |
| 8 | Backend WS | `/ws` Command Channel | WebSocket channel ingesting control payloads and streaming binary telemetry | Text JSON (`auth`, `autopilot`, `{action, zone}`) | Binary FlatBuffers telemetry, auth responses | Rejects unauthenticated commands if token enforced | `server/main.go` |
| 9 | Simulation Engine | `PublishCommand` & Latching | Normalizes override verbs, updates zone state, latches 15m, publishes MQTT | `action`, `zoneRef` | Updates `z.Setpoint`, `z.LightsOn`, `z.OverrideUntil`, publishes MQTT | Forwards unknown verbs verbatim | `server/simulation/engine.go` |
| 10 | Simulation Engine | Zone Command Application | Parses tokenized commands (`LIGHTS_x`, `SETPOINT=`) into zone state | Command string (e.g. `LIGHTS_OFF;SETPOINT=18.0`) | Mutates `ZoneSim` struct fields | Silently ignores unparseable tokens | `server/simulation/engine.go` |
| 11 | Edge Tests | ESP32 Off-Target Harness | 93-test C++17 opaque-box test runner for dual-mode and PIR/camera comms | Off-target test binary | Exit code 0, test logs | Exit code 1 on assertion failure | `edge/esp32/test/run_all_e2e_tests.sh` |
| 12 | Dashboard Scripts | Puppeteer DOM/Error Verifiers | Ad-hoc Puppeteer scripts verifying page load, mobile layouts, stability | HTTP URL `http://localhost:5173` | Console logs, screenshots | Logs page errors and exits | `dashboard/verify_stable.js`, `check_errors.js` |

### Edge Cases
| # | Feature | Input | Observed Behavior |
|---|---------|-------|-------------------|
| 1 | Puppeteer in macOS Sandbox | `puppeteer.launch({headless: 'new'})` without `--no-sandbox` | Fails with Mach port rendezvous permission error unless `--no-sandbox` and `BypassSandbox: true` are used. |
| 2 | Backend Auth Enforcement | Command sent over WebSocket without prior `auth` handshake when `ECON_ADMIN_TOKEN` is set | Server rejects command with `{"type":"error","error":"unauthorized..."}`. In demo mode (default), auth is not enforced. |
| 3 | Pre-cool Duplicate Trigger | Triggering "ACTIVATE PRE-COOLING" while a window is already active | Button displays `✓ OPEN UNTIL <time>` and disables clicking. Backend returns current active window. |
| 4 | Unknown Zone Reference | Overriding a zone reference that does not match any zone ID or MQTT topic | `resolveZone` returns nil; command is published to MQTT `econ/commands/<zoneRef>` verbatim without engine zone mutation. |
| 5 | Custom High-Level Verbs | Action verb `"purge"`, `"cool"`, `"reset"` | `normalizeOverride` translates `"purge"` -> `LIGHTS_OFF;SETPOINT=18.0`, `"cool"` -> `LIGHTS_ON;SETPOINT=20.0`, `"reset"` -> `LIGHTS_ON;SETPOINT=<BaseSetpoint>`. |

---

## 5. Specific Gaps Between Existing Coverage and Requirements

| Requirement in ORIGINAL_REQUEST.md | Existing Coverage | Specific Gaps |
|---|---|---|
| **R1. AI Panel & Recommendations Wiring** | `useRecommendations.js` fetches `/api/recommendations`, `AiInsightsPanel.jsx` renders cards. | No automated test verifies that the dashboard UI properly renders live recommendations when `/api/recommendations` returns data vs empty state. |
| **R2. Action Interactivity** | Action buttons in `AiInsightsPanel.jsx` call `sendManualOverride`. | Existing Puppeteer scripts (`check_errors.js`, `verify_stable.js`) only check static page load; none click buttons or trigger AI actions programmatically. |
| **R3. Real Sensor & Backend State Updates** | Backend `PublishCommand` sets `z.Setpoint`, `z.LightsOn`, latches 15m, and dispatches MQTT. Unit tested in `hardware_test.go`. | No end-to-end integration test verifies that clicking a UI action -> sends WS message -> updates `/api/hardware` or `/api/precool` -> verifies sensor state change in the running system. |
| **Acceptance Criteria: Automated Verification Script** | Ad-hoc test scripts exist in pieces (`hardware_test.go`, `check_errors.js`), but no unified E2E integration test script exists. | Need a dedicated automated script (e.g. `dashboard/verify_ai_actions.js` or `test/verify_ai_e2e.js`) that automates: (1) action trigger from UI/API, (2) backend receipt verification, (3) sensor state update verification via `/api/hardware` and `/api/precool`. |
| **Acceptance Criteria: npm test script** | `dashboard/package.json` only contains `"dev"`, `"build"`, `"preview"`, `"dev:local"`. | Missing `"test"` target in `dashboard/package.json`. |

---

## 6. Recommended Automated Verification Harness Design

To fully satisfy the Acceptance Criteria in `ORIGINAL_REQUEST.md`, an automated integration test script should be structured as follows:

```
[Test Harness Orchestrator]
   ├── 1. Ensure Backend (:8080) and Dashboard (:5173) are running
   ├── 2. Test Strategy Action (Pre-Cooling):
   │     ├── Trigger Pre-Cooling via AI Panel button or WebSocket {action: "precool", zone: "GLOBAL"}
   │     ├── Query GET /api/precool
   │     └── Assert: active === true, durationMinutes === 20, until is populated
   ├── 3. Test Recommendation Action (Purge / Flood Cooling):
   │     ├── Send WebSocket override {action: "purge", zone: "<target_zone>"}
   │     ├── Query GET /api/hardware (or inspect engine zone state)
   │     └── Assert: zone setpoint === 18.0, lightsOn === false, override latched
   ├── 4. Test Zone Veto Action (Max Cool / Force Off):
   │     ├── Send WebSocket override {action: "LIGHTS_ON;SETPOINT=20.0", zone: "<target_zone>"}
   │     ├── Query GET /api/hardware
   │     └── Assert: zone setpoint === 20.0, lightsOn === true
   ├── 5. Puppeteer UI Interaction Test (Headless):
   │     ├── Launch Chrome (--no-sandbox)
   │     ├── Navigate to http://localhost:5173
   │     ├── Find and click AI panel action button
   │     └── Verify network / WS frame dispatch and subsequent UI state reflection (e.g. "✓ ENGAGED")
   └── 6. Return Exit Code 0 on 100% pass, non-zero on failure
```

---

## 7. 5-Component Handoff Protocol

### 1. Observation
- Inspected all test files across `server/` (17 Go test files), `dashboard/` (10 JS scripts, `package.json`), `edge/esp32/test/` (93 C++ tests in `run_all_e2e_tests.sh`), and `backend/forecasting/` (`test_predict.py`).
- Executed `cd server && go test ./...` -> All Go unit tests pass in 0.492s.
- Executed `cd edge/esp32 && ./test/run_all_e2e_tests.sh` -> All 93 host E2E tests pass with exit code 0.
- Executed Puppeteer launch with `--no-sandbox` and `BypassSandbox: true` -> Successfully connected to Chrome v149.
- Traced `AiInsightsPanel.jsx` action handlers (`sendManualOverride`) -> WebSocket `/ws` -> `server/main.go` -> `engine.PublishCommand` & `engine.StartPreCool` -> `applyCommandToZone` -> `/api/hardware` & `/api/precool`.

### 2. Logic Chain
1. The backend implements full WebSocket and HTTP endpoints for manual vetoes, recommendations, pre-cooling, and hardware status inspection.
2. The frontend AI panel has hooks (`useRecommendations`, `useOpsStatus`) and event handlers (`sendManualOverride`) wired to these channels.
3. Unit tests exist in Go (`hardware_test.go`) validating `PublishCommand` state changes, and ad-hoc Puppeteer scripts exist in `dashboard/` validating page rendering and DOM stability.
4. However, there is a gap in automated integration testing: no single test script links UI button actuation to live WebSocket dispatch and validates the resulting sensor/zone state update on the backend API.
5. Creating a dedicated verification script (e.g. `dashboard/verify_ai_actions.js` or `test/verify_ai_e2e.js`) with an `npm test` script entry will bridge this gap and fulfill the Acceptance Criteria.

### 3. Caveats
- Running Puppeteer on macOS requires `--no-sandbox` and `BypassSandbox: true` due to OS-level Mach port sandbox restrictions.
- In production mode with `ECON_ADMIN_TOKEN` set, WebSocket clients must send an `auth` message before dispatching overrides; in default demo mode, commands are accepted unconditionally.
- TimescaleDB and Mosquitto broker are optional for local Go unit tests (which run in-memory), but live MQTT dispatch verification requires either Mosquitto or an in-memory mock handler.

### 4. Conclusion
The repository has mature unit testing (Go engine) and firmware host testing (ESP32 C++), but lacks an end-to-end automated verification script that tests the AI panel action -> WebSocket -> Backend -> Sensor state update loop. Creating this verification script will provide 100% compliance with the Acceptance Criteria in `ORIGINAL_REQUEST.md`.

### 5. Verification Method
1. Run Backend Tests: `cd server && go test ./...` (Expected: PASS).
2. Run Edge Firmware Tests: `cd edge/esp32 && ./test/run_all_e2e_tests.sh` (Expected: 93/93 PASS).
3. Test Puppeteer Runtime: `node -e "import('puppeteer').then(async p => { const b = await p.default.launch({headless: 'new', args: ['--no-sandbox']}); console.log(await b.version()); await b.close(); })"` (Expected: Chrome version printed).
4. Run integration verification script once implemented (Expected: exit code 0).
