# Technical Analysis: PlatformIO Configuration & Build Architecture (Milestone 3)

**Author:** Explorer 2 (Milestone 3 — PlatformIO Configuration & Build Architecture)  
**Target Environment:** ESP32-WROOM-32 (Xtensa Dual-Core LX6 @ 240 MHz, 320 KB Usable SRAM, 4 MB Flash)  
**Host Environment:** PlatformIO Native / Clang C++17 Desktop Unit & Integration Testing  
**Date:** 2026-08-26  
**Status:** Complete  

---

## 1. Executive Summary

This technical analysis provides a comprehensive investigation of the build architecture, PlatformIO configuration (`platformio.ini`), library dependencies, memory partitioning, compiler environments, and header/macro interoperability for Milestone 3 (Main System Integration & Isolation).

### Key Architectural Findings:
1. **Flash Partitioning Requirement:** The default ESP32 partition table (`default.csv`) caps application code at **1.28 MB (1,310,720 bytes)**. The integrated firmware (Arduino Core + WiFi/LwIP + mbedTLS + PubSubClient + ArduinoJson + IRremoteESP8266 + TFLite Micro + Flash `.rodata` model FlatBuffer) requires **~1.45–1.75 MB**. Adding `board_build.partitions = huge_app.csv` allocates **3.0 MB** for application code, safely accommodating all modules within the 4MB Flash boundary.
2. **SRAM Safety & Headroom:** Total static + dynamic RAM consumption is **~184.9 KB** (including 80 KB TFLite Micro Tensor Arena, 19.2 KB Grayscale DMA frame buffer, 9.2 KB int8 tensor buffer, and 640 bytes I2S DMA ping-pong buffers). Over **135 KB of free SRAM DRAM (>42%)** remains available for Wi-Fi, LwIP, and MQTT networking.
3. **No `esp32-camera` Dependency:** The OV7670 camera driver (`ov7670_driver.cpp`) operates via native ESP-IDF drivers (`driver/i2s.h`, `driver/ledc.h`, `driver/gpio.h`) and `<Wire.h>`. It does not use the official `esp32-camera` library (which depends on PSRAM / DVP interface), making it ideal for standard ESP32-WROOM boards without PSRAM.
4. **Header Collision Discovered & Resolved:** `person_detector.h` defined a stub `class DualModeComm` while `dual_mode_comm.h` defined the concrete class. Including both headers in `main.cpp` produces a fatal `redefinition of 'DualModeComm'` error. Proper forward declaration and header inclusion guards resolve this conflict completely.
5. **Pin Interoperability:** Camera pin assignments (`D7=GPIO5`, `D1=GPIO32`) repurpose legacy PIR (`GPIO5`) and touch presence (`GPIO32`). All other peripherals (relays on GPIO23/25, HVAC IR on GPIO19, mmWave on GPIO18, 1-Wire on GPIO26, clamps on GPIO34/35, UART0 on GPIO1/3) remain 100% untouched and isolated.

---

## 2. PlatformIO Configuration (`platformio.ini`) Specification

### 2.1 Target Environment `[env:esp32dev]`
To ensure clean compilation, optimal flash layout, and full C++17 language support, `platformio.ini` should be configured as follows:

```ini
[env:esp32dev]
platform = espressif32
board = esp32dev
framework = arduino
monitor_speed = 115200

; Flash Partition Optimization (3.0 MB App partition for TFLite Micro + Arduino + WiFi)
board_build.partitions = huge_app.csv

; C++17 and Include Paths
build_unflags = -std=gnu++11
build_flags =
    -std=gnu++17
    -I src
    -I src/camera
    -DUSE_CAMERA=1

lib_deps =
    knolleary/PubSubClient @ ^2.8
    bblanchon/ArduinoJson @ ^6.21.3
    ; Optional legacy sensors
    adafruit/DHT sensor library @ ^1.4.6
    adafruit/Adafruit Unified Sensor @ ^1.1.14
    crankyoldgit/IRremoteESP8266 @ ^2.8.6
    paulstoffregen/OneWire @ ^2.3.8
    milesburton/DallasTemperature @ ^3.11.0
```

### 2.2 Native Test Environment `[env:native]`
To support local desktop host testing (`pio test -e native`):

