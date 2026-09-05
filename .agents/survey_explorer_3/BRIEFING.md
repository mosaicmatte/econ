# BRIEFING — 2026-08-26T04:05:30Z

## Mission
Investigate and design the Dual-Mode Communication (Wi-Fi real-time broadcast + automatic USB Serial fallback) mechanism for ESP32 WROOM, including telemetry schemas, reconnection state machines, and architecture isolation.

## 🔒 My Identity
- Archetype: explorer
- Roles: investigation, synthesis
- Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_3
- Original parent: 6848b659-e430-4aa8-9ca3-ab02a9ba213d
- Milestone: survey

## 🔒 Key Constraints
- Read-only investigation — do NOT implement changes in source code (only write to our own folder .agents/survey_explorer_3)
- Changes must respect architecture isolation rules (isolated camera module, no modifications outside camera module scope)
- Dual-mode communication: Wi-Fi broadcast primary, USB Serial automatic fallback
- Output structured survey report and handoff report

## Current Parent
- Conversation ID: 6848b659-e430-4aa8-9ca3-ab02a9ba213d
- Updated: 2026-08-26T04:05:30Z

## Investigation State
- **Explored paths**: `edge/esp32/src/main.cpp`, `edge/esp32/src/node_config.h`, `edge/esp32/platformio.ini`, `edge/esp32/test/`, `server/mqtt.go`, `server/simulation/engine.go`, `edge/pico/bridge.py`, `edge/pico/main.py`, `ai_modules/branch_a_occupancy/yolo_bytetrack/yolo_tracker.py`, `docs/BACKEND_ARCHITECTURE.md`.
- **Key findings**: Identified blocking boot loops and offline telemetry blackouts in current ESP32 code. Designed hybrid Wi-Fi broadcast (UDP broadcast on port 4210 + MQTT) and automatic USB Serial fallback with framed JSON lines. Designed non-blocking reconnection state machine (<0.2ms tick) and standardized BIM/topology telemetry schema.
- **Unexplored areas**: None. Investigation and architectural specification complete.

## Key Decisions Made
- Selected UDP Broadcast (port 4210) + MQTT as primary Wi-Fi mode for low SRAM overhead and multi-client distribution.
- Standardized newline JSON with `_topic` framing over UART0 (115200 baud) for automatic fallback, fully compatible with `bridge.py`.
- Specified non-blocking state machine to guarantee real-time camera inference loop is never starved or blocked.
- Designed clean C++ interface in `src/camera/dual_mode_comm.h` under `-DUSE_CAMERA_DETECTION=1`.

## Artifact Index
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_3/DISPATCH.md` — Inbound message log
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_3/progress.md` — Liveness & task checklist
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_3/survey_report.md` — Comprehensive analysis and design report
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_3/handoff.md` — Self-contained 5-component handoff report
