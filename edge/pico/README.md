# Edge Node: Raspberry Pi Pico

| Component | Function |
| :--- | :--- |
| **RP2040 Internal Temp Sensor** | Provides real `tempReal` temperature reading |
| **BOOTSEL Button** | Toggles presence — press for ~half a second (sampled at 4 Hz, because each BOOTSEL read suspends flash + IRQs and hammering it can wedge the USB stack) |
| **GP16 → GND jumper** (optional) | Wired presence: occupied while the jumper is seated — immune to the BOOTSEL caveat |
| **Onboard LED** | Actuates zone lights |

An 8 s hardware watchdog is armed in both run modes: if anything ever freezes the
firmware (wedged USB, blocked read), the board reboots itself and resumes publishing.

A plain Pico has no radio, so it speaks JSON over USB serial and `bridge.py` gives it an MQTT presence; a Pico W with WiFi configured connects to the broker directly without the bridge.

## 1. Flash MicroPython
Hold the `BOOTSEL` button while plugging in the board over USB. An `RPI-RP2` drive will appear on your computer. Drag the official MicroPython `.uf2` file (download from [MicroPython for Pico](https://micropython.org/download/RPI_PICO/) or [Pico W](https://micropython.org/download/RPI_PICO_W/)) onto the drive. The board will automatically reboot.

## 2. Copy the firmware
Run `pip install mpremote` then `mpremote cp main.py :main.py` (or paste the code via Thonny). The node starts automatically on power-up from then on.

## 3. Start the stack + bridge (plain Pico)
Run `cd econ/server && docker compose up -d` (broker on :1883), then run `cd econ/edge/pico && pip install -r requirements.txt && python bridge.py`.

## 4. What you should see
- The engine log line `[edge] node "Pico Lab" bound to zone ...` appears.
- `GET http://localhost:8080/api/hardware` lists the node.
- The bound zone's dashboard temperature follows the chip's reading.

## Live demo script
- Press `BOOTSEL` -> the zone shows occupied and the engine commands `LIGHTS_ON` (LED lights up).
- Press again -> after the vacancy delay the engine sends `LIGHTS_OFF;SETPOINT=+4` setback (LED off).
- Warm the RP2040 chip with a fingertip -> the zone's temperature curve rises on the dashboard in seconds (sparkline persists it to TimescaleDB).

## Config
| Variable | Description | Location |
| :--- | :--- | :--- |
| `ZONE_TOPIC` | MQTT topic suffix | `main.py` |
| `ZONE_LABEL` | Human label sent as "zone" | `main.py` |
| `PUBLISH_S` | Telemetry period | `main.py` |
| `OCCUPIED_COUNT` | People reported while presence is toggled on | `main.py` |
| `TEMP_OFFSET_C` | Temperature calibration offset | `main.py` |
| `WIFI_SSID` | WiFi network name (Pico W only) | `main.py` |
| `WIFI_PASS` | WiFi network password | `main.py` |
| `MQTT_HOST` | Broker LAN IP (Pico W mode only) | `main.py` |
| `MQTT_PORT` | Broker port | `main.py` |
| `MQTT_BROKER` | Broker address (defaults to 127.0.0.1) | `bridge.py` environment variable |
| `PICO_PORT` | Serial port override | `bridge.py` environment variable |
| `BAUD` | Serial baud rate (defaults to 115200) | `bridge.py` environment variable |

## Pico W
No bridge is needed for a Pico W. Set `WIFI_SSID`, `WIFI_PASS`, and `MQTT_HOST` in `main.py`. Note that `umqtt.simple` ships in official Pico W builds; if it's missing, you can install it using `mpremote mip install umqtt.simple`.

## Development note (watchdog)
Once `main.py` is running, the watchdog cannot be disarmed — interrupting to a REPL
still reboots the board within 8 s. `mpremote cp main.py :main.py` completes in ~2 s,
so updates work normally; if a reboot bites mid-session, just rerun the command, or
hold BOOTSEL while plugging in and copy via the bootloader instead.
