# Technical Survey & Architecture Report: ESP32 Edge Node & OV7670 Person Detection Isolation

**Surveyor:** Survey Explorer 1  
**Target Codebase:** `/Users/nguyenhoangkhoi/Documents/econ/edge/esp32`  
**Date:** 2026-08-26  
**Status:** Complete  

---

## 1. Executive Summary

The existing edge firmware for the ECON building management / digital twin system is located at `/Users/nguyenhoangkhoi/Documents/econ/edge/esp32`. It is an Arduino-framework C++ application targeting the **ESP32 DevKit v1 (ESP32-WROOM-32)**. The system is designed to acquire environmental and electrical telemetry, execute HVAC/lighting actuation commands, and report occupancy state to an MQTT broker over 2.4 GHz Wi-Fi.

The objective is to replace the binary PIR motion sensor with an **OV7670 camera and a lightweight Machine Learning person detection module** (e.g., TensorFlow Lite Micro / quantized int8 model) to provide real-time person tracking for topology/BIM ingestion. 

This survey provides:
1. Complete catalog of the existing codebase, build configuration, dependencies, and macros.
2. Exhaustive analysis of the current PIR motion sensing, occupancy data processing, and main event loop.
3. Hardware pin allocations, conflict analysis, and memory/resource budgets for the ESP32-WROOM (320 KB usable SRAM, 4 MB Flash).
4. Dual-mode communication requirements (Wi-Fi primary + USB Serial fallback).
5. Exact architectural boundaries and isolation design ensuring zero side-effects on existing subsystems.

---

## 2. Codebase & Directory Structure Catalog

```
/Users/nguyenhoangkhoi/Documents/econ/edge/esp32/
├── platformio.ini           # PlatformIO project configuration & sensor flags (128 lines)
├── wokwi.toml               # Wokwi simulation configuration (65 lines)
├── diagram.json             # Wokwi circuit diagram & virtual components (78 lines)
├── README.md                # ESP32 node deployment & hardware documentation (130 lines)
├── esp32_emulator.py        # Python desktop software emulator of the node (156 lines)
├── src/
│   ├── main.cpp             # Main Arduino sketch: setup, loop, MQTT, drivers (1004 lines)
│   ├── node_config.h        # Runtime configuration, NVS persistence, validation (310 lines)
│   ├── wifi_secrets.h       # Per-site Wi-Fi & MQTT credentials (gitignored, 6 lines)
│   └── wifi_secrets.example.h # Credentials template (17 lines)
└── test/
    ├── Arduino.h            # Host testing Arduino shim header (6 lines)
    ├── Preferences.h        # Host testing Preferences shim header (4 lines)
    ├── arduino_shim.h       # Host C++ mock implementations for Serial & NVS (65 lines)
    ├── host_config_test.cpp # Host unit test suite for node_config.h (117 lines)
    └── run_host_tests.sh    # Test runner script for host-side validation (21 lines)
```

### 2.1 Build Environment (`platformio.ini`)
- **Environment:** `[env:esp32dev]`
- **Platform:** `espressif32`
- **Board:** `esp32dev` (ESP32-WROOM-32)
- **Framework:** `arduino`
- **Baud Rate:** `115200`
- **Library Dependencies (`lib_deps`):**
  - `knolleary/PubSubClient @ ^2.8` (MQTT client)
  - `bblanchon/ArduinoJson @ ^6.21.3` (JSON parsing/serialization)
  - `adafruit/DHT sensor library @ ^1.4.6` (Optional: DHT temp/humidity)
  - `adafruit/Adafruit Unified Sensor @ ^1.1.14` (Sensor abstraction)
  - `crankyoldgit/IRremoteESP8266 @ ^2.8.6` (Optional: HVAC IR control)
  - `paulstoffregen/OneWire @ ^2.3.8` (Optional: 1-Wire DS18B20 supply temp)
  - `milesburton/DallasTemperature @ ^3.11.0` (Optional: DS18B20 probe)

