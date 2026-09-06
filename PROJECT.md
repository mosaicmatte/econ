# Project: econ IoT Smart Building — Firmware Refinement, Backend Recommendations & UI Integration

## Architecture
The system integrates an edge-to-cloud IoT smart building platform consisting of:
1. **Edge Firmware (ESP32 / C++)**:
   - High-speed ADC1 True-RMS sampling over 100 ms integer AC cycle windows.
   - Stage 1: Intra-window dynamic DC offset removal, intrinsic ADC noise variance subtraction (`NOISE_VARIANCE = 300.0`), voltage divider ratio scaling (`0.5`), and noise floor cutoff (`0.15A`).
   - Stage 2: Inter-window Exponential Moving Average (EMA, $\alpha = 0.35$) with jitter deadband (`0.03A`) and instantaneous zero-snapping.
   - Host verification shims in `edge/esp32/test/arduino_shim.h`.
2. **Go Backend Server (`server/`)**:
   - `server/simulation/engine.go`: Digital twin simulation engine maintaining zone physics, occupancy, telemetry, and autonomous optimization.
   - `server/simulation/recommend.go`: Anomaly detection scoring generating ranked recommendations, including vacant room AC shutoff (`turn_off_ac`), thermal surge (`cool`), CO2 purge (`purge`), and load spike (`precool`).
   - `server/main.go`: REST API and WebSocket hubs. Package-level `commandHandler(engine)` processing manual overrides via `POST /api/command`.
   - Autonomous vs Manual arbitration: 15-minute latch (`z.OverrideUntil`) on `engine.PublishCommand` giving operator manual overrides priority over autonomous `actuate()`.
3. **React Dashboard (`dashboard/`)**:
   - Real-time FlatBuffers binary WebSocket streaming (`/ws`) via `useDigitalTwin.js`.
   - Periodic recommendation polling via `useRecommendations.js` (`GET /api/recommendations`).
   - Interactive recommendation cards with manual action buttons in `AiInsightsPanel.jsx` (desktop) and `MobileAIScreen.jsx` (mobile).
   - Dual-transport manual override dispatch: WebSocket primary with HTTP `POST /api/command` fallback.
   - Automated Puppeteer verification suite (`verify_ai_actions.js`).

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Edge ADC Denoising Algorithm | Intra-window variance subtraction & inter-window EMA with deadband in `current_denoiser.h` | M1 | ORIGINAL_REQUEST §R1 |
| 2 | Clean 0W Noise Cutoff & Ghost Suppression | Reliable zero-snapping at <0.15A suppressing ESP32 ADC noise floor & transients | M1 | ORIGINAL_REQUEST §R1 |
| 3 | Decoupled Firmware Denoising Module | Pure C++ header `edge/esp32/src/current_denoiser.h` shared between firmware & host tests | M1 | ORIGINAL_REQUEST §R1 |
| 4 | Automated C++ Noisy ADC Test Harness | `test_denoise.cpp` feeding mock noisy ADC waveforms (0A, loaded, steps) verifying output stability | M1 | Acceptance Criteria §Firmware |
| 5 | Anomalous Telemetry Recommendations | Recommendation engine evaluates telemetry and outputs `turn_off_ac` for vacant rooms with active AC | M2 | ORIGINAL_REQUEST §R3 |
| 6 | Extracted `/api/command` Handler | Package-level `commandHandler(engine)` supporting `command` and `action` with strict validation | M2 | ORIGINAL_REQUEST §R3 |
| 7 | Manual Override Latching & Zone Actuation | 15-minute operator override latch (`OverrideUntil`) and internal zone state sync (`HVAC_SET:OFF`) | M2 | ORIGINAL_REQUEST §R3 |
| 8 | Go Backend Unit Test Suite | Unit tests asserting anomalous telemetry -> `turn_off_ac` and `/api/command` routing and latching | M2 | Acceptance Criteria §Backend |
| 9 | UI Recommendation Action Button Wiring | Map `turn_off_ac: 'TURN OFF AC'` in `AiInsightsPanel.jsx` and `MobileAIScreen.jsx` to render action button | M3 | ORIGINAL_REQUEST §R2, §R3 |
| 10 | Dual-Transport Manual Action Override | `sendManualOverride` dispatches via WebSocket with HTTP `POST /api/command` fallback | M3 | ORIGINAL_REQUEST §R2 |
| 11 | Automated Frontend UI & Action Verification | Extend `verify_ai_actions.js` asserting UI components mount and expose manual recommendation buttons | M3 | Acceptance Criteria §Frontend |
| 12 | Comprehensive Dual-Track Verification & Forensic Audit | Verification across all tiers, 2 Reviewers, 2 Challengers, and 1 Forensic Auditor | M4 | Project Protocol & Acceptance Criteria |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | Firmware Sensor Denoising & Verification | Features 1–4: `current_denoiser.h`, `main.cpp` integration, config fix, `test_denoise.cpp` | none | PLANNED |
| 2 | Backend Recommendations & Command Routing | Features 5–8: `commandHandler`, `engine.go` sync, `command_recommendation_test.go` | none | PLANNED |
| 3 | Frontend Dashboard Wiring & Action Overrides | Features 9–11: `AiInsightsPanel.jsx`, `MobileAIScreen.jsx`, `useDigitalTwin.js`, `verify_ai_actions.js` | M2 | PLANNED |
| 4 | Dual-Track End-to-End Test Pass & Forensic Integrity Gate | Feature 12: Full test suite execution across firmware, backend, frontend; Reviewers, Challengers, Auditor | M1, M2, M3 | PLANNED |

