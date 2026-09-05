# Forecasting Models & Services Survey Report

**Agent**: Explorer 1 (`explorer_survey_models`)  
**Date**: 2026-08-29T21:00:00Z  
**Scope**: Forecasting models (LSTM, TimesFM), Python services, data pipelines, Go server interfaces, telemetry logging, and integration with recommendations/AI panels.

---

## 1. Observation

### 1.1 Forecasting Architecture & Service Directory
The primary forecasting microservice is located in `/Users/nguyenhoangkhoi/Documents/econ/backend/forecasting/` and runs as a FastAPI HTTP service (port `8000`).

| File Path | Role & Key Responsibilities |
|---|---|
| `backend/forecasting/config.py` | Defines shared model contract (`FEATURES`, `INPUT_SIZE=4`, `SENSOR_FEATURES=2`, `HIDDEN_SIZE=64`, `NUM_LAYERS=2`, `SEQ_LEN=12`, artifact paths `WEIGHTS_PATH`, `SCALER_PATH`). |
| `backend/forecasting/model.py` | Implements `PeakLoadLSTM(nn.Module)` — PyTorch sequence-to-scalar regressor (`(batch, seq_len=12, 4)` → `(batch, 1)` peak MW). |
| `backend/forecasting/timesfm_forecaster.py` | Implements `TimesFmForecaster` — Lazy thread-safe loader for Google TimesFM (`google/timesfm-2.5-200m-transformers` / `google/timesfm-2.0-500m-pytorch`), zero-shot univariate time-series predictor with decile quantile heads (`q1..q9`). |
| `backend/forecasting/main.py` | FastAPI application exposing `/health`, `/model/info`, `/model/artifacts`, `POST /predict` (LSTM), and `POST /forecast/load` (TimesFM). |
| `backend/forecasting/data_loader.py` | Outdoor weather fetching (OpenWeather API with 10-min cache and `(30.0, 70.0)` fallback) + TimescaleDB historical telemetry loader (`load_training_sequences`). |
| `backend/forecasting/train.py` | Model training script with synthetic physics-grounded generation fallback (`synthesize`) and StandardScaler serialization (`scaler.pkl`, `model_weights.pth`). |
| `backend/forecasting/test_predict.py` | Smoke and monotonicity sanity test suite against running FastAPI service. |
| `backend/forecasting/requirements.txt` | Python dependencies: `torch>=2.2`, `transformers>=4.45`, `fastapi>=0.110`, `uvicorn>=0.27`, `scikit-learn>=1.3`, `psycopg2-binary>=2.9`. |

---

### 1.2 Model Ingestion, Data Structures & Prediction Outputs

#### A. Supervised PyTorch LSTM Model
- **Input Endpoint**: `POST /predict` (`backend/forecasting/main.py:158`)
- **Request Schema**:
  ```json
  {
    "sensor_sequence": [[24.5, 0.45], [24.7, 0.50], ...], // 12 timesteps of [room_temp °C, airflow_fraction 0..1]
    "outdoor_temp": 31.5,                                  // Optional weather handover from engine
    "outdoor_humidity": 72.0                               // Optional weather handover from engine
  }
  ```
- **Feature Assembly**:
  Combines the 2 sensor features with 2 weather features `[outdoor_temp, outdoor_humidity]` to create a `(1, 12, 4)` tensor, scaled with `StandardScaler` (`scaler.pkl`).
- **Prediction Generation**:
  Passes tensor through 2-layer LSTM (hidden size 64) and linear layer: `self.fc(out[:, -1, :])`.
- **Response Schema**:
  ```json
  {
    "predicted_peak_load": 2.385,
    "outdoor_temp_used": 31.5,
    "outdoor_humidity_used": 72.0,
    "weather_source": "engine" // "engine" | "live" | "cache" | "fallback"
  }
  ```
- **Output Characteristics**:
  Single scalar float representing predicted peak cooling load in MW. It does NOT generate a future time trajectory or series array.

#### B. Google TimesFM Foundation Model (Zero-Shot)
- **Input Endpoint**: `POST /forecast/load` (`backend/forecasting/main.py:114`)
- **Request Schema**:
  ```json
  {
    "history": [0.015, 0.018, 0.022, 0.019, 0.024, 0.021, 0.020, 0.025], // >= 8 recorded load samples (MW)
    "horizon": 12,                                                         // 1..256 steps (default 12)
    "context_len": null                                                    // Optional context window override
  }
  ```
- **Prediction Generation**:
  Executes `_model(past_values=[tensor(history)])` without requiring training or scaling on the target building. Decodes `mean_predictions` and `full_predictions` (quantile heads).
