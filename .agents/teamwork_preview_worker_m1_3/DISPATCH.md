## 2026-09-05T04:25:04Z

You are a Worker subagent for the econ project.
Your identity: worker_m1_3
Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_worker_m1_3
Workspace directory: /Users/nguyenhoangkhoi/Documents/econ
Authoritative user request path: /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md

You MUST read /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md before starting work.
Also read:
- Scope document: /Users/nguyenhoangkhoi/Documents/econ/PROJECT.md
- Survey reports:
  - /Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_explorer_survey_server_3/handoff.md
  - /Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_explorer_survey_sustainability_3/handoff.md
  - /Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_explorer_survey_market_3/handoff.md

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

File Ownership:
You exclusively own:
- `server/carbon.go` (create)
- `server/carbon_test.go` (create)
- `server/main.go` (modify to bind `/api/sustainability` route)
Do NOT modify other existing server files unless strictly necessary, and ensure all existing functionality and existing tests remain 100% intact.

Requirements to implement:
R1. Carbon Accounting (Scope 2 & Operational Carbon):
- In `server/carbon.go` (package main), implement Scope 2 operational carbon tracking:
  - Translate electrical energy consumption (from `plugW`, `stripW`, and AC states) into kgCO2e.
  - Integration: E_kWh = (P_watts * dt_seconds) / 3.6e6, and kgCO2e = E_kWh * gridFactor.
  - Default grid emission factor = 0.5 kgCO2e/kWh (configurable via `GRID_EMISSION_FACTOR` environment variable, defaulting to 0.5).
  - Provide both instantaneous power/emissions rate and cumulative emissions.
  - Include breakdown of power/emissions across `plugW`, `stripW`, and `acW`.

R2. Predictive Maintenance & Space Utilization:
- Implement equipment health monitoring:
  - Detect over-capacity wattage (e.g. power strip > 2000W, AC > 3500W).
  - Detect transient power surges (e.g. delta > 1000W).
  - Track total cumulative equipment runtime hours; raise predictive maintenance alert when runtime exceeds 2000 hours.
- Implement space utilization efficiency:
  - Compute building-wide and per-zone space utilization percentage using live `Occupancy` from `engine` and design capacity calculated from zone area (e.g. office: 10 m²/person, meeting room: 2.5 m²/person; exclude non-occupiable service zones).

R3. Carbon Credit Recommendations (Live Data):
- Compare cumulative Scope 2 emissions against a target carbon budget (default 50.0 kgCO2e, configurable via `CARBON_BUDGET_KG`).
- If over budget, calculate deficit in kgCO2e, metric tons (1 metric ton = 1000 kg), and whole certificates (ceil).
- Outbound HTTP client:
  - Makes an outbound HTTP GET to pull live carbon market pricing (default URL: `https://api.coingecko.com/api/v3/simple/price?ids=toucan-protocol-base-carbon-tonne&vs_currencies=usd`, configurable via `CARBON_MARKET_URL`).
  - Log demonstrably: `log.Printf("[carbon-market] outbound HTTP GET %s", url)`.
  - In-memory `sync.RWMutex` cache with 10-minute TTL to prevent rate limits.
  - 8-second HTTP timeout.
  - Fallback price of $12.50/ton with `isLive: false` if network request fails or is blocked by sandbox policy.
  - Calculate estimated offset purchase cost in USD.

R4. Sustainability API Endpoint:
- Create `GET /api/sustainability` (and OPTIONS CORS preflight) exposing the JSON schema specified in `PROJECT.md`.
- Register the route in `server/main.go`.

Testing & Verification:
- Write `server/carbon_test.go` verifying:
  1. Exact programmatic assertion: 1000W drawn for 1 hour (3600s) with 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon.
  2. Demonstrable outbound HTTP request to live carbon market pricing using `httptest.NewServer`.
  3. Caching behavior (subsequent calls within TTL do not make outbound requests).
  4. Graceful fallback when upstream market API fails.
  5. Predictive maintenance alert generation on over-capacity load.
  6. Space utilization efficiency calculation.
- Run `go build .` in `server/` and verify clean build.
- Run `go test -v ./...` in `server/` and verify all tests pass.

Write `changes.md` and `handoff.md` in your working directory (/Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_worker_m1_3) documenting your implementation, exact commands run, and build/test outputs.
When done, message parent with your completion report.
