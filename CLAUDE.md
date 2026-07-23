# Orientation

Read this before changing anything. It is the map, not the manual — the manual is
[README.md](README.md).

ECON is a building digital twin: a Go physics engine that simulates a real digitized
office, ingests live telemetry from physical edge nodes over MQTT, learns each room's
thermal and CO₂ behaviour from its own data, and actuates lighting, sockets and air
conditioners back through those nodes.

## Where things are

| Path | What lives there |
|---|---|
| `server/` | The Go engine. `simulation/` is the physics and the learned models; the files beside it are the HTTP/MQTT/DB surface. |
| `server/data/` | The building fixture, the programme library, and persisted model state. **Data, not code — see the rule below.** |
| `dashboard/` | React + Three.js operator UI. Talks to the engine over a websocket. |
| `edge/esp32`, `edge/pico` | Node firmware. `edge/raspberry_pi` is the gateway failsafe. |
| `edge/SHOPPING_LIST.md`, `edge/WIRING.md` | What to buy and how to wire it. Prices are real and dated. |
| `ai_modules/branch_a_occupancy` | YOLO + ByteTrack head count, publishes on the same MQTT contract as a node. |
| `ai_modules/branch_b_digitization` | DeepFloorplan → building fixture. |
| `backend/forecasting` | Python LSTM + TimesFM load forecaster. |
| `tools/` | Repo-level utilities. `officeize_fixture.py` regenerates the building fixture. |
| `PAPER.md` | The research paper: the evidence, the mathematics, and what is and is not demonstrated. |
| `COMMISSIONING.md` | Turn on and test every component, each step with a test that fails loudly. |
| `docs/` | [ROADMAP](docs/ROADMAP.md) (what is *not* built), [EVIDENCE](docs/EVIDENCE.md) (why any of this), [RUNNING](docs/RUNNING.md), [BACKEND_ARCHITECTURE](docs/BACKEND_ARCHITECTURE.md). |

## The engine, in the order data moves

1. `engine.go` — `tick()` integrates a 2R1C heat balance per zone; `actuate()` decides
   setbacks and publishes commands; `broadcast()` computes the metrics the dashboard shows.
2. `dynamics.go` — recursive least squares fits each room's thermal and CO₂ balance from
   its own history. This is the differentiator: time constant, cooling authority, measured
   air-change rate.
3. `baselines.go` — per (zone, metric, hour) learned normals; anomalies scored in σ.
4. `recommend.go` — turns both models into ranked recommendations.
5. `plugs.go` — the plug-load sweep, the end use a conventional BMS cannot see.

## Four rules this codebase actually enforces

**1. Never fabricate a measurement.** A modelled value must never travel on a channel that
implies it was measured. `tempReal`, `acReal` and `Co2Live` exist for this; firmware
*omits* a field when its sensor is absent rather than sending a plausible default. A
fabricated zero on a current clamp tells the twin the compressor is off.

**2. Building coefficients live in `server/data/programme-library.json`, not in Go.** Every
lighting density, U-value, setpoint and ventilation rate is there with its source. If you
find yourself typing a number that describes the *building* into a `.go` file, it belongs
in the library instead. Physics stays in Go; the coefficients it is evaluated with do not.

**3. Do not claim a saving from a mechanism that does not exist.** If `docs/ROADMAP.md`
lists it as unbuilt, `docs/EVIDENCE.md` must not credit it. This has been violated once and
caught once.

**4. An identified model beats a configured constant.** Where a room has been identified
from real data, its measured coefficient supersedes the library default. The library is the
prior, not the answer.

## Things that will mislead you

- **The fixture is generated, not authored.** `server/data/building-data.json` comes from
  `tools/officeize_fixture.py`. Edit the generator or the library, never the JSON.
  `building-data.json.digitizer-raw` is the untouched segmenter output, kept for audit.
- **Zone and VAV ids are opaque.** `zone-server-room-59-lvl1` may well be a cellular
  office — ids are keys minted by the digitizer and referenced across the Brick ontology,
  so they survive re-classification. Label from `name`/`zoneType`, never by parsing an id.
- **Sim time runs at 1× in nominal operation.** Room identification samples every 300 sim
  seconds and matures at 36 samples, so it needs ~3 h of uptime. A cold demo shows zero
  identified rooms; that is expected, not a bug.
- **Two commits' worth of history is not the whole story.** `docs/EVIDENCE.md` explains why
  the design is shaped the way it is, grounded in the case studies it cites.

## Working here

- Build and test: `cd server && go build ./... && go test ./...`
- Regenerate the fixture: `python3 tools/officeize_fixture.py` (dry run) then `--write`.
- The dashboard dev server caches dependencies; if it renders blank, clear
  `dashboard/node_modules/.vite` **and** `~/node_modules/.vite`.
