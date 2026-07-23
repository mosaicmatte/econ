# Commissioning — turning everything on, and proving it works

Components in dependency order. **Each step has a test that fails loudly if the step did not
work**, because the failure mode this document exists to prevent is a system that looks alive
and is quietly reporting fiction.

Two rules while working through this:

1. **Do not skip the test.** A component that starts is not a component that works. Every
   test below distinguishes "running" from "doing its job."
2. **A missing sensor is not a failed test.** The firmware omits fields it cannot measure.
   `co2` absent from a node with no NDIR is correct behaviour, not a fault.

Prerequisites: Docker, Go 1.21+, Node 18+, Python 3.10+. For hardware, PlatformIO
(`python3 -m platformio`) and `mpremote`.

---

## 0. Fast path — software only, no hardware

If you only need the twin running:

```bash
cd econ/server && docker compose up -d && go run .
```

```bash
cd econ/dashboard && npm install --legacy-peer-deps && npm run dev
```

Open http://localhost:5188. Then jump to §7 to verify. Everything below is the full path.

---

## 1. Broker (MQTT)

Everything edge-side depends on this, so it goes first.

```bash
cd econ/server && docker compose up -d mqtt
```

**Test** — publish and subscribe to yourself:

```bash
docker exec -it server-mqtt-1 mosquitto_sub -t 'econ/#' -v
```

In a second terminal:

```bash
docker exec -it server-mqtt-1 mosquitto_pub -t 'econ/test' -m 'hello'
```

**Pass:** `econ/test hello` appears in the first terminal.
**Fail:** nothing appears → the container is not up, or port 1883 is taken (`lsof -i :1883`).

Leave the subscriber running. It is the single most useful diagnostic window for everything
that follows.

---

## 2. Database (TimescaleDB)

Optional. Without it the engine runs and the dashboard works; you lose history and the
`/api/series` endpoints return empty.

```bash
docker compose up -d db
```

**Test:**

```bash
docker exec -it server-db-1 psql -U postgres -d econ -c "\dt"
```

**Pass:** a table list including `readings`.

---

## 3. Engine

```bash
cd econ/server && go build ./... && go test ./... && go run .
```

**Test 1 — the programme library loaded.** Watch the first lines of output:

```
[library] loaded ./data/programme-library.json v2: 9 programmes (2 critical), fresh-air 10 L/s/person
```

**Pass:** that line, with a non-zero programme count.
**Fail:** `falling back to built-in physics` means the library was not found — the building
is running on default coefficients and is **not site-calibrated**. Check your working
directory; the path is relative.

**Test 2 — the building is the right size.**

```bash
curl -s localhost:8080/api/plugs | python3 -m json.tool | head -20
```

**Pass:** `totalKw` in the low hundreds for the bundled 39,777 m² office.
**Fail:** tens of thousands means you are running the raw digitizer fixture — see §8.

**Test 3 — the API is up.**

```bash
curl -s localhost:8080/api/hardware | head -c 200
```

---

## 4. Forecaster (optional)

```bash
cd econ/backend/forecasting && pip install -r requirements.txt && uvicorn main:app --port 8000
```

**Test:**

```bash
curl -s localhost:8000/health
```

**Pass:** `{"model_ready": true, ...}`. `model_ready: false` means the service is up but
untrained — the engine degrades to its own statistical forecast and says so.

---

## 5. Dashboard

```bash
cd econ/dashboard && npm install --legacy-peer-deps && npm run dev
```

**Test:** open http://localhost:5188.

**Pass:** the 3D building renders and the header shows a load figure.
**Fail — blank page:** this is almost always a stale Vite dependency cache. Clear **both**:

```bash
rm -rf econ/dashboard/node_modules/.vite ~/node_modules/.vite
```

If it is still blank, the error boundary will now tell you what threw — read the message on
screen rather than guessing.

---

## 6. Edge hardware

### 6.1 ESP32 node

Set the board's identity and its fitted sensors in `edge/esp32/platformio.ini` — **not** via
shell environment variables, which split labels containing spaces:

