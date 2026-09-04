# Project: ECON stripW Telemetry Integration

## Architecture
The ECON system connects physical and modelled edge sensor nodes to a central digital twin and optimization engine:
1. **Edge Firmware (`edge/esp32`)**: ESP32 C++ firmware running on FreeRTOS/Arduino framework. Reads sensors (including ACS712 analog current sensor on GPIO 35), calculates True-RMS current, multiplies by mains voltage to compute real RMS power (`stripW`), and publishes JSON telemetry over MQTT to topic `econ/telemetry/<zone>` while echoing to serial monitor.
2. **Backend & Database (`server/`)**: Go 1.22 server managing MQTT subscriptions, TimescaleDB persistence, and digital twin simulation. Unmarshals MQTT JSON payloads into `telemetryMsg`, records readings in TimescaleDB hypertable `sensor_readings` (with new column `strip_w`), updates in-memory `simulation.Engine`, and relays live telemetry over WebSocket (using Google FlatBuffers binary stream) and REST (`/api/hardware`, `/api/series`).
3. **Frontend Dashboard (`dashboard/`)**: Vite 5 + React 18 SPA. Ingests telemetry via FlatBuffers WebSocket (`useDigitalTwin.js`) and REST polling (`/api/hardware`). Displays building-wide and zone-level energy metrics in `GlobalMetricsPanel.jsx`, featuring the new "Power Strip" card alongside existing Power cards (TOTAL LOAD, ENERGY SAVED, BESS), and provides bring-up diagnostics in `HardwareInspector.jsx`.

```
[ACS712 Current Sensor (GPIO 35)]
               │
               ▼ (Analog 12-bit ADC)
     [ESP32 Firmware: edge/esp32]
               │
               ▼ (MQTT JSON payload: {"stripW": ...} on econ/telemetry/<zone>)
     [Eclipse Mosquitto MQTT Broker (:1883)]
               │
               ▼
     [Go Backend Server: server/]
        ├── TimescaleDB (:5432) -> INSERT INTO sensor_readings (..., strip_w)
        └── Digital Twin Engine -> /api/hardware & /ws (FlatBuffers)
               │
               ▼
     [Vite React Dashboard: dashboard/]
        ├── GlobalMetricsPanel.jsx -> "Power Strip" Card
        └── HardwareInspector.jsx -> ACS712 Live Bring-Up
```

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | ACS712 Sensor Read | Read ACS712 analog sensor on GPIO 35 in `edge/esp32` | M1 | ORIGINAL_REQUEST §R1 |
| 2 | Calibration Multiplier | Add and apply `stripCalAPerV` calibration multiplier in `node_config.h` | M1 | ORIGINAL_REQUEST §R1 |
| 3 | RMS Power Calculation | Calculate 100ms True-RMS current with DC offset cancellation and multiply by mains voltage | M1 | ORIGINAL_REQUEST §R1 |
| 4 | MQTT JSON stripW Append | Expand JSON buffer and append `"stripW"` to MQTT telemetry JSON payload in `readAndPublish()` | M1 | ORIGINAL_REQUEST §R1 |
| 5 | Go MQTT Struct Update | Modify `telemetryMsg` in `server/mqtt.go` to parse `stripW` JSON field | M2 | ORIGINAL_REQUEST §R2 |
| 6 | TimescaleDB Schema Migration | Execute `ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION` and create `telemetry` compatibility view | M2 | ORIGINAL_REQUEST §R2 |
| 7 | Go SQL Insert Update | Update `reading` struct and 7-column batch SQL INSERT in `server/db.go` `writeLoop()` | M2 | ORIGINAL_REQUEST §R2 |
| 8 | Backend Hardware & Stream Relay | Ingest `stripW` into `simulation.Measurement` and `HardwareNode`, expose via `/api/hardware` and FlatBuffers `ZoneData` | M2 | ORIGINAL_REQUEST §R2 |
| 9 | Dashboard Telemetry Parsing | Parse `stripW` in `dashboard/src/telemetry/zone-data.ts` and `dashboard/src/useDigitalTwin.js` | M3 | ORIGINAL_REQUEST §R3 |
| 10 | Power Strip Dashboard Card | Render new "Power Strip" card in `GlobalMetricsPanel.jsx` alongside existing Power metrics | M3 | ORIGINAL_REQUEST §R3 |
| 11 | Hardware Inspector Tooling | Register `stripW` in `dashboard/src/HardwareInspector.jsx` fields registry | M3 | ORIGINAL_REQUEST §R3 |
| 12 | Firmware Build & Serial Verification | Verify PlatformIO compilation and serial output publish format | M4 | ORIGINAL_REQUEST §Acceptance Criteria |
| 13 | Backend Ingestion & Docker Logs Verification | Verify Go backend parses `stripW` and inserts into database without SQL errors | M4 | ORIGINAL_REQUEST §Acceptance Criteria |
| 14 | Historical Data Retention Verification | Verify no data loss in TimescaleDB (no `DROP TABLE` used) | M4 | ORIGINAL_REQUEST §Acceptance Criteria |
| 15 | Dashboard Dynamic Render Verification | Verify Vite dashboard builds cleanly and dynamically renders `stripW` | M4 | ORIGINAL_REQUEST §Acceptance Criteria |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | ESP32 Firmware Update | Features 1, 2, 3, 4: GPIO 35 reading, `stripCalAPerV` config, True-RMS math, JSON serialization | none | DONE |
| 2 | Go Backend & TimescaleDB Update | Features 5, 6, 7, 8: MQTT unmarshaling, DB migration, 7-column SQL insert, engine relay | none (can parallelize contractually with M1) | IN_PROGRESS |
| 3 | Frontend Dashboard Update | Features 9, 10, 11: FlatBuffers/REST ingestion, "Power Strip" card, Hardware Inspector | M2 interface contract | PLANNED |
| 4 | E2E Integration & Verification | Features 12, 13, 14, 15: Full-system build, DB check, telemetry end-to-end flow, forensic audit | M1, M2, M3 | PLANNED |

