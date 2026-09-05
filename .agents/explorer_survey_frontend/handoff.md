# Explorer 3 Handoff Report: Frontend Dashboard, AI Panel, Forecast Visualization & Testing

## 1. Observation

### 1.1 Architecture & Component Hierarchy
- **Framework & Build**: React 18 (`18.2.0`), Vite (`5.0.0`), ES Modules (`"type": "module"` in `dashboard/package.json`).
- **3D & Canvas**: `@react-three/fiber` (`8.17.10`), `@react-three/drei` (`9.106.0`), `three` (`0.184.0`), `three-bvh-csg` (`0.0.18`).
- **Graph / Flow Canvas**: `@xyflow/react` (`12.11.0`) for interactive BMS P&ID node schematic.
- **Visual Charting / Graph Rendering**:
  - `recharts` (`^3.8.1`): `LineChart`, `Line`, `XAxis`, `YAxis`, `CartesianGrid`, `Tooltip`, `ResponsiveContainer`, `ReferenceLine`, `ReferenceArea`, `AreaChart`, `Area`, `ScatterChart`, `Scatter`, `PieChart`, `Pie`, `BarChart`, `Bar`.
  - Raw SVG `<svg>`: Handcrafted responsive SVG sparklines and trajectory curves in `RecommendationEvidence.jsx` (`TrajectoryStrip`, `SigmaStrip`), `HardwareInspector.jsx` (`ForecastSpark`), and `AiInsightsPanel.jsx` (`AfddDriftDetail`).
- **Component Layout Hierarchy**:
  - `dashboard/src/main.jsx` (Entry, fetches building JSON before mount) $\rightarrow$ `dashboard/src/Root.jsx` (Responsive routing switch: desktop vs mobile)
  - **Desktop Root** (`dashboard/src/App.jsx`):
    - **3D Canvas Layer**: `BuildingModel.jsx`, `ConstrainedAirflow.jsx`, `AirflowVectorField.jsx`, `FloorInfrastructure.jsx`, `LiveWeatherBackground.jsx`.
    - **P&ID Flow Layer**: `ReactFlow` with `ThermalNode`, `AHUNode`, `VAVNode`, `UnitNode`.
    - **Left Dock Tabs** (`activeLeftTab`):
      - `'ai'` (Default): `dashboard/src/AiInsightsPanel.jsx`
      - `'telemetry'`: `dashboard/src/TelemetryPanel.jsx` (Comfort envelope scatter chart, zone profile rows)
      - `'logs'`: `dashboard/src/TelemetryLogs.jsx` (Event-driven transitions)
      - `'plugs'`: `dashboard/src/PlugLoadPanel.jsx` (Standby sweep control)
    - **Right Dock**: `dashboard/src/GlobalMetricsPanel.jsx` (Load sparkline, COP, tariff clock, BACnet micro-HUD)
    - **Drawers / Overlays**: `dashboard/src/MaintenanceDrawer.jsx` (Zone thermal drift history with `AreaChart`), `dashboard/src/BlueprintImportPanel.jsx`, `dashboard/src/HardwareInspector.jsx`.
  - **Mobile Root** (`dashboard/src/MobileApp.jsx`):
    - `dashboard/src/MobileAIScreen.jsx` (Mobile AI insights & recommendations)
    - `dashboard/src/MobileEnergyScreen.jsx` (Mobile energy curves)
    - `dashboard/src/MobileImpactScreen.jsx` (Mobile carbon & peak-shaving bar charts)

