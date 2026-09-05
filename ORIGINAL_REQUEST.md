# Original User Request

## 2026-09-04T05:58:10Z

# Teamwork Project Prompt — Draft

> Status: Launched
> Goal: Craft prompt → get user approval → delegate to teamwork_preview
> Requested team: full team

End-to-end integration of a new telemetry metric `stripW` (Power Strip Watts) using an ACS712 sensor. This requires updating the ESP32 C++ firmware to read GPIO 35, the Go backend to parse and store the metric in TimescaleDB, and the Next.js React dashboard to display it.

Working directory: `d:\ECON1\econ`
Integrity mode: development

## Requirements

### R1. ESP32 Firmware Update
Update the C++ firmware in `edge/esp32` to read the ACS712 analog sensor on GPIO 35. Apply a new `stripCalAPerV` calibration multiplier, calculate the RMS power, and append `stripW` to the MQTT telemetry JSON payload.

### R2. Go Backend & Database Update
Modify the Go MQTT server structs to parse the new `stripW` field from the JSON payload. Alter the TimescaleDB `telemetry` database schema to include a `strip_w` column using an `ALTER TABLE` SQL command to preserve historical data, and update the Go SQL insert statements.

### R3. Frontend Dashboard Update
Update the Next.js/React frontend in the `dashboard` directory to parse `stripW` from the WebSocket/API and display it as a new "Power Strip" card on the dashboard, alongside the existing Power metrics.

## Acceptance Criteria

### End-to-End Verification
- [ ] ESP32 firmware compiles and successfully flashes via `python -m platformio run -t upload` in the `edge/esp32` directory.
- [ ] ESP32 serial output confirms successful JSON MQTT publish including the `"stripW"` field.
- [ ] Go backend correctly parses `stripW` and inserts it without SQL errors (verified via docker logs).
- [ ] The `telemetry` table retains all historical data (no `DROP TABLE` used).
- [ ] The Next.js dashboard compiles successfully via `npm run dev` and dynamically renders the new metric.

## 2026-09-04T19:45:40Z

This is a single self-contained fix; keep it small and focused.

Investigate and audit the ACS712 current sensor integration in the ESP32 firmware, diagnosing algorithm issues in ADC sampling, RMS calculation, noise floor cutoff, and calibration.

Working directory: /Users/nguyenhoangkhoi/Documents/econ
Integrity mode: development

## Requirements

### R1. Audit ACS712 Firmware Sampling and Mathematics
Analyze `readStripAmps()` in `edge/esp32/src/main.cpp`. Identify issues regarding ADC sampling timing, sample count, DC offset removal, ESP32 ADC non-linearity, and True-RMS variance calculations.

### R2. Noise Floor and Calibration Verification
Evaluate the noise threshold logic (`amps < 0.10`) and sensitivity scaling (`stripCalAPerV`, `plugMainsV`). Determine why the sensor may output 0 W under load or trigger false ghost readings, and provide calibrated, robust thresholding.

### R3. Implementation and Verification Harness
Implement the necessary fixes in `edge/esp32/src/main.cpp` and provide a reproducible host test (or test script) that validates the RMS calculation against synthetic AC sine waveforms (e.g., 0A, 0.5A, 2A, 10A) with typical ESP32 ADC noise.

## Acceptance Criteria

### Audit & Analysis
- [ ] Root-cause analysis documented explaining mathematical and sampling behavior of `readStripAmps()`.

### Code & Tests
- [ ] Unit test or validation script verifying that the current calculation correctly reconstructs known test waveforms within 5% accuracy.
- [ ] Firmware compiles cleanly without errors or warnings via PlatformIO (`pio run` in `edge/esp32`).
- [ ] Noise floor cutoff prevents ghost readings at 0A while accurately measuring loads above the noise threshold.

## 2026-09-05T02:47:06Z

# Teamwork Project Prompt — Draft

> Status: Launched
> Goal: Craft prompt → get user approval → delegate to teamwork_preview
> Requested team: [none — teamwork routes from the description]

Implement a core "Sustainability & Decarbonization" backend module for the `econ` building management system. Based on the provided domain knowledge, this module should translate existing telemetry into carbon metrics, support predictive maintenance, and expose a unified API for the dashboard that includes live carbon credit purchasing recommendations.

Working directory: /Users/nguyenhoangkhoi/Documents/econ
Integrity mode: development

## Requirements

### R1. Carbon Accounting (Scope 2 & Operational Carbon)
Implement a Go backend module (e.g., `server/carbon.go`) that continuously calculates **Scope 2 Operational Carbon** by translating energy consumption (from `plugW`, `stripW`, and AC states) into kgCO2e using configurable grid emission factors.

### R2. Predictive Maintenance & Space Utilization
Extend the existing engine logic (e.g., `ZoneSim`) to track equipment health. Flag abnormal power draws or track total runtime hours to simulate **Predictive Maintenance** alerts. Utilize the existing `occupancy` data to calculate space utilization efficiency.

### R3. Carbon Credit Recommendations (Live Data)
Implement logic that compares the calculated emissions against a target "carbon budget". If the infrastructure fails to meet the requirement, the system must fetch live carbon credit pricing data from the internet (via public APIs or web scraping) and recommend purchasing the exact amount of carbon certificates needed to offset the difference, including the estimated cost.

### R4. Sustainability API Endpoint
Create a new REST endpoint (e.g., `/api/sustainability`) that exposes the aggregated data: total Scope 2 emissions, current space utilization efficiency, active predictive maintenance warnings, and the dynamic carbon credit recommendations.

## Verification Resources
The current backend is written in Go and runs in the `server` directory. It has existing telemetry streams handling `plugW`, `stripW`, and `occupancy`.

## Acceptance Criteria

### Code Integrity
- [ ] The `server` directory compiles successfully (`go build .`) with the new code.
- [ ] The existing backend functionality is not broken.

### Mathematical & External Verification
- [ ] A new Go test (e.g., `carbon_test.go`) is written to programmatically verify the carbon calculation logic (e.g., asserting that 1000W drawn for 1 hour with a 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon).
- [ ] The backend demonstrably makes an outbound HTTP request to pull live carbon market pricing.
- [ ] `go test ./...` passes successfully.

### API Functionality
- [ ] A `curl` request to the new endpoint (`/api/sustainability`) returns a valid JSON payload containing carbon totals, maintenance alerts, and (if over budget) the recommended carbon credit offset amount and live cost.

