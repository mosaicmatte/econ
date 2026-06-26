# How to Create Test Data for ECON

Step-by-step guide for an agent to create, deploy, and exercise test data for the whole ECON
system (Go physics engine + React twin). Read [`ai_modules/branch_b_digitization/LAYOUT_SCHEMA.md`](ai_modules/branch_b_digitization/LAYOUT_SCHEMA.md)
first — it defines the exact schema and is the source of truth for every field.

## What "test data" is
Two JSON files, both consumed by the engine and the dashboard:
1. **`server/data/building-data.json`** — floors → zones (polygons, types, thermalProperties,
   hvacMapping). The dashboard reads its **own copy** at `dashboard/src/building-data.json`, so a
   build must update **both**.
2. **`server/data/brick-ontology.json`** — the Brick semantic graph (`brick:feeds` AHU→VAV→zone +
   electrical panel→circuit→zone, `brick:hasPoint` zone→sensor/camera) that drives the React-Flow
   systems map. Served to the dashboard at runtime via `GET /api/ontology` (engine copy only).

To *drive* the system you also feed live MQTT occupancy + WS scenario commands (see §5).

---

## 1. Method A — comprehensive generator (recommended)
`server/preprocessing/generate_test_building.py` emits a diverse, internally-consistent building
**+ matching ontology** in one shot. By default it writes safe `*.testbed.json` files (never
clobbers the live data).

```bash
cd econ/server
python3 preprocessing/generate_test_building.py
#  -> data/building-data.testbed.json + data/brick-ontology.testbed.json
#  14 floors / 126 zones (office, conference, corridor, server-room, mechanical, lobby, retail),
#  8 server rooms across 2 floors (fault targets), 141 ontology nodes / 756 relationships.
```
**Customize:** edit the constants at the top of the script:
- `FW, FD, FLOOR_HEIGHT` — footprint & floor height (metres).
- `SLOTS` / `CORE` — the 9-slot room layout (8 perimeter + central core) as polygons.
- `ARCH` — per-zone-type thermal archetype (`setpoint, deadband, baseHeatLoad, occ`); `cAir` and
  `solarGainMultiplier` are derived (area × height × 1210; perimeter rooms get solar).
- `FLOORS` — which room types sit in which slot for each floor archetype.
- `LEVEL_TYPE` / `N_FLOORS` — assign archetypes to levels (more `server` floors ⇒ more fault targets).

It guarantees the invariants the engine/twin rely on: unique `zoneId`/`vavId`, every zone has full
`thermalProperties` + `hvacMapping`, and the ontology zone-nodes exactly match the building zones.

## 2. Method B — digitize a real floorplan
Turn a 2D plan into the schema (great for "real building" test data):
```bash
cd econ/ai_modules/branch_b_digitization
python3 floorplan_to_buildingdata.py --image deepfloorplan/real_floorplan.png \
        --out /tmp/building-data.json --floors 14 --footprint 60x40 --debug /tmp/annotated.png
```
- Room segmentation is `deepfloorplan_infer.py` (a multi-task wall→watershed→room-type port;
  swap in the real CubiCasa5K/TF2DeepFloorplan model via `_rooms_neural`). It currently produces
  *geometry-based* type hints, so review/relabel server-rooms before deploying.
- This path writes `building-data.json` only; generate a matching ontology by adapting
  `generate_test_building.py`'s ontology block to the digitized zones (or hand-author it).

---

## 3. ⚠️ ID wiring — DO THIS or the demo breaks
Regenerating data changes `zoneId`s. A few places still reference specific ids; update them to ids
that exist in your new data (or — better — make them data-driven):

| What | File | Fix |
|---|---|---|
| MQTT demo occupancy zone | `server/simulation/engine.go` (`demoZoneAlias`, ~L189) | point `"zone_1"`/`"Level 4"` to a real **office** zoneId in your data (e.g. `zone-leasing-office-lvl1`). Or make `resolveZone` fall back to the first office zone. |
| Fault target default | `dashboard/src/useDigitalTwin.js` (~L53–54, `useState('zone-server-lvl8')`) | a real **server-room** zoneId in your data. |
| Fault dropdown options | `dashboard/src/App.jsx` (~L764, `<option value="zone-server-lvl8">`) | list your server-room zoneIds (ideally derive `buildingData.floors[].zones[].filter(zoneType==='server-room')`). |

**Robust pattern (preferred):** derive `faultTarget` options and the demo zone from the loaded
`building-data.json` instead of hard-coding — then any regenerated data "just works."

---

## 4. Deploy
```bash
# 1. promote testbed -> live (engine reads data/, dashboard reads its own copy)
cp econ/server/data/building-data.testbed.json econ/server/data/building-data.json
cp econ/server/data/brick-ontology.testbed.json econ/server/data/brick-ontology.json
cp econ/server/data/building-data.testbed.json econ/dashboard/src/building-data.json
# 2. do the §3 ID wiring
# 3. rebuild — the Docker image BAKES data/ at build time, so a plain restart is NOT enough:
cd econ/server && docker compose up -d --build server
# 4. rebuild the dashboard
cd ../dashboard && npm run build
```
(`go`/`flatc` are not on the host — everything Go is Docker-only. See `BACKEND_ARCHITECTURE.md`.)

## 5. Drive & test the whole system (no hardware needed)
Use the broker's own clients to replay occupancy and trigger scenarios:
```bash
# watch actuation commands the engine emits
docker exec econ-mqtt-1 mosquitto_sub -t 'econ/commands/#' -v &

# occupancy schedule on the demo zone -> expect a setback when it goes empty
docker exec econ-mqtt-1 mosquitto_pub -t econ/telemetry/zone_1 -m '{"zone":"Level 4","occupancy":12}'   # occupied
docker exec econ-mqtt-1 mosquitto_pub -t econ/telemetry/zone_1 -m '{"zone":"Level 4","occupancy":0}'    # vacant
#   ~3s later: econ/commands/zone_1  LIGHTS_OFF;SETPOINT=26.0   (energySavedMw rises)

# scenarios go over the dashboard WS as text: "peak" | "fault:<zoneId>" | "remediating"
```
**Expected behaviour to assert:**
- vacant zone → `LIGHTS_OFF;SETPOINT=…` command + `energySavedMw > 0`;
- `fault:<server-zoneId>` → that room ramps green→yellow→red, `systemHealth` drops, `plantCop` dips;
- all `GlobalData` metrics (`buildingLoadMw`, `coolingOutputMw`, `plantCop`, `energySavedMw`) move
  dynamically — nothing should be constant/hard-coded.

## 6. Verify in the dashboard
Start the preview (`.claude/launch.json` server `dashboard`, port 5188) and confirm via the preview
tools (never ask the user): console is error-free, the 3D model renders all floors, the topology
shows the new zones/edges, and triggering a fault visibly reddens the targeted server room. Decode
a WS frame to assert the metrics are live (see `BACKEND_ARCHITECTURE.md` §3).

---

### Quick reference — files
```
server/preprocessing/generate_test_building.py     comprehensive generator (building + ontology)
ai_modules/branch_b_digitization/floorplan_to_buildingdata.py   floorplan -> building-data
ai_modules/branch_b_digitization/deepfloorplan_infer.py         room segmenter (port)
ai_modules/branch_b_digitization/LAYOUT_SCHEMA.md   schema + archetype table (source of truth)
server/data/*.testbed.json                          ready-to-deploy comprehensive test set
```
