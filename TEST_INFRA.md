# E2E Test Infra: econ — Forecasting, API & Telemetry

## Test Philosophy
- Opaque-box, requirement-driven automated verification.
- Validates all 3 key user requirements (R1, R2, R3) and automated acceptance criteria.

## Feature Inventory & Test Mapping
| # | Feature | Requirement | Tier 1 (Unit/Contract) | Tier 2 (Boundary/Edge) | Tier 3 (Integration) | Tier 4 (UI/Scenario) |
|---|---------|-------------|:----------------------:|:----------------------:|:--------------------:|:--------------------:|
| 1 | Full MQTT JSON Logging | R3 | `server/mqtt_test.go` | Malformed/empty payloads | Broker message stream | Telemetry log validator |
| 2 | Debug Logging Configuration | R3 | Env var flags | Missing env fallbacks | Cross-service logging | Service logs check |
| 3 | Recommendations API Forecast Data | R2 | `server/recommendapi_test.go` | Cold-start / 0 history | Go proxy -> Python model | Full JSON contract |
| 4 | AI Panel Forecast Graph Rendering | R1 | Recharts/SVG components | Empty series / fallback | React hook data flow | `verify_ai_actions.js` Puppeteer |
| 5 | Mobile Forecast Graph Rendering | R1 | Mobile AI screen | Viewport resize 390x844 | Mobile touch interactions | Puppeteer Mobile Suite |

## Test Architecture
- **Go Server Suite**: `cd server && go test -v ./...`
- **Dashboard & UI Puppeteer Suite**: `cd dashboard && npm test` (`node verify_ai_actions.js`)
- **Edge Host Test Suite**: `cd edge/esp32 && ./test/run_all_e2e_tests.sh`

## Acceptance Thresholds
- Automated test asserting `GET /api/recommendations` returns forecast graph data: MUST PASS.
- Programmatic Puppeteer test asserting forecast chart element rendering in AI panel: MUST PASS.
- Automated test validating that backend logs output full MQTT telemetry JSON payloads: MUST PASS.
- Zero regressions across existing 18 dashboard tests, 17 Go test packages, and 93 edge test cases.
