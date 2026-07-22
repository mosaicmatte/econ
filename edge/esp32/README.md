# Edge Node: ESP32

PlatformIO/C++ firmware that turns an ESP32 devkit into a live sensor/actuator node for
one building zone. Every capability is a `-D` build flag (off by default), so a bare board
demos with zero wiring and a real deployment enables exactly the sensors it has:

| Direction | What | How | Flag |
|---|---|---|---|
| senses | presence | capacitive **touch pin GPIO32** on a bare board (zero wiring) | *(default)* |
| senses | presence (motion) | PIR on **GPIO5** | `-DUSE_PIR=1` |
| senses | presence (incl. stationary) | HLK-LD2410C 24 GHz radar on **GPIO18** — holds occupancy for still people at a desk | `-DUSE_MMWAVE=1` |
| senses | temperature / humidity | SHT30 on I²C (**SDA 21 / SCL 22**, addr 0x44) | `-DUSE_SHT30=1` |
| senses | temperature / humidity | DHT22 on **GPIO4** (fallback if no SHT30) | `-DUSE_DHT=1` |
| senses | CO₂ | ASAIR ACD1200 NDIR on the same I²C bus (addr 0x2A) | `-DUSE_CO2=1` |
| senses | plug-circuit power | SCT-013 current clamp on **GPIO34** (ADC1, input-only) — true-RMS watts | `-DUSE_PLUG=1` |
| actuates | zone lights | relay on **GPIO23** (active HIGH; onboard LED on GPIO2 shows MQTT link) | *(default)* |
| actuates | plug sockets (APLC sweep) | second relay on **GPIO25** (active HIGH, fail-**energized** on boot) | `-DUSE_PLUG=1` |
| actuates | HVAC setpoint | IR emitter on **GPIO19** (extension point for IRremoteESP8266) | *(default)* |

Anything not compiled in is simulated and flagged honestly — a modelled temperature is sent
as `tempReal:false` so the engine never pins zone physics to a fake, and a sensor that fails
its read simply stops publishing that field (zero always means "not measuring", never
"measured zero").

Wire contract: publishes JSON to `econ/telemetry/<ZONE_TOPIC>` every 5 s (instantly on a
presence change), carrying whichever of `occupancy`, `temp`/`tempReal`, `humidity`, `co2`,
`plugW` are live; executes `LIGHTS_ON|OFF;SETPOINT=<°C>` and (with the plug node)
`PLUG_ON|PLUG_OFF` from `econ/commands/<ZONE_TOPIC>`; and keeps a retained online/offline
flag on `econ/status/<ZONE_TOPIC>` via MQTT Last Will.

## 1. Configure

Credentials live in a gitignored header so they never reach the repo:

```bash
cd econ/edge/esp32
cp src/wifi_secrets.example.h src/wifi_secrets.h   # then fill in the three values
```

`WIFI_SSID`/`WIFI_PASS` must be a **2.4 GHz** network (the ESP32 has no 5 GHz).
`MQTT_HOST` is whatever machine hosts the Mosquitto broker — with the default stack
that is the computer running `docker compose up` in `econ/server` (find its IP with
`ipconfig getifaddr en0` on macOS). The compose file already exposes port 1883 to the
LAN and the broker allows anonymous clients. Per-board identity (`ZONE_TOPIC`,
`ZONE_LABEL`) stays at the top of `src/main.cpp` — give each board a unique suffix.

## 2. Flash

```bash
pip install platformio          # or use the PlatformIO VS Code extension
cd econ/edge/esp32
pio run -t upload               # auto-detects the board's serial port
pio device monitor              # watch it join WiFi + MQTT (115200 baud)
```

## 3. See it in the twin

With the stack up (`cd econ/server && docker compose up -d` and the dashboard running):

- The engine log prints `[edge] node "Level 4" bound to zone zone-office-…` on the
  first message — each physical board gets its own office zone automatically.
- `curl http://localhost:8080/api/hardware` lists the node, its readings, and whether
  its temperature is currently pinning the zone.
- **Touch demo (bare board):** pinch a jumper wire seated in GPIO32 → the bound zone
  shows 3 occupants within ~0.2 s and the engine commands `LIGHTS_ON`. Let go → after
  the vacancy delay the engine sends `LIGHTS_OFF;SETPOINT=+4 °C` setback and the relay
  pin drops. Every step is visible in `pio device monitor`.
- Manual override from the dashboard (Profiler → zone actions) publishes straight to
  this board and latches the optimizer out for 15 minutes.

Debug the bus directly: `mosquitto_sub -t 'econ/#' -v`.

## Real sensors

Enable each sensor with its `-D` flag — individually, or several at once — in
`platformio.ini` `build_flags` (per board) or via `PLATFORMIO_BUILD_FLAGS`. Telemetry then
carries the real fields (`tempReal:true` for measured temperature) and the engine pins the
zone to the physical readings — warm an SHT30 with your hand and the dashboard (and
TimescaleDB history) follows within seconds.

```ini
; platformio.ini — a fully-instrumented office node
build_flags =
  -DZONE_TOPIC_OVERRIDE=\"zone_2\"
  -DUSE_SHT30=1     ; temperature + humidity (I2C SDA21/SCL22, addr 0x44)
  -DUSE_CO2=1       ; ASAIR ACD1200 NDIR on the same bus (addr 0x2A)
  -DUSE_MMWAVE=1    ; LD2410C radar on GPIO18 — keeps still occupants "present"
  -DUSE_PLUG=1      ; SCT-013 clamp on GPIO34 + plug relay on GPIO25
```

Notes per sensor:

- **CO₂ (`USE_CO2`)** shares the SHT30's I²C bus. For a 24/7 space that never empties, add
  `-DCO2_ABC_OFF=1` to disable the ACD1200's automatic baseline calibration (which assumes
  the room reaches ~400 ppm outdoor air periodically — false for a server room, so ABC
  would slowly mis-zero it).
- **Plug metering (`USE_PLUG`)** reads a current-output SCT-013-000 through a 33 Ω burden +
  1.65 V bias into GPIO34 (`-DPLUG_CAL_A_PER_V=60.6`); for the 1 V voltage-output
  SCT-013-030 use `30.0` and skip the burden. `-DPLUG_MAINS_V=230` for Vietnam. The plug
  relay boots **closed** (fail-energized) so a crashed node never dark-kills a live socket,
  and the engine's after-hours sweep opens it only on verified vacancy.
- **mmWave (`USE_MMWAVE`)** is worth it in offices: PIR drops people who sit still, radar
  holds them, so the optimizer doesn't set back an occupied-but-quiet room.

No hardware at all? Run `python esp32_emulator.py` to watch the command side of the contract.
`USE_REAL_SENSORS=1` remains supported as shorthand for "DHT + PIR" on older build configs.