## Interface Contracts

### Edge Firmware ↔ Backend MQTT
- **Telemetry Topic**: `econ/telemetry/<deviceId>`
- **Payload Schema (JSON)**:
  ```json
  {
    "zone": "zone-office-a",
    "stripW": 450.2,
    "plugW": 120.0,
    "temperature": 23.5,
    "humidity": 55.0,
    "co2": 620.0,
    "occupancy": 0,
    "pirState": false,
    "irState": "COOL_22"
  }
  ```
- **Command Topic**: `econ/commands/<topic>`
- **Command Payload**: String (e.g., `"HVAC_SET:OFF"`, `"LIGHTS_OFF;SETPOINT=18.0"`)

### Backend `/api/command` REST Endpoint
- **Method**: `POST`, `OPTIONS`
- **Request Headers**: `Content-Type: application/json`
- **Request Body**:
  ```json
  {
    "zone": "zone-office-a",
    "command": "turn_off_ac",
    "action": "turn_off_ac"
  }
  ```
- **Response**: `200 OK` on success, `400 Bad Request` if zone or command is empty / malformed, `405 Method Not Allowed` for non-POST.
- **Side Effect**: Calls `engine.PublishCommand`, sets `z.OverrideUntil = now + 15m`, sets `z.IrState = "OFF"` if `turn_off_ac`, publishes to MQTT.

### Backend ↔ Frontend Recommendations
- **Endpoint**: `GET /api/recommendations`
- **Response Schema**:
  ```json
  {
    "report": {
      "recommendations": [
        {
          "id": "vacant_ac:zone-office-a",
          "zone": "zone-office-a",
          "label": "Office A",
          "metric": "occupancy",
          "severity": "info",
          "basis": "standard",
          "title": "Room Vacant but AC may be ON",
          "message": "Office A is vacant but its AC might still be running. Turn off the AC to save energy.",
          "value": 0,
          "unit": "people",
          "samples": 1,
          "action": "turn_off_ac",
          "score": 4.0
        }
      ]
    }
  }
  ```

### Frontend Action Dispatch
- **WebSocket Frame**: Text JSON `{"action": "turn_off_ac", "zone": "zone-office-a"}`
- **HTTP Fallback**: `POST /api/command` with `{"zone": "zone-office-a", "command": "turn_off_ac"}`

## Code Layout
- `edge/esp32/src/current_denoiser.h`: Pure C++ two-stage sensor denoiser class (Stage 1 variance subtraction, Stage 2 EMA + deadband).
- `edge/esp32/src/main.cpp`: ESP32 firmware integrating `CurrentDenoiser`.
- `edge/esp32/src/node_config.h`: Calibration constants (`STRIP_CAL_A_PER_V = 15.0f`).
- `edge/esp32/src/camera/camera_config.h`: GPIO definitions (`PIN_CAM_D7 = 5`).
- `edge/esp32/test/test_denoise.cpp`: Automated C++ test feeding mock noisy ADC data and verifying output stability.
- `edge/esp32/test/run_host_tests.sh`: Host test orchestration script.
- `server/main.go`: Route registration and package-level `commandHandler`.
- `server/command_recommendation_test.go`: Unit tests for telemetry injection, recommendation engine outputs, and `/api/command` routing.
- `server/simulation/engine.go`: Zone simulation engine command normalization and actuation.
- `dashboard/src/AiInsightsPanel.jsx`: Desktop AI insights panel with action button rendering.
- `dashboard/src/MobileAIScreen.jsx`: Mobile AI screen with action button rendering.
- `dashboard/src/useDigitalTwin.js`: Digital twin hook with dual-transport override dispatch.
- `dashboard/verify_ai_actions.js`: Headless Puppeteer test runner verifying mounting, rendering, and action execution.