- **Response Schema**:
  ```json
  {
    "forecast": [0.021, 0.023, 0.024, 0.026, 0.025, 0.027, 0.028, 0.029, 0.028, 0.027, 0.026, 0.025],
    "quantiles": {
      "q1": [0.018, 0.019, 0.020, 0.021, 0.020, 0.022, ...],
      "q2": [0.019, 0.020, 0.021, 0.022, 0.021, 0.023, ...],
      "q5": [0.021, 0.023, 0.024, 0.026, 0.025, 0.027, ...],
      "q9": [0.025, 0.028, 0.030, 0.032, 0.031, 0.034, ...]
    },
    "engine": "timesfm",
    "variant": "2.5-200m",
    "repo": "google/timesfm-2.5-200m-transformers",
    "device": "mps", // "mps" | "cuda" | "cpu"
    "zero_shot": true,
    "context_used": 8
  }
  ```
- **Output Characteristics**:
  Full future horizon trajectory (`forecast`: array of floats) + uncertainty spread across deciles (`quantiles: {"q1"..."q9"}`).

---

### 1.3 Go Backend Consumption & Exposure Interfaces

The Go server (`server/`) communicates with the Python forecasting service via HTTP (`FORECAST_URL`, default `http://localhost:8000` or `http://forecasting:8000`):

1. **`GET /api/forecast`** (`server/forecast.go:528`):
   - Extracts 12-step rolling window of average `[temp, airflow]` from `engine.ForecastWindow(12)` (`server/simulation/engine.go:2089`) and outdoor weather from `engine.OutdoorForForecast()`.
   - POSTs to Python `/predict`.
   - Annotates with `window_real_samples`, `window_len`, and plausibility against observed range (`implausible`, `plausibility_judged`, `plausibility`, `observed_min_mw`, `observed_max_mw`, `observed_samples`).

2. **`GET /api/forecast/load[?horizon=N]`** (`server/forecast.go:66`):
   - Extracts `engine.LoadHistory()` (5-minute load snapshots).
   - POSTs to Python `/forecast/load`.
   - Annotates with `history_samples`, `step_minutes` (5), `horizon_minutes` (`horizon * 5`).

3. **`GET /api/forecast/compare[?horizon=N]`** (`server/forecast.go:246`):
   - Runs concurrent queries to both `queryLSTM` and `queryTimesFM`.
   - Validates plausibility for both models against `engine.ObservedLoadRange()`.
   - Extracts highest quantile (`PeakUpperMw`, `UpperQuantile`) via `highestQuantile(quantiles)`.
   - Computes `agreement`: `{ comparable: bool, deltaMw: float, relativeDiff: float, higher: "lstm"|"timesfm" }`.

4. **`GET /api/forecast/engines`** (`server/forecast.go:491`):
   - Proxies Python `GET /model/info` to report model readiness and reasons.

5. **`precoolLoop` Automation** (`server/precool.go:65`):
   - Background ticker every 5 minutes: evaluates zero-shot TimesFM first, then LSTM.
   - Triggers pre-cooling window when predicted peak exceeds `LoadForecastThreshold` (mean + 1.5σ) or fallback trigger `PRECOOL_TRIGGER_MW` (2.0 MW).

6. **Recommendations API `GET /api/recommendations`** (`server/recommendapi.go:120`):
   - Calls `engine.Recommendations(8)` (`server/simulation/engine.go:1159`).
   - Generates anomaly recommendations from `baselines.Recommend` (`server/simulation/recommend.go:105`) and predictive room recommendations from `dynamics.PredictiveRecommendations` (`server/simulation/recommend.go:241`).
   - *Current limitation*: Does not currently embed the forecast series/graph data inside the recommendation report response.

---

### 1.4 Frontend Ingestion & Graph Rendering

- **`dashboard/src/useForecastCompare.js`**:
  Polls `GET /api/forecast/compare` every 60s, returning `{ data, lstm, timesfm, series, upperBand, upperQuantile, peakUpperMw, agreement, stepMinutes }`.
- **`dashboard/src/AiInsightsPanel.jsx`**:
  - Contains insight card `id === 'forecast'`.
  - When expanded, renders Recharts `<LineChart>`:
    * Primary Line: TimesFM trajectory (`dataKey="mw"`, stroke `var(--accent-blue)`).
    * Dashed Line: Upper decile quantile band (`dataKey="hi"`, stroke `var(--accent-blue)` dashed).
    * Reference Line: LSTM predicted peak (`ReferenceLine y={peak}`, stroke `var(--accent-red)`).