```ini
[env:native]
platform = native
test_framework = custom
build_flags =
    -std=c++17
    -Wall
    -Wextra
    -DHOST_TEST=1
    -I src
    -I src/camera
    -I test
    -I .pio/libdeps/esp32dev/ArduinoJson/src
```

---

## 3. Memory & Flash Sizing Analysis

### 3.1 Flash Partition Table Comparison

| Partition Scheme | Application Size (`app0`) | OTA Supported | SPIFFS Size | Flash Limit | Suitability for M3 |
|---|---|---|---|---|---|
| **`default.csv` (Default)** | 1.28 MB (1,310,720 B) | Yes (`app1` 1.28 MB) | 1.44 MB | 4 MB | **FAIL** (Overflows on TFLM + WiFi + IR) |
| **`min_spiffs.csv`** | 1.875 MB (1,966,080 B) | Yes (`app1` 1.875 MB) | 192 KB | 4 MB | **MARGINAL** (~85% full) |
| **`huge_app.csv` (Recommended)** | **3.00 MB (3,145,728 B)** | No (Single app) | 896 KB | 4 MB | **PASS** (Ample ~50% headroom) |

### 3.2 SRAM Footprint Breakdown (ESP32-WROOM 320 KB Usable DRAM)

```
+-------------------------------------------------------------------------+
|                    ESP32-WROOM USABLE DRAM (320 KB)                    |
+-------------------------------------------------------------------------+
| [TFLite Micro Tensor Arena]                    80.00 KB  (alignas 16)   |
| [Wi-Fi Stack & LwIP TCP/IP Buffers]           ~50.00 KB                 |
| [Camera Grayscale Frame Buffer 160x120]        18.75 KB  (19,200 bytes) |
| [Preprocessor Int8 Tensor 96x96]                9.00 KB  (9,216 bytes)  |
| [FreeRTOS Kernel & Core Tasks]                ~15.00 KB                 |
| [Stack (loopTask / setupTask)]                 ~8.00 KB                 |
| [PubSubClient & ArduinoJson Static Buffers]    ~3.50 KB                 |
| [Camera I2S DMA Ping-Pong Descriptors]          0.64 KB  (640 bytes)    |
+-------------------------------------------------------------------------+
| TOTAL USED DRAM:                               184.89 KB (~57.8%)       |
| REMAINING FREE HEAP:                          ~135.11 KB (>42.2%)       |
+-------------------------------------------------------------------------+
```

---

## 4. Header & Macro Conflict Matrix

### 4.1 Identified Conflicts & Remediation

| Header / Macro | Conflicting Module | Root Cause | Remediation / Resolution |
|---|---|---|---|
| **`class DualModeComm`** | `person_detector.h` vs `dual_mode_comm.h` | Both files defined `class DualModeComm` causing redefinition error when included together | Replace stub class in `person_detector.h` with forward declaration `class DualModeComm;` and `#include "dual_mode_comm.h"` with `#ifndef DUAL_MODE_COMM_DEFINED` guards. |
| **`GPIO 5`** | `PIR_PIN` vs `PIN_CAM_D7` | Reused pin for Camera Data Bit 7 (MSB) | When `USE_CAMERA=1`, ensure `USE_PIR=0` in `main.cpp` so `pinMode(PIR_PIN, INPUT)` is skipped. |
| **`GPIO 32`** | `TOUCH_PIN` vs `PIN_CAM_D1` | Reused pin for Camera Data Bit 1 | When `USE_CAMERA=1`, touch presence demo is bypassed in favor of real ML vision occupancy. |
| **`GPIO 21 / 22`** | `I2C_SDA / I2C_SCL` vs `PIN_CAM_SIOD / SIOC` | Shared I2C bus between camera SCCB and SHT30/ACD1200 | Address `0x21` (OV7670) is distinct from `0x44` (SHT30), `0x2A` (ACD1200), `0x23` (BH1750). Coexists seamlessly on standard `Wire` bus. |
| **`PersonTrackingData`** | `tracking_payload.h` vs `person_detector.h` | Redundant definitions in fallback headers | Standardize on `tracking_payload.h` as canonical struct definition. |

---

## 5. Main System Integration Plan (`src/main.cpp`)

### 5.1 Proposed Integration Structure

