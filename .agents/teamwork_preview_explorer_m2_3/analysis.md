# Milestone 2 Investigation Analysis: Go Build, Test Suite, and Docker Verification Strategy

**Author**: `teamwork_preview_explorer_m2_3` (Explorer Subagent)  
**Date**: 2026-09-04  
**Project**: ECON (`d:\ECON1\econ`)  
**Scope**: Server Backend (`server/`), TimescaleDB (`econ_wifi_ch_a-db-1`), and Go test suite

---

## 1. Executive Summary

- **Go Toolchain & Compilation**: Go is NOT installed on the host Windows PATH (`go: command not found`). All builds and tests must execute inside Docker (`golang:1.22-alpine`).
- **Compilation & Test Status**: `go build -o /tmp/econ-server .` compiles cleanly with **0 compilation errors**. `go test -v ./...` executed in Docker completed with **0 failures across all 26 unit and simulation tests** in `econ` and `econ/simulation`.
- **Running Containers**: 5 active containers under the compose project `econ_wifi_ch_a`:
  - `econ_wifi_ch_a-server-1` (Up 2 hours, port 8080)
  - `econ_wifi_ch_a-db-1` (TimescaleDB pg14, Up 2 hours, port 5432)
  - `econ_wifi_ch_a-mqtt-1` (Eclipse Mosquitto 2, Up 2 hours, port 1883)
  - `econ_wifi_ch_a-forecasting-1` (FastAPI LSTM, Up 2 hours, port 8000)
  - `econ_wifi_ch_a-digitizer-1` (FastAPI Digitizer, Up 2 hours, port 8090)
- **Critical Forensic Finding (DB Ingestion Deadlock)**:
  - At container startup (12:44:24 UTC), `econ_wifi_ch_a-server-1` failed to connect to `db:5432` because Postgres was still initializing (`dial tcp 172.22.0.5:5432: connect: connection refused`).
  - Because `initDB()` in `server/db.go` lacks a reconnection loop on boot, `DB` was set to `nil`.
  - As a consequence, `migrateSchema()` was never executed, `writeCh` was never allocated, and **zero telemetry samples have been persisted to TimescaleDB** (`SELECT COUNT(*) FROM sensor_readings` is 0).
  - Furthermore, `econ_wifi_ch_a-server-1` was built 2 hours ago, prior to commit `ff615d22` which introduced `stripW` into `server/db.go` and `server/db/init.sql`. The running TimescaleDB hypertable `sensor_readings` still has only 6 columns and does NOT have `strip_w` yet.
- **Milestone 2 Verification Strategy**: Formulated an end-to-end verification protocol spanning Worker compilation & container rebuild, DB schema migration, batch insert validation, and Reviewer/Challenger edge-case fuzzing.

---

## 2. Go Toolchain & Compilation Status

### 2.1 Host Environment Check
```powershell
go version
# Output:
# go : The term 'go' is not recognized as the name of a cmdlet, function, script file, or operable program.
```
**Finding**: The host machine lacks a local Go SDK installation. Any attempt to invoke `go` directly on PowerShell will fail.

### 2.2 Docker-based Toolchain Verification
Using the official `golang:1.22-alpine` container image matching the project's `Dockerfile` target:

```powershell
docker run --rm -v "d:\ECON1\econ\server:/app" -w /app golang:1.22-alpine go build -o /tmp/econ-server .
```
- **Exit Code**: `0`
- **Output**: Clean compilation, binary produced without warnings or errors.

