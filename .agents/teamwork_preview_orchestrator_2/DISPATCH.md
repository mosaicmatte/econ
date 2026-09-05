## 2026-09-05T02:47:45Z

You are the Project Orchestrator for the Sustainability & Decarbonization backend module in econ.

Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_orchestrator_2
Workspace directory: /Users/nguyenhoangkhoi/Documents/econ
Authoritative user request: /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md

Task summary:
Implement a core "Sustainability & Decarbonization" backend module for the econ building management system. Based on the provided domain knowledge, this module should translate existing telemetry into carbon metrics, support predictive maintenance, and expose a unified API for the dashboard that includes live carbon credit purchasing recommendations.

Requirements:
R1. Carbon Accounting (Scope 2 & Operational Carbon):
Implement a Go backend module (e.g., `server/carbon.go`) that continuously calculates Scope 2 Operational Carbon by translating energy consumption (from `plugW`, `stripW`, and AC states) into kgCO2e using configurable grid emission factors.

R2. Predictive Maintenance & Space Utilization:
Extend the existing engine logic (e.g., `ZoneSim`) to track equipment health. Flag abnormal power draws or track total runtime hours to simulate Predictive Maintenance alerts. Utilize the existing occupancy data to calculate space utilization efficiency.

R3. Carbon Credit Recommendations (Live Data):
Implement logic that compares the calculated emissions against a target "carbon budget". If the infrastructure fails to meet the requirement, the system must fetch live carbon credit pricing data from the internet (via public APIs or web scraping) and recommend purchasing the exact amount of carbon certificates needed to offset the difference, including the estimated cost.

R4. Sustainability API Endpoint:
Create a new REST endpoint (e.g., `/api/sustainability`) that exposes the aggregated data: total Scope 2 emissions, current space utilization efficiency, active predictive maintenance warnings, and the dynamic carbon credit recommendations.

Acceptance Criteria:
- The server directory compiles successfully (`go build .`) with the new code.
- Existing backend functionality is not broken.
- A new Go test (e.g., `carbon_test.go`) is written to programmatically verify the carbon calculation logic (e.g., asserting that 1000W drawn for 1 hour with a 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon).
- The backend demonstrably makes an outbound HTTP request to pull live carbon market pricing.
- `go test ./...` passes successfully.
- A curl request to the new endpoint (`/api/sustainability`) returns a valid JSON payload containing carbon totals, maintenance alerts, and (if over budget) the recommended carbon credit offset amount and live cost.

Maintain progress.md and BRIEFING.md in your working directory. Orchestrate specialists as needed and report completion when verified.
