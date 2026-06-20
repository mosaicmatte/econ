# Edge Gateway: Raspberry Pi

The Pi is the facility's **local edge gateway**. It does two jobs:

1. **Hosts the Mosquitto MQTT broker** — the LAN bus every device talks on
   (ESP32 nodes, the YOLO CV node on the Mac M4, and the Go engine).
2. **Runs `gateway.py`** — an autonomous **failsafe rules engine**. The Go engine
   (`econ/server`) is the optimizing brain; this gateway only intervenes when the
   brain is unreachable, so a vacant zone is never left lit/cooling.

See `econ/BACKEND_ARCHITECTURE.md` for the full picture and the MQTT contract.

## Setup (on the Pi, or any Linux/Mac for testing)

```bash
# 1. Broker
sudo apt-get install -y mosquitto mosquitto-clients
sudo systemctl enable --now mosquitto         # listens on :1883

# 2. Gateway
cd econ/edge/raspberry_pi
pip install -r requirements.txt
python gateway.py                              # MQTT_BROKER defaults to 127.0.0.1
```

Point the other devices at the Pi's LAN IP:
- ESP32 firmware: `MQTT_HOST = "<pi-ip>"`.
- `yolo_tracker.py`: `MQTT_BROKER = "<pi-ip>"`.
- Go engine: `MQTT_BROKER=tcp://<pi-ip>:1883` (compose already sets `tcp://mqtt:1883`
  when the broker runs as the `mqtt` service alongside the engine).

## Tuning (env vars)
| var | default | meaning |
|---|---|---|
| `MQTT_BROKER` | `127.0.0.1` | broker host |
| `MQTT_PORT` | `1883` | broker port |
| `ENGINE_TIMEOUT_S` | `10` | no engine command for this long ⇒ brain considered offline |
| `FAILSAFE_DELAY_S` | `30` | a vacant zone must stay empty this long before a local cut |
| `SETBACK_C` | `28` | setpoint the failsafe parks an empty zone at |

## Verify the failsafe (no engine running)
```bash
# stop the Go engine so the brain is "offline", then:
mosquitto_sub -t 'econ/commands/#' -v &
mosquitto_pub -t econ/telemetry/zone_1 -m '{"zone":"Level 4","occupancy":0}'
# after FAILSAFE_DELAY_S the gateway emits:
#   econ/commands/zone_1  LIGHTS_OFF;SETPOINT=28.0;SRC=FAILSAFE
```

> Note: the old `raspberry_backend/server.py` was an MQTT↔WebSocket bridge for an
> earlier architecture (the dashboard now streams FlatBuffers straight from the Go
> engine). It has been replaced by this failsafe gateway.
