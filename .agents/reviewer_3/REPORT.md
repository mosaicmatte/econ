# Adversarial Review Report — Reviewer 3

> [!WARNING] **Skepticism Disclaimer**
> High confidence in software verification, dual PIR state propagation, non-blocking telemetry dispatch, and comprehensive host and E2E test suite execution; physical warm-up settling time on real PIR silicon remains an inherent hardware property.

## 1. What the prior attempt got wrong / Weaknesses Identified

1. **Host-Side Clock Truncation & Zero Timestamp Flakiness in `test_m2_camera_ml.cpp`:**
   - **Input:** Running `./test/run_host_tests.sh` (executing Step 6 on `test_m2_camera_ml.cpp`).
   - **Expected:** Telemetry generation assigns a strictly positive monotonic timestamp (`timestamp_ms > 0`), satisfying check `5.1.4 Telemetry timestamp_ms is valid (>0)`.
   - **Actual in prior attempt:** `getDetectorMillis()` in `person_detector.cpp` and `getHostMillis()` in `ov7670_driver.cpp` initialized `static auto start = std::chrono::steady_clock::now();`. When invoked within the first sub-millisecond of initialization, `duration_cast<std::chrono::milliseconds>(now - start).count()` evaluated to 0 ms. `data.timestamp_ms` was assigned 0, causing `check(data.timestamp_ms > 0)` to fail intermittently.
   - **Root Cause:** Zero-offset baseline for off-target host steady_clock duration calculation produced 0 ms on high-speed execution environments.

## 2. What I changed

- `edge/esp32/src/camera/person_detector.cpp`:
  - Updated `getDetectorMillis()` to initialize `start` with a 1,000 ms offset (`std::chrono::steady_clock::now() - std::chrono::milliseconds(1000)`), ensuring monotonic timestamps are strictly non-zero (> 0) from the very first frame.
- `edge/esp32/src/camera/ov7670_driver.cpp`:
  - Updated `getHostMillis()` to initialize `start` with a 1,000 ms offset, ensuring non-zero capture timestamps across all mock/host driver runs.
- `edge/esp32/test/arduino_shim.h`:
  - Updated `hostStartTime()` with a 1,000 ms offset to ensure `millis()` in unmocked host timing contexts returns non-zero positive timestamps.

## 3. Verification Record

- **Deep Verification (ran actual tests):**
  - `./test/run_all_e2e_tests.sh`: 93/93 tests passed across all 4 tiers (100% pass rate, exit code 0).
  - `./test/run_host_tests.sh`: All 10 host test suites across all 6 verification stages completed with exit code 0:
    1. `host_config_test.cpp` (Node configuration unit & safety tests) -> PASSED
    2. `test_m1_dual_mode.cpp` (M1 Dual-mode comms unit tests) -> PASSED
    3. `test_adversarial_m1.cpp` (M1 Challenger 1 stress tests) -> PASSED
    4. `test_adversarial_m1_challenger2.cpp` (M1 Challenger 2 adversarial suite) -> PASSED
    5. `test_m3_integration.cpp` (M3 Main system integration & dual PIR tests) -> PASSED
    6. `test_adversarial_m3_challenger1.cpp` (M3 Challenger 1 adversarial suite) -> PASSED
    7. `test_adversarial_m3_challenger2.cpp` (M3 Challenger 2 adversarial suite) -> PASSED
    8. `test_adversarial_challenger2_full.cpp` (Full adversarial stress & failover suite) -> PASSED
    9. `test_m2_camera_ml.cpp` (M2 Camera driver & TFLite ML pipeline tests: 79/79 passed) -> PASSED
    10. `test_adversarial_m2_ml.cpp` (M2 Adversarial stress & boundary tests: 89/89 passed) -> PASSED
- **Shallow Verification (manual only):**
  - Verified GPIO pin allocations on ESP32 DevKit v1: `PIR1_PIN` (GPIO 5) and `PIR2_PIN` (GPIO 18 / GPIO 17 fallback) avoid I2C bus pins (GPIO 21/22), relay actuators (GPIO 23/25), HVAC IR emitter (GPIO 19), capacitive touch pin (GPIO 32), and ADC current clamp (GPIO 34/35).
  - Verified compilation flags in `platformio.ini` (`-DUSE_CAMERA=0`, `-DUSE_PIR=1`).
- **Unverified aspects:**
  - Physical silicon testing with physical HC-SR501 / AM312 PIR sensors.
  - Physical PIR sensor power-on settling/warm-up delay (30-60s) on bare-metal hardware.

## 4. Known Issues
- `Minor Robustness Risk` — Physical PIR sensors require 30–60 seconds calibration settling time upon cold boot.
- `Shallow Verification` — Host test environment relies on mock time and digital pin state shims rather than physical oscilloscope signal traces.

## 5. Remaining risk & next step
- All requirements R1 (Dual PIR sensor integration and telemetry broadcast), R2 (Retain but disable camera/ML code in main loop), and R3 (Test suite alignment) are fully satisfied and verified across all 11 test targets and 100% pass rate. The project is ready for final delivery.
