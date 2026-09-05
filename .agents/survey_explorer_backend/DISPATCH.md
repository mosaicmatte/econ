## 2026-08-31T04:28:32Z
<USER_REQUEST>
You are survey_explorer_backend.
Your working directory is: /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/
Authoritative user request file: /Users/nguyenhoangkhoi/Documents/econ/ORIGINAL_REQUEST.md

Task: Survey the backend and physics simulation engine for the requirements in ORIGINAL_REQUEST.md (lines 21-45):
1. R1: Live Data Integration — Audit `server/` and `server/simulation/` for any hardcoded/mock data or static constants that should be replaced with live sensor data or dynamic calculations.
2. R2: Smart Fallbacks for Missing Sensors — Analyze how the Go physics simulation engine currently handles missing physical sensors (e.g. missing temperature, occupancy, solar irradiance, power/current clamps, CO2, humidity). How does the 2R1C thermal model, solar radiation, HVAC/chiller COP, and ambient airflow derive realistic estimates from available sensors (e.g. outdoor weather, adjacent zone temperatures, thermal inertia, energy balance)? What changes are needed to ensure no static mock values are used?
3. R3 / Acceptance Criterion 1: Go unit/integration tests asserting that when specific sensor inputs are omitted, the simulation engine calculates realistic derived values using physics models instead of static mock data.

Inspect the codebase, identify existing tests and implementation files, and write a comprehensive survey report to `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_backend/report.md` and your `handoff.md`.
Send a completion message when done.
</USER_REQUEST>
