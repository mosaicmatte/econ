# BRIEFING — 2026-08-31T04:31:00Z

## Mission
Survey the Go backend and physics simulation engine for live data integration, smart physics-based fallbacks for missing sensors, and test coverage requirements.

## 🔒 My Identity
- Archetype: explorer
- Roles: survey_explorer_backend
- Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend
- Original parent: 91798708-ba91-491c-a1cc-fb74bf8aa93a
- Milestone: backend-survey

## 🔒 Key Constraints
- Read-only investigation — do NOT implement
- Inspect server/ and server/simulation/ and related tests
- Produce structured report at /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/report.md and handoff.md

## Current Parent
- Conversation ID: 91798708-ba91-491c-a1cc-fb74bf8aa93a
- Updated: 2026-08-31T04:31:00Z

## Investigation State
- **Explored paths**:
  - `server/main.go`, `server/weather.go`, `server/mqtt.go`, `server/devices.go`, `server/blueprint.go`, `server/forecast.go`, `server/modelcatalog.go`, `server/recommendapi.go`
  - `server/simulation/engine.go`, `server/simulation/dynamics.go`, `server/simulation/library.go`, `server/simulation/plugs.go`, `server/simulation/bess.go`, `server/simulation/baselines.go`, `server/simulation/site.go`
  - `server/simulation/*_test.go` (12 test suites: measured, hardware, occupancy, plugs, dynamics, state_provenance, bess_sizing, baselines, autopilot, site, forecast_window, protocol_stress)
- **Key findings**:
  - Physics engine operates on "omission over fabrication": missing sensors do not use static mock values.
  - Smart fallbacks derive realistic values: 2R1C thermal model differential equations (temperature), diurnal profile & area density (occupancy), façade aperture multiplier (solar gain), strain-dependent COP & ventilation enthalpy (AC power), area standby + occupancy active load (plug loads), engineering design discharge temp (supply air temp), mass-balance estimation (CO2), and Hardy-Cross duct network solver (airflow/static pressure).
  - BIM switching context isolation atomically resets load history, global baselines, and rescales VAV resistances and fan curves to the new geometry.
  - Defined test matrix for Acceptance Criterion 1 (`smart_fallback_test.go`).
- **Unexplored areas**: None. Backend investigation complete.

## Key Decisions Made
- Produced comprehensive survey report at `report.md` and 5-component handoff report at `handoff.md`.

## Artifact Index
- /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/report.md — Comprehensive backend survey report
- /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/handoff.md — 5-component handoff report
- /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/DISPATCH.md — Dispatch log
- /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/progress.md — Task progress heartbeat