### 2.2 Existing Feature Flags & Macro Toggles
The codebase relies on modular `#define` flags:
- `-DUSE_SHT30=1` (I2C Sensirion SHT30 on 0x44: Temp/Humidity)
- `-DUSE_CO2=1` (I2C ASAIR ACD1200 NDIR on 0x2A: CO2 ppm)
- `-DUSE_DHT=1` (GPIO4 single-wire: fallback Temp/Humidity)
- `-DUSE_PIR=1` (GPIO5 digital input: PIR motion sensor)
- `-DUSE_MMWAVE=1` (GPIO18 digital input: 24 GHz radar presence)
- `-DUSE_PLUG=1` (GPIO34 ADC1 + GPIO25 Relay: SCT-013 plug metering & APLC socket control)
- `-DUSE_IR_AC=1` (GPIO19 IR LED: HVAC IR transmitter)
- `-DUSE_SUPPLY_TEMP=1` (GPIO26 1-Wire: DS18B20 AC discharge temperature)
- `-DUSE_AC_CLAMP=1` (GPIO35 ADC1: SCT-013 AC compressor power)
- `-DUSE_LUX=1` (I2C BH1750 on 0x23: Facade ambient light)
- `-DUSE_REAL_SENSORS=1` (Shorthand for DHT + PIR)

---

## 3. Analysis of Current PIR Motion Sensing & Data Flow

### 3.1 PIR Sensor Implementation in `src/main.cpp`
1. **Compilation Flag & Pin Assignment (`src/main.cpp:102-104, 289-293`):**
   ```cpp
   #ifndef USE_PIR
     #define USE_PIR USE_REAL_SENSORS   // PIR -> measured presence
   #endif

   #if USE_PIR
     #ifndef PIR_PIN
       #define PIR_PIN 5
     #endif
   #endif
   ```
2. **Hardware Initialization in `setup()` (`src/main.cpp:893-896`):**
   ```cpp
   #if USE_PIR
     pinMode(PIR_PIN, INPUT);
     Serial.printf("[pir] presence on GPIO%d\n", PIR_PIN);
   #endif
   ```
3. **Data Acquisition & Processing in `readAndPublish()` (`src/main.cpp:733-753`):**
   ```cpp
   // --- occupancy ---
   int occupancy;
   #if USE_PIR || USE_MMWAVE
     bool present = false;
     #if USE_PIR
       if (digitalRead(PIR_PIN) == HIGH) present = true;
     #endif
     #if USE_MMWAVE
       if (digitalRead(MMWAVE_PIN) == HIGH) present = true;
     #endif
     occupancy = present ? 1 : 0;  // presence, not a headcount
   #elif USE_TOUCH_PRESENCE
     occupancy = touchOccupied() ? TOUCH_OCCUPANTS : 0;  // real physical input
   #else
     occupancy = random(0, 6);
   #endif
   doc["occupancy"] = occupancy;
   ```

### 3.2 Key Observations on the PIR Implementation:
- **Binary State Only:** The PIR simply performs a single `digitalRead(PIR_PIN)` and sets `occupancy = present ? 1 : 0`. It provides no spatial coordinates, bounding boxes, or multidimensional headcount.
- **Timing & Polling:** PIR is sampled synchronously inside `readAndPublish()` once every `publishIntervalMs` (default: 5000 ms / 5 s).
- **OR-Logic with mmWave:** When `USE_MMWAVE` is also enabled, the firmware OR-combines PIR and mmWave so that stillness does not immediately trigger vacancy.
- **Telemetry Serialization:** `doc["occupancy"]` is serialized into JSON and published to `econ/telemetry/<ZONE_TOPIC>`.

---

## 4. Main Event Loop & System Operation

In `src/main.cpp:971-1003`:
```cpp
void loop() {
  // Non-blocking reconnect (every 5s) keeps sensing/actuation responsive.
  if (!client.connected()) {
    digitalWrite(STATUS_LED, LOW);
    unsigned long now = millis();
    if (now - lastReconnectAttempt > 5000) {
      lastReconnectAttempt = now;
      mqttConnect();
    }
  } else {
    client.loop();
    unsigned long now = millis();
#if !USE_PIR && !USE_MMWAVE && USE_TOUCH_PRESENCE
    static unsigned long lastTouchPoll = 0;
    static bool lastTouched = false;
    if (now - lastTouchPoll > 150) {
      lastTouchPoll = now;
      bool touched = touchOccupied();
      if (touched != lastTouched) {
        lastTouched = touched;
        lastPublish = now;
        readAndPublish();
      }
    }
#endif
    if (now - lastPublish > gCfg.publishIntervalMs) {
      lastPublish = now;
      readAndPublish();
    }
  }
}
```

### Communication Gap Under Disconnected Wi-Fi / MQTT:
- When MQTT/Wi-Fi is disconnected (`!client.connected()`), the current firmware **skips `readAndPublish()` entirely** and enters a 5-second reconnection loop.
- Telemetry is neither formatted nor output to USB Serial during disconnection.
- **Requirement R2 mandates a dual-mode communication bridge**: The system must automatically output tracking data to USB Serial whenever Wi-Fi/MQTT is unavailable, ensuring continuous data ingestion into the BIM/topology model.