- **`dashboard/src/useRecommendations.js`**:
  Polls `GET /api/recommendations` every 10s, returning `{ recommendations, model, report }`.
- **`dashboard/src/MobileAIScreen.jsx`**:
  Renders recommendations and forecast cards in the mobile touch UI.

---

### 1.5 Logging & Telemetry Verbosity State

1. **Python Forecasting Service (`backend/forecasting/`)**:
   - Uses basic `print()` statements in `main.py`, `timesfm_forecaster.py`, `data_loader.py`.
   - No Python `logging` module configuration.
   - No log levels (`DEBUG`, `INFO`, `ERROR`) or command line / environment log-level filtering.
   - Request and response payloads are not logged.

2. **Go Backend Server (`server/`)**:
   - Uses standard library `log.Printf` across handlers.
   - In `server/mqtt.go:146` (`handleTelemetry`), incoming MQTT telemetry messages are logged as:
     `[mqtt] telemetry %s occ=%d src=%q real_temp=%v (zone=%q)`
     **Full JSON payload is NOT included in the log output.**
   - No global debug flag gating verbose tracing.

3. **Edge & Simulation Services (`edge/`, `ai_modules/`)**:
   - `ai_modules/branch_a_occupancy/yolo_bytetrack/yolo_tracker.py`:
     Hardcoded `logging.basicConfig(level=logging.INFO)`.
   - `edge/esp32/esp32_emulator.py`:
     Uses raw unformatted `print()` statements.
   - `edge/raspberry_pi/gateway.py`:
     Hardcoded `logging.basicConfig(level=logging.INFO)`.

---

## 2. Logic Chain & Requirements Gap Analysis

### R1. Forecast Graph Rendering
- **Observation**: `AiInsightsPanel.jsx` (lines 430–445) uses Recharts `<LineChart>` to render the TimesFM forecast series and upper quantile band when `id === 'forecast'` card is expanded.
- **Inference**: The rendering foundation exists via `useForecastCompare()`, but:
  1. Forecast graph data is not currently displayed inline inside recommendation cards (`RecommendationEvidence.jsx` or general recommendation list).
  2. If TimesFM is unavailable or still loading, the UI falls back to showing text numbers rather than a fallback graph or placeholder indicator.
  3. Acceptance Criteria requires an automated verification script (Puppeteer) asserting that the forecast graph/chart element actually renders in the AI panel UI.

### R2. End-to-End Forecast Wiring
- **Observation**:
  - Forecasting backend generates series in `POST /forecast/load` and scalar in `POST /predict`.
  - Go server proxies them via `GET /api/forecast/compare` and `GET /api/forecast/load`.
  - However, `GET /api/recommendations` (the central recommendations endpoint) only returns `{ recommendations: [...], model: {...} }` without forecast graph data.
- **Inference**:
  - The project requirement states: *"Ensure the forecasting backend exposes the graph data, the Go server API proxies/delivers it, and the frontend consumes it to display the chart alongside the existing recommendations."*
  - Acceptance Criteria states: *"- [ ] Integration tests are updated or added to assert that the `GET /api/recommendations` (or equivalent) endpoint correctly returns the forecast graph data."*
  - To achieve full end-to-end wiring, either:
    1. `GET /api/recommendations` should embed the latest forecast graph / series (or include it in the `load:GLOBAL` recommendation payload), OR
    2. `GET /api/forecast/compare` and `GET /api/recommendations` should have an integrated test asserting forecast graph delivery to the recommendations view.

### R3. Detailed Telemetry & Logging
- **Observation**:
  - `server/mqtt.go:146` omits `string(payload)` when logging incoming telemetry.
  - Forecasting backend does not use `logging.getLogger()` or output debug-level logs with full payload details.
  - Edge scripts (`yolo_tracker.py`, `gateway.py`, `esp32_emulator.py`) default to `INFO` or `print()`.
- **Inference**:
  - Need to configure `logging.basicConfig(level=logging.DEBUG)` / `DEBUG=1` across Python forecasting and edge services.
  - Need to update `server/mqtt.go` to log full raw JSON telemetry payloads (e.g. `log.Printf("[mqtt] telemetry %s payload: %s", suffix, string(payload))`).
  - Need an automated test script verifying backend MQTT logs output full JSON telemetry payloads.

---

