# Adversarial Review Report — Reviewer 1

> [!WARNING] **Skepticism Disclaimer**
> High confidence in software verification, pin multiplexing logic, and dual PIR state propagation on host simulation; bare-metal hardware execution on physical silicon with physical settling times remains unverified.

## 1. What the prior attempt got wrong / Weaknesses Identified

1. **GPIO18 Pin Collision when `USE_MMWAVE=1` is simultaneously enabled:**
   - **Input:** Compiling firmware with `-DUSE_PIR=1 -DUSE_MMWAVE=1` without manual pin overrides.
   - **Expected:** `PIR2_PIN` should automatically remap to a safe, available pin (e.g. GPIO17) to prevent shared contention on GPIO18 with `MMWAVE_PIN`.
   - **Actual in prior attempt:** `PIR2_PIN` unconditionally defaulted to GPIO 18, causing both PIR2 and mmWave to configure and read from the same physical pin unless explicitly overridden via build flags.
   - **Root Cause:** Missing `#elif USE_MMWAVE` fallback check in the preprocessor pin definition cascade in `edge/esp32/src/main.cpp`.

2. **Step Numbering Mismatch in Host Test Runner Logs:**
   - **Input:** Running `./test/run_host_tests.sh`.
   - **Expected:** Consistent step indexing `[1/6]` through `[6/6]`.
   - **Actual in prior attempt:** Steps were logged with mismatched totals `[1/3]`, `[2/3]`, `[3/3]`, `[4/5]`, `[5/5]`, `[6/6]`.
   - **Root Cause:** Incremental test additions without updating step total banners.

## 2. What I changed

- `edge/esp32/src/main.cpp`:
  - Added intelligent preprocessor fallback for `PIR2_PIN`: when `USE_MMWAVE=1`, `PIR2_PIN` defaults to GPIO 17 (TX2), completely eliminating pin collisions with `MMWAVE_PIN` on GPIO 18.
- `edge/esp32/test/run_host_tests.sh`:
  - Updated step numbering banner logs to cleanly reflect `[1/6]` through `[6/6]`.

## 3. Verification Record

- **Deep Verification (ran actual tests):**
  - `./test/run_all_e2e_tests.sh`: All 93/93 tests passed (100% pass rate).
  - `./test/run_host_tests.sh`: All 6 test suites passed with exit code 0.
  - Full suite batch verification across all 10 test files (`test_adversarial_challenger2_full.cpp`, `test_adversarial_m1.cpp`, `test_adversarial_m1_challenger2.cpp`, `test_adversarial_m2_ml.cpp`, `test_adversarial_m3_challenger1.cpp`, `test_adversarial_m3_challenger2.cpp`, `test_e2e_opaque_box.cpp`, `test_m1_dual_mode.cpp`, `test_m2_camera_ml.cpp`, `test_m3_integration.cpp`): 100% pass rate.
- **Shallow Verification (manual only):**
  - Verified GPIO allocation table across ESP32 DevKit v1 pinout to ensure zero collisions with actuators, I2C, touch presence, ADC current clamps, and status LEDs.
- **Unverified aspects:**
  - Physical execution on a physical ESP32 board with physical HC-SR501 PIR sensors.
  - Sensor power-on warm-up delay (30-60s) on physical hardware.

## 4. Known Issues
- `Minor Robustness Risk` — Physical PIR sensors take 30–60 seconds after power-on to calibrate; during this window, physical PIR sensors may report HIGH before settling.
- `Shallow Verification` — Host test environment relies on mock time and digital pin state shims rather than physical oscilloscope signal traces.

## 5. Remaining risk & next step
- The implementation fully addresses all requirements (R1, R2, R3) and meets all acceptance criteria. The codebase is ready for integration and handoff.
