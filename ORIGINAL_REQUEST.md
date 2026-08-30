# Original User Request

## Initial Request — 2026-08-29T16:56:34Z

# Teamwork Project Prompt — Draft

> Status: Launched
> Goal: Wait for teamwork_preview execution to complete
> Requested team: The full agent team

Wire the forecasting data (from the TimeFM or LSTM models) directly into the dashboard's recommendations and AI panels, and increase the detail and verbosity of system logs and telemetry across the project.

Working directory: /Users/nguyenhoangkhoi/Documents/econ
Integrity mode: development

## Requirements

### R1. Forecast Graph Rendering
Extend the dashboard's AI panel and recommendations UI to render a visual chart/graph of the output from the TimeFM or LSTM forecasting models. 

### R2. End-to-End Forecast Wiring
Ensure the forecasting backend exposes the graph data, the Go server API proxies/delivers it, and the frontend consumes it to display the chart alongside the existing recommendations.

### R3. Detailed Telemetry & Logging
Increase the logging verbosity to debug level across all relevant services (forecasting, server, and edge). Include the full JSON payloads in the MQTT telemetry logs to make system observability more detailed.

## Acceptance Criteria

### Automated Verification
- [ ] Integration tests are updated or added to assert that the `GET /api/recommendations` (or equivalent) endpoint correctly returns the forecast graph data.
- [ ] A programmatic verification script (e.g., Puppeteer/Playwright) successfully runs and checks that the forecast graph/chart element actually renders in the AI panel UI.
- [ ] A test or script validates that the backend logs now output full MQTT telemetry JSON payloads.

## Follow-up — 2026-08-30T01:10:29Z

# Teamwork Project Prompt — Draft

> Status: Launched
> Goal: Fix UI bugs and add domestic home toggle
> Requested team: A small focused team

This is a single self-contained fix; keep it small and focused.
Fix several UI bugs on the dashboard and add a toggle to switch the 3D view between the 1-level domestic home model and the full multi-level building model.

Working directory: /Users/nguyenhoangkhoi/Documents/econ/dashboard
Integrity mode: development

## Requirements

### R1. Fix UI Rendering Bugs
- Fix the top tab bar where text is being cut off (e.g., "PLU..." instead of the full tab name).
- Fix the 3D visualization rendering bug where the blue connection lines/rays are misaligned, shooting off to incorrect points, or creating a chaotic web when zoomed/panned.
- Fix the issue where the entire visualization/screen appears darkened (e.g., stuck backdrop overlay, missing ambient lighting, or incorrect opacity settings).
- Fix the duplicated AI Load Forecast cards in the AI Insights panel. The "AI Load Forecast Trajectory" and "Load Forecast Not Yet Checked" cards currently display the exact same chart and data. Merge them or redesign the UI so they don't duplicate the chart and instead provide complementary, valuable information.

### R2. Add Domestic Home Toggle
- The codebase already contains the 3D asset for a 1-level domestic home.
- Add a UI toggle (e.g., a button or switch) below the 3D view that allows the user to switch between the 1-level domestic home model and the default full multi-level building model.
- Wire this toggle so that the selected 3D asset is correctly loaded and rendered in the viewer.

## Acceptance Criteria

### Automated UI Verification
- [ ] A Puppeteer/Playwright test script is provided or updated in the `dashboard` directory.
- [ ] The script automatically toggles the view to the domestic home model and verifies that the UI updates (e.g., by checking for a specific DOM element, canvas state, or log).
- [ ] The script verifies that the top tab bar renders without truncation.