## 3. Caveats
1. **Cold Start & History Requirement**: TimesFM requires at least 8 historical load samples (`len(history) >= 8`, 40 minutes of engine runtime) to generate a zero-shot forecast. Below 8 samples, `GET /api/forecast/load` and `GET /api/forecast/compare` return `503 Service Unavailable` with `"not enough load history yet"`.
2. **Torch Dependency in Test Environments**: In certain restricted/sandbox environments where `torch` or `typing_extensions` are missing, the Python forecasting service gracefully degrades to report `ready: false` via `/health` and `/model/info`. Go backend handles this without crashing.
3. **Plausibility & Out-of-Distribution Gating**: The Go backend enforces plausibility clamping (`outOfDistributionFactor = 4.0`). If a model trained on a large building is evaluated against a small building, `implausible: true` is set, and agreement comparison is intentionally suppressed.

---

## 4. Conclusion & Actionable Recommendations

### 4.1 Schema and Endpoint Summary

```
+---------------------------------------------------------------------------------------------------+
|                                 FORECASTING & TELEMETRY ARCHITECTURE                              |
+---------------------------------------------------------------------------------------------------+
|                                                                                                   |
|  [Python Forecasting Service] (FastAPI :8000)                                                     |
|    - POST /predict        -> PyTorch LSTM peak load scalar (MW) + weather metadata                |
|    - POST /forecast/load  -> Google TimesFM 12-step horizon series (MW) + quantiles (q1..q9)      |
|    - GET /model/info      -> Model readiness, architecture, and diagnostics                       |
|    - GET /model/artifacts -> Base64 export of weights & scaler for offline bundling               |
|                                                                                                   |
|                            ▲ HTTP (FORECAST_URL)                                                  |
|                            │                                                                      |
|  [Go Backend Server] (Port 8080)                                                                  |
|    - GET /api/forecast         -> Proxies LSTM prediction with engine input window                |
|    - GET /api/forecast/load    -> Proxies TimesFM series with engine LoadHistory                  |
|    - GET /api/forecast/compare -> Concurrently evaluates LSTM & TimesFM + plausibility checks     |
|    - GET /api/recommendations  -> Ranked anomaly report + room dynamics predictions              |
|    - MQTT Subscriber           -> econ/telemetry/+, econ/status/+                                 |
|                                                                                                   |
|                            ▲ HTTP / WebSocket                                                     |
|                            │                                                                      |
|  [React Frontend Dashboard]                                                                       |
|    - useForecastCompare.js -> Consumes /api/forecast/compare                                      |
|    - useRecommendations.js -> Consumes /api/recommendations                                       |
|    - AiInsightsPanel.jsx   -> Renders LineChart of forecast series, upper decile & LSTM peak      |
|                                                                                                   |
+---------------------------------------------------------------------------------------------------+
```

### 4.2 Concrete Implementation Roadmap for Subsequent Milestones
1. **Forecasting Service Logging (R3)**:
   - Add structured `logging` configuration in `backend/forecasting/main.py` and `timesfm_forecaster.py` with `LOG_LEVEL=DEBUG` support.
   - Add debug logs for all incoming requests, tensors, scaled features, and prediction outputs.
2. **Go Server & MQTT Logging (R3)**:
   - Update `server/mqtt.go` `handleTelemetry` to log the full JSON payload:
     `log.Printf("[mqtt] telemetry %s payload: %s", suffix, string(payload))`.
   - Ensure debug logging flag / environment variable is supported.
3. **Forecasting to Recommendations Wiring (R2)**:
   - Update `server/recommendapi.go` or `engine.Recommendations()` so `GET /api/recommendations` includes or references the forecast graph data / compare payload.
   - Update integration tests in Go (`server/recommendapi_test.go` or `forecast_test.go`) asserting that recommendations include forecast graph series.
4. **Forecast Graph UI Enhancements & Automated Verification (R1)**:
   - Ensure the forecast chart in `AiInsightsPanel.jsx` renders seamlessly with proper fallbacks.
   - Extend `dashboard/verify_ai_actions.js` Puppeteer suite to verify that the forecast chart element (`.recharts-responsive-container`, `svg`, forecast lines) is rendered in the DOM.

---

## 5. Verification Method

To independently reproduce and verify all findings in this report:

1. **Verify Go Server Tests**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/server
   go test -v ./...
   ```
   *Expected result*: All unit and plausibility tests (`forecast_plausibility_test.go`, `forecast_window_test.go`, etc.) PASS.

2. **Verify Dashboard Automated E2E Verification Harness**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/dashboard
   npm test
   ```
   *Expected result*: Puppeteer headless test suite executes and passes 18/18 tests across backend APIs, simulation engine, and desktop/mobile UI components.

3. **Verify Forecasting Service Syntax & Structure**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ
   python3 -m py_compile backend/forecasting/*.py
   ```
   *Expected result*: Zero syntax errors.

4. **Verify MQTT Telemetry Logging Invariant**:
   Inspect `server/mqtt.go:146` to confirm payload logging format.