```ini
build_flags =
  -DZONE_TOPIC_OVERRIDE=\"zone_2\"
  -DZONE_LABEL_OVERRIDE=\"Level 5 East\"
  -DUSE_SHT30=1 -DUSE_MMWAVE=1 -DUSE_IR_AC=1 -DIR_AC_PROTOCOL=COOLIX
```

Flash and watch:

```bash
cd econ/edge/esp32 && python3 -m platformio run -t upload && python3 -m platformio device monitor
```

**Test 1 — identity took.** Before flashing a floor's worth:

```bash
python3 -m platformio run && strings .pio/build/esp32dev/firmware.elf | grep zone_2
```

**Test 2 — it is on the bus.** In the §1 subscriber window you should see telemetry every
5 s on `econ/telemetry/zone_2`.

**Test 3 — the temperature is real.** In that payload:

- `"tempReal": true` → a genuine sensor is pinning zone physics.
- `"tempReal": false` → the board is reporting a placeholder. **This is correct and honest**
  if no SHT30 is fitted; it is a fault if one is.

Warm the SHT30 between your fingers — the value should move within 5 s.

**Test 4 — presence.** Pinch GPIO32 (zero-wiring demo) or wave at the radar. `occupancy`
changes within ~0.2 s. With a radar, **sit still** and confirm it stays 1 — that is the whole
reason for choosing mmWave over PIR.

**Test 5 — I²C bus.** Serial prints `[i2c] bus up on SDA=GPIO21 SCL=GPIO22`. With an
ACD1200 fitted, allow **120 s preheat**; readings outside 300–10000 ppm are rejected rather
than published.

**Test 6 — the control loop actually closes.** At boot:

```
[hvac] IR AC control ACTIVE: COOLIX on GPIO19
```

Then in telemetry, `"acReal": true`. **If `acReal` is false, every setpoint the twin sends
this zone reaches nothing, and no saving attributed to its setback is real.** Point the
emitter at the indoor unit and confirm the unit beeps on a setpoint change.

**Test 7 — plug metering (build D).** With `-DUSE_PLUG=1`, switch a known load on the
clamped circuit and confirm `plugW` tracks it. Calibrate by scaling `PLUG_CAL_A_PER_V` by the
ratio against a known wattage.

> ⚠️ **Clamp the LIVE conductor only.** Around a two-core cord the currents cancel and you
> read ~0 A while everything looks correctly wired.

### 6.2 Pico node

```bash
cd econ/edge/pico && mpremote cp main.py :main.py && mpremote reset
```

A **plain Pico** has no radio and needs the bridge:

```bash
python3 bridge.py
```

**Test:** telemetry appears on the bus. **No bridge running, no Pico data.** A Pico W with
`WIFI_SSID` set connects directly and needs no bridge.

**Expect `tempReal: false`** with no SHT30 attached — the RP2040's internal sensor measures
the die, not the room, and has been observed reporting 16.5 °C in a 29 °C room.

### 6.3 CV occupancy node

```bash
cd econ/ai_modules/branch_a_occupancy/yolo_bytetrack
python3 yolo_tracker.py --source 0 --zone "Level 4" --topic zone_1
```

**Test:** walk across the counting line; `occupancy` on that topic changes.

> ⚠️ **One occupancy source per zone.** The engine takes whichever arrives last and does not
> arbitrate by source, so a radar's 0/1 will overwrite a camera's head count every 5 s. Use
> the camera where the count matters, the radar everywhere else.

### 6.4 Gateway failsafe

```bash
cd econ/edge/raspberry_pi && python3 gateway.py
```

**Test:** stop the engine. The gateway should keep a vacant zone from being left lit and
cooled, publishing commands tagged `;SRC=FAILSAFE`.

---

## 7. End-to-end verification

Work down. Each proves the one below is worth attempting.

