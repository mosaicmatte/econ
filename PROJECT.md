# Project: econ — Forecast Graph Rendering, E2E Wiring & Detailed Telemetry Logging

## Architecture
- **Forecasting Backend** (`backend/forecasting/`): Python FastAPI service running PyTorch PeakLoadLSTM and Google TimesFM zero-shot models. Exposes `/predict` (scalar peak MW) and `/forecast/load` (univariate load series + quantile deciles).
- **Go Server & Engine** (`server/`): High-concurrency physics simulation engine, MQTT broker client, REST API router, and FlatBuffers WebSocket stream. Proxies forecast queries, serves `GET /api/recommendations`, and ingests MQTT telemetry from edge devices.
- **Frontend Dashboard** (`dashboard/`): React 18 + Vite dashboard with 3D canvas, P&ID flow schematic, AI Insights Panel (`AiInsightsPanel.jsx`), Telemetry Panel, and Mobile UI (`MobileAIScreen.jsx`). Uses Recharts and SVG sparklines.
- **Edge Layer** (`edge/`, `ai_modules/`): ESP32 firmware, Raspberry Pi failsafe gateway, RP2040 Pico bridge, and YOLOv8 occupancy tracker publishing telemetry to MQTT.

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Full MQTT Telemetry JSON Logging | Log full raw JSON payloads for all incoming MQTT telemetry in Go server | M1 | ORIGINAL_REQUEST §R3 |
| 2 | Configurable Debug Logging across Services | Enable debug-level logging across Go server, Python forecasting, and Edge services | M1 | ORIGINAL_REQUEST §R3 |
| 3 | MQTT Telemetry Logging Automated Test | Automated test asserting full JSON payloads appear in server logs | M1 | ORIGINAL_REQUEST §Acceptance Criteria |
| 4 | Forecast Graph in RecommendationReport Schema | Extend Go RecommendationReport struct with ForecastGraph schema (series, peak, quantiles, horizon) | M2 | ORIGINAL_REQUEST §R2 |
| 5 | Recommendations API Forecast Data Delivery | Wire `GET /api/recommendations` to fetch and embed forecast graph data from forecasting backend | M2 | ORIGINAL_REQUEST §R2 |
| 6 | Recommendations API Forecast Integration Test | Automated test verifying `GET /api/recommendations` returns valid forecast graph data | M2 | ORIGINAL_REQUEST §Acceptance Criteria |
| 7 | AI Panel Forecast Graph Component | Render visual chart/graph of TimesFM/LSTM forecast directly in AI Panel | M3 | ORIGINAL_REQUEST §R1 |
| 8 | Recommendations UI & Mobile Forecast Integration | Wire recommendations hook and mobile AI screen to display forecast graphs | M3 | ORIGINAL_REQUEST §R1, §R2 |
| 9 | Programmatic UI Verification Script | Puppeteer automated test verifying forecast graph element renders in DOM | M4 | ORIGINAL_REQUEST §Acceptance Criteria |
| 10 | 100% E2E Verification & Adversarial Hardening | Full multi-tier test suite pass with zero regressions | M4 | ORIGINAL_REQUEST §Acceptance Criteria |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| M1 | Backend Telemetry & Debug Logging | `server/mqtt.go`, `server/mqtt_test.go`, `backend/forecasting/`, `edge/raspberry_pi/`, `ai_modules/` | none | DONE |
| M2 | End-to-End Forecast API Integration | `server/simulation/recommend.go`, `server/recommendapi.go`, `server/recommendapi_test.go`, `server/forecast.go` | M1 | DONE |
| M3 | Frontend Forecast Graph Rendering | `dashboard/src/AiInsightsPanel.jsx`, `dashboard/src/useRecommendations.js`, `dashboard/src/MobileAIScreen.jsx` | M2 | DONE |
| M4 | Comprehensive E2E Verification & Adversarial Hardening | `dashboard/verify_ai_actions.js`, multi-tier E2E tests, review, audit | M1, M2, M3 | DONE |

## Interface Contracts
### Forecast Graph in Recommendations Payload
`GET /api/recommendations` response JSON contract:
```json
{
  "recommendations": [ ... ],
  "model": { ... },
  "forecast": {
    "engine": "timesfm" | "lstm" | "fallback",
    "series": [0.021, 0.023, 0.024, ...],
    "upperBand": [0.025, 0.028, ...],
    "upperQuantile": "q9",
    "peakUpperMw": 0.034,
    "lstmPeakMw": 0.029,
    "stepMinutes": 5,
    "horizonMinutes": 60,
    "plausible": true,
    "samples": 8
  }
}
```

### MQTT Telemetry Log Format Contract
In `server/mqtt.go`:
```
[mqtt] telemetry <suffix> payload=<raw_json_string> occ=<occ> src=<src> real_temp=<bool> (zone=<zone>)
```

## Code Layout
- `server/mqtt.go`: MQTT client telemetry handling & logging
- `server/mqtt_test.go`: MQTT telemetry logging verification test
- `server/simulation/recommend.go`: `RecommendationReport` and `ForecastGraphData` struct definitions
- `server/recommendapi.go`: `GET /api/recommendations` HTTP handler
- `server/recommendapi_test.go`: Recommendations API integration test
- `backend/forecasting/main.py`: Python FastAPI logging & endpoints
- `dashboard/src/AiInsightsPanel.jsx`: AI Insights Panel forecast chart rendering
- `dashboard/src/useRecommendations.js`: Frontend recommendations hook
- `dashboard/src/MobileAIScreen.jsx`: Mobile AI panel forecast rendering
- `dashboard/verify_ai_actions.js`: Puppeteer E2E verification test suite
