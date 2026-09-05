# Handoff Report: Survey Explorer 1 (ESP32 Codebase & Module Isolation)

## 1. Observation
- **Codebase Root & Structure:** Target edge node is in `/Users/nguyenhoangkhoi/Documents/econ/edge/esp32`. Contains `platformio.ini` (128 lines), `wokwi.toml` (65 lines), `diagram.json` (78 lines), `README.md` (130 lines), `esp32_emulator.py` (156 lines), `src/main.cpp` (1004 lines), `src/node_config.h` (310 lines), `src/wifi_secrets.h` (6 lines), and `test/` directory.
- **PlatformIO Configuration (`platformio.ini:1-16`):** Target environment `[env:esp32dev]`, `platform = espressif32`, `board = esp32dev`, `framework = arduino`, `monitor_speed = 115200`. Dependencies: `knolleary/PubSubClient @ ^2.8`, `bblanchon/ArduinoJson @ ^6.21.3`, `adafruit/DHT sensor library @ ^1.4.6`, `adafruit/Adafruit Unified Sensor @ ^1.1.14`, `crankyoldgit/IRremoteESP8266 @ ^2.8.6`, `paulstoffregen/OneWire @ ^2.3.8`, `milesburton/DallasTemperature @ ^3.11.0`.
- **Existing PIR Sensor Implementation (`src/main.cpp`):**
  - Definition (`lines 102-104, 289-293`): `#define PIR_PIN 5` gated by `#if USE_PIR`.
  - Setup (`lines 893-896`): `pinMode(PIR_PIN, INPUT);`.
  - Acquisition (`lines 733-753`): `if (digitalRead(PIR_PIN) == HIGH) present = true; occupancy = present ? 1 : 0; doc["occupancy"] = occupancy;`.
- **Communication & Event Loop (`src/main.cpp:971-1003`):**
  - When connected to MQTT, publishes every `gCfg.publishIntervalMs` (5000 ms) via `readAndPublish()` (`lines 691-811`).
  - When disconnected (`!client.connected()`), calls `mqttConnect()` every 5 seconds. In this state, `readAndPublish()` is NOT called, meaning zero telemetry is transmitted via USB Serial or Wi-Fi during disconnection.
- **Hardware Constraints & Flash/RAM Budgets:**
  - Board target is ESP32-WROOM-32 (320 KB usable SRAM DRAM, 4 MB SPI Flash, no external PSRAM).
  - Existing compiled binary (`.pio/build/esp32dev/firmware.bin`): 791,488 bytes (~773 KB). Default partition app capacity is 1,310,720 bytes (1.31 MB).
  - 96x96 grayscale frame buffer requires 9,216 bytes (9.2 KB). Quantized int8 TFLite Micro person detection tensor arena requires ~40-60 KB.
- **Host Testing Execution:** Executed `./test/run_host_tests.sh`, which ran `host_config_test.cpp` and returned `PASSED (0 failures)`.

## 2. Logic Chain
1. *From PIR Observation:* The current PIR implementation is a simple digital pin read returning binary presence (0 or 1) every 5 seconds. Replacing it with an OV7670 camera and ML person detection requires replacing the occupancy source with an ML inference result that produces headcount, confidence score, and detection status.
2. *From Communication Loop Observation:* Requirement R2 specifies dual-mode communication (Wi-Fi primary, USB Serial fallback). Since the current `loop()` suppresses publishing when `!client.connected()`, the new camera person detector module must provide a communication dispatcher that outputs telemetry over USB Serial (`Serial.printf`) whenever Wi-Fi/MQTT is unavailable.
3. *From Memory/Resource Observation:* Standard ESP32-WROOM-32 has no PSRAM. A full VGA RGB565 frame (640x480x2 = 614.4 KB) cannot fit in internal SRAM. However, a 96x96 or 160x120 grayscale frame (9.2 KB to 19.2 KB) and a TFLite Micro tensor arena (~50 KB) fit comfortably within the 140-180 KB free heap. Total firmware size with an int8 quantized model (~250-350 KB) will be ~1.05-1.15 MB, well within the 1.31 MB app partition.
4. *From Isolation Requirement:* To ensure changes do not modify unrelated subsystems (SHT30, ACD1200, DHT, IR AC, plug relays, `node_config.h`), the camera driver, ML inference engine, and dual-mode serializer must be encapsulated within dedicated module files (`src/camera_person_detector.h`, `src/camera_person_detector.cpp`, `src/model_data.h`) and conditionally linked via a compile-time feature flag (e.g. `-DUSE_OV7670_PERSON_DETECTION=1`).

## 3. Caveats
- Standard ESP32 devkit pin header assignments: GPIO 6-11 are hard-wired to internal SPI flash and must never be assigned to camera DVP lines.
- The build environment requires PlatformIO CLI (`pio`) or PlatformIO Core inside VS Code/Python virtualenv. Host-side tests were verified using the native host C++ compiler (`clang++`/`g++`).
- OV7670 camera configuration registers are transmitted via SCCB/I2C (address `0x21`). The bus can safely share the existing I2C pins (GPIO 21/22) because addresses do not collide with SHT30 (`0x44`), ACD1200 (`0x2A`), or BH1750 (`0x23`).

## 4. Conclusion
The existing codebase is clean, modular, and well-structured for sensor addition. Replacing the PIR sensor with the OV7670 person detection module can be achieved with strict isolation by encapsulating the camera driver, TFLite Micro inference pipeline, and dual-mode communication bridge in a standalone module. The memory and flash footprints on the ESP32-WROOM are well within physical safety limits when using 96x96 grayscale frames and quantized int8 weights.

## 5. Verification Method
1. **Host Configuration Tests:**
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/edge/esp32
   ./test/run_host_tests.sh
   ```
   *Expected result:* `PASSED (0 failures)`.
2. **Firmware Compilation Verification:**
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/edge/esp32
   pio run -e esp32dev
   ```
   *Expected result:* Build completes with `SUCCESS`, firmware size < 1.31 MB (or within partition ceiling).
3. **Module Isolation Inspection:**
   Inspect `/Users/nguyenhoangkhoi/Documents/econ/edge/esp32/src/` to confirm that files outside the camera module scope remain unpolluted and untouched.