### 2.3 Test Suite Execution & Coverage
Command:
```powershell
docker run --rm -v "d:\ECON1\econ\server:/app" -w /app golang:1.22-alpine go test -v ./...
```
- **Total Tests Executed**: 26 unit tests across `econ` and `econ/simulation`.
- **Result**: `PASS` (0 failures, 0 skips).
- **Key Test Cases Validated**:
  1. `TestAppendedFieldsRoundTrip` in `server/telemetry_schema_test.go`:
     - Serializes `ZoneDataAddStripW(b, 185.4)` into FlatBuffers buffer.
     - Decodes `z.StripW()` and asserts exact float value `185.4`.
     - Validates backward compatibility: decoding without appended fields defaults `StripW()` to `0.0`.
  2. `TestStripWIngestedAndSurfaced` in `server/simulation/hardware_test.go`:
     - Ingests `Measurement{ StripW: fp(185.4) }` into `simulation.Engine`.
     - Asserts `HardwareNode.StripW == 185.4` and `targetZone.HwStripW == 185.4`.
     - Validates staleness: simulates time elapsing > 15s (`hwStaleAfter`), confirms `stripFresh() == false` and `HardwareStatus` resets `StripW` to `0.0`.
     - Validates offline status transition: `SetNodeStatus("zone_1", false)` invalidates freshness.
  3. Security & Plausibility:
     - Origin validation (`TestCheckOrigin*` in `server/auth_test.go`).
     - Peak load plausibility filters (`TestForecastPlausibility` in `server/forecast_plausibility_test.go`).
     - Hardware tiers classifier (`TestRecommendModelPicksTierForRealMachines` in `server/modelcatalog_test.go`).

---

## 3. Docker Compose & Container Health Analysis

### 3.1 Active ECON Containers (`docker ps`)
| Container ID | Image | Status | Ports | Names |
|---|---|---|---|---|
| `cebe3ae38513` | `econ_wifi_ch_a-server` | Up 2 hours | `0.0.0.0:8080->8080/tcp` | `econ_wifi_ch_a-server-1` |
| `b741a4ddc431` | `econ_wifi_ch_a-forecasting` | Up 2 hours | `0.0.0.0:8000->8000/tcp` | `econ_wifi_ch_a-forecasting-1` |
| `e713c5b49561` | `timescale/timescaledb:latest-pg14` | Up 2 hours | `0.0.0.0:5432->5432/tcp` | `econ_wifi_ch_a-db-1` |
| `7962ab9c5484` | `eclipse-mosquitto:2` | Up 2 hours | `0.0.0.0:1883->1883/tcp` | `econ_wifi_ch_a-mqtt-1` |
| `0b2e119e4bae` | `econ_wifi_ch_a-digitizer` | Up 2 hours | `0.0.0.0:8090->8000/tcp` | `econ_wifi_ch_a-digitizer-1` |

### 3.2 Running Server Logs (`docker logs econ_wifi_ch_a-server-1`)
- **MQTT Connectivity**: The server is successfully connected to `tcp://mqtt:1883` and receiving telemetry published by the ESP32 node every 5 seconds:
  ```
  [mqtt] telemetry zone_1 occ=0 src="esp32" real_temp=true (zone="Level 4")
  ```
- **Zero Runtime Crashes**: No Go runtime panics, fatal errors, or unexpected process restarts.
- **Database Status**: Silent failure on startup (see Section 4).

---

## 4. Critical Forensic Discovery: DB Startup Race & Table State

### 4.1 Root Cause of Ingestion Failure
Inspection of the initial container boot logs revealed:
```text
2026/09/04 12:44:24 [library] loaded data/programme-library.json v2: 15 programmes (2 critical)
2026/09/04 12:44:24 [db] DB not reachable (is the container up?): dial tcp 172.22.0.5:5432: connect: connection refused
2026/09/04 12:44:24 [devices] hardware inspector at /api/devices (TEMPORARY bring-up module)
```

In `server/db.go`:
```go
func initDB() {
    ...
    if err = DB.Ping(); err != nil {
        log.Printf("[db] DB not reachable (is the container up?): %v", err)
        DB = nil
        return
    }
    ...
    migrateSchema()
    writeCh = make(chan reading, 8192)
    go writeLoop()
    log.Println("[db] Connected to TimescaleDB.")
}
```
**Mechanism**:
1. When `docker compose up` starts containers simultaneously, `server` started milliseconds before `db` had completed its Postgres socket initialization.
2. `DB.Ping()` returned `connection refused`.
3. `initDB()` set `DB = nil` and returned permanently without any retry mechanism (unlike `startMQTT()` which uses `SetConnectRetryInterval(5 * time.Second)`).
4. As a result:
   - `migrateSchema()` was never invoked.
   - `writeCh` remained `nil`. Any call to `enqueue()` silently drops the reading (`if writeCh == nil { return }`).
   - `writeLoop()` was never spawned.
   - The server has been running in an unpersisted, in-memory-only degraded state for 2 hours.

