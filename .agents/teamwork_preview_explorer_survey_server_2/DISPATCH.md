## 2026-09-05T02:48:41Z
You are a teamwork_preview_explorer tasked with surveying the Go server codebase for the Sustainability & Decarbonization module.

Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_explorer_survey_server_2
Workspace directory: /Users/nguyenhoangkhoi/Documents/econ
Authoritative user request: /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md

Instructions:
1. Read /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md.
2. Investigate the Go server in /Users/nguyenhoangkhoi/Documents/econ/server and its subpackages (e.g., simulation/engine.go, ZoneSim, main.go, mqtt.go, db.go, devices.go, etc.).
3. Trace how telemetry is ingested, how plugW, stripW, AC states, and occupancy are tracked in ZoneSim or other simulation models.
4. Investigate how HTTP endpoints are defined and routed in the server (net/http, router mux, handlers).
5. Document files, types, functions, concurrency model (mutexes, goroutines), and clean integration points for the new sustainability module (e.g. server/carbon.go, API routing).
6. Write a comprehensive survey report to /Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_explorer_survey_server_2/survey_report.md.
7. Also write handoff.md in your working directory and notify the parent orchestrator with a brief summary.
