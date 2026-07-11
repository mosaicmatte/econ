# Edge Node: ESP32

PlatformIO/C++ firmware that turns an ESP32 devkit into a live sensor/actuator node for
one building zone:

| Direction | What | How |
|---|---|---|
| senses | presence | capacitive **touch pin GPIO32** on a bare board (zero wiring), or a PIR on GPIO5 with `USE_REAL_SENSORS=1` |
| senses | temperature / humidity | DHT22 on GPIO4 (`USE_REAL_SENSORS=1`); simulated otherwise — and flagged `tempReal:false` so the engine never pins zone physics to a fake |
| actuates | zone lights | relay on GPIO23 (onboard LED on GPIO2 shows MQTT link) |
| actuates | HVAC setpoint | IR emitter stub on GPIO22 (extension point for IRremoteESP8266) |

Wire contract: publishes JSON to `econ/telemetry/<ZONE_TOPIC>` every 5 s (instantly on
a presence change), executes `LIGHTS_ON|OFF;SETPOINT=<°C>` from
`econ/commands/<ZONE_TOPIC>`, and keeps a retained online/offline flag on
`econ/status/<ZONE_TOPIC>` via MQTT Last Will.

## 1. Configure

Edit the CONFIG block at the top of `src/main.cpp`:

```cpp
const char* WIFI_SSID = "...";        // 2.4 GHz network (ESP32 has no 5 GHz)
const char* WIFI_PASS = "...";
const char* MQTT_HOST = "192.168.x.x"; // LAN IP of the machine running docker compose
const char* ZONE_TOPIC = "zone_1";     // one unique suffix per board
```

`MQTT_HOST` is whatever machine hosts the Mosquitto broker — with the default stack
that is the computer running `docker compose up` in `econ/server` (find its IP with
`ipconfig getifaddr en0` on macOS). The compose file already exposes port 1883 to the
LAN and the broker allows anonymous clients.

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

Set `USE_REAL_SENSORS 1` in `src/main.cpp` (or per-board via
`build_flags = -DUSE_REAL_SENSORS=1` in `platformio.ini`), wire DHT22 → GPIO4 and
PIR → GPIO5. Telemetry then carries `tempReal:true` and the engine pins the zone's
temperature to the physical reading — warm the sensor with your hand and the
dashboard (and TimescaleDB history) follows within seconds. No hardware at all? Run
`python esp32_emulator.py` to watch the command side of the contract.
