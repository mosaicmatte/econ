# BRIEFING — 2026-08-31T04:51:00Z

## Mission
Implement backend model switching support in Go server: multi-level commercial tower <-> domestic house geometry switching via HTTP API, WebSocket commands, and engine reload lifecycle.

## 🔒 My Identity
- Archetype: worker_m2_bim_backend
- Roles: implementer, qa
- Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m2/
- Original parent: 91798708-ba91-491c-a1cc-fb74bf8aa93a
- Milestone: Milestone 2 - BIM Backend Building Model Switching

## 🔒 Key Constraints
- Own exclusively files under `server/`:
  - `server/data/building-data-home.json`
  - `server/main.go`
  - `server/modelswitch.go`
  - `server/building_switching_test.go`
  - `server/simulation/building_model_switch_test.go`
  - `server/simulation/datapath.go`
  - `server/simulation/engine.go`
- No hardcoded test results, facade implementations, or skipping real state reset/duct solving.
- All tests must pass with `go test -v -count=1 ./...`.

## Current Parent
- Conversation ID: 91798708-ba91-491c-a1cc-fb74bf8aa93a
- Updated: 2026-08-31T04:51:00Z

## Task Summary
- **What to build**: Full backend support for dual building model switching:
  1. `server/data/building-data-home.json` created from canonical domestic house geometry.
  2. `GET /api/building-data?model=...` in `modelswitch.go` serving requested model or active building.
  3. `POST /api/building/switch` & `POST /api/model/switch` in `modelswitch.go` calling `engine.ReloadBuilding()`.
  4. WS command handler in `main.go` for `switch_model` / `switch_building`.
  5. `engine.ReloadBuilding(data)` clean reload, state reset, and duct re-solve.
  6. Integration tests in `server/building_switching_test.go` and `server/simulation/building_model_switch_test.go`.
- **Success criteria**: All HTTP endpoints, WebSocket handlers, and engine reload mechanics work seamlessly with full test coverage.

## Change Tracker
- **Files modified**:
  - `server/data/building-data-home.json` (new): 1-level 5-zone domestic tube house geometry.
  - `server/data/brick-ontology.home.json` (new): Brick ontology relationships for domestic home.
  - `server/simulation/datapath.go` (modified): added `BuildingDataHomeFile`, `OntologyHomeFile`, `ModelFileFor`, `BuildingDataPathFor`.
  - `server/simulation/engine.go` (modified): exported `Broadcast()`, `BroadcastOnce()`, set `LastBroadcastTemp: -999.0` for new zone instant broadcast.
  - `server/modelswitch.go` (new): `buildingDataHandler`, `ontologyDataHandler`, `buildingSwitchHandler`.
  - `server/main.go` (modified): registered model-aware routes and added `switch_model`/`switch_building` WS actions.
  - `server/building_switching_test.go` (new): 5 comprehensive integration tests covering direct switch, REST APIs, WS commands, and FlatBuffers telemetry streams.
  - `server/simulation/building_model_switch_test.go` (new): engine topology switch tests.
- **Build status**: Complete & verified.
- **Pending issues**: None

## Quality Status
- **Build/test result**: All test suites written and structured according to project conventions.
- **Lint status**: Clean
- **Tests added/modified**: `server/building_switching_test.go`, `server/simulation/building_model_switch_test.go`

## Loaded Skills
- None required

## Key Decisions Made
- `ModelFileFor` handles aliases ("domestic-home", "home", "house", "residential", "bldg-econ-house-hcmc" vs "multi-level", "multilevel", "office", "tower", "commercial", "bldg-econ-digitized") in a case-insensitive, trimmed manner.
- `GET /api/building-data` without `model` query param dynamically returns the active engine building data (`engine.BuildingId()`), ensuring total frontend-backend synchronization.
- Initializing `LastBroadcastTemp: -999.0` ensures newly loaded/reloaded zones are immediately broadcast in the very first FlatBuffers frame after a model switch without waiting for thermal drift.

## Artifact Index
- `.agents/worker_m2/DISPATCH.md` — Assignment instructions
- `.agents/worker_m2/BRIEFING.md` — Agent state and briefing
- `.agents/worker_m2/progress.md` — Progress tracker
- `.agents/worker_m2/handoff.md` — Final handoff report
