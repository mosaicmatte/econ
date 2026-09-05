## 2026-08-26T04:09:25Z

You are Worker 1 for Milestone 1: Dual-Mode Communication & Tracking Payload Schema.
Your working directory is /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/worker_1.
Parent conversation ID: 3cee995f-cd2f-457a-bf5e-c3b5fab6c68f.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

MANDATORY INPUT FILES TO READ:
1. /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md
2. /Users/nguyenhoangkhoi/Documents/econ/PROJECT.md
3. /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/SCOPE.md
4. /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/explorer_1/analysis.md
5. /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/explorer_2/analysis.md
6. /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/explorer_3/analysis.md

SCOPE & EXCLUSIVELY OWNED FILES:
- `edge/esp32/src/camera/dual_mode_comm.h`
- `edge/esp32/src/camera/dual_mode_comm.cpp`
- `edge/esp32/src/camera/tracking_payload.h`
- `edge/esp32/src/camera/tracking_payload.cpp`
- `edge/esp32/test/test_m1_dual_mode.cpp`
- (And if needed for host testing: `edge/esp32/test/run_host_tests.sh` or supporting test header shims in `edge/esp32/test/`)

TASKS:
1. Implement `tracking_payload.h` and `tracking_payload.cpp`:
   - `PersonTrackingData` struct: `bool person_detected`, `float confidence`, `int person_count`, `unsigned long timestamp_ms`, `const char* zone_id`, `const char* sensor_id`, optional bounding box / telemetry metadata.
   - `size_t serializeTrackingPayload(const PersonTrackingData& data, char* buffer, size_t max_len);`
   - `size_t serializeTrackingPayloadForSerial(const PersonTrackingData& data, const char* topic, char* buffer, size_t max_len);`
   - Zero dynamic heap allocation, bounded execution time (<20µs), complete buffer overflow protection.
2. Implement `dual_mode_comm.h` and `dual_mode_comm.cpp`:
   - Non-blocking state machine with `<0.2ms` execution time per `tick()`.
   - Primary Mode: Wi-Fi UDP Broadcast on port 4210 to `255.255.255.255` and MQTT publishing hook (`econ/telemetry/<zone>`).
   - Fallback Mode: Automatic zero-delay failover to USB Serial (UART0 115200 baud) JSON stream when Wi-Fi is unavailable/disconnected.
   - Reconnection interval management without blocking.
3. Implement `edge/esp32/test/test_m1_dual_mode.cpp` and update `edge/esp32/test/run_host_tests.sh`:
   - Full host-based unit tests covering:
     * JSON schema serialization correctness, edge cases (0 count, 0.0 confidence, null pointers), buffer overflow safety.
     * Wi-Fi connected mode (UDP broadcast on port 4210 + MQTT).
     * Wi-Fi disconnected fallback mode (zero-delay Serial output).
     * Dynamic failover transitions (online -> offline -> online).
     * Non-blocking timing benchmark (<0.2ms per tick).
4. Run the build and host test commands:
   - Execute `./edge/esp32/test/run_host_tests.sh` and ensure all tests pass cleanly.
   - Check compilation with PlatformIO if available.
5. Write your handoff report to `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/worker_1/handoff.md` and send a completion message with test output.