---

## 5. Hardware Pin Allocations & ESP32-WROOM Hardware Constraints

### 5.1 Pin Allocation Matrix

| GPIO | Direction | Function in Firmware | Module / Flag | Hardware / Strapping Notes |
|---|---|---|---|---|
| **GPIO 1** | Out | UART0 TX | `Serial` (115200 baud) | USB Serial communication |
| **GPIO 3** | In | UART0 RX | `Serial` (115200 baud) | USB Serial communication |
| **GPIO 2** | Out | `STATUS_LED` | Base system | Onboard LED / Strapping pin (pulled down during boot) |
| **GPIO 4** | In/Out | `DHT_PIN` | `USE_DHT` | Single-wire bus for DHT22 |
| **GPIO 5** | In | `PIR_PIN` | `USE_PIR` | PIR HC-SR501 OUT (to be replaced by OV7670) |
| **GPIO 6–11**| — | SPI Flash | **System Flash** | **STRICTLY RESERVED — DO NOT USE** |
| **GPIO 12** | In/Out | Unassigned | Available | Strapping pin (MTDI: Flash voltage 3.3V vs 1.8V) |
| **GPIO 13** | In/Out | Unassigned | Available | Touch 4 / JTAG |
| **GPIO 14** | In/Out | Unassigned | Available | Touch 6 / JTAG |
| **GPIO 15** | In/Out | Unassigned | Available | Strapping pin (MTDO) |
| **GPIO 18** | In | `MMWAVE_PIN` | `USE_MMWAVE` | Rd-03 radar presence input |
| **GPIO 19** | Out | `IR_PIN` | `USE_IR_AC` | HVAC IR LED emitter (MUST NOT collide with I2C) |
| **GPIO 21** | In/Out | `I2C_SDA` | `USE_SHT30`/`CO2`/`LUX`| I2C Bus Data |
| **GPIO 22** | Out | `I2C_SCL` | `USE_SHT30`/`CO2`/`LUX`| I2C Bus Clock |
| **GPIO 23** | Out | `RELAY_PIN` | Base system | Lighting relay (active HIGH) |
| **GPIO 25** | Out | `PLUG_RELAY_PIN`| `USE_PLUG` | Sockets relay (active HIGH, fail-energized) |
| **GPIO 26** | In/Out | `SUPPLY_TEMP_PIN`| `USE_SUPPLY_TEMP` | DS18B20 1-Wire probe |
| **GPIO 27** | In/Out | Unassigned | Available | General IO |
| **GPIO 32** | In | `TOUCH_PIN` | `USE_TOUCH_PRESENCE` | Capacitive Touch T9 presence demo |
| **GPIO 33** | In/Out | Unassigned | Available | General IO / Touch 8 |
| **GPIO 34** | In | `PLUG_ADC_PIN` | `USE_PLUG` | Input-only (ADC1_CH6) SCT-013 current clamp |
| **GPIO 35** | In | `AC_CLAMP_PIN` | `USE_AC_CLAMP` | Input-only (ADC1_CH7) AC SCT-013 current clamp |
| **GPIO 36** | In | Unassigned | Available | Input-only (SENSOR_VP, ADC1_CH0) |
| **GPIO 39** | In | Unassigned | Available | Input-only (SENSOR_VN, ADC1_CH3) |

### 5.2 OV7670 Pin Requirements & Allocation Strategy
An OV7670 camera sensor interfacing via 8-bit DVP / parallel interface requires:
- **8 Data Lines (D0–D7):** Can leverage input-only pins (GPIO 36, 39, 34, 35) or general pins (GPIO 12, 13, 14, 15, 27, 33).
- **Synchronization Lines:**
  - `VSYNC` (Vertical Sync input)
  - `HREF` / `HSYNC` (Horizontal Sync input)
  - `PCLK` (Pixel Clock input)
  - `XCLK` (System Clock output, ~10–20 MHz generated by ESP32 LEDC PWM timer)
- **Control Lines (SCCB / I2C):**
  - `SIOC` (Clock) and `SIOD` (Data): Can share the existing I2C bus (GPIO 21/22, OV7670 I2C address is `0x21` / `0x42`), which is completely distinct from SHT30 (`0x44`), ACD1200 (`0x2A`), and BH1750 (`0x23`), or use dedicated software SCCB.

