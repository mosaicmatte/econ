# CONTINUE HERE — agent continuation instructions

If the current session ends, pick up from this file. Read it top to bottom, then read the two
deep docs it points to before editing anything.

## Reference docs (read these)
- `econ/BACKEND_ARCHITECTURE.md` — the buildable backend spec (engine, MQTT + FlatBuffers
  contracts, how to add a streamed metric, build/run with no local `go`/`flatc`).
- `context-/CLAUDE_CONTEXT.md` — whole-project history/status.
- `econ/edge/raspberry_pi/README.md` — edge/gateway setup + MQTT contract.

## What is DONE & verified (do NOT regress)
- **Frontend**: desktop HUD + mobile Tesla-style viewer (hybrid heatmap that points to the
  faulting room). `CanvasErrorBoundary` wraps every `<Canvas>` — the "blacking out" fix; keep it.
- **Backend (Go, `econ/server`)**: 2R1C physics; MQTT occupancy ingestion; occupancy-driven
  optimizer (`actuate`) with safety delay; publishes `econ/commands/<zone>`. Runs in Docker.
- **Real metrics**: `GlobalData` streams `buildingLoadMw, systemHealth, totalOccupants,
  coolingOutputMw, plantCop (dynamic), energySavedMw`. Dashboard shows ONLY real values
  (`useDigitalTwin.js` → `globalMetrics`). **No hard-coded numbers — keep it that way.**
- **Edge**: ESP32 firmware `econ/edge/esp32/src/main.cpp` (parses `LIGHTS_ON|OFF;SETPOINT=c`,
  publishes telemetry JSON). Raspberry Pi failsafe gateway `econ/edge/raspberry_pi/gateway.py`
  (+ copy at `raspberry_backend/server.py`) — verified: it engages `LIGHTS_OFF;SETPOINT=28.0;
  SRC=FAILSAFE` when the engine is offline and a zone stays vacant.

## Hard workflow facts (cost hours if ignored)
- `go` and `flatc` are NOT on the host PATH. Build/run the engine ONLY via Docker:
  `cd econ/server && docker compose up -d --build server`. The image bakes `data/` at build
  time, so editing `data/*.json` or any `.go` needs `--build`.
- Add a Go dep via Docker: `docker run --rm -v "$PWD":/app -w /app golang:1.22 sh -c "go get <m>@<v> && go mod tidy"`.
- Docker Desktop sometimes stops → `open -ga Docker`, wait ~40s, retry.
- Dashboard preview: `.claude/launch.json` server `dashboard` (port 5188, prefix `econ/dashboard`).
  Verify via preview tools (console error-free + screenshot + eval), never ask the user.

## ACTIVE TASK — Branch B: floorplan → detailed `building-data.json`
Goal: ingest a real 2D floorplan and emit the **detailed layout schema** the engine/frontend
already consume, so the twin is built from a real blueprint instead of `generate_bim.js`.
- The detailed schema + room→archetype mapping is documented in
  `econ/ai_modules/branch_b_digitization/LAYOUT_SCHEMA.md`.
- The bridge pipeline is `econ/ai_modules/branch_b_digitization/floorplan_to_buildingdata.py`:
  floorplan image → room polygons (DeepFloorplan adapter, OpenCV fallback) → metric-normalized
  zones with `zoneType / thermalProperties / hvacMapping / centroid / bim_asset_id` → a floor in
  the detailed schema → optionally stamped across N levels into a full `building-data.json`.
- DeepFloorplan is the upgrade segmenter (needs the TF model + weights); the OpenCV path works now.

### Remaining for Branch B (do next)
1. Wire the real DeepFloorplan model into `segment_rooms()` (adapter stub is there). Use the
   TF2 port `zcemycl/TF2DeepFloorplan`; output room-type + boundary masks → polygons.
2. Add door/opening detection → `brick:feeds` adjacency so the React-Flow topology reflects the
   real plan (today the topology comes from `brick-ontology.json`; regenerate it from the
   digitized adjacency graph — see `geometry_merge.py` / `topologic_graph.json`).
3. Replace `data/building-data.json` with a digitized building and rebuild the engine image;
   confirm the 3D model + topology + physics still render (the dashboard reads the same schema).

## Other open items (lower priority)
- Manual-override veto: dashboard `ws.send({"action","zone"})` → `main.go` → publish
  `econ/commands/<zone>` (human-in-the-loop). Sketched in `BACKEND_ARCHITECTURE.md` §4.
- TimescaleDB history persistence (compose `db` is reserved, unused).
- Hardware-in-the-loop tests need the user's Mac M4 (YOLO `device=mps`) + a physical ESP32.
