# SWE Light Adversarial Review Round 2 — Progress

## 1. Independent Task Understanding & Requirements Audit
- **R1 (Sampling & Mathematics)**: Audit `readStripAmps()` in `edge/esp32/src/main.cpp` for sampling timing, cycle coverage (50 Hz / 60 Hz integer multiples), sample pacing, dynamic DC offset removal ($E[V^2] - (E[V])^2$), and ESP32 ADC non-linearity / clipping behavior.
- **R2 (Noise Floor & Calibration)**: Evaluate physical noise floor cutoff vs fixed `amps < 0.10` cutoff, sensitivity scaling across ACS712 models (5A, 20A, 30A), and `gCfg.stripCalAPerV` / `gCfg.plugMainsV`.
- **R3 (Implementation & Verification Harness)**: Verify host test suites and synthetic waveform reconstruction (0A, 0.5A, 2A, 10A) with typical ESP32 ADC noise ($\sigma = 8.0-10.0$ counts), confirming error $\le 5\%$ and zero false triggers at 0A.

## 2. Adversarial Findings in Prior Attempt
1. **Unverified PlatformIO Compilation Claim**:
   - **Input**: Command `pio run` in `edge/esp32`.
   - **Expected**: Successful firmware build reproducing the reported `[SUCCESS] Took 11.51 seconds`, RAM 14.2%, Flash 71.0%.
   - **Actual**: PlatformIO command failed (`zsh:1: command not found: pio`). In the sandboxed macOS execution environment, external packages and binaries in `~/.platformio` or `~/.local/share` cannot be accessed or installed due to OS-level sandbox isolation (`Operation not permitted`) and restricted network egress.
   - **Root cause**: The prior reviewer copied build metrics from an earlier unconstrained/Windows environment without independently verifying that `pio` was runnable in this environment.
2. **Prior Fixes Confirmed**:
   - Firmware comment discrepancy corrected: Line 618 now reads `12.0 counts RMS (~9.7 mV)`.
   - C++ host test labels and assertions in Test Group 3 updated to physical counts (`< 12.0 counts RMS`).
   - Compiler warning `-Wunused-variable` in `empirical_payload_test.cpp` resolved via `check(n == measured, ...)`.
   - All 5 host test suites integrated into `edge/esp32/test/run_host_tests.sh`.
   - PRNG seed deterministic in `edge/esp32/test/verify_strip_power.py`.
   - Multi-model sensitivity scaling and edge cases (wide 45/65 Hz drift, 30A motor inrush) verified.

## 3. Verification Record
- **Deep Verification (ran actual tests)**:
  - `./edge/esp32/test/run_host_tests.sh` ran completely with exit code 0:
    - `host_config_test.cpp`: 30/30 assertions passed (0 warnings under `-Wall -Wextra`).
    - `host_strip_power_test.cpp`: All 11 test groups passed (0 warnings under `-Wall -Wextra`).
    - `empirical_fuzz_test.cpp`: All 7 fuzzing scenarios passed (0 warnings under `-Wall -Wextra`).
    - `empirical_payload_test.cpp`: All 5 buffer boundary scenarios passed (0 warnings under `-Wall -Wextra`).
    - `verify_strip_power.py`: All 12 adversarial test cases passed (0 failures).
- **Shallow Verification**:
  - Code inspection of FreeRTOS task yield and watchdog safety during 100 ms `readStripAmps()` window.
  - Verification that `readStripAmps()` returns `-1.0f` on starvation ($n < 100$), cleanly omitting `stripW` from JSON telemetry.
- **Unverified Aspects**:
  - `pio run` execution on this host due to absence of local PlatformIO binary in sandbox.
  - Physical flashing onto a physical ESP32 MCU board with a physical ACS712 sensor on live mains.
