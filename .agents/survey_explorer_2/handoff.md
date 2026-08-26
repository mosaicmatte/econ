# Handoff Report: OV7670 & TFLite Micro Person Detection on ESP32-WROOM

**Agent:** Survey Spec Miner 2  
**Working Directory:** `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_2`  
**Target:** Parent Orchestrator / Implementers  
**Date:** 2026-08-26  

---

## 1. Observation

1. **Hardware & Existing Codebase State:**
   - Target Board: ESP32-WROOM-32 (Xtensa Dual-Core 32-bit LX6 @ 240 MHz, 4 MB Flash, ~320 KB internal DRAM, **No external PSRAM**).
   - Existing codebase in `/Users/nguyenhoangkhoi/Documents/econ/edge/esp32`:
     - `src/main.cpp`: Implements MQTT telemetry (`econ/telemetry/<zone>`), command parsing (`LIGHTS_ON`, `SETPOINT=`), sensor drivers (SHT30, DHT, ACD1200 CO2, SCT-013 CT clamp, IR HVAC emitter, capacitive touch).
     - Existing presence sensing relies on `USE_PIR` (GPIO 5), `USE_MMWAVE` (GPIO 18), or capacitive touch (GPIO 32).
     - `platformio.ini`: Configured with `[env:esp32dev]`, framework `arduino`, `PubSubClient`, `ArduinoJson`, `IRremoteESP8266`, `OneWire`, `DallasTemperature`.
     - Host unit tests (`test/run_host_tests.sh`) execute and pass cleanly (0 failures).

2. **Camera Requirements & Memory Footprint:**
   - OV7670 camera uses 8-bit parallel data (D0–D7), pixel clock (PCLK), vertical sync (VSYNC), horizontal reference (HREF), master clock (XCLK), and SCCB (I2C) control.
   - Operating in QQVGA (160×120) Grayscale mode yields **19,200 bytes (~18.75 KB)** per frame buffer.
   - Frame buffer is allocated in internal DRAM via ESP32 I2S parallel DMA engine (`MALLOC_CAP_DMA`).

3. **TFLite Micro Machine Learning Model:**
   - Visual Wake Words (VWW) int8 quantized MobileNet v1 ($\alpha = 0.25$) person detection model:
     - Input tensor: `[1, 96, 96, 1]` (96×96 single-channel grayscale `int8`).
     - Output tensor: `[1, 2]` (`not_person` logit, `person` logit).
     - Flash storage requirement: **~250–300 KB** in `.rodata`.
     - SRAM Tensor Arena requirement: **~80 KB** contiguous DRAM.

4. **Dual-Mode Communication (R2):**
   - Primary: Wi-Fi MQTT publishing to `econ/telemetry/<zone>`.
   - Fallback: Automatic USB Serial (`UART0` @ 115200 baud) stream when Wi-Fi or MQTT broker is disconnected.

---

## 2. Logic Chain

1. **Memory Feasibility on Non-PSRAM ESP32-WROOM:**
   - Total DRAM = 320 KB. Free heap after RTOS + WiFi + MQTT initialization is ~200–240 KB.
   - Allocating 19.2 KB for the QQVGA camera frame buffer + 80 KB for the TFLite Micro tensor arena consumes ~100 KB total.
   - This leaves **>100 KB of free DRAM headroom**, preventing heap starvation, stack overflows, or Wi-Fi packet drops.
   - Therefore, the system is fully viable on standard ESP32-WROOM without requiring external PSRAM.

2. **Flash Capacity & Partitioning:**
   - Standard 4 MB flash with default partition (`default.csv`) provides 1.25 MB for the application partition.
   - The compiled binary with Arduino core (~1 MB), TFLite Micro engine (~250 KB), model weights (~300 KB), and WiFi/MQTT (~150 KB) totals ~1.7 MB.
   - Switching `platformio.ini` partition to `huge_app.csv` (3.14 MB app space) eliminates flash overflow risks.

3. **Inference Latency & Framerate:**
   - Xtensa LX6 @ 240 MHz processes the 96×96 depthwise separable CNN in **~380–500 ms**.
   - This translates to **~2.0–2.6 FPS**, which is more than sufficient for real-time room occupancy tracking, HVAC/lighting automation, and feeding spatial topology/BIM models.

4. **Module Isolation (R1):**
   - Encapsulating all camera hardware initialization, DMA frame capture, downsampling, tensor arena allocation, and TFLite interpreter invocation inside a dedicated `CameraPersonDetector` class cleanly isolates the module.
   - Replacing the legacy PIR motion reading in `src/main.cpp` with `cameraDetector.getOccupancy()` ensures zero disruption to existing HVAC, lighting, and power-metering features.

---

## 3. Caveats

1. **Wokwi Simulator Camera Support:**
   - Wokwi does not currently have a cycle-accurate virtual OV7670 camera peripheral part.
   - The firmware implementation must feature graceful hardware detection: if `esp_camera_init()` returns an error or is unattached in simulation, the detector logs the status and provides a safe fallback (or test pattern) without causing an ESP32 kernel panic.
2. **GPIO Pin Budgeting:**
   - The OV7670 requires 13–14 GPIOs (8 data, 3 sync, 1 XCLK, 2 I2C).
   - If other optional analog sensors (e.g. dual CT clamps on GPIO 34/35) are simultaneously wired, pins must be mapped to input-only GPIOs (36, 39) and remaining digital GPIOs (13, 14, 15, 27, 32, 33).

---

## 4. Conclusion

- The OV7670 camera integration and TFLite Micro person detection model can be successfully implemented on ESP32-WROOM without external PSRAM.
- Internal DRAM usage is bounded at ~100 KB (19.2 KB DMA + 80 KB tensor arena), leaving >100 KB safety headroom.
- Dual-mode broadcasting over Wi-Fi MQTT with USB Serial fallback completely fulfills requirement R2.
- The comprehensive survey report is available at:  
  `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_2/survey_report.md`

---

## 5. Verification Method

1. **Host Configuration Tests:**
   - Run: `./test/run_host_tests.sh` from `/Users/nguyenhoangkhoi/Documents/econ/edge/esp32`.
   - Verify all test assertions pass (0 failures).
2. **Survey Report Inspection:**
   - View `/Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_2/survey_report.md`.
   - Verify complete coverage of camera DMA, XCLK clocking, SCCB I2C address, pinout tables, model architecture, SRAM/Flash budgets, inference pipeline, PlatformIO dependencies, and edge case tables.