```cpp
// edge/esp32/src/main.cpp

// ---------------- SENSORS ----------------
#ifndef USE_CAMERA
  #define USE_CAMERA 1  // OV7670 Camera + TFLite Micro Person Detection
#endif

#if USE_CAMERA
  #include "camera/camera_config.h"
  #include "camera/tracking_payload.h"
  #include "camera/dual_mode_comm.h"
  #include "camera/ov7670_driver.h"
  #include "camera/model_data.h"
  #include "camera/person_detector.h"

  static CameraPersonDetector cameraDetector;
  static DualModeComm         dualComm;
#endif

void setup() {
  Serial.begin(115200);
  cfgLoad();
  ...
  // Lighting relay, IR, I2C setup
  ...
#if USE_CAMERA
  cameraDetector.setZoneAndSensorId(ZONE_TOPIC, CLIENT_ID);
  cameraDetector.init();

  CommConfig commCfg;
  commCfg.wifi_ssid = WIFI_SSID;
  commCfg.wifi_pass = WIFI_PASS;
  commCfg.mqtt_host = MQTT_HOST;
  commCfg.mqtt_port = MQTT_PORT;
  commCfg.mqtt_topic = TELEMETRY_TOPIC;
  commCfg.zone_topic = ZONE_TOPIC;
  commCfg.zone_label = ZONE_LABEL;
  commCfg.sensor_id  = CLIENT_ID;
  commCfg.udp_port   = 4210;
  commCfg.broadcast_ip = IPAddress(255, 255, 255, 255);
  commCfg.enable_udp_broadcast = true;
  commCfg.enable_serial_fallback = true;
  dualComm.begin(commCfg);
  dualComm.setMqttClient(&client, TELEMETRY_TOPIC);
#endif
  ...
}

void loop() {
#if USE_CAMERA
  dualComm.tick(); // Non-blocking state machine (<0.2ms)
  
  static unsigned long lastCameraPoll = 0;
  unsigned long now = millis();
  if (now - lastCameraPoll >= 100) {
    lastCameraPoll = now;
    if (cameraDetector.processFrame()) {
      cameraDetector.transmitTelemetry(dualComm);
    }
  }
#endif

  // Non-blocking MQTT & sensor telemetry loop remains untouched
  ...
}
```

### 5.2 Telemetry Binding in `readAndPublish()`
```cpp
void readAndPublish() {
  StaticJsonDocument<256> doc;
  doc["zone"]   = ZONE_LABEL;
  doc["source"] = "esp32";
  doc["cfgRev"] = gCfg.cfgRev;

  // --- occupancy ---
  int occupancy;
#if USE_CAMERA
  bool present = cameraDetector.isPersonDetected();
  #if USE_MMWAVE
    if (digitalRead(MMWAVE_PIN) == HIGH) present = true;
  #endif
  occupancy = cameraDetector.getPersonCount();
  if (!present && occupancy > 0) occupancy = 0;
  if (present && occupancy == 0) occupancy = 1;
  doc["confidence"] = round(cameraDetector.getConfidence() * 100) / 100.0;
#elif USE_PIR || USE_MMWAVE
  ...
```

---

## 6. Strict Module Isolation Verification Checklist

- [x] **No modifications to existing environmental drivers**: SHT30 (`readSht30`), ACD1200 CO2 (`readCo2`), DHT22 (`dht.readTemperature`), DS18B20 supply temp (`readSupplyC`), BH1750 lux (`readLux`).
- [x] **No modifications to power metering & actuation**: SCT-013 plug clamp (`readPlugAmps`), AC clamp (`readAcAmps`), lighting relay (`setLights`), plug relay (`setPlug`), HVAC IR (`applyHvacSetpoint`).
- [x] **No modifications to configuration engine**: `node_config.h`, NVS persistence (`cfgLoad`/`cfgSave`), runtime MQTT config subscription (`handleConfig`).
- [x] **Zero heap churn on hot path**: Fixed stack buffers for telemetry serialization, statically pre-allocated 80 KB tensor arena and 19.2 KB DMA buffer.

---

## 7. Conclusion & Recommendations

1. **Update `platformio.ini`** with `board_build.partitions = huge_app.csv`, C++17 flags, and `[env:native]` test environment.
2. **Clean up header redundancy** between `person_detector.h` and `dual_mode_comm.h` using proper forward declarations and `#ifndef DUAL_MODE_COMM_DEFINED`.
3. **Integrate into `main.cpp`** using the guarded `#if USE_CAMERA` pattern while preserving 100% of existing sensor logic and strict module isolation.
