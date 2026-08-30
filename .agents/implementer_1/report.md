# Implementer Report: Dual PIR Motion Sensor Integration

## 1. Objective
Revert active person detection to use dual PIR motion sensors instead of the OV7670 camera, while retaining all ML/camera driver code in the codebase, and align the test suites.

## 2. Changes Summary
- `edge/esp32/src/main.cpp`:
  - Set default `USE_CAMERA=0` and `USE_PIR=1`.
  - Added pin defines `PIR1_PIN` (default GPIO 5) and `PIR2_PIN` (default GPIO 18).
  - Wired dual PIR sensing into `setup()`, `loop()`, and `readAndPublish()`.
  - Bound the combined boolean OR state (`pir1 || pir2`) to `PersonTrackingData` and `DualModeComm` engine.
  - Disabled camera capture and TFLite Micro inference in `loop()` so zero CPU cycles are consumed for image processing during runtime.
- `edge/esp32/platformio.ini`:
  - Adjusted build flags for ESP32 target environment (`-DUSE_CAMERA=0 -DUSE_PIR=1`).
- `edge/esp32/test/arduino_shim.h`:
  - Added simulated GPIO pin state dictionary for unit and E2E test verification of digital inputs.
- `edge/esp32/test/test_e2e_opaque_box.cpp`:
  - Integrated `DualPirSensor` testing suite into Feature 7 (truth table, state transitions, UDP broadcast dispatch, non-blocking timing).
- `edge/esp32/test/test_m3_integration.cpp`:
  - Added Test 1.6 to Suite 1 validating dual PIR truth table, tracking payload serialization, and DualModeComm UDP packet transmission.

## 3. Test Verification
- Ran `./test/run_all_e2e_tests.sh`: 93/93 tests passed (100% pass rate).
- Ran `./test/run_host_tests.sh`: All 6 host test suites passed with exit code 0.
