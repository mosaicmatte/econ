# Handoff Report — ACS712 Current Sensor Integration Audit & Fix

## 1. Observation
- The ESP32 firmware function `readStripAmps()` in `edge/esp32/src/main.cpp` exhibited several algorithm and mathematical defects:
  1. Window duration used `millis() - start < 100`, which has $\pm 1$ ms resolution jitter. On a 50 Hz mains line (20 ms period), jitter caused partial-cycle truncation and spectral leakage, distorting window mean and variance calculation.
  2. The sampling loop was unpaced, saturating CPU Core 1 and inducing SAR ADC switching noise.
  3. The noise floor cutoff was hardcoded in physical amperes (`amps < 0.10`). Because ESP32 internal SAR ADC has intrinsic thermal/quantization noise of $\sigma \approx 6\text{--}10$ counts RMS, an unloaded ACS712-30A (`stripCalAPerV = 15.0`) produced raw idle readings of $\approx 0.12\text{ A}$ ($28\text{ W}$ ghost power). Conversely, for lower current models (e.g. ACS712-05B at $5.4\text{ A/V}$), 0.10A corresponded to 23 counts, falsely suppressing real loads between 5W and 22W.
  4. Non-linearity of the ESP32 ADC near the rails (0–0.15V and 3.15–3.3V) limits positive AC swing when the sensor is directly biased at 2.5V; using a divider or 1.65V bias preserves symmetric AC headroom.

## 2. Logic Chain
- **R1 (Sampling & Mathematics)**:
  - Switched windowing from `millis()` to `micros() < 100000UL`, enforcing an exact 100 ms duration (5 integer cycles at 50 Hz, 6 cycles at 60 Hz) and eliminating partial-cycle spectral leakage.
  - Added deterministic pacing `delayMicroseconds(100)`, yielding an effective ~9.1 kHz sampling rate (~182 samples/cycle at 50 Hz), settling the ADC sample-and-hold capacitor and yielding CPU time.
  - Dynamic mean subtraction $\text{Var} = E[V^2] - (E[V])^2$ isolates AC variance from quiescent DC bias automatically.
- **R2 (Noise Floor & Sensitivity Scaling)**:
  - Replaced the fixed ampere threshold (`amps < 0.10`) with a count-based physical noise floor cutoff: `STRIP_NOISE_FLOOR_COUNTS = 12.0` counts RMS (~9.7 mV).
  - This matches the physical noise distribution of the ESP32 ADC ($\sigma \approx 6\text{--}10$ counts) and scales proportionally with `stripCalAPerV` across all ACS712 variants (5A, 20A, 30A), suppressing 100% of 0A idle ghost readings while detecting small valid loads.
- **R3 (Harness & Test Verification)**:
  - Provided automated host C++ tests (`host_strip_power_test.cpp`) and Python tests (`verify_strip_power.py`) testing DC offset independence, noise gating, starved window handling, non-linear SMPS harmonics, grid frequency deviations, multi-model scaling, extreme frequency drift, 30A motor inrush clipping, and noisy synthetic AC sine waveforms (0A, 0.5A, 2A, 10A with $\sigma = 8.0$ counts noise).
  - All test loads reconstruct within $< 2.6\%$ error ($\le 5\%$ required).
  - Integrated into `./edge/esp32/test/run_host_tests.sh`.

## 3. Caveats
- Real-world PCB layout, unshielded jumper wires, or Wi-Fi RF power transmission bursts can introduce transient EMI spikes exceeding 12 counts on physical hardware.
- For extremely small loads where signal RMS is below 25 counts ($< 0.11\text{ A}$ on ACS712-05B / $< 0.31\text{ A}$ on ACS712-30A), noise variance addition ($\text{Var}_{meas} = \text{Var}_{sig} + \sigma^2$) inflates measured current by 5% to 10% unless board-specific noise variance subtraction is calibrated.
- If both `USE_AC_CLAMP` and `USE_STRIP` are enabled simultaneously, `STRIP_ADC_PIN` and `AC_CLAMP_PIN` must be assigned to separate GPIOs (both currently default to GPIO 35 if not reassigned).

## 4. Conclusion
- All requirements (R1, R2, R3) and acceptance criteria have been fully resolved and verified.
- The SWE Light lifecycle completed 1 implementer round and 3 adversarial review rounds.
- A blocking victory audit was performed by `teamwork_preview_victory_auditor`, confirming a VICTORY verdict.

## 5. Verification Method
- Host Test Suites: `bash edge/esp32/test/run_host_tests.sh` (100% pass across all C++ and Python test suites).
- Firmware Compilation: PlatformIO Core (`pio run` in `edge/esp32`) compiled cleanly without errors or warnings (`[SUCCESS] Took 1.48 seconds`, RAM 14.2%, Flash 71.0%).
