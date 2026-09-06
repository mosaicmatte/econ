# Project: econ IoT Smart Building — Occupancy AI, Authentic Forecast & Edge Compute Offload

## Architecture
The system integrates an edge-to-cloud IoT smart building platform consisting of:
1. **Edge Firmware (ESP32 / Pico / C++)**:
   - ESP32 ADC1 True-RMS current sampling with `CurrentDenoiser` (spike filter, linear detrending, noise variance subtraction $\sigma^2=300$, normalized covariance autocorrelation gating, and EMA with deadband).
   - Dynamic CPU strain detection (DSP window execution timing $>15\text{ ms}$ or sample starvation $n<400$) and synthetic strain injection (`simulateCpuStrain` in `NodeConfig` or `CPU_STRAIN:HIGH` MQTT command).
   - Pass-through mode: skips heavy local DSP, toggles `rawFallback: true`, and streams decimated raw ADC sample buffers (`rawStripSamples: 50..100` samples) or statistical moments in expanded 768-byte JSON buffers.
2. **Go Backend Server (`server/`)**:
   - `server/simulation/engine.go`: Digital twin simulation engine maintaining zone physics, occupancy, telemetry, and autonomous optimization. Ingests telemetry every cycle.
   - `server/simulation/baselines.go`: Online statistical baseline learning (Welford/EWMA mean, variance, count, z-score per zone and metric). Registered `"occupancy"` with variance tracking.
   - `server/simulation/recommend.go`: Occupancy AI anomaly detection using learned baseline distributions ($N \ge 20$, $z \le -1.5$, `Basis: "learned"`) rather than hardcoded zero-checks, with cold-start standard fallback ($N < 20$, `Basis: "standard"`).
   - `server/simulation/dsp.go`: Native Go port of `CurrentDenoiser` implementing the full 2-stage DSP pipeline with identical hardware parameters ($\text{dividerRatio}=0.5$, $\text{calAPerV}=15.0$, $\sigma^2=300.0$).
   - `server/mqtt.go`: Telemetry parser detecting `rawFallback: true`, executing server-side DSP to calculate clean Amps and Watts before storing into zone measurements and database.
   - `server/forecast.go`: Peak load and trajectory forecasting. Eliminates synthetic cubic spline and sine fallbacks, returning empty series when models lack sequence predictions.
3. **React Dashboard (`dashboard/`)**:
   - `dashboard/src/ForecastChart.jsx`: Authentic forecast rendering. Removes cubic spline fabrication (`smooth = t * t * (3 - 2 * t)`). Renders honest "insufficient data" state (`data-testid="forecast-insufficient-data"`) inside `data-testid="forecast-chart"` when true sequence data is unavailable.
   - Real-time FlatBuffers binary WebSocket streaming (`/ws`) and REST APIs.

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Learned Occupancy Metric Baseline | Register "occupancy" in `metricSpecs` in `server/simulation/baselines.go` with variance tracking | M1 | ORIGINAL_REQUEST §R1 |
| 2 | Statistical Occupancy Recommendation Engine | Replace hardcoded zero check in `recommend.go` with learned threshold scoring ($z \le -1.5$, `Basis: "learned"`) | M1 | ORIGINAL_REQUEST §R1 |
| 3 | Cold-Start Statistical Fallback | Retain standard basis ($N < 20$, `Basis: "standard"`) during model warm-up ensuring zero regression | M1 | Backend Survey |
| 4 | Backend Occupancy AI Unit Test Suite | Go unit tests injecting varying occupancy over time asserting learned threshold behavior vs zero-check | M1 | Acceptance Criteria §Backend |
| 5 | Removal of Synthetic Spline in ForecastChart | Eliminate cubic Hermite curve synthesis (`smooth = t*t*(3-2*t)`) in `dashboard/src/ForecastChart.jsx` | M2 | ORIGINAL_REQUEST §R2 |
| 6 | Honest Insufficient Data State UI | Render `[data-testid="forecast-insufficient-data"]` badge and 0 curve paths when forecast series is empty/down | M2 | ORIGINAL_REQUEST §R2 |
| 7 | Backend Forecast Graph Reconciliation | Reconcile `server/forecast.go` to avoid emitting synthetic spline/sine sequences | M2 | Frontend Survey |
| 8 | Automated Frontend Verification Suite | Scriptable Puppeteer/Node test asserting insufficient data rendering and zero fake curves | M2 | Acceptance Criteria §Frontend |
| 9 | Firmware CPU Strain Detection & Simulation | Timing-based strain detection ($>15\text{ ms}$) and `simulateCpuStrain` in `NodeConfig` (`edge/esp32/`) | M3 | ORIGINAL_REQUEST §R3 |
| 10 | Firmware Pass-Through Streaming Mode | Microcontroller bypasses DSP, sets `rawFallback: true`, and streams raw ADC counts in 768-byte buffer | M3 | ORIGINAL_REQUEST §R3 |
| 11 | C++ High-Strain Verification Harness | Off-target C++ test in `edge/esp32/test/` verifying CPU strain detection and pass-through mode toggle | M3 | Acceptance Criteria §Firmware |
| 12 | Backend Server-Side Go DSP Engine | Native Go implementation of `CurrentDenoiser` in `server/simulation/dsp.go` matching C++ mathematics | M3 | ORIGINAL_REQUEST §R3 |
| 13 | Backend Raw Fallback Ingestion & Processing | `server/mqtt.go` detects `rawFallback: true`, applies server-side DSP, and stores clean telemetry | M3 | Acceptance Criteria §Backend |
| 14 | Hardware Compatibility & Constraints Audit | Audit ADC1 pins, ACS712 voltage divider (0.5), calibration scaling, and Pico USB serial bandwidth | M3 | ORIGINAL_REQUEST §R4 |
| 15 | Comprehensive Dual-Track Verification & Forensic Audit | Full test suite across Tiers 1-4, 2 Reviewers, 2 Challengers, and 1 Forensic Integrity Auditor | M4 | Project Protocol & Acceptance Criteria |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | Genuine Occupancy AI Model (Backend) | Features 1–4: `baselines.go`, `recommend.go`, `recommend_occupancy_test.go` | none | DONE |
| 2 | Authentic Forecast Chart (Frontend & Backend) | Features 5–8: `ForecastChart.jsx`, `forecast.go`, `verify_forecast_chart.js` | none | IN_PROGRESS |
| 3 | Edge Compute Offload Fallback (Firmware & Backend) | Features 9–14: `node_config.h`, `main.cpp`, `test_cpu_strain_fallback.cpp`, `dsp.go`, `mqtt.go`, `mqtt_fallback_test.go` | none | DONE |
| 4 | Dual-Track End-to-End Test Pass & Forensic Integrity Gate | Feature 15: Full test suite execution across firmware, backend, frontend; Reviewers, Challengers, Auditor | M1, M2, M3 | PLANNED |

