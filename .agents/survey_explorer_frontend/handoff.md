# Frontend Dashboard Investigation Report: AI Panel, Recommendations, and Actions

## 1. Observation

### 1.1 Project Structure & Build Configuration
- **Root Directory**: `/Users/nguyenhoangkhoi/Documents/econ/dashboard`
- **Build System & Framework**:
  - `package.json` specifies:
    ```json
    "name": "econ-dashboard",
    "private": true,
    "version": "0.0.0",
    "type": "module",
    "scripts": {
      "dev": "vite --host",
      "build": "vite build",
      "preview": "vite preview --host",
      "dev:local": "vite"
    },
    "dependencies": {
      "@react-three/drei": "^9.106.0",
      "@react-three/fiber": "^8.17.10",
      "@xyflow/react": "^12.11.0",
      "flatbuffers": "^25.9.23",
      "lucide-react": "^0.294.0",
      "puppeteer": "^25.1.0",
      "react": "^18.2.0",
      "react-dom": "^18.2.0",
      "react-is": "^19.2.7",
      "recharts": "^3.8.1",
      "three": "^0.184.0",
      "three-bvh-csg": "^0.0.18"
    },
    "devDependencies": {
      "@types/react": "^18.2.37",
      "@types/react-dom": "^18.2.15",
      "@vitejs/plugin-react": "^4.2.0",
      "vite": "^5.0.0"
    }
    ```
  - `vite.config.js`:
    ```javascript
    import { defineConfig } from 'vite'
    import react from '@vitejs/plugin-react'
    export default defineConfig({
      plugins: [react()],
    })
    ```
  - Executed `npm run build`: built in 3.15s with 0 errors.

