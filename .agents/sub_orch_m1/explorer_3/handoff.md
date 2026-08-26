# Handoff Report: Milestone 1 Test Infrastructure & Host Mocking

**Author:** Explorer 3 (Milestone 1 Sub-Orchestration)  
**Date:** 2026-08-26  
**Working Directory:** `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/explorer_3`  
**Handoff Type:** Hard (Task complete)

---

## 1. Observation

1. **Existing Host Testing Harness**:
   - `edge/esp32/test/run_host_tests.sh` lines 12-20:
     ```bash
     JSON=.pio/libdeps/esp32dev/ArduinoJson/src
     if [ ! -d "$JSON" ]; then
       echo "ArduinoJson not found at $JSON — run 'pio run -e esp32dev' once to fetch lib_deps." >&2
       exit 1
     fi
     OUT=$(mktemp -d)/cfgtest
     c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/host_config_test.cpp -o "$OUT"
     "$OUT"
     ```
   - Running `./edge/esp32/test/run_host_tests.sh` passes 100% on the build machine (macOS/Linux) in 0.05 seconds.
2. **Current Shim Coverage in `edge/esp32/test/arduino_shim.h`**:
   - Lines 16-27: defines `SerialShim Serial` with `printf`, `println`, `print`.
   - Lines 29-64: defines `PrefStore` and `Preferences` in-memory NVS shim.
   - Missing: Mock definitions for `WiFiClass`, `WiFiUDP`, `PubSubClient`, Arduino `String`, `IPAddress`, and controllable mock time (`millis()`, `micros()`, `advanceMockMillis()`).
3. **Target Scope Contracts**:
   - `PROJECT.md` lines 63-76 and `SCOPE.md` lines 48-60 specify `PersonTrackingData` struct (`person_detected`, `confidence`, `person_count`, `timestamp_ms`, `zone_id`, `sensor_id`) and serialization format.
   - `SCOPE.md` lines 34-47 specify non-blocking state machine (<0.2ms tick), UDP broadcast to port 4210, MQTT publish to `econ/telemetry/...`, and automatic zero-delay fallback to Serial output on UART0 115200.
4. **Header-Only Library Assets**:
   - `.pio/libdeps/esp32dev/ArduinoJson/src/ArduinoJson.h` exists and is header-only C++17 compatible.
   - `.pio/libdeps/esp32dev/PubSubClient/src/PubSubClient.h` is present.

---

## 2. Logic Chain

1. **Host-Side Independence Requirement (Observation 1 & 3)**:
   Off-target unit testing for `DualModeComm` and `TrackingPayload` must run completely on local host machines (macOS/Linux) without requiring physical ESP32 boards, real Wi-Fi networks, or active MQTT brokers.
2. **Mock Harness Design (Observation 2 & 4)**:
   By augmenting `arduino_shim.h` (and providing `test/WiFi.h`, `test/WiFiUdp.h`, `test/PubSubClient.h`), we can supply lightweight mock classes that intercept all network and serial I/O. Virtual clock manipulation (`setMockMillis`, `advanceMockMillis`) enables deterministic testing of periodic reconnection routines (e.g. 5000ms intervals) without artificial sleep delays.
3. **Test Domain Completeness (Observation 3)**:
   Designing 5 test groups in `test_m1_dual_mode.cpp` completely validates all acceptance criteria:
   - Group 1: JSON schema compliance, 64-bit timestamps, boundary canary safety.
   - Group 2: Wi-Fi connected mode (UDP port 4210 + MQTT publish, Serial silent).
   - Group 3: Wi-Fi disconnected fallback (Newline-delimited JSON on Serial, zero network emissions, <50µs zero-delay return).
   - Group 4: Dynamic online -> offline -> online failover and UDP fault recovery.
   - Group 5: Non-blocking execution (<0.2ms tick budget benchmark) and reconnect cooldown.
4. **Build Runner Integration (Observation 1)**:
   Updating `edge/esp32/test/run_host_tests.sh` to compile `test_m1_dual_mode.cpp` alongside `tracking_payload.cpp` and `dual_mode_comm.cpp` creates an automated one-command verification pipeline.

---

## 3. Caveats

1. **Hardware-Specific Timings**: The benchmark test verifies execution speed on the host CPU (<200 µs), which will be confirmed on physical ESP32 hardware in later milestones (M4).
2. **Library Dependency Assumption**: Assumes `.pio/libdeps/esp32dev/ArduinoJson` has been fetched (already verified present in the project).
3. **No other caveats**: The mock harness and test suite specifications are fully self-contained.

---

## 4. Conclusion

The host unit testing architecture and mock harness for Milestone 1 are completely designed, documented, and ready for implementation.
- Detailed design and test suite specification are in `.agents/sub_orch_m1/explorer_3/analysis.md`.
- Implementers can directly adopt the mock header designs and test scenarios for `test/test_m1_dual_mode.cpp`, `src/camera/tracking_payload.*`, and `src/camera/dual_mode_comm.*`.

---

## 5. Verification Method

To verify the test architecture once implemented:
1. Inspect the comprehensive analysis report at:
   `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/explorer_3/analysis.md`
2. Execute the host test command:
   ```bash
   ./edge/esp32/test/run_host_tests.sh
   ```
3. Invalidation Conditions:
   - If any mock class leaks state across test cases.
   - If `comm.tick()` exceeds 0.2 ms execution budget.
   - If buffer overflow canary bytes are modified during JSON serialization.
