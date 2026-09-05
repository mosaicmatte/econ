# SWE Light Adversarial Review Round 1 — Progress

## 1. Independent Task Understanding & Requirements Audit
- **R1 (Sampling & Mathematics)**: `readStripAmps()` in `edge/esp32/src/main.cpp` must sample the ACS712 current sensor across integer AC cycles (50 Hz / 60 Hz), dynamically remove DC quiescent bias (whether 2.5V raw or 1.65V divided), and compute True-RMS AC variance without task starvation or window truncation.
- **R2 (Noise Floor & Calibration)**: Analyze noise floor cutoff and sensitivity scaling across ACS712 models (5A, 20A, 30A). Prevent ghost readings at 0A from ESP32 SAR ADC thermal/quantization noise (sigma ~ 6-10 counts) while eliminating false zeroing of sub-0.10A real loads on high-sensitivity sensors.
- **R3 (Harness & Verification)**: Implement fixes and reproducible host test suites verifying 0A suppression and <5% accuracy on known waveforms with noise. Ensure clean compilation with PlatformIO.

## 2. Adversarial Findings in Prior Attempt
1. **Comment Discrepancy in Firmware (`edge/esp32/src/main.cpp`)**:
   - Firmware header comment stated `10.5 counts RMS (~8.5 mV)` while the code implemented `const double STRIP_NOISE_FLOOR_COUNTS = 12.0;`. Empirical simulation over 10,000 runs proved that at $\sigma = 10.0$ counts noise, a 10.5 count threshold leaks 1.31% ghost readings, whereas 12.0 counts achieves 0.00% leakage. Updated firmware comments to match code (12.0 counts RMS, ~9.7 mV).
2. **Outdated Noise Threshold Descriptions in C++ Host Test (`edge/esp32/test/host_strip_power_test.cpp`)**:
   - Test Group 3 title and assertion messages still referred to `< 0.10A` rather than `< 12.0 counts RMS`. Corrected to accurately describe physical count thresholding.
3. **Compiler Warning in Empirical Payload Limit Test (`edge/esp32/test/empirical_payload_test.cpp`)**:
   - `size_t measured = measureJson(docHost);` in Scenario 4 was unused, causing `-Wunused-variable` compiler warnings. Added `check(n == measured, ...)` matching other scenarios, eliminating the warning and validating serialization length.
4. **Missing Test Coverage in Host Test Runner (`edge/esp32/test/run_host_tests.sh`)**:
   - `run_host_tests.sh` only ran `host_config_test.cpp`, `host_strip_power_test.cpp`, and `verify_strip_power.py`, omitting `empirical_fuzz_test.cpp` and `empirical_payload_test.cpp`. Added both empirical test suites to the script.
5. **Non-Deterministic PRNG in Python Test Suite (`edge/esp32/test/verify_strip_power.py`)**:
   - `verify_strip_power.py` used `random.gauss()` without setting `random.seed(42)`. Added fixed seed initialization for reproducible test runs.
6. **Untested Multi-Model Sensitivity Scaling & Extreme Edge Cases**:
   - Prior attempt did not test sensitivity scaling across ACS712-05B (`5.4 A/V`) and ACS712-20A (`10.0 A/V`).
   - Added Test Group 10 to C++ host test and Test 11 to Python script. Found that light loads of 0.08A on ACS712-05B have SNR yielding +8.6% error due to $\sigma=8.0$ noise variance addition, but are detected (>0A) instead of being suppressed by the old 0.10A threshold. Calibrated 0.15A load reconstructed within 2.15% (< 5%).
   - Added Test Group 11 to C++ host test and Test 12 to Python script verifying wide generator frequency wander (45 Hz & 65 Hz) has < 2% leakage error, and 30A motor inrush hard clipping at rails does not cause NaN/Inf/hangs.
7. **PlatformIO C++17 Standard Flag**:
   - Added `-std=gnu++17` to `build_flags` in `edge/esp32/platformio.ini`, eliminating inline variable compiler warnings on `node_config.h`.

## 3. Verification Record
- PlatformIO Compilation: `pio run` passed cleanly in release mode (`[SUCCESS] Took 11.51 seconds`, RAM 14.2%, Flash 71.0%).
- Automated Host Test Runner (`./edge/esp32/test/run_host_tests.sh`):
  - `host_config_test.cpp`: 30/30 passed.
  - `host_strip_power_test.cpp`: All 11 test groups passed (including multi-model scaling and extreme drift & inrush).
  - `empirical_fuzz_test.cpp`: All 7 fuzz test scenarios passed (0 failures).
  - `empirical_payload_test.cpp`: All 5 buffer safety scenarios passed (0 failures, 0 warnings).
  - `verify_strip_power.py`: All 12 adversarial test cases passed (0 failures).
