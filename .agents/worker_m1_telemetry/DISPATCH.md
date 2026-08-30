# Dispatch for Worker M1 (Telemetry & Debug Logging)

## 2026-08-29T20:59:45Z
- **Working directory**: /Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m1_telemetry
- **Files Owned**:
  - `server/mqtt.go`
  - `server/mqtt_test.go`
  - `backend/forecasting/main.py`
  - `backend/forecasting/timesfm_forecaster.py`
  - `backend/forecasting/data_loader.py`
  - `edge/raspberry_pi/gateway.py`
  - `edge/esp32/esp32_emulator.py`
  - `ai_modules/branch_a_occupancy/yolo_bytetrack/yolo_tracker.py`
- **Reference**: `/Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md` and `/Users/nguyenhoangkhoi/Documents/econ/PROJECT.md`
- **Scope**:
  1. In `server/mqtt.go`, update `handleTelemetry` to output full raw JSON MQTT telemetry payloads in logs:
     `log.Printf("[mqtt] telemetry %s payload=%s occ=%d src=%q real_temp=%v (zone=%q)", suffix, string(payload), occ, msg.Source, msg.TempReal && msg.Temperature != nil, msg.Zone)`
  2. Implement debug logging / configurable log level via `LOG_LEVEL` / `DEBUG` environment variable in Go server.
  3. In `backend/forecasting/main.py`, `timesfm_forecaster.py`, and `data_loader.py`, configure Python `logging` with `DEBUG` level support (controlled by `LOG_LEVEL` env var, default DEBUG or configurable) and log full incoming request payloads and model outputs.
  4. In `edge/raspberry_pi/gateway.py`, `edge/esp32/esp32_emulator.py`, and `ai_modules/branch_a_occupancy/yolo_bytetrack/yolo_tracker.py`, enable debug level logging.
  5. Create automated test in `server/mqtt_test.go` that feeds MQTT telemetry messages and validates that the log output contains the full raw JSON telemetry payload.
  6. Run `go test -v ./...` in `server/` to verify all tests pass.
