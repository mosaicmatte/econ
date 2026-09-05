# Milestone 1 Code Review & Verification Report

**Reviewer:** Reviewer 1 (Reviewer & Adversarial Critic)  
**Milestone:** Milestone 1 — Dual-Mode Communication & Tracking Payload Schema  
**Date:** 2026-08-26  
**Gate Verdict:** **APPROVE**

---

## 1. Observation

1. **Host Test Suite Execution**:
   - Command: `./test/run_host_tests.sh` executed from `/Users/nguyenhoangkhoi/Documents/econ/edge/esp32`.
   - Results:
     - `host_config_test`: 100% passed (0 failures).
     - `test_m1_dual_mode`: 95 / 95 tests passed (100% success, 0 failures).
   - Strict compilation test: `c++ -std=c++17 -Wall -Wextra -Werror -pedantic` compiled cleanly with 0 warnings and 0 errors.

2. **Source Code Implementation Inspection**:
   - `edge/esp32/src/camera/tracking_payload.h` & `tracking_payload.cpp`:
     - Implements `PersonTrackingData`, `serializeTrackingPayloadPtr`, `serializeTrackingPayloadForSerialPtr`, `serializeExtendedTrackingPayloadPtr`, and `deserializeTrackingPayload`.
     - Zero dynamic heap allocation on hot path (`malloc`, `new`, `std::string`, `String` are absent in serialization).
     - Strict bounds checking with `snprintf` return validation (`written < 0 || (size_t)written >= max_len`).
     - Clamps `confidence` to `[0.0, 1.0]` and `person_count` to `[0, INT_MAX]`.
     - Safe fallback strings (`"unknown_sensor"`, `"unknown_zone"`) on `nullptr`.
   - `edge/esp32/src/camera/dual_mode_comm.h` & `dual_mode_comm.cpp`:
     - Implements non-blocking state machine (`COMM_STATE_UNINITIALIZED`, `COMM_STATE_SERIAL_ONLY`, `COMM_STATE_CONNECTING`, `COMM_STATE_CONNECTED`, `COMM_STATE_DISCONNECTED`).
     - Primary transport: UDP broadcast to `255.255.255.255:4210` and MQTT publishing (`econ/telemetry/<zone_id>`).
     - Fallback transport: Automatic USB Serial (UART0 115200 baud with newline termination `\n`).
     - Socket-level error handling: If UDP broadcast socket returns error during active Wi-Fi, immediately triggers Serial failover without dropping telemetry frames.
     - Reconnection logic: Non-blocking rate-limiting timer using `reconnect_interval_ms` (5000ms cooldown) avoiding CPU spinning.

3. **Performance & Timing Benchmarks**:
   - `DualModeComm::tick()` execution time: **0.010 µs - 0.030 µs** per tick (budget: `< 200 µs` / `< 0.2ms`). Margin: >6,000x faster than requirement.
   - `serializeTrackingPayload()` execution time: **0.184 µs - 0.379 µs** per serialization (budget: `< 20 µs`). Margin: >50x faster than requirement.
   - Hot-path dynamic heap allocation: strictly **0 bytes**.

4. **Adversarial Stress Test Results**:
   - Executed standalone stress test with extreme edge cases (`NaN`, `+Inf`, `-Inf`, `INT_MIN`, `INT_MAX`, `UINT64_MAX`, 1000-character string overflows, 50,000 rapid online/offline state cycles, memory placement canary buffers).
   - All tests passed with zero memory corruptions, zero buffer overruns, and zero crashes.

5. **Integrity Violation Analysis**:
   - No hardcoded test responses or bypasses.
   - No dummy/facade implementations.
   - Genuine parsing, formatting, socket transmission, and state transitions.

---

## 2. Logic Chain

1. **R2 Requirement Alignment**: `ORIGINAL_REQUEST.md` (R2) requires dual-mode communication (Wi-Fi real-time broadcast primary + automatic USB Serial fallback). `PROJECT.md` and `SCOPE.md` specify UDP port 4210, MQTT hook, UART0 115200 baud JSON fallback, `<0.2ms` per tick, and standardized schema.
2. **Implementation Verification**: In `dual_mode_comm.cpp`, `transmit()` first verifies `isPrimaryTransportActive()`. If online, it sends UDP broadcast packet and optional MQTT packet. If offline or if socket write fails, it routes the payload to `_serial` with newline termination.
3. **Memory Safety Verification**: Memory buffer sizes are strictly validated. Stack-allocated buffers are used throughout `transmit` and serialization. Offsets in multi-segment serialization (`serializeExtendedTrackingPayloadPtr`) are checked before each write.
4. **Timing Verification**: Execution profiling across 10,000 iterations demonstrates `tick()` runs in `<0.03 µs`, ensuring camera I2S DMA frame capture and TFLite Micro inference will never be starved.
5. **Contract Conformance**: Data structures and method signatures conform to `PROJECT.md` specifications.

---

## 3. Caveats

- Milestone 1 encompasses exclusively the communication layer, payload serializer, and unit test harness. Camera I2S DMA driver and TFLite Micro inference belong to Milestone 2.
- Main loop integration into `src/main.cpp` belongs to Milestone 3.

---

## 4. Conclusion

**Verdict: APPROVE**

Milestone 1 is completely, robustly, and genuinely implemented. All 95 host test assertions pass with 100% success. Zero dynamic memory allocation is achieved on the hot path, memory buffers are safe and guarded against overflow, timing requirements are satisfied with substantial margin, and no integrity violations exist.

---

## 5. Verification Method

To independently reproduce and verify this review:

1. **Execute Host Test Suite**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ/edge/esp32
   ./test/run_host_tests.sh
   ```
   *Expected Result*: Output ends with `Summary: 95 / 95 tests passed`, `Result: PASSED (100% SUCCESS)`, and exit code 0.

2. **Execute Strict Compilation & Adversarial Stress Tests**:
   ```bash
   cd /Users/nguyenhoangkhoi/Documents/econ
   c++ -std=c++17 -Wall -Wextra -Werror -pedantic \
       -I edge/esp32/.pio/libdeps/esp32dev/ArduinoJson/src \
       -I edge/esp32/src \
       -I edge/esp32/src/camera \
       -I edge/esp32/test \
       edge/esp32/src/camera/tracking_payload.cpp \
       edge/esp32/src/camera/dual_mode_comm.cpp \
       .agents/sub_orch_m1/reviewer_1/scratch/adversarial_stress_test.cpp \
       -o .agents/sub_orch_m1/reviewer_1/scratch/adversarial_test && \
   .agents/sub_orch_m1/reviewer_1/scratch/adversarial_test
   ```
   *Expected Result*: `All adversarial stress tests PASSED!` with exit code 0.