### 1.2 API Client & Connection Architecture
- `src/api.js` (lines 10-18, 34-50):
  - Dynamic host resolution:
    ```javascript
    const backendHost =
      import.meta.env.VITE_BACKEND_HOST ||
      (typeof window !== 'undefined' && window.location.hostname) ||
      'localhost';
    const BACKEND_PORT = import.meta.env.VITE_BACKEND_PORT || '8080';
    export const API_BASE = `http://${backendHost}:${BACKEND_PORT}`;
    export const WS_URL = `ws://${backendHost}:${BACKEND_PORT}/ws`;
    ```
  - Admin token storage in `localStorage['econ.adminToken']` via `getAdminToken()` and `setAdminToken(token)`.
- `src/useDigitalTwin.js` (lines 170-245):
  - WebSocket lifecycle at `WS_URL` with binary FlatBuffers for inbound state (`SimState.getRootAsSimState(buf)`) at 1 Hz.
  - Outbound command dispatcher:
    ```javascript
    const sendManualOverride = (action, zoneId) => {
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.send(JSON.stringify({ action, zone: zoneId }));
      }
    };
    ```
  - Auto-Pilot toggle:
    ```javascript
    const setAutoPilot = (next) => {
      const val = typeof next === 'function' ? next(autoPilotRef.current) : next;
      autoPilotRef.current = val;
      setAutoPilotState(val);
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.send(JSON.stringify({ action: 'autopilot', value: val }));
      }
    };
    ```
  - Scenario loader: `loadScenario(key)` sends `ws.send(key)`.

### 1.3 AI Panel and Recommendations Implementation
- **Desktop Component**: `src/AiInsightsPanel.jsx` (1070 lines)
  - Receives props: `{ simData, activeScenario, faultTarget, aiForecast, setAutoPilot, hardwareNodes, setSelectedZone, sendManualOverride, onOpenPlugs }`.
  - Consumes live backend data through hooks:
    - `useRecommendations()` (`src/useRecommendations.js`, lines 13-31): polls `GET /api/recommendations` every 10s.
    - `useOpsStatus()` (`src/useOpsStatus.js`): polls `GET /api/precool` and `GET /api/weather` every 15s.
    - `usePlugs()` (`src/usePlugs.js`): polls `GET /api/plugs` every 10s.
    - `useLibrary()` (`src/useLibrary.js`): fetches `GET /api/library`.
    - `useRoomModels()` (`src/useRoomModels.js`): polls `GET /api/rooms/models` every 30s.
    - `useForecastCompare()` (`src/useForecastCompare.js`): polls `GET /api/forecast/compare` every 60s.
    - `useLocalModel()` (`src/useLocalModel.js`): calls `POST /api/model/recommend`.
  - Insight card generation (lines 82-371):
    1. **Scenario Fault**: id `fault`, action `FLOOD ZONE WITH COOLING` -> calls `sendManualOverride('cool', faultTarget)`.
    2. **Edge Node Offline**: id `offline`, action `SHOW IN 3D` -> calls `setSelectedZone(deadNodes[0].zoneId)`.
    3. **Hardware In The Loop**: id `hardware`, action `INSPECT NODES`.
    4. **AFDD Physics Divergence**: id `afdd`, action `VIEW DRIFT HISTORY` -> expands `AfddDriftDetail` querying `GET /api/series?zone=...&metric=afddResidual&minutes=120`.
    5. **Learned Recommendations** (from `recommendations` array):
       - Action labels: `purge` -> `PURGE ZONE`, `cool` -> `FLOOD COOLING`, `precool` -> `ACTIVATE PRE-COOLING`.
       - Badges: `PREDICTED`, `CAPABILITY`, `LEARNED`, `ASHRAE STD`.
       - Handler: `onAction: () => sendManualOverride(rec.action, rec.zone)`.
       - Toggle: `▾ why this fired` -> expands `RecommendationEvidence.jsx`.
    6. **Peak Tariff**: id `peak`, action `ACTIVATE PRE-COOLING` -> calls `sendManualOverride('precool', 'GLOBAL')`.
    7. **LSTM Forecast**: id `forecast`, action `VIEW PREDICTIONS`.
    8. **Weather Feed Stale**: id `weather`.
    9. **Plug Sweep**: id `plugs`, action `OPEN PLUGS TAB` -> calls `onOpenPlugs()`.
    10. **Unoccupied Zones**: id `wasting`, action `OPEN PLUGS TAB`.
    11. **Autonomous Operations**: id `general`, action `VIEW MODEL METRICS`.
  - Subcomponents embedded in `AiInsightsPanel.jsx`:
    - `RoomModelsCard` (lines 980-1069): displays online RLS identified models for each room.
    - `ModelExportCard` (lines 759-915): offline model bundle download (`GET /api/model/export`).
    - `AfddDriftDetail` (lines 922-963): Recharts line chart of persisted residual drift from `/api/series`.

- **Mobile Component**: `src/MobileAIScreen.jsx` (512 lines)
  - Renders master Auto-Pilot switch (`setAutoPilot(!autoPilot)`).
  - Renders cards from `useRecommendations()` with action buttons (`PURGE ZONE`, `FLOOD COOLING`, `ACTIVATE PRE-COOLING`) calling `sendManualOverride(rec.action, rec.zone)`.
  - Pre-cooling card calling `sendManualOverride('precool', 'GLOBAL')`.

- **Detailed Evidence Component**: `src/RecommendationEvidence.jsx` (208 lines)
  - `SigmaStrip`: Renders $\sigma$ bounds ($\pm 2\sigma$, $\pm 4\sigma$) and current z-score.
  - `TrajectoryStrip`: Evaluates first-order step response $v(t) = eq + (now - eq) \cdot e^{-t/\tau}$ over prediction horizon.
  - Tabular breakdown: Reading now, Learned normal, Deviation ($\sigma$), Hour bucket, Time to breach, Supply-air basis (% measured probe vs design).

### 1.4 Other Action / Interaction Surfaces in the Frontend
- `src/App.jsx`:
  - Lines 584-607: `showAiModal` modal with button `EXECUTE RECOMMENDATION` calling `executeRemediation()` (lines 509-515), which invokes `loadScenario('remediating')` then `loadScenario('peak')`.
  - Lines 713-715: Micro-HUD drill-down buttons:
    - `FORCE OFF` -> `sendManualOverride('LIGHTS_OFF;SETPOINT=26.0', selectedZone)`
    - `MAX COOL` -> `sendManualOverride('LIGHTS_ON;SETPOINT=20.0', selectedZone)`
    - `IR FAN` -> `sendManualOverride('IR_SEND:NEC:0xFF00FF:32', selectedZone)`
- `src/GlobalMetricsPanel.jsx`:
  - Lines 478-493: Micro-metrics buttons:
    - `PURGE` -> `sendManualOverride('purge', selectedNode.id)`
    - `MAX COOL` -> `sendManualOverride('cool', selectedNode.id)`
    - `RESET` -> `sendManualOverride('reset', selectedNode.id)`
- `src/MaintenanceDrawer.jsx`:
  - Lines 158-168: `FLOOD ZONE WITH COOLING` button -> `sendManualOverride('cool', zoneId)`.
- `src/PlugLoadPanel.jsx`:
  - Lines 42-45: `SAVE POLICY` button -> `updateConfig(cfg)` (`POST /api/plugs`).
- `src/TelemetryPanel.jsx`:
  - Lines 33: `applySuggestion` -> `setAutoPilot(true)`.

### 1.5 Backend Action Mapping & Execution Flow
- In `server/main.go` (lines 251-280):
  - Inbound WebSocket messages starting with `{`:
    - `{"action": "autopilot", "value": true/false}` -> `engine.SetAutoPilot(*ap.Value)`
    - `{"action": "precool"}` -> `engine.StartPreCool(precoolWindow)`
    - `{"action": action, "zone": zone}` -> `engine.PublishCommand(action, zone)`
- In `server/simulation/engine.go` (lines 2545-2619):
  - `PublishCommand(action, zoneRef)`:
    - Normalizes action verbs:
      - `purge` -> `"LIGHTS_OFF;SETPOINT=18.0"`
      - `cool` -> `"LIGHTS_ON;SETPOINT=20.0"`
      - `reset` -> `"LIGHTS_ON;SETPOINT=24.0"` (or zone BaseSetpoint)
      - Explicit wire format (`LIGHTS_x;SETPOINT=y`) passes through unchanged.
    - Sets 15-minute human veto latch: `z.OverrideUntil = time.Now().Add(15 * time.Minute)`.
    - Applies immediately to simulation zone: `applyCommandToZone(z, cmd)`.
    - Publishes MQTT command: `econ/commands/<topic>` with `cmd` to physical edge device.

---

## 2. Logic Chain

1. **Analysis of Recommendation Generation**:
   - `useRecommendations.js` fetches live anomaly reports directly from the backend endpoint `GET /api/recommendations`.
   - `server/recommendapi.go` calls `engine.Recommendations(8)`, which merges online statistical baselines (`simulation/baselines.go`) and online recursive least squares room dynamics (`simulation/dynamics.go`).
   - The frontend `AiInsightsPanel.jsx` iterates over the returned recommendations (`recommendations.forEach((rec) => ...)`), instantiating actionable UI cards.

2. **Analysis of Action Handling**:
   - When an operator clicks an action button (e.g., `FLOOD COOLING`, `PURGE ZONE`, `ACTIVATE PRE-COOLING`):
     - For zone recommendations: `sendManualOverride(rec.action, rec.zone)` is executed.
     - For pre-cool: `sendManualOverride('precool', 'GLOBAL')` is executed.
   - `sendManualOverride` sends a JSON payload `{ action, zone }` over the persistent WebSocket connection.
   - The Go backend receives the WebSocket text frame, unmarshals the JSON, and calls `engine.PublishCommand(action, zone)`.
   - `PublishCommand` converts high-level actions (`purge`, `cool`, `reset`) into hardware-compatible commands (`LIGHTS_OFF;SETPOINT=18.0`, etc.), updates local zone setpoints, latches the override for 15 minutes, and publishes to MQTT topic `econ/commands/<device_topic>`.
   - The updated zone setpoint immediately alters the thermal simulation and streams back in the next FlatBuffer frame.

3. **Identification of Mocked / Legacy Remnants vs Real APIs**:
   - `AiInsightsPanel.jsx` and `MobileAIScreen.jsx` are already wired to real endpoints (`/api/recommendations`, `/api/precool`, `/api/weather`, `/api/plugs`, `/api/library`, `/api/rooms/models`, `/api/forecast/compare`, `/api/series`, and WebSocket commands).
   - **Legacy remnant identified**: In `App.jsx` (lines 509-515 and 584-607), the popup modal `showAiModal` ("AI Override Recommendation") triggered during `activeScenario === 'fault'` still uses a synthetic helper `executeRemediation()` that calls `loadScenario('remediating')` instead of issuing a real manual override `sendManualOverride('cool', faultTarget)`.

---

## 3. Caveats

- **No Code Changes Made**: In accordance with the Explorer persona and read-only constraints, no source code was modified.
- **Edge Device Physical Availability**: If physical hardware (ESP32/Pico) is offline, `PublishCommand` updates the 2R1C digital twin zone and latches the state, while MQTT dispatch is buffered/published to the broker.

---

## 4. Conclusion & Answers to Specific Objectives

### Q1: How AI Panel, Recommendations, and Actions are Implemented
- **Desktop UI**: `AiInsightsPanel.jsx` rendered inside the left dock under the `insights` tab.
- **Mobile UI**: `MobileAIScreen.jsx` rendered when the mobile view is active.
- **State Management**: React state + Custom Hooks (`useRecommendations`, `useOpsStatus`, `usePlugs`, `useLibrary`, `useRoomModels`, `useForecastCompare`, `useLocalModel`, `useDigitalTwin`).
- **UI Elements**: Responsive cards with severity coloring (`accent-red`, `accent-yellow`, `accent-blue`), provenance badges (`PREDICTED`, `CAPABILITY`, `LEARNED`, `ASHRAE STD`), inline action buttons, collapsible evidence sections (`RecommendationEvidence.jsx`), and offline model export panels.

### Q2: Location of Hardcoded / Mocked Data vs Real API Endpoints
- **Real Backend APIs**:
  - `GET /api/recommendations`: Live statistical baseline + dynamic prediction report.
  - `GET /api/rooms/models`: Identified room thermal constants ($\tau$, residuals, air change).
  - `GET /api/forecast/compare`: Side-by-side LSTM vs TimesFM load predictions.
  - `GET /api/precool`: Pre-cooling window status (`active`, `until`).
  - `GET /api/weather`: Outdoor temperature and live vs fallback status.
  - `GET /api/plugs` & `POST /api/plugs`: Plug load sweep status and policy updates.
  - `GET /api/library`: Building geometry, physics constants, critical zone types.
  - `GET /api/series`: Historical metric/residual queries from TimescaleDB.
  - `POST /api/model/recommend` & `GET /api/model/export`: Local model sizing and bundle export.
  - `WS /ws`: 1Hz binary FlatBuffers (`SimState`) and bidirectional JSON control.
- **Mocked / Legacy Remnant**:
  - `App.jsx` (lines 509-515, 584-607): `executeRemediation` modal sends synthetic scenario strings (`remediating`, `peak`) via `loadScenario` rather than executing `sendManualOverride('cool', faultTarget)`.

### Q3: UI Interactions / Buttons for Applying Recommendations & Event Handling
| UI Element / Button | Location | Click Handler | Wire / API Action |
|---|---|---|---|
| `PURGE ZONE` | `AiInsightsPanel.jsx` / `MobileAIScreen.jsx` | `onAction()` | `sendManualOverride('purge', rec.zone)` -> WS JSON `{ action: "purge", zone }` |
| `FLOOD COOLING` | `AiInsightsPanel.jsx` / `MobileAIScreen.jsx` | `onAction()` | `sendManualOverride('cool', rec.zone)` -> WS JSON `{ action: "cool", zone }` |
| `ACTIVATE PRE-COOLING` | `AiInsightsPanel.jsx` / `MobileAIScreen.jsx` | `onAction()` | `sendManualOverride('precool', 'GLOBAL')` -> WS JSON `{ action: "precool" }` |
| `▾ why this fired` | `AiInsightsPanel.jsx` | `toggle(id)` | Expands `RecommendationEvidence.jsx` |
| `EXECUTE RECOMMENDATION` (Modal) | `App.jsx` | `executeRemediation()` | `loadScenario('remediating')` (Legacy scenario string) |
| `FORCE OFF` / `MAX COOL` / `IR FAN` | `App.jsx` (Micro-HUD) | `onClick` | `sendManualOverride(...)` with raw firmware strings |
| `PURGE` / `MAX COOL` / `RESET` | `GlobalMetricsPanel.jsx` | `onClick` | `sendManualOverride(action, selectedNode.id)` |
| `FLOOD ZONE WITH COOLING` | `MaintenanceDrawer.jsx` | `onClick` | `sendManualOverride('cool', zoneId)` |
| `Auto-Pilot Toggle` | `MobileAIScreen.jsx` / `TelemetryPanel.jsx` | `setAutoPilot` | WS JSON `{ action: "autopilot", value: boolean }` |
| `SAVE POLICY` | `PlugLoadPanel.jsx` | `save()` | `POST /api/plugs` with `X-Admin-Token` |
| `DOWNLOAD MODEL BUNDLE` | `AiInsightsPanel.jsx` | `<a>` link | `GET /api/model/export?tier=...` |

### Q4: Build System, Dependencies, Framework, Scripts, API Client Config
- **Framework**: React 18.2.0 (Three.js, R3F, Recharts, Lucide-React, FlatBuffers).
- **Build Tool**: Vite 5.0.0 (`@vitejs/plugin-react` 4.2.0).
- **Scripts**: `"dev": "vite --host"`, `"build": "vite build"`, `"preview": "vite preview --host"`.
- **API Client**:
  - `src/api.js`: Computes `API_BASE` (`http://<host>:8080`) and `WS_URL` (`ws://<host>:8080/ws`).
  - Auth token: `localStorage['econ.adminToken']`, passed via `X-Admin-Token` in HTTP and `{"action":"auth","token":"..."}` in WebSocket.
  - Streaming: Native `WebSocket` with arraybuffer FlatBuffers decoding.

### Q5: Exact List of Files, Functions, and Components Involved

| File Path | Role / Key Components & Functions |
|---|---|
| `dashboard/src/api.js` | `API_BASE`, `WS_URL`, `getAdminToken()`, `setAdminToken()` |
| `dashboard/src/useRecommendations.js` | `useRecommendations(pollMs)`: polls `/api/recommendations` |
| `dashboard/src/AiInsightsPanel.jsx` | `AiInsightsPanel`, `RoomModelsCard`, `ModelExportCard`, `AfddDriftDetail` |
| `dashboard/src/MobileAIScreen.jsx` | `MobileAIScreen`: Mobile twin of AI insights panel |
| `dashboard/src/RecommendationEvidence.jsx` | `RecommendationEvidence`, `SigmaStrip`, `TrajectoryStrip` |
| `dashboard/src/useDigitalTwin.js` | `useDigitalTwin()`, `sendManualOverride()`, `setAutoPilot()`, `loadScenario()` |
| `dashboard/src/useOpsStatus.js` | `useOpsStatus()`, `untilLabel()`: polls `/api/precool`, `/api/weather` |
| `dashboard/src/usePlugs.js` | `usePlugs()`, `updateConfig()`: polls/posts `/api/plugs` |
| `dashboard/src/useLibrary.js` | `useLibrary()`, `fetchLibrary()`: fetches `/api/library` |
| `dashboard/src/useRoomModels.js` | `useRoomModels()`: polls `/api/rooms/models` |
| `dashboard/src/useForecastCompare.js` | `useForecastCompare()`: polls `/api/forecast/compare` |
| `dashboard/src/useLocalModel.js` | `useLocalModel()`, `profileHardware()`: calls `/api/model/recommend` |
| `dashboard/src/App.jsx` | Main dashboard shell, `executeRemediation()`, micro-HUD drilldown |
| `dashboard/src/MobileApp.jsx` | Mobile shell, `RoomDetailDrawer` |
| `dashboard/src/GlobalMetricsPanel.jsx` | Right dock, manual zone override buttons (`PURGE`, `MAX COOL`, `RESET`) |
| `dashboard/src/MaintenanceDrawer.jsx` | `MaintenanceDrawer`: `FLOOD ZONE WITH COOLING` action |
| `dashboard/src/PlugLoadPanel.jsx` | `PlugLoadPanel`: APLC socket control and schedule edits |
| `dashboard/src/TelemetryPanel.jsx` | `TelemetryPanel`: Auto-Pilot toggle, zone performance scatter |
| `dashboard/package.json` | Dependencies, scripts, build tooling |
| `dashboard/vite.config.js` | Vite configuration |

---

## 5. Verification Method

To independently verify this investigation report:

1. **Verify Frontend Build**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard
   npm run build
   ```
   *Expected*: Vite builds bundle successfully with code 0.

2. **Verify Backend Endpoints & Test Suite**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v ./simulation -run "TestRecommendations|TestPublishCommand"
   ```
   *Expected*: Passes with exit code 0, demonstrating `Recommendations()` and `PublishCommand()` behavior.

3. **Verify Source References**:
   - Inspect `dashboard/src/useRecommendations.js` (lines 13-31) to confirm `/api/recommendations` endpoint.
   - Inspect `dashboard/src/AiInsightsPanel.jsx` (lines 177-202) to confirm recommendation action binding to `sendManualOverride`.
   - Inspect `dashboard/src/useDigitalTwin.js` (lines 174-192) to confirm WebSocket JSON payload dispatch.
   - Inspect `server/main.go` (lines 251-280) and `server/simulation/engine.go` (lines 2545-2619) to confirm backend action handling.
