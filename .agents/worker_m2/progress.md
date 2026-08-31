# Progress — Milestone 2 BIM Backend

Last visited: 2026-08-31T04:51:00Z

## Status
- [x] Read background reports and original request.
- [x] Inspect existing `server/` codebase (`main.go`, `engine.go`, `blueprint.go`, etc.) and `dashboard/src/building-data-home.json`.
- [x] Place `server/data/building-data-home.json` and `server/data/brick-ontology.home.json`.
- [x] Implement/Update `GET /api/building-data` with `model` query param support in `modelswitch.go` and `main.go`.
- [x] Implement `POST /api/building/switch` and `POST /api/model/switch` in `modelswitch.go` and `main.go`.
- [x] Implement WebSocket `switch_model` / `switch_building` message actions in `main.go`.
- [x] Ensure `engine.ReloadBuilding(data)` performs clean reload, state reset, and duct re-solve.
- [x] Implement `server/building_switching_test.go` integration tests.
- [x] Implement `server/simulation/building_model_switch_test.go` simulation engine tests.
- [x] Verify file integrity and syntax.
- [ ] Generate `handoff.md` and send completion message.