| # | Check | Pass |
|---|---|---|
| 1 | `docker ps` | mqtt, db, forecasting containers up |
| 2 | Engine log | `[library] loaded ... v2: 9 programmes` |
| 3 | `curl localhost:8080/api/plugs` | `totalKw` in the hundreds, not thousands |
| 4 | Dashboard header | Load figure, 3D renders, no error boundary |
| 5 | `mosquitto_sub -t 'econ/#' -v` | Telemetry every 5 s per node |
| 6 | `curl localhost:8080/api/hardware` | `online: true`, correct `source` per node |
| 7 | Node telemetry | `tempReal` and `acReal` **match what is physically fitted** |
| 8 | `curl localhost:8080/api/recommendations` | `model.established` climbing |
| 9 | `curl localhost:8080/api/rooms/models` | `thermalSamples` climbing, ~1 per 5 min |

### The one that takes patience

Room identification matures at **36 samples, one per 300 simulated seconds**. Simulation time
runs at **1× real time** in nominal operation, so this is **~3 hours of continuous uptime**
before the first room is identified. A cold demo will show `roomsIdentified: 0`. That is
expected, not a fault.

Convergence you can watch, all 735 zones:

```bash
curl -s localhost:8080/api/rooms/models | python3 -c "
import json,sys,statistics
rooms=json.load(sys.stdin)
s=[x['thermalSamples'] for x in rooms]
a=[x['thermalTheta'] for x in rooms if any(x['thermalTheta'])]
tc=sorted(1/t[0] for t in a if t[0]>1e-9)
print('samples median %d max %d | ready %d'%(statistics.median(s),max(s),sum(1 for x in rooms if x.get('thermalReady'))))
print('tau median %.1f h | in 1-40h %.0f%% | neg per-occupant %.0f%%'%(
  statistics.median(tc), 100*sum(1 for t in tc if 1<=t<=40)/len(tc),
  100*sum(1 for t in a if t[2]<0)/len(a)))
"
```

Healthy convergence looks like this — the same instance at two sample depths:

| Samples (median) | τ median | In 1–40 h | Negative per-occupant |
|---|---|---|---|
| 1 | 375 h | 7% | 88% |
| 3 | 1.1 h | 70% | 4% |

**If τ stays in the hundreds of hours as samples climb, suspect the building, not the
estimator.** That signature means envelope resistance is wrong — the identifier is correctly
measuring a building modelled as a thermos. See §8.

---

## 8. Rebuilding the building fixture

The bundled fixture is **generated, not authored**. Edit the generator or the library, never
`building-data.json`.

```bash
python3 tools/officeize_fixture.py            # dry run — prints the mix and calibration
python3 tools/officeize_fixture.py --write    # apply
python3 tools/officeize_fixture.py --restore  # back to raw digitizer output
```

**Test — read the dry-run output:**

- `in the 1-40 h band a real office shows: N/735` should be **≥ 90%**.
- The indicative EUI should print **`in cohort`** against 109.6 kWh/m²·yr.
- No programme should hold an implausible W/m² (a comms room is hundreds; an office is ~9).

After `--write`, **delete the stale model state** so identification is not carrying
coefficients fitted to the old physics:

```bash
rm -f server/data/room-dynamics.json && go run .
```

---

## 9. When something is wrong

| Symptom | Cause |
|---|---|
| Dashboard blank | Stale Vite cache — clear both `.vite` directories (§5) |
| Grid power in the megawatts | Raw digitizer fixture; run §8 |
| `roomsIdentified: 0` | Normal before ~3 h uptime |
| τ in the hundreds of hours | Envelope resistance wrong, not the estimator |
| `acReal: false` | Setpoints reach no machine; no saving is real |
| `tempReal: false` unexpectedly | Sensor absent or failing; zone is modelled |
| SHT30 dies on setpoint change | IR emitter on GPIO22 — it belongs on GPIO19 |
| CO₂ reads high then drifts low | ABC re-zeroing in a 24/7 space; build `-DCO2_ABC_OFF=1` |
| `plugW` off by a constant | Wrong `PLUG_CAL_A_PER_V` — 60.6 with a 33 Ω burden, 30.0 for an SCT-013-030 |
| Occupancy flapping between count and 0/1 | Camera and radar on the same zone (§6.3) |
| Node reboots when a relay clicks | Relay coils on 3V3 — they belong on VIN |