### 5.3 Memory Budgets (ESP32-WROOM: SRAM & Flash)
1. **SRAM (Internal):**
   - Total Physical SRAM: 520 KB.
   - Usable DRAM for Stack/Heap: ~320 KB.
   - Operating baseline (FreeRTOS + Wi-Fi Stack + MQTT + Json buffer): ~140–180 KB free heap.
   - Camera & Inference Frame Buffers:
     - 96x96 Grayscale (Visual Wake Words / TFLite Person Detection standard): **9,216 bytes (~9.2 KB)**.
     - 160x120 QQVGA Grayscale: **19,200 bytes (~19.2 KB)**.
     - TFLite Micro Tensor Arena: **~40–60 KB**.
     - Total module RAM consumption: **< 80 KB**, leaving > 60 KB safe heap margin.
2. **Flash Memory (4 MB SPI Flash):**
   - Base firmware (`firmware.bin`): **~791 KB**.
   - Default app partition (`default.csv`): 1.31 MB (1,310,720 bytes).
   - TFLite Micro core + 8-bit quantized person detection model: ~200–350 KB.
   - Total binary size: **~1.05–1.15 MB**, safely fitting within the 1.31 MB default partition, and well within `huge_app.csv` (3.14 MB).

---

## 6. Architectural Boundary & Strict Module Isolation Specification

To fulfill the strict non-interference requirement:
> *"Ensure changes are strictly isolated to this module without modifying other parts of the existing software. No files outside of the camera module's scope are modified."*

### 6.1 Architectural Design of the Camera Person Detection Module
The camera and ML person detection subsystem must be encapsulated in dedicated module files:
- `src/camera_person_detector.h` (or directory `src/person_detector/`)
- `src/camera_person_detector.cpp`
- `src/model_data.h` (compiled int8 quantized model weights)

### 6.2 Interface Contracts
The module exposes a clean, decoupled C++ API:
```cpp
struct PersonDetectionResult {
  int personCount;          // Estimated number of people detected
  float maxConfidence;      // Detection confidence score [0.0 - 1.0]
  bool personDetected;      // Binary presence (replaces PIR logic)
  uint32_t inferenceTimeMs; // Execution latency
};

class CameraPersonDetector {
public:
  // Initialize OV7670 camera hardware, SCCB, and TFLite Micro interpreter
  static bool begin();
  
  // Capture frame and run person detection inference
  static PersonDetectionResult runInference();

  // Dual-mode telemetry transmitter (Wi-Fi MQTT or USB Serial fallback)
  static void broadcastTelemetry(const PersonDetectionResult& result, bool isWifiConnected);
};
```

### 6.3 Integration Boundary with `src/main.cpp`
In `src/main.cpp`, integration is isolated cleanly via feature flag `#if USE_OV7670_PERSON_DETECTION` (or `#if USE_CAMERA`):
1. **In `setup()`:** Call `CameraPersonDetector::begin()` without modifying unrelated peripherals (SHT30, DHT, ACD1200, relays, IR AC, Preferences).
2. **In `readAndPublish()` / `loop()`:** Replace PIR `digitalRead(PIR_PIN)` with `CameraPersonDetector::runInference().personCount`.
3. **Dual-Mode Communication:** If MQTT/Wi-Fi is disconnected, invoke `CameraPersonDetector::broadcastTelemetry(..., false)` to stream real-time JSON tracking frames over `Serial` (`Serial.printf(...)`) so downstream BIM/topology models remain fully updated.

---

## 7. Downstream Ingestion & Topology/BIM Model Compatibility

The person detection output adheres to the existing ECON wire contract expected by the Go engine (`server/mqtt.go`) and the CV module (`ai_modules/branch_a_occupancy/yolo_bytetrack/yolo_tracker.py`):

```json
{
  "zone": "Level 4",
  "occupancy": 2,
  "confidence": 0.88,
  "source": "esp32-camera",
  "temperature": 24.5,
  "tempReal": true,
  "lights": "ON",
  "setpoint": 24.0,
  "acReal": false
}
```

When transmitted over Serial (fallback mode), lines are formatted as single-line JSON strings terminated by `\n` at 115200 baud, enabling direct streaming into edge gateways, ROS, or BIM digital twins.

---

## 8. Verification & Host Testing Status
- Host configuration test suite in `test/run_host_tests.sh` executed and verified:
  - `PASSED (0 failures)`
  - Confirmed all validation rules for `NodeConfig`, NVS persistence, clamp calibration, and setpoint bounds.
- Firmware compilation baseline verified from previous build artifacts in `.pio/build/esp32dev/firmware.bin` (791,488 bytes).