## Interface Contracts

### ESP32 ↔ MQTT Broker ↔ Go Backend
- **Topic**: `econ/telemetry/<zone>` (e.g. `econ/telemetry/zone_1`)
- **Payload Format**: JSON object
- **Field Name**: `"stripW"`
- **Data Type**: Float / number rounded to 1 decimal place (e.g., `185.4`), representing Real RMS Watts.
- **Starvation / Omission**: If the ADC sampling window is starved or sensor is absent, `"stripW"` is omitted from the JSON payload (not defaulted to 0).
- **Serial Mirror**: Serial log outputs `[mqtt] pub econ/telemetry/<zone> -> {...,"stripW":...}`.

### Go Backend ↔ TimescaleDB Database
- **Physical Hypertable**: `public.sensor_readings`
- **Compatibility View**: `public.telemetry` (`SELECT * FROM sensor_readings;`)
- **Column Name**: `strip_w`
- **Data Type**: `DOUBLE PRECISION` (`NULL` allowed when not reported or for historical records)
- **Insert Query**: `INSERT INTO sensor_readings (time, zone_id, sensor_type, value, device_id, quality, strip_w) VALUES ($1,$2,$3,$4,$5,$6,$7)`
- **Data Integrity**: Migration uses `ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION;` preserving 100% of historical records.

### Go Backend ↔ Frontend Dashboard
1. **REST API `/api/hardware`**:
   - Returns JSON array of `HardwareNode` objects.
   - Field: `stripW: number` (watts, 0 if no sensor reporting).
2. **WebSocket `/ws` (Google FlatBuffers)**:
   - Schema: `Telemetry.ZoneData`
   - Field: `stripW: float` at vtable slot 11 (offset 26).
3. **REST API `/api/series?zone=<id>&metric=stripW`**:
   - Returns time-bucketed historical averages of `stripW`.

## Code Layout
- `edge/esp32/`:
  - `src/main.cpp`: ESP32 main program, ADC reading loop, MQTT publication.
  - `src/node_config.h`: NodeConfig struct, defaults, range validation, JSON serialization.
  - `test/host_config_test.cpp`: C++ host unit tests for configuration.
- `server/`:
  - `mqtt.go`: MQTT subscriber, `telemetryMsg` struct.
  - `db.go`: TimescaleDB connection, `migrateSchema()`, `writeLoop()` batch insertion.
  - `db/init.sql`: Base database schema and hypertable definitions.
  - `devices.go`: Device telemetry tracking and persistence triggers.
  - `simulation/engine.go`: `Measurement` struct, `HardwareNode` struct, twin simulation.
  - `schema/Telemetry/ZoneData.go`: Go FlatBuffers serialization for ZoneData.
- `dashboard/`:
  - `src/telemetry/zone-data.ts`: TypeScript FlatBuffers deserialization.
  - `src/useDigitalTwin.js`: WebSocket binary message unpacker.
  - `src/GlobalMetricsPanel.jsx`: Main metrics panel and "Power Strip" card rendering.
  - `src/HardwareInspector.jsx`: Hardware diagnostics panel and sensor field definitions.
