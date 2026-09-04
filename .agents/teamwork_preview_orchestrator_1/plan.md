# Implementation Plan: stripW Telemetry Integration

## Phase 0: Survey & Scoping (COMPLETED)
- [x] Survey ESP32 firmware in `edge/esp32` (Report: `.agents/teamwork_preview_explorer_survey_esp32/handoff.md`)
- [x] Survey Go backend and TimescaleDB in `server/` (Report: `.agents/teamwork_preview_explorer_survey_backend/handoff.md`)
- [x] Survey frontend dashboard in `dashboard/` (Report: `.agents/teamwork_preview_explorer_survey_frontend/handoff.md`)
- [x] Synthesize findings and create `PROJECT.md` with Feature Inventory, Milestones, and Interface Contracts

## Phase 1: Milestone 1 — ESP32 Firmware Update (R1)
- [ ] Dispatch Worker to update `edge/esp32/src/main.cpp` and `edge/esp32/src/node_config.h`:
  - Define `STRIP_ADC_PIN 35` and `#define USE_STRIP 1`
  - Add `stripCalAPerV` calibration multiplier to `NodeConfig` with 1.0–500.0 A/V validation and JSON parsing
  - Implement 100ms True-RMS reading loop `readStripAmps()` with DC offset cancellation
  - Expand JSON document to `StaticJsonDocument<384>` and `char buf[384]`
  - Append `doc["stripW"] = round(stripAmps * gCfg.plugMainsV * 10) / 10.0;`
  - Update `test/host_config_test.cpp` and verify host tests and PlatformIO build
- [ ] Dispatch Reviewers, Challengers, and Forensic Auditor for M1
- [ ] Gate M1

## Phase 2: Milestone 2 — Go Backend & TimescaleDB Update (R2)
- [ ] Dispatch Worker to update `server/`:
  - Update `server/mqtt.go`: `StripW *float64 json:"stripW"` in `telemetryMsg`
  - Update `server/db.go`: `ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION`, create `telemetry` view, update `reading` struct and 7-column batch INSERT in `writeLoop()`
  - Update `server/db/init.sql`: add `strip_w DOUBLE PRECISION`
  - Update `server/devices.go`: track `stripW`
  - Update `server/simulation/engine.go`: add `StripW` to `Measurement` and `HardwareNode`
  - Update `server/schema/Telemetry/ZoneData.go`: FlatBuffers `ZoneData` vtable slot 11
  - Verify Go tests pass via Docker
- [ ] Dispatch Reviewers, Challengers, and Forensic Auditor for M2
- [ ] Gate M2

## Phase 3: Milestone 3 — Frontend Dashboard Update (R3)
- [ ] Dispatch Worker to update `dashboard/`:
  - Update `src/telemetry/zone-data.ts`: add `stripW` getter and builder
  - Update `src/useDigitalTwin.js`: parse `stripW` from WebSocket
  - Update `src/GlobalMetricsPanel.jsx`: render "Power Strip" card in Overview and Zone diagnostics
  - Update `src/HardwareInspector.jsx`: register `stripW` in `FIELDS`
  - Verify `npm run build` succeeds
- [ ] Dispatch Reviewers, Challengers, and Forensic Auditor for M3
- [ ] Gate M3

## Phase 4: Milestone 4 — End-to-End Integration & Verification
- [ ] Verify complete pipeline:
  - ESP32 firmware build and serial publish output
  - Go backend parsing and TimescaleDB insert without SQL errors
  - TimescaleDB historical data retention (verify existing rows preserved)
  - Dashboard build and rendering
- [ ] Final E2E verification gate and Forensic Audit
- [ ] Synthesize results, update documentation, and claim victory to Sentinel
