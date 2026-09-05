# ACS712 Integration Audit & Firmware Fix — Progress

## 1. Audit Findings (Root-Cause Analysis)
- **ADC Sampling Timing**: `readStripAmps()` previously used `while (millis() - start < 100)`. Because `millis()` has a 1 ms resolution, the window duration varied between 99 ms and 101 ms. For a 50 Hz mains signal (20 ms period) or 60 Hz (16.67 ms period), this partial-cycle boundary truncation caused spectral leakage and distorted the DC mean subtraction.
- **Sampling Pacing**: Tight unpaced loops executed `analogRead()` as fast as possible (~10,000 conversions in 100 ms), burning 100% CPU Core 1, preventing FreeRTOS task yielding, and injecting internal switching noise into the ESP32 SAR ADC.
- **DC Offset Removal**: The one-pass dynamic mean subtraction Var = E[V^2] - (E[V])^2 accurately tracks the quiescent zero-current DC voltage of the ACS712 without hardcoding 2.5V or requiring manual zero-point calibration.
- **ESP32 ADC Non-Linearity**: ESP32 ADC is non-linear below ~0.15V and above ~3.15V. At 2.5V quiescent bias, positive swing headroom is only 0.8V (~12A peak / 8.5A RMS with ACS712-30A). With a voltage divider to 1.65V, full symmetric dynamic range is achieved.
- **Root-Cause of False Ghost Readings**: ESP32 internal SAR ADC has intrinsic thermal/quantization noise with sigma ~ 6-10 counts RMS. Because Var_measured = Var_signal + sigma_noise^2, at 0A load the calculated RMS is ~ sigma_noise. For ACS712-30A (stripCalAPerV = 15.0), 10 counts of noise translates to 10 * (3.3 / 4095) * 15.0 = 0.121 A. The previous fixed cutoff `amps < 0.10` was exceeded by normal ADC noise, reporting 0.121 A * 230 V ~ 28 W on an empty socket.
- **Root-Cause of 0 W Under Load**: The previous cutoff `amps < 0.10` fixed in amperes suppressed any real physical load drawing under 23W at 230V (e.g. 5W–20W standby/LED loads). Furthermore, for more sensitive sensors like ACS712-05B (5.4 A/V), 0.10A corresponded to 23 ADC counts, needlessly suppressing loads up to 23W.

## 2. Firmware Fixes (`edge/esp32/src/main.cpp`)
- Replaced `millis()` with `micros()` for exact 100,000 µs (100.0 ms) sub-millisecond windowing (exact integer cycles: 5 at 50 Hz, 6 at 60 Hz).
- Added `delayMicroseconds(100)` pacing between conversions, settling the ADC sample-and-hold capacitor, yielding uniform ~9.1 kHz sampling (~180 samples/cycle at 50 Hz), and eliminating CPU starvation.
- Replaced fixed ampere cutoff with count-based physical noise floor cutoff: `STRIP_NOISE_FLOOR_COUNTS = 12.0` counts RMS (~9.7 mV). This directly tracks ESP32 SAR ADC physical noise and scales proportionally with `stripCalAPerV` across all ACS712 models (5A, 20A, 30A).

## 3. Verification Suite
- Updated `edge/esp32/test/host_strip_power_test.cpp` with R3 waveform reconstruction tests (0A, 0.5A, 2A, 10A with typical ESP32 ADC noise sigma = 8.0 counts).
- Updated `edge/esp32/test/verify_strip_power.py` with Test 10 asserting 0A suppression and <5% accuracy on 0.5A, 2A, 10A waveforms.
- Added host tests to `edge/esp32/test/run_host_tests.sh`.
- Ran `pio run` confirming clean compilation without errors or warnings.
- Ran all host tests and fuzz suites: 100% pass rate.