### 1.2 AI Panel & Recommendations UI Implementation
- **`AiInsightsPanel.jsx` (`dashboard/src/AiInsightsPanel.jsx`)**:
  - Props received from `App.jsx`:
    ```jsx
    <AiInsightsPanel
      simData={simData}
      activeScenario={activeScenario}
      faultTarget={faultTarget}
      aiForecast={aiForecast}
      setAutoPilot={setAutoPilot}
      hardwareNodes={hardwareNodes}
      setSelectedZone={setSelectedZone}
      sendManualOverride={sendManualOverride}
      onOpenPlugs={() => setActiveLeftTab('plugs')}
    />
    ```
  - State hooks: `useRecommendations()` (polled from `GET /api/recommendations`), `useForecastCompare()` (polled from `GET /api/forecast/compare`), `useOpsStatus()` (`/api/precool`, `/api/weather`), `usePlugs()` (`/api/plugs`), `useRoomModels()` (`/api/rooms/models`), `useLibrary()` (`/api/library`).
  - Card Generation Logic (`insights` `useMemo`):
    1. Critical fault / scenario remediation card (`fault`)
    2. Edge node offline warning (`offline`)
    3. Hardware-in-the-loop active (`hardware`)
    4. Physics divergence / AFDD drift (`afdd`)
    5. **Learned anomaly / predictive recommendations** from `useRecommendations()`: mapped to cards (`rec.action`: `'purge'`, `'cool'`, `'precool'`). Each card has an action button and an evidence toggle (`<RecommendationEvidence>`).
    6. Peak TOU tariff / pre-cooling card (`peak`)
    7. **LSTM / AI Forecast card (`forecast`)**: Created if `aiForecast && aiForecast.predicted_peak_load`.
       - Renders `renderDetail('forecast')` when expanded:
         - If `forecastSeries.length > 0`: renders Recharts `<LineChart data={forecastSeries}>` (TimesFM series in blue `var(--accent-blue)`, upper decile dashed line, LSTM peak red dashed `<ReferenceLine y={peak} stroke="var(--accent-red)">`).
         - If `forecastSeries.length === 0`: renders textual numbers (Live load vs LSTM predicted peak).
    8. Weather feed status (`weather`), Plug load sweep (`plugs`), Unoccupied cooling (`wasting`), Autonomous operations (`general`).
  - Supporting sub-components in `AiInsightsPanel.jsx`:
    - `AfddDriftDetail`: SVG polyline of residual drift history.
    - `RoomModelsCard`: Summary of online identified room models.
    - `ModelExportCard`: Edge model zip bundle download (`/api/model/export`).

### 1.3 How Recommendations and Forecast Data Are Fetched & Handled
- **Recommendations Hook (`dashboard/src/useRecommendations.js`)**:
  - Polls `GET /api/recommendations` every 10s:
    ```js
    export function useRecommendations(pollMs = 10000) {
      const [report, setReport] = useState(null);
      const load = useCallback(() => {
        fetch(`${API_BASE}/api/recommendations`)
          .then((r) => (r.ok ? r.json() : null))
          .then((s) => { if (s) setReport(s); })
          .catch(() => {});
      }, []);
      ...
      return { recommendations: report?.recommendations || [], model: report?.model || null, report, reload: load };
    }
    ```
- **Forecast Compare Hook (`dashboard/src/useForecastCompare.js`)**:
  - Polls `GET /api/forecast/compare` every 60s:
    ```js
    export function useForecastCompare(pollMs = 60000) {
      const [data, setData] = useState(null);
      ...
      const timesfm = data?.timesfm || null;
      const lstm = data?.lstm || null;
      const series = Array.isArray(timesfm?.series) && timesfm.series.length > 0 ? timesfm.series : null;
      const upperBand = Array.isArray(timesfm?.quantiles?.[timesfm?.upperQuantile]) ? timesfm.quantiles[timesfm.upperQuantile] : null;
      return { data, lstm, timesfm, series, upperBand, upperQuantile: timesfm?.upperQuantile, peakUpperMw: timesfm?.peakUpperMw, agreement: data?.agreement, stepMinutes: data?.stepMinutes || 5, reload: load };
    }
    ```
- **Digital Twin & WebSocket Hook (`dashboard/src/useDigitalTwin.js`)**:
  - Connects to `/ws` for streaming FlatBuffers binary telemetry.
  - Periodically polls `GET /api/forecast` every 30s into `aiForecast` state (`{ predicted_peak_load, weather_source, window_real_samples, window_len, implausible, plausibility_judged, plausibility }`).

