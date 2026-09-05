# Implementer Handoff: Dual PIR Sensor Integration & Camera Deactivation

## Task Summary
Reverted active person detection on the ESP32 edge firmware from the OV7670 camera back to dual PIR motion sensors, while preserving all existing camera drivers, TFLite Micro ML pipeline files, and DualModeComm telemetry broadcasting architecture.

## Key Changes
1. **`edge/esp32/src/main.cpp`**:
   - Gated camera execution: set `USE_CAMERA=0` and `USE_PIR=1` as default configuration.
   - Defined `PIR1_PIN` (GPIO 5 / `PIR_PIN`) and `PIR2_PIN` (GPIO 18 / `PIR_PIN_2`).
   - Configured `DualModeComm` and `PersonTrackingData` to be unconditionally active.
   - In `setup()`: Configured both PIR pins as `INPUT`, initialized `DualModeComm`. Disabled camera/ML initialization in main loop.
   - In `loop()`: Polled both PIR sensors every 50ms, computing combined boolean state (`pir1 || pir2`). Dispatched immediate telemetry burst via `DualModeComm::transmit(trackData)` upon state transition.
   - In `readAndPublish()`: Wired dual PIR combined state to `occupancy`, `confidence`, and `person_count`, broadcasting via `DualModeComm`.
2. **`edge/esp32/platformio.ini`**:
   - Updated `build_flags` for `[env:esp32dev]` to `-DUSE_CAMERA=0 -DUSE_PIR=1`.
3. **`edge/esp32/test/arduino_shim.h`**:
   - Added simulated GPIO pin state map in `ArduinoShimInternal` to support off-target digital pin state reading (`setMockPinState`, `digitalWrite`, `digitalRead`, `clearMockPinStates`).
4. **`edge/esp32/test/test_e2e_opaque_box.cpp`**:
   - Added `econ::pir::DualPirSensor` module under `econ::pir`.
   - Updated Feature 7 tests to verify dual PIR boolean OR combinations, detection polling, telemetry transmission dispatch, non-blocking execution, and transition bursts.
   - Updated Tier 3 test 8 (`test_T3_08_Sensor_PIR_Fallback_When_Camera_Unavailable`) to verify dual PIR operation.
5. **`edge/esp32/test/test_m3_integration.cpp`**:
   - Added Test 1.6 to Suite 1 verifying dual PIR truth table, tracking payload generation, and DualModeComm UDP packet transmission.

## Verification Record
- Ran `./test/run_all_e2e_tests.sh`: 93/93 tests passed (100% pass rate).
- Ran `./test/run_host_tests.sh`: All 6 test suites passed with exit code 0.
