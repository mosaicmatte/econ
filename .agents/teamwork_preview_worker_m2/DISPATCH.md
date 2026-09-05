## 2026-09-04T06:43:48Z

DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

You are teamwork_preview_worker_m2.
Your working directory is d:\ECON1\econ\.agents\teamwork_preview_worker_m2.

Read these documents before doing any work:
- d:\ECON1\econ\.agents\ORIGINAL_REQUEST.md
- d:\ECON1\econ\PROJECT.md
- d:\ECON1\econ\.agents\teamwork_preview_explorer_survey_backend\handoff.md
- d:\ECON1\econ\.agents\teamwork_preview_explorer_survey_backend\analysis.md

Your exclusive write ownership:
- server/mqtt.go
- server/db.go
- server/db/init.sql
- server/devices.go
- server/simulation/engine.go
- server/schema/Telemetry/ZoneData.go
- Any test files in server/

Milestone M2 Objective (Requirement R2):
1. In server/mqtt.go:
   - Add StripW *float64 `json:"stripW"` to telemetryMsg struct.
   - In handleTelemetry(): pass StripW into simulation.Measurement{StripW: msg.StripW, ...}.
2. In server/db.go:
   - In migrateSchema():
     - Add `ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION;`
     - Add `CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;`
   - In reading struct: add stripW *float64.
   - In writeLoop(): update batch insert statement and parameters to 7 columns:
     INSERT INTO sensor_readings (time, zone_id, sensor_type, value, device_id, quality, strip_w) VALUES ($1,$2,$3,$4,$5,$6,$7)...
   - In seriesAllowed map: add `"stripW": true` so /api/series queries work.
3. In server/db/init.sql:
   - Add strip_w DOUBLE PRECISION to CREATE TABLE sensor_readings.
   - Add CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;.
4. In server/devices.go:
   - In (*deviceRegistry).observe(): when msg.StripW != nil, track("stripW", *msg.StripW) and queue reading to writeCh with stripW: msg.StripW.
5. In server/simulation/engine.go:
   - Add StripW *float64 to Measurement struct.
   - Add HwStripW float64, HwStripAt time.Time, and stripFresh() method to ZoneSim.
   - In IngestTelemetry(): if m.StripW != nil { z.HwStripW = *m.StripW; z.HwStripAt = time.Now() }.
   - In HardwareNode struct: add StripW float64 `json:"stripW"`.
   - In HardwareStatus(): populate StripW (using z.stripFresh()).
   - In FlatBuffers zone serialization: call Telemetry.ZoneDataAddStripW(b, float32(stripW)).
6. In server/schema/Telemetry/ZoneData.go:
   - Add StripW() float32 getter (vtable offset 26) and ZoneDataAddStripW builder (field 11).
7. Apply migration to running database:
   - Run ALTER TABLE on running db container:
     docker exec -i econ_wifi_ch_a-db-1 psql -U econ -d econ -c "ALTER TABLE sensor_readings ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION; CREATE OR REPLACE VIEW telemetry AS SELECT * FROM sensor_readings;"
8. Run verification:
   - Run tests: docker run --rm -v "d:\ECON1\econ\server:/app" -w /app golang:1.22-alpine go test -v ./...
   - Check DB schema: docker exec -i econ_wifi_ch_a-db-1 psql -U econ -d econ -c "\d sensor_readings"
   - Check row count preservation: docker exec -i econ_wifi_ch_a-db-1 psql -U econ -d econ -c "SELECT COUNT(*), COUNT(strip_w) FROM sensor_readings;"
   - Recompile/restart server container: docker compose -f server/docker-compose.yml up -d --build server
   - Check docker logs: docker logs --tail 50 econ_wifi_ch_a-server-1
   - Verify 0 SQL errors and 0 test failures.

Write handoff.md in your working directory with all commands and verification results.
Send a message to parent orchestrator (3d053cc7-022e-47ba-9164-0325863f09a2) with your completion report.