### 4.2 Current Database Schema in `econ_wifi_ch_a-db-1`
Inspection of `econ_wifi_ch_a-db-1` via `psql`:
```text
                     Table "public.sensor_readings"
   Column    |           Type           | Collation | Nullable | Default 
-------------+--------------------------+-----------+----------+---------
 time        | timestamp with time zone |           | not null | 
 zone_id     | text                     |           | not null | 
 sensor_type | text                     |           | not null | 
 value       | double precision         |           |          | 
 device_id   | text                     |           |          | 
 quality     | text                     |           |          | 
```
- **Existing Columns**: 6 columns (`time`, `zone_id`, `sensor_type`, `value`, `device_id`, `quality`).
- **Missing Column**: `strip_w` is **NOT yet present** in the running database!
- **Data Count**: `SELECT COUNT(*) FROM sensor_readings` is `0`.

### 4.3 Migration Feasibility Proof
We executed a transactional migration test on the live database:
```sql
BEGIN;
ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION;
CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;
ROLLBACK;
```
Result: **Execution succeeded cleanly**. TimescaleDB allows adding `strip_w` to the hypertable without table re-creation or data disruption.

---

## 5. Milestone 2 Verification Protocol

To ensure 100% adherence to acceptance criteria and prevent regression, the following protocol must be followed:

### 5.1 Protocol for Worker (Implementation & Deployment)

#### Step 1: Add Reconnection Resilience to `server/db.go`
Update `initDB()` in `server/db.go` so that if `DB.Ping()` fails on first contact, it retries with exponential backoff or runs a background retry loop (e.g., retrying every 2 seconds for up to 30 seconds) until TimescaleDB is ready, rather than giving up permanently.

#### Step 2: Run Unit & Integration Tests in Docker
Worker must execute tests with cached modules:
```powershell
docker run --rm -v "d:\ECON1\econ\server:/app" -v "gocache:/go/pkg/mod" -w /app golang:1.22-alpine go test -v ./...
```
Verification criteria:
- Both `econ` and `econ/simulation` packages report `PASS`.
- `TestAppendedFieldsRoundTrip` passes.
- `TestStripWIngestedAndSurfaced` passes.

#### Step 3: Apply TimescaleDB Schema Migration
Run the idempotent migration directly or verify it runs on container startup:
```powershell
docker exec econ_wifi_ch_a-db-1 psql -U econ -d econ -c "ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION; CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;"
```
Inspect schema:
```powershell
docker exec econ_wifi_ch_a-db-1 psql -U econ -d econ -c "\d sensor_readings"
```
Verification criteria:
- `strip_w` appears in `sensor_readings` with type `double precision`.
- Compatibility view `telemetry` is valid.

#### Step 4: Rebuild & Restart Server Container
Rebuild the server image and restart `econ_wifi_ch_a-server-1`:
```powershell
docker compose -p econ_wifi_ch_a build server
docker compose -p econ_wifi_ch_a up -d --no-deps server
```
Inspect startup logs:
```powershell
docker logs --tail 30 econ_wifi_ch_a-server-1
```
Verification criteria:
- Must see `[db] Connected to TimescaleDB.`
- Must NOT see `[db] DB not reachable`.

#### Step 5: Verify Batch Insert Execution & Data Ingestion
After the ESP32 publishes telemetry (every 5 seconds):
1. Check server logs for any batch insert errors:
   ```powershell
   docker logs --tail 100 econ_wifi_ch_a-server-1 | Select-String -Pattern "batch insert failed"
   ```
   Must return empty (no batch insert failures).
