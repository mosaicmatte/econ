# Original User Request

## Initial Request — 2026-08-30T13:40:25Z

You are the SWE Light orchestrator (teamwork_preview_swe).
Your working directory is: /Users/nguyenhoangkhoi/Documents/econ/.agents/swe_1
Workspace root: /Users/nguyenhoangkhoi/Documents/econ
Authoritative user request file: /Users/nguyenhoangkhoi/Documents/econ/ORIGINAL_REQUEST.md

Task Details:
1. Dynamic Level Toggle: Update the dashboard codebase so that toggling a building level successfully fetches and displays real telemetry/building data for that specific level, removing any reliance on hardcoded mock data for this feature.
2. Codebase Scan Report: Scan the frontend and backend codebase for unimplemented features, hardcoded values, and mock data. Produce a clear markdown report (`mock_data_report.md` in the root directory) detailing your findings and where they are located.

Acceptance Criteria:
- A new Puppeteer/Node test script (e.g., `dashboard/verify_level_toggle.js`) is created and passes. It must programmatically interact with the level toggle in the UI and assert that the underlying data or DOM elements correctly update.
- `mock_data_report.md` exists in the root directory and contains categorized findings of mock data, hardcoded values, and unimplemented features across the frontend and backend.

Execute the SWE Light process and notify me when complete.


## 2026-08-31T04:27:29Z

Replace all remaining hardcoded data in the application with live telemetry from connected sensors. Implement smart fallback logic for unavailable sensors using the simulation engine's physics, and ensure data accurately updates when switching between different Building Information Models (e.g., Office vs. Domestic House).

Working directory: /Users/nguyenhoangkhoi/Documents/econ
Integrity mode: development

## Requirements

### R1. Live Data Integration
Audit the codebase and replace any remaining hardcoded or mocked data with live telemetry from the connected hardware sensors and backend APIs.

### R2. Smart Fallbacks for Missing Sensors
When a specific physical sensor is unavailable, do not use static mock data. Instead, leverage the Go backend's physics and simulation engine to estimate and derive realistic values based on the data from the sensors that *are* available.

### R3. BIM Context Switching
Implement functionality to switch the active Building Information Model (BIM) entirely—specifically between the office building model and the domestic house model. Ensure that all associated data, rendering, and telemetry context update accurately according to the newly chosen model.

## Acceptance Criteria

### Verification
- [ ] Go unit/integration tests are written and pass, explicitly asserting that when specific sensor inputs are omitted, the simulation engine calculates realistic derived values using physics models instead of falling back to static mock data.
- [ ] A new Puppeteer/Node test script (e.g., `dashboard/verify_bim_switching.js`) is created and passes. It must programmatically interact with the BIM toggle to switch between the Office and Domestic House models, and assert that the underlying telemetry context and UI accurately reflect the newly chosen model.

Execute the SWE Light process and notify me when complete.