### 1.4 Test Infrastructure & E2E Verification Harness
- **NPM Configuration (`dashboard/package.json`)**:
  - Test command: `"test": "node verify_ai_actions.js"`
  - Dependencies include `puppeteer`: `^25.1.0`.
- **E2E Test Runner (`dashboard/verify_ai_actions.js`)**:
  - Standalone NodeJS test script using Puppeteer in headless mode.
  - Contains 5 test suites (18 tests total, all passing in ~8.7s):
    1. `Backend API & Recommendation Schemas` (GET `/api/recommendations`, `/api/precool`, `/api/hardware`)
    2. `Simulation Engine Actuation & Override Normalization` (`purge`, `cool`, direct setback, reset, autopilot, MQTT commands)
    3. `Desktop Puppeteer UI & Action Interactivity` (`PURGE ZONE`, `FLOOD COOLING`, `ACTIVATE PRE-COOLING`, micro-HUD manual vetoes, AI modal)
    4. `Mobile Viewport & Screen (MobileAIScreen) Interactivity` (touch tap on 390x844 viewport)
    5. `Edge Firmware Protocol Invariants` (ESP32 command syntax, IR forward)
- **Backend Logging (`server/mqtt.go`)**:
  - `handleTelemetry` currently logs a formatted summary:
    `log.Printf("[mqtt] telemetry %s occ=%d src=%q real_temp=%v (zone=%q)", suffix, occ, msg.Source, msg.TempReal && msg.Temperature != nil, msg.Zone)`
  - Full raw MQTT JSON payload is parsed into `telemetryMsg` and passed to `registry.observe(suffix, msg, payload)` but not logged directly to stdout at debug level.

---

## 2. Logic Chain

### 2.1 Tracing R1: Forecast Graph Rendering in AI Panel & Recommendations UI
1. **Current UI Behavior**:
   - `AiInsightsPanel.jsx` (lines 380–482) already has a `renderDetail('forecast')` function that uses Recharts `<ResponsiveContainer>` and `<LineChart>` to draw TimesFM time series (`dataKey="mw"`) and upper quantiles (`dataKey="hi"`) with LSTM peak line (`ReferenceLine`).
   - However, this chart is **hidden inside an expandable card** (`insight.expandable = true`) that requires clicking `"VIEW PREDICTIONS"` to reveal.
   - On `MobileAIScreen.jsx` (lines 182–195), the forecast card is purely text; no graph or chart component is rendered on mobile.
   - In `RecommendationEvidence.jsx`, recommendation cards for zone metrics (temperature / CO2) render `<TrajectoryStrip>` (room dynamics step response), but whole-building load recommendations (`load:GLOBAL`) do not render a load forecast chart.
2. **Requirement R1 Solution**:
   - Make the forecast chart/graph directly visible or renderable alongside recommendations in the AI panel without requiring hidden nested expansion.
   - Provide visual chart/sparkline rendering on mobile (`MobileAIScreen.jsx`) and in recommendation evidence views for load recommendations.

### 2.2 Tracing R2: End-to-End Forecast Wiring
1. **Backend Exposing Graph Data**:
   - Currently, `backend/forecasting/main.py` exposes `POST /forecast/load` (TimesFM load series) and `POST /predict` (LSTM peak).
   - `server/forecast.go` proxies this via `GET /api/forecast/load` and `GET /api/forecast/compare`.
   - `server/recommendapi.go` (`GET /api/recommendations`) calls `engine.Recommendations(8)` in `server/simulation/engine.go`, which currently returns only `{ recommendations: [...], model: {...} }` without top-level forecast graph series or attaches forecast series to the `load:GLOBAL` recommendation.
2. **End-to-End Contract**:
   - `GET /api/recommendations` (or `RecommendationReport`) should directly include the forecast graph data (e.g. `forecast` or `forecastGraph` / `timesfm_series`), or the recommendations API should embed the forecast horizon directly in the response payload.
   - `useRecommendations.js` and `AiInsightsPanel.jsx` consume this graph data to render the chart alongside the recommendations list.

