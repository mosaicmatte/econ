# Reviewer Round 3: ACS712 Firmware Sampling and True-RMS Audit

## Status: COMPLETE / VERIFIED

### 1. Independent Audit Summary (R1, R2, R3)

#### R1. Audit ACS712 Firmware Sampling and Mathematics
- **Sampling Timing & Sample Count**:
  - Original: `millis()` jitter of +/-1 ms caused window truncation (fractional cycle cutoff on 50/60 Hz waveforms) and unpaced sampling (~100 kHz back-to-back `analogRead()`) burned 100% CPU on Core 1 and starved FreeRTOS tasks.
  - Fix: Microsecond-precision windowing `(micros() - start) < 100000UL` provides an exact 100.0 ms window (5 full cycles of 50 Hz, 6 full cycles of 60 Hz). Paced sampling via `delayMicroseconds(100)` yields ~909 samples at ~9.1 kHz, satisfying Nyquist for up to the 40th harmonic while allowing ADC S/H settling and CPU yielding.
- **Dynamic DC Offset Removal**:
  - `double mean = sum / n;`
  - AC variance `fmax(0.0, sumSq / n - mean * mean)` isolates the AC residue from the DC quiescent bias without requiring a hardcoded offset. Accommodates direct 2.5V bias or 1.65V divided bias, as well as thermal drift. `fmax(0.0, ...)` prevents `NaN` from floating point rounding under pure DC.
- **ESP32 ADC Non-Linearity**:
  - ESP32 SAR ADC compression near rails (< 0.1V, > 3.15V) is avoided by operating around 1.65V (with divider) or 2.5V (nominal ACS712 VCC/2). Linear operating range spans nominal currents up to 8A RMS without clipping.

#### R2. Noise Floor and Calibration Verification
- **Fixed Ampere Threshold Bug (`amps < 0.10`)**:
  - ACS712-30A sensitivity (66 mV/A, `stripCalAPerV` = 15.0 A/V): 8.5 counts noise produces 8.5 * (3.3/4095) * 15.0 = 0.1028 A > 0.10 A, triggering false ghost readings (~23.6W) at 0A.
  - ACS712-05B sensitivity (185 mV/A, `stripCalAPerV` = 5.4 A/V): 0.10A corresponds to 23 counts. A small 15W load (0.065A / ~15 counts) evaluated to < 0.10 A, falsely zeroing out real loads.
- **Count-Based Physical Thresholding (`STRIP_NOISE_FLOOR_COUNTS = 12.0`)**:
  - Evaluates noise floor directly in ADC counts (~9.7 mV RMS).
  - Gating on 12.0 counts provides margin above typical ESP32 ADC noise (sigma ~ 6 - 8 counts), reliably eliminating ghost readings at 0A across all models.
  - Scales proportionally with `stripCalAPerV` (0.052 A ~ 12.0 W on 05B; 0.097 A ~ 22.3 W on 20A; 0.145 A ~ 33.4 W on 30A).

#### R3. Implementation and Verification Harness
- Verified `readStripAmps()` in `edge/esp32/src/main.cpp`.
- Clean PlatformIO compilation: `pio run` executed via `/Users/nguyenhoangkhoi/Library/Python/3.13/bin/pio` with `[SUCCESS]` (RAM 14.2%, Flash 71.0%, 0 errors, 0 warnings).
- Host test suite executed via `./edge/esp32/test/run_host_tests.sh`:
  - `host_config_test.cpp`: 30/30 passed.
  - `host_strip_power_test.cpp`: All 11 test groups passed.
  - `empirical_fuzz_test.cpp`: All 7 fuzzing scenarios passed.
  - `empirical_payload_test.cpp`: All 5 buffer boundary scenarios passed.
  - `verify_strip_power.py`: All 12 test cases passed.
- Waveform reconstruction on 0A, 0.5A, 2A, 10A with typical ESP32 ADC noise (sigma = 8.0 counts):
  - 0A: 0.0000A (0% error, ghost reading prevented).
  - 0.5A: 0.5040A (0.79% error, <= 5%).
  - 2.0A: 2.0045A (0.23% error, <= 5%).
  - 10.0A: 10.0019A (0.02% error, <= 5%).
