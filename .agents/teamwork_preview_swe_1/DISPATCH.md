# Dispatch History

## 2026-09-05T02:46:18+07:00

You are the SWE Light Orchestrator for this task.

Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/teamwork_preview_swe_1
Project root / Workspace directory: /Users/nguyenhoangkhoi/Documents/econ
Authoritative user request: /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md

Task summary:
This is a single self-contained fix; keep it small and focused.
Investigate and audit the ACS712 current sensor integration in the ESP32 firmware, diagnosing algorithm issues in ADC sampling, RMS calculation, noise floor cutoff, and calibration.
Working directory: /Users/nguyenhoangkhoi/Documents/econ
Integrity mode: development

Requirements:
R1. Audit ACS712 Firmware Sampling and Mathematics:
Analyze `readStripAmps()` in `edge/esp32/src/main.cpp`. Identify issues regarding ADC sampling timing, sample count, DC offset removal, ESP32 ADC non-linearity, and True-RMS variance calculations.

R2. Noise Floor and Calibration Verification:
Evaluate the noise threshold logic (`amps < 0.10`) and sensitivity scaling (`stripCalAPerV`, `plugMainsV`). Determine why the sensor may output 0 W under load or trigger false ghost readings, and provide calibrated, robust thresholding.

R3. Implementation and Verification Harness:
Implement the necessary fixes in `edge/esp32/src/main.cpp` and provide a reproducible host test (or test script) that validates the RMS calculation against synthetic AC sine waveforms (e.g., 0A, 0.5A, 2A, 10A) with typical ESP32 ADC noise.

Acceptance Criteria:
- Root-cause analysis documented explaining mathematical and sampling behavior of `readStripAmps()`.
- Unit test or validation script verifying that the current calculation correctly reconstructs known test waveforms within 5% accuracy.
- Firmware compiles cleanly without errors or warnings via PlatformIO (`pio run` in `edge/esp32`).
- Noise floor cutoff prevents ghost readings at 0A while accurately measuring loads above the noise threshold.

Please execute the SWE Light lifecycle (implementer and review rounds), maintain progress.md in your working directory, establish correctness by running tests, and report completion when done.
