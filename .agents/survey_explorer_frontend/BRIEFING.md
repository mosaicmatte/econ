# BRIEFING — 2026-08-29T16:33:15Z

## Mission
Investigate the dashboard frontend codebase to identify AI panel implementation, recommendation/action data flows, mock vs API integration points, UI buttons and event handling, build system/framework setup, and involved files/components.

## 🔒 My Identity
- Archetype: Explorer
- Roles: survey_explorer_frontend
- Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_frontend
- Original parent: 034328b5-8dfe-43bd-b927-52e21282a318
- Milestone: milestone-3-dashboard-wiring

## 🔒 Key Constraints
- Read-only investigation — do NOT implement changes in source code
- Produce self-contained handoff report at /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_frontend/handoff.md
- Update progress.md with timestamps
- Communicate back to parent via send_message

## Current Parent
- Conversation ID: 034328b5-8dfe-43bd-b927-52e21282a318
- Updated: 2026-08-29T16:33:15Z

## Investigation State
- **Explored paths**:
  - `dashboard/package.json`, `dashboard/vite.config.js`, `dashboard/index.html`
  - `dashboard/src/main.jsx`, `dashboard/src/Root.jsx`, `dashboard/src/buildingStore.js`, `dashboard/src/api.js`
  - `dashboard/src/App.jsx`, `dashboard/src/MobileApp.jsx`
  - `dashboard/src/AiInsightsPanel.jsx`, `dashboard/src/MobileAIScreen.jsx`, `dashboard/src/RecommendationEvidence.jsx`
  - `dashboard/src/useDigitalTwin.js`, `dashboard/src/useRecommendations.js`, `dashboard/src/useOpsStatus.js`, `dashboard/src/usePlugs.js`, `dashboard/src/useLibrary.js`, `dashboard/src/useRoomModels.js`, `dashboard/src/useForecastCompare.js`, `dashboard/src/useLocalModel.js`
  - `dashboard/src/GlobalMetricsPanel.jsx`, `dashboard/src/MaintenanceDrawer.jsx`, `dashboard/src/PlugLoadPanel.jsx`, `dashboard/src/TelemetryPanel.jsx`, `dashboard/src/HardwareInspector.jsx`, `dashboard/src/StreamStatus.jsx`
  - Backend integration points: `server/main.go`, `server/recommendapi.go`, `server/simulation/engine.go`
- **Key findings**:
  - The dashboard uses React 18 with Vite 5, Tailwind/CSS variables, Three.js/R3F, Recharts, and FlatBuffers.
  - Telemetry is streamed over WebSocket at 1Hz (`/ws`) using binary FlatBuffers; commands and overrides are sent over WebSocket as JSON (`{"action":..., "zone":...}`).
  - Learned recommendations are polled from `GET /api/recommendations` via `useRecommendations.js`.
  - Recommendation cards in `AiInsightsPanel.jsx` and `MobileAIScreen.jsx` wire real action handlers calling `sendManualOverride(rec.action, rec.zone)` or `sendManualOverride('precool', 'GLOBAL')`.
  - `App.jsx` still has a legacy modal `showAiModal` with `executeRemediation()` that triggers synthetic scenario switches (`loadScenario('remediating')`) rather than real device overrides.
- **Unexplored areas**: None for frontend scope.

## Key Decisions Made
- All 5 required areas analyzed with complete line-by-line evidence.

## Artifact Index
- /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_frontend/BRIEFING.md — Persistent agent state
- /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_frontend/progress.md — Liveness & task progress
- /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_frontend/DISPATCH.md — Incoming messages log
- /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_frontend/handoff.md — Final investigation report
