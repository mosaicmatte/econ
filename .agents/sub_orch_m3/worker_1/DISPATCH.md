## 2026-08-26T04:25:15Z

You are Worker 1 for Milestone 3: Main System Integration, Strict Module Isolation & PlatformIO Compilation.
Your working directory is /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/worker_1.
Your parent conversation ID is 25b89dd0-edb1-4020-a99b-5de00d21e502.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

MANDATORY FIRST STEP:
Read the following files before taking any action:
- /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md
- /Users/nguyenhoangkhoi/Documents/econ/PROJECT.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/SCOPE.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_1/handoff.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_1/analysis.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_2/handoff.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_2/analysis.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_3/handoff.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_3/analysis.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_3/proposed_test_m3_integration.cpp

EXCLUSIVELY OWNED FILES:
- `edge/esp32/src/main.cpp`
- `edge/esp32/platformio.ini`
- `edge/esp32/src/camera/person_detector.h` (for fixing DualModeComm header collision if needed)
- `edge/esp32/test/test_m3_integration.cpp`
- `edge/esp32/test/run_host_tests.sh`

OBJECTIVES & TASKS:
1. **Header & Module Conflict Resolution**:
   - Ensure `edge/esp32/src/camera/person_detector.h` and `edge/esp32/src/camera/dual_mode_comm.h` can be included simultaneously without `DualModeComm` redefinition collisions.
2. **Main System Integration (`edge/esp32/src/main.cpp`)**:
   - Include camera and dual-mode communication headers.
   - Instantiate `CameraPersonDetector` and `DualModeComm`.
   - In `setup()`: Initialize camera detector (`cameraDetector.init()`) and dual-mode comms.
   - In `loop()`: Service camera frame acquisition / inference (`cameraDetector.processFrame()`) and non-blocking comms state machine (`dualComm.tick()`).
   - In `readAndPublish()`: Replace legacy PIR sensor motion detection (`digitalRead(PIR_PIN)`) with `cameraDetector.isPersonDetected()` and `cameraDetector.getPersonCount()` / occupancy.
   - Ensure telemetry data transmits over Wi-Fi (UDP :4210 + MQTT) when connected, and automatically falls back to USB Serial when offline.
   - **STRICT ISOLATION**: Do NOT alter, modify, or break any legacy sensor drivers, I2C polling (SHT30, ACD1200, BH1750), 1-Wire DS18B20, SCT-013 CT current sampling, HVAC IR control, relay controls, or NVS configuration logic. All must remain 100% functional and isolated.
3. **PlatformIO Configuration (`edge/esp32/platformio.ini`)**:
   - Add `board_build.partitions = huge_app.csv` (or partition table that fits ESP32-WROOM 4MB Flash with ~3MB app space for TFLM + WiFi + MQTT).
   - Add `-std=gnu++17` and `-I src/camera` to build flags.
   - Ensure `pio run -e esp32dev` builds cleanly.
4. **Integration Test Suite (`edge/esp32/test/test_m3_integration.cpp` & `run_host_tests.sh`)**:
   - Integrate the full test suite from Explorer 3 (`proposed_test_m3_integration.cpp`) into `edge/esp32/test/test_m3_integration.cpp`.
   - Update `edge/esp32/test/run_host_tests.sh` to run M1, M2, and M3 integration tests.
5. **Compilation & Verification**:
   - Run `pio run -e esp32dev` (using platformio) and verify clean compilation with 0 errors.
   - Run `./edge/esp32/test/run_host_tests.sh` and verify all tests pass (M1, M2, M3).
   - Verify Flash and SRAM memory footprints fit ESP32-WROOM limits.
6. **Deliverables**:
   - Document all changes in `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/worker_1/changes.md`.
   - Deliver handoff in `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/worker_1/handoff.md`.
   - Send completion message to parent when done.
