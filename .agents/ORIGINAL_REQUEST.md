# Original User Request

## 2026-08-30T13:40:06Z

This is a single self-contained fix; keep it small and focused. 
Update the dashboard so that data changes dynamically when toggling a building level in the UI. Additionally, scan the project to identify unimplemented features, hardcoded values, and mock data.

Working directory: /Users/nguyenhoangkhoi/Documents/econ
Integrity mode: development

## Requirements

### R1. Dynamic Level Toggle
Update the dashboard codebase so that toggling a building level successfully fetches and displays real telemetry/building data for that specific level, removing any reliance on hardcoded mock data for this feature.

### R2. Codebase Scan Report
Scan the frontend and backend codebase for unimplemented features, hardcoded values, and mock data. Produce a clear markdown report (`mock_data_report.md`) detailing your findings and where they are located.


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