### 2.3 Tracing R3 & Automated Acceptance Criteria
1. **MQTT Telemetry Logging**:
   - `server/mqtt.go`: `handleTelemetry` receives raw `payload []byte`. Updating `log.Printf` to log `[mqtt] telemetry %s raw payload: %s` satisfies R3 and acceptance criteria.
2. **Automated Verification Script (`verify_ai_actions.js`)**:
   - Must add test cases asserting:
     - `GET /api/recommendations` contains valid forecast graph data/series.
     - Puppeteer loads AI Insights panel and verifies that the forecast chart element (e.g. `.recharts-responsive-container`, `svg.forecast-chart`, or `.forecast-sparkline`) is rendered in the DOM.
     - Telemetry test script verifies that MQTT telemetry logs output full JSON payloads.

---

## 3. Caveats

1. **Cold-Start Horizon**: TimesFM requires at least 8 recorded load samples ($8 \times 5 = 40\text{ minutes}$ of sim history) before emitting a forecast series. In cold-start or unit-test environments without history, the forecaster returns a structured cold-start status (`"need": 8, "samples": N`). Frontend charts must gracefully handle empty series or mock/simulated fallback trajectories during initial startup.
2. **Dual-Dashboard Parity**: Both `AiInsightsPanel.jsx` (desktop) and `MobileAIScreen.jsx` (mobile viewport) should remain in sync regarding data models and forecast visualization capabilities.
3. **Chart Responsiveness & Bundle Size**: `recharts` is already bundled (`^3.8.1`), so no new external charting library is needed. For ultra-compact micro-charts (e.g. within recommendation cards), the existing lightweight SVG sparkline pattern (`TrajectoryStrip`, `ForecastSpark`) is available and has 0KB bundle overhead.

---

## 4. Conclusion

- **Dashboard Architecture**: High quality, modular React 18 application with clean separation of hooks (`useRecommendations`, `useForecastCompare`, `useDigitalTwin`), components (`AiInsightsPanel`, `TelemetryPanel`, `HardwareInspector`), and layout engines.
- **Charting Tools Available**: `recharts` (LineChart, AreaChart, ScatterChart, BarChart, ResponsiveContainer) and custom pure SVG generators (`<svg>`, `<path>`, `<line>`).
- **Gaps Identified**:
  1. `GET /api/recommendations` does not currently return the full forecast graph series directly in its payload.
  2. The AI Panel forecast graph in `AiInsightsPanel.jsx` is collapsed by default behind `"VIEW PREDICTIONS"`, and `MobileAIScreen.jsx` has no forecast graph at all.
  3. `verify_ai_actions.js` does not yet test DOM rendering of the forecast chart or the updated `/api/recommendations` forecast schema.
  4. `server/mqtt.go` logs summary fields rather than the full raw JSON payload.

---

## 5. Verification Method

To independently verify the frontend dashboard, test harnesses, and backend APIs:

1. **Run Frontend E2E Test Suite**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard
   npm test
   # Runs: node verify_ai_actions.js
   ```
2. **Verify Server & Simulation Engine Unit Tests**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test ./...
   ```
3. **Verify Frontend Vite Build**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard
   npm run build
   ```
4. **Key Source Files for Inspection**:
   - `dashboard/src/AiInsightsPanel.jsx`: lines 380–482 (Forecast chart rendering)
   - `dashboard/src/useRecommendations.js`: lines 10–33 (Recommendation polling hook)
   - `dashboard/src/useForecastCompare.js`: lines 20–62 (Forecast comparison hook)
   - `dashboard/src/RecommendationEvidence.jsx`: lines 58–98 (`TrajectoryStrip` SVG response curve)
   - `dashboard/src/HardwareInspector.jsx`: lines 384–408 (`ForecastSpark` component)
   - `dashboard/verify_ai_actions.js`: lines 484–752 (Puppeteer E2E UI verification suite)
   - `server/recommendapi.go` & `server/simulation/recommend.go` (Recommendation API & model)
   - `server/forecast.go` (Forecast proxy & comparison endpoints)
   - `server/mqtt.go`: lines 111–148 (MQTT telemetry ingestion & logging)
