## 2026-08-31T04:43:42Z
You are worker_m3_frontend_ui.
Your working directory is: /Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m3_frontend_ui/
Authoritative user request file: /Users/nguyenhoangkhoi/Documents/econ/ORIGINAL_REQUEST.md (specifically lines 21-45: Requirement R3 and Acceptance Criterion 2).

Background reports to read before starting:
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2/report.md`
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_frontend/report.md`

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A forensic auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

File Ownership: You own exclusively files under `dashboard/`:
- `dashboard/src/buildingStore.js`
- `dashboard/src/useDigitalTwin.js`
- `dashboard/src/sustainability.js`
- `dashboard/src/GlobalMetricsPanel.jsx`
- `dashboard/src/App.jsx`
- `dashboard/verify_bim_switching.js`
- `dashboard/package.json`

Milestone 3 Scope & Implementation Tasks:
1. Frontend BIM Model Switching Synchronization (Requirement R3):
   - In `dashboard/src/buildingStore.js`:
     - When `setBuildingModelType(type)` is called, notify the Go backend via `fetch('${API_BASE}/api/building/switch', { method: 'POST', body: JSON.stringify({ model: type }) })` and/or WebSocket message so the backend reloads the active building model in sync.
   - In `dashboard/src/useDigitalTwin.js`:
     - Subscribe to building model changes or reset `simData.zones` on model switch so that incoming telemetry packets immediately bind to the new building zones without displaying stale/orphaned zones from the previous model.
   - In `dashboard/src/sustainability.js`:
     - Update `FLOOR_AREA_M2`, `ZONE_MIX`, and `IS_IT_DOMINATED` to evaluate dynamically against `getBuilding()` so that floor area reflects ~72 m² for Domestic House and ~39,776 m² for Office Tower.
   - Ensure 3D canvas (`BuildingModel.jsx`), floor level buttons, active floor selection, P&ID topology, and telemetry profiler reactively adapt when switching between Office and Domestic House.
2. Puppeteer Verification Test (Acceptance Criterion 2):
   - Implement `dashboard/verify_bim_switching.js`:
     - Launch headless Puppeteer (with `--no-sandbox`, `--disable-setuid-sandbox` args).
     - Must programmatically interact with the BIM toggle (`data-testid="building-model-toggle"`, `data-testid="toggle-domestic-home"`, `data-testid="toggle-multilevel"`).
     - Assert that initial state is Multi-Level Office model (15 floor buttons, ~39,776 m² floor area, multiple levels).
     - Click `toggle-domestic-home` and assert:
       - Active model switches to Domestic House (`bldg-econ-house-hcmc`).
       - Level toggle reduces to 1 button (`L1`).
       - Selected level display shows `L1`.
       - Topology nodes update to 6 nodes (1 AHU + 5 Domestic House zones).
       - Global metrics and telemetry reflect 5 domestic zones.
     - Click `toggle-multilevel` and assert clean restoration of Office model (15 floors, 90+ zones).
     - Include rapid toggle stress test (switching back and forth with zero errors/crashes).
     - Return exit code 0 on success, non-zero on failure.
   - In `dashboard/package.json`:
     - Add `"test"` script: `"node verify_bim_switching.js && node verify_level_toggle.js && node verify_ai_actions.js"` (or ensure `npm test` runs `verify_bim_switching.js`).
3. Verification:
   - Run `cd /Users/nguyenhoangkhoi/Documents/econ/dashboard && npm run build`
   - Run `node verify_bim_switching.js`
   - Run `npm test`
   - Document all changes and test outputs in `/Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m3_frontend_ui/handoff.md`.
   - Send completion message when finished.
