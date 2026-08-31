# DISPATCH

## 2026-08-31T04:43:42Z
**Mission**: Milestone 2 - BIM Backend Implementation for Building Model Switching
**Working Directory**: /Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m2/
**Target Files**:
- `server/data/building-data-home.json`
- `server/main.go`
- `server/blueprint.go` (or dedicated `server/modelswitch.go`)
- `server/building_switching_test.go`
- `server/` engine reload mechanics if needed

**Tasks**:
1. Copy or provide `server/data/building-data-home.json` (from `dashboard/src/building-data-home.json`).
2. Update `GET /api/building-data` with `?model=` support.
3. Implement `POST /api/building/switch` and `POST /api/model/switch`.
4. Implement WebSocket action `switch_model` / `switch_building`.
5. Ensure `engine.ReloadBuilding` cleanly replaces state, resets baselines, re-solves duct network, and streams matching FlatBuffers packets.
6. Implement `server/building_switching_test.go` verifying commercial -> domestic house -> commercial switching.
7. Run `cd server && go test -v -count=1 ./...` and record in `handoff.md`.
