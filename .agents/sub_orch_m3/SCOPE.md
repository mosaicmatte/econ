# Scope: Milestone 3 - Main System Integration, Strict Module Isolation & PlatformIO Compilation

## Objectives
1. Integrate camera person detection and dual-mode communication modules (`src/camera/`) into `edge/esp32/src/main.cpp`.
2. Replace legacy PIR motion sensor acquisition with `cameraDetector.getOccupancy()` / `cameraDetector.processFrame()`.
3. Ensure dual-mode communication transmits real-time tracking data over Wi-Fi (UDP broadcast + MQTT) when connected, and automatically falls back to USB Serial when disconnected.
4. STRICT ISOLATION REQUIREMENT: Do NOT modify any other sensor drivers, HVAC IR controls, energy monitoring (SCT-013), DHT/SHT30/ACD1200, or unrelated logic in `main.cpp`.
5. Update `platformio.ini` as needed (e.g. partition table `huge_app.csv`, build flags, include paths) so that PlatformIO compiles cleanly for target `[env:esp32dev]` and fits ESP32-WROOM Flash and RAM limits.
6. Verify compilation via PlatformIO and run all host/integration tests.
7. Verify with 2 Reviewers, 2 Challengers, 1 Forensic Auditor.

## Exclusively Owned Files
- `edge/esp32/src/main.cpp`
- `edge/esp32/platformio.ini`
- `edge/esp32/test/test_m3_integration.cpp` (and associated integration test files)

## Dependencies & Reference Artifacts
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md`
- `/Users/nguyenhoangkhoi/Documents/econ/PROJECT.md`
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/handoff.md`
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m2/handoff.md`

## Milestone Status
| Milestone | Status | Description | Key Outputs |
|-----------|--------|-------------|-------------|
| M3: Integration & Compilation | DONE | Camera & dual-mode comms integrated into main.cpp with strict isolation & PlatformIO config | `main.cpp`, `platformio.ini`, `test_m3_integration.cpp`, all test suites PASS (100%) |
