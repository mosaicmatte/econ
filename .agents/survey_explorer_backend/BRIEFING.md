# BRIEFING — 2026-08-29T16:33:30Z

## Mission
Investigate the backend and sensor integration codebase to identify architecture, endpoints, data flow, sensor simulation/dispatch, execution commands, and schemas.

## 🔒 My Identity
- Archetype: explorer
- Roles: survey explorer (backend & sensor integration)
- Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend
- Original parent: 034328b5-8dfe-43bd-b927-52e21282a318
- Milestone: backend_sensor_investigation

## 🔒 Key Constraints
- Read-only investigation — do NOT implement
- Write only to /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/
- Keep handoff.md structured according to the 5-component protocol

## Current Parent
- Conversation ID: 034328b5-8dfe-43bd-b927-52e21282a318
- Updated: 2026-08-29T16:33:30Z

## Investigation State
- **Explored paths**: `server/`, `server/simulation/`, `backend/`, `edge/`, `dashboard/src/`, `docs/`, `PROJECT.md`, `CLAUDE.md`
- **Key findings**:
  - Primary backend is Go 1.22 in `server/` running physics, MQTT, WebSocket, and HTTP APIs. `backend/core_engine` is legacy/superseded.
  - Recommendations endpoint is `GET /api/recommendations`, returning ranked learned baseline anomalies & predictive dynamics with structured actions (`cool`, `purge`, `precool`).
  - Action execution is dispatched primarily via WebSocket `/ws` messages `{"action":"...", "zone":"..."}` which triggers `engine.PublishCommand()`, applies state in-memory (15-min latch), and publishes to MQTT `econ/commands/<topic>`.
  - Live sensor states and hardware nodes are inspected via `GET /api/hardware`, `GET /api/devices`, and 30 fps FlatBuffers `/ws` binary stream.
  - Full test suite in `server/` verified passing (`go test ./...`).
- **Unexplored areas**: None for this survey scope.

## Key Decisions Made
- Documented all 23 HTTP/WS endpoints, payload schemas, and MQTT wire formats in `handoff.md`.

## Artifact Index
- handoff.md — Comprehensive backend and sensor integration survey report
- progress.md — Liveness and task progress
- DISPATCH.md — Dispatch log
