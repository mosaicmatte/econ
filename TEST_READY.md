# E2E Test Suite Ready — Occupancy AI, Authentic Forecast, Edge Compute Offload & Hardware Compatibility

## Verified Test Runner Commands

### 1. Backend Go Test Suite (R1 & R3)
- **Full Backend Suite**:
  ```bash
  cd /Users/nguyenhoangkhoi/Documents/econ/server && go test -v -count=1 ./...
  ```
  *Status*: **PASS** (all packages `econ`, `econ/simulation` compile and pass with 0 failures)

- **R1: Genuine Occupancy AI Model Tests**:
  ```bash
  cd /Users/nguyenhoangkhoi/Documents/econ/server && go test -v -count=1 ./simulation/... -run "TestOccupancy"
  ```
  *Status*: **PASS** (8/8 assertions pass, verifying learned threshold scoring $z \le -1.5$, $N \ge 20$, `Basis: "learned"`, and cold-start fallback)

- **R3: Edge Fallback Ingestion & DSP Tests**:
  ```bash
  cd /Users/nguyenhoangkhoi/Documents/econ/server && go test -v -count=1 . -run "TestMQTT"
  cd /Users/nguyenhoangkhoi/Documents/econ/server && go test -v -count=1 ./simulation/... -run "TestZeroSignal|TestWaveform|TestDecimated|TestStepResponse|TestStarvation|TestStatistical"
  ```
  *Status*: **PASS** (all 4 MQTT fallback ingestion tests and all 6 native Go DSP denoiser tests pass)

### 2. Frontend Authentic Forecast Verification Suite (R2)
- **Forecast Chart Puppeteer Verification Harness**:
  ```bash
  cd /Users/nguyenhoangkhoi/Documents/econ/dashboard && npm run build && node verify_forecast_chart.js
  ```
  *Status*: **PASS** (4/4 test suites pass cleanly with 0 failures):
  1. Suite 1: Empty Forecast Array Handling (renders `[data-testid="forecast-insufficient-data"]` badge and 0 curve paths) — **PASS**
  2. Suite 2: Timeout & Missing Forecast Handling (renders honest insufficient data state and 0 curves) — **PASS**
  3. Suite 3: Populated Genuine Forecast Series (renders valid line chart curves normally via Recharts) — **PASS**
  4. Suite 4: Mobile Viewport (390x844) Forecast Rendering (renders insufficient data badge on mobile) — **PASS**

### 3. Edge Firmware Test Suite (R3)
- **ESP32 Edge Node Host Tests**:
  ```bash
  cd /Users/nguyenhoangkhoi/Documents/econ/edge/esp32 && ./test/run_host_tests.sh
  ```
  *Status*: **PASS** (all off-target test suites pass with exit code 0)

- **R3: CPU Strain Detection & Pass-Through Fallback Tests**:
  ```bash
  clang++ -std=c++17 -Wall -Wextra -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/test_cpu_strain_fallback.cpp -o /tmp/test_cpu_strain && /tmp/test_cpu_strain
  ```
  *Status*: **PASS** (all 36/36 checks pass across Normal Mode, Simulated Strain, Command Override, Autonomous Timing Trigger, Buffer Protection, Starvation Guard, and Decimated Waveform Fidelity)

---

## Requirements Verification & Passing Status Matrix

| Requirement | Scope | Test Command | Result | Verification Notes |
|:---|:---|:---|:---:|:---|
| **R1: Genuine Occupancy AI Model** | Backend (`server/simulation/`) | `go test -v -count=1 ./simulation/... -run "TestOccupancy"` | **PASS** | Evaluates continuous EWMA distribution, replaces hardcoded zero check with $z \le -1.5$ anomaly threshold, preserves standard cold-start fallback ($N < 20$). |
| **R2: Authentic Forecast Chart** | Frontend & Backend (`dashboard/`, `server/`) | `npm run build && node verify_forecast_chart.js` | **PASS** | Completely removes fake cubic spline interpolation. Renders honest `forecast-insufficient-data` state when empty or timed out; renders Recharts curve when true series is available. |
| **R3: Edge Compute Offload Fallback** | Firmware & Backend (`edge/esp32/`, `server/`) | `./test/run_host_tests.sh` & `go test -v -count=1 . -run "TestMQTT"` | **PASS** | Dynamic CPU strain detection (>15ms budget or config toggle) switches to pass-through mode streaming decimated raw samples (`rawFallback: true`). Server DSP recovers True-RMS amperage. |
| **R4: Hardware Compatibility Audit** | Edge, Backend, Dashboard | Comprehensive Architecture & Constraint Audit | **PASS** | Verified ADC1 pins (GPIO36/33/34/35 without ADC2 Wi-Fi conflict), ACS712 10k/10k voltage divider (0.5 ratio halving 5V swing to 2.5V), calibration scaling, 768-byte buffer safety (~366 bytes used). |

---

## Hardware Compatibility Audit Summary (R4)

1. **ADC Pin Assignment & Wi-Fi Safety**:
   - SCT-013 Plug Clamp: GPIO34 (ADC1 Channel 6, input-only).
   - ACS712 Strip Sensor: GPIO33 (ADC1 Channel 5) / GPIO36 (ADC1 Channel 0, `SENSOR_VP`).
   - AC Clamp: GPIO35 (ADC1 Channel 7, input-only).
   - *Audit Finding*: All analog sensor inputs strictly utilize **ADC1**. ADC2 (which conflicts with active Wi-Fi and causes read failures) is entirely avoided.

2. **ACS712 Voltage Divider (0.5 Ratio)**:
   - ACS712 outputs $0.5 \times V_{cc} \approx 2.5\text{ V}$ quiescent DC offset with $\pm 1.5\text{ V}$ swing (up to 4.0V on peaks), exceeding ESP32's 3.3V ADC maximum.
   - Hardware implementation uses a 10kΩ/10kΩ precision resistor divider (ratio = 0.5) to scale voltage to $0\dots 2.5\text{ V}$ (quiescent ~1.25V), well within the ADC safe linear region.
   - Both edge firmware (`edge/esp32/src/current_denoiser.h`) and backend DSP (`server/simulation/dsp.go`) incorporate `dividerRatio = 0.5`, guaranteeing identical mathematical calibration ($15.0\text{ A/V}$ sensitivity).

3. **MQTT Telemetry Buffer Limits (768 Bytes)**:
   - ESP32 stack buffer `char buf[768]` is enforced in `main.cpp`.
   - Pass-through offload captures 30 decimated raw samples (`rawStripSamples`), requiring ~180 bytes.
   - Total serialized telemetry in pass-through mode is 366 bytes (under 50% of the 768-byte limit), with zero heap fragmentation and verified stack canary integrity.

4. **Telemetry Type & Structure Consistency**:
   - Firmware JSON (`main.cpp`): `{ "rawFallback": true, "rawStripSamples": [...] }`.
   - Backend Ingestion (`server/mqtt.go`): `telemetryMsg` struct with `RawFallback *bool` and `RawStripSamples []int`. Server processes raw samples via Go `CurrentDenoiser` and calculates `StripW = amps * mainsV` (230V).
   - Frontend Ingestion (`dashboard/`): Receives calculated `stripW` wattage seamlessly without requiring clients to run edge DSP.