2. Query TimescaleDB for persisted readings:
   ```powershell
   docker exec econ_wifi_ch_a-db-1 psql -U econ -d econ -c "SELECT time, zone_id, sensor_type, value, device_id, quality, strip_w FROM sensor_readings WHERE sensor_type = 'stripW' OR strip_w IS NOT NULL ORDER BY time DESC LIMIT 5;"
   ```
   Verification criteria:
   - Rows exist with `sensor_type = 'stripW'`.
   - `strip_w` contains non-null floating point numbers matching the sensor readings (e.g. ~`185.4`).

---

### 5.2 Protocol for Reviewer & Challenger (Edge Cases & Invariants)

#### A. Reviewer Audit Checklist
1. **MQTT JSON Parsing**:
   - `server/mqtt.go`: `telemetryMsg.StripW` must be `*float64` (pointer) so absence is distinguishable from `0.0`.
2. **7-Column SQL Batch Query Match**:
   - `server/db.go`:
     - SQL statement columns: `(time, zone_id, sensor_type, value, device_id, quality, strip_w)` = 7 columns.
     - Parameter placeholders: `($1,$2,$3,$4,$5,$6,$7)` = 7 parameters per row.
     - Arguments slice: `args = append(args, r.t, r.zone, r.stype, r.value, dev, r.quality, stripW)` = 7 arguments.
     - Batch query limit: 512 rows * 7 parameters = 3,584 parameters (strictly below PostgreSQL prepared statement ceiling of 65,535).
3. **FlatBuffers Schema & Vtable Alignment**:
   - `server/schema/telemetry.fbs`: `stripW: float = 0;` at slot 11.
   - `server/schema/Telemetry/ZoneData.go`:
     - `builder.StartObject(12)`
     - `ZoneDataAddStripW`: `builder.PrependFloat32Slot(11, stripW, 0.0)`
     - `StripW()`: `rcv._tab.Offset(26)` (offset = 4 + 2 * 11 = 26).
   - Both Go FlatBuffers and TypeScript/frontend FlatBuffers must match slot 11 / offset 26.

#### B. Challenger Stress & Edge-Case Verification
1. **Edge Case 1: Absent / Nil `stripW` in Telemetry**:
   - Scenario: Telemetry published by firmware without `stripW` (e.g. older node or sensor error).
   - Ingestion behavior: `msg.StripW` is `nil`.
   - In `devices.go`: `fs.Omitted` is incremented; `persistMeasured` is NOT called for `stripW`.
   - In `writeLoop()`: `r.stripW == nil` and `r.stype != "stripW"` -> `stripW` variable is `nil` -> inserted into Postgres as SQL `NULL`.
   - Assertion: `sensor_readings` allows `NULL` in `strip_w` without constraint violation.
2. **Edge Case 2: Negative Wattage Readings**:
   - Scenario: ADC calibration offset drifts negative, sending `{"stripW": -2.5}`.
   - Database behavior: TimescaleDB `DOUBLE PRECISION` accepts negative numbers (no `CHECK (strip_w >= 0)` constraint exists).
   - UI / FlatBuffers behavior: `-2.5` serializes cleanly into `float32`.
3. **Edge Case 3: Extreme Spikes / Out-of-Range Values**:
   - Scenario: Inductive spike or open circuit produces `{"stripW": 99999.0}`.
   - Ingestion: Unmarshals into `float64`, fits inside `float32` (max ~`3.4e38`), inserts into DB without numeric overflow.
4. **Edge Case 4: High-Frequency Telemetry Bursts**:
   - Scenario: 50 nodes publishing simultaneously.
   - Channel buffer: `writeCh` has capacity 8,192.
   - Non-blocking enqueue: If saturated, `enqueue()` drops reading rather than stalling the 30 fps engine loop.
   - Batching: `writeLoop` drains up to 512 readings per database transaction every 500ms.
