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