## Interface Contracts

### Edge Firmware ↔ Backend MQTT Raw Offload
- **Telemetry Topic**: `econ/telemetry/<deviceId>`
- **Payload Schema (JSON, max 768 bytes)**:
  ```json
  {
    "zone": "zone-office-a",
    "rawFallback": true,
    "rawStripSamples": [2048, 2180, 2350, 1920, 1750, 2040],
    "temperature": 23.5,
    "humidity": 55.0,
    "co2": 620.0,
    "occupancy": 0
  }
  ```
- **Configuration Topic**: `econ/config/<deviceId>`
- **Config Payload (JSON)**:
  ```json
  {
    "simulateCpuStrain": true
  }
  ```

### Backend Simulation Engine & Baseline Scoring
- **Metric**: `"occupancy"`
- **Distribution Model**: Continuous EWMA ($\alpha = 0.05$), minimum sample count $N \ge 20$ for maturity.
- **Threshold**: $z \le -1.5$ (statistically significant vacancy relative to zone's historical pattern).
- **Output Recommendation**:
  ```json
  {
    "id": "vacant_ac:zone-office-a",
    "zone": "zone-office-a",
    "metric": "occupancy",
    "severity": "info",
    "basis": "learned",
    "samples": 35,
    "value": 0,
    "action": "turn_off_ac"
  }
  ```

### Backend ↔ Frontend Forecast API
- **Endpoint**: `GET /api/forecast/compare`
- **Response Schema when no sequence model is available**:
  ```json
  {
    "series": [],
    "lstmPeakMw": 0.0285,
    "model": "lstm-scalar-only",
    "status": "insufficient_data"
  }
  ```
- **UI Render Contract**:
  - Container element `data-testid="forecast-chart"` must be present.
  - Inner element `data-testid="forecast-insufficient-data"` rendered with informative text.
  - Zero `.recharts-line-curve` SVG elements rendered.

## Code Layout
- `server/simulation/baselines.go`: Statistical baseline engine with `"occupancy"` metric spec.
- `server/simulation/recommend.go`: Statistical anomaly recommendation engine using learned thresholds.
- `server/simulation/recommend_occupancy_test.go`: Go unit tests for statistical occupancy recommendations.
- `server/simulation/dsp.go`: Native Go implementation of `CurrentDenoiser`.
- `server/simulation/dsp_test.go`: Unit tests for Go DSP denoiser matching C++ calculations.
- `server/mqtt.go`: Telemetry handler detecting `rawFallback` and routing to `server/simulation/dsp.go`.
- `server/mqtt_fallback_test.go`: Go unit test verifying edge raw fallback detection and DSP application.
- `server/forecast.go`: Forecast service returning empty series instead of synthetic splines.
- `dashboard/src/ForecastChart.jsx`: Frontend chart removing fake cubic spline and rendering insufficient data badge.
- `dashboard/verify_forecast_chart.js`: Automated Puppeteer test verifying authentic forecast rendering.
- `edge/esp32/src/node_config.h`: Node configuration including `simulateCpuStrain`.
- `edge/esp32/src/main.cpp`: ESP32 firmware with CPU strain detection and pass-through fallback streaming.
- `edge/esp32/test/test_cpu_strain_fallback.cpp`: Off-target C++ test for CPU strain pass-through mode.
- `edge/esp32/test/run_host_tests.sh`: Host test orchestration script.
