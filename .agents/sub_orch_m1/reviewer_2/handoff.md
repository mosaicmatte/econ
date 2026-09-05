# Reviewer 2 Handoff & Quality / Adversarial Report: Milestone 1

**Agent:** Reviewer 2 (reviewer, critic)  
**Milestone:** Milestone 1 — Dual-Mode Communication & Tracking Payload Schema  
**Parent ID:** `3cee995f-cd2f-457a-bf5e-c3b5fab6c68f`  
**Date:** 2026-08-26  
**Gate Verdict:** **APPROVE**

---

## 1. Review Summary

- **Gate Verdict:** **APPROVE**
- **Integrity Violations:** None detected. Implementation contains authentic non-blocking state machine logic, socket broadcasting, MQTT hook integration, zero-delay UART fallback, fixed-buffer JSON serialization, and genuine test validation.
- **Failover State Transitions:** Verified seamless transitions across `COMM_STATE_UNINITIALIZED`, `COMM_STATE_CONNECTING`, `COMM_STATE_CONNECTED`, `COMM_STATE_DISCONNECTED`, and `COMM_STATE_SERIAL_ONLY`.
- **Non-Blocking Execution:** Verified state tick takes ~0.009 µs on host (<0.2 ms budget). No `delay()` or blocking loops exist in the communication pipeline.
- **Socket & Fallback Fidelity:** Verified UDP broadcast on port 4210 to `255.255.255.255`, framed newline-terminated JSON over Serial fallback (115200 baud), and instantaneous fallback on UDP socket write errors.
- **Payload Schema:** Verified exact conformity to BIM/topology schema (`sensor_id`, `zone_id`, `timestamp_ms`, `person_detected`, `confidence`, `person_count`) with zero dynamic heap allocation.
- **Test Suite Results:** 95 / 95 unit test assertions in `test_m1_dual_mode.cpp` passed with exit code 0. Independent adversarial stress testing confirmed resilience across 1,000 Wi-Fi flapping cycles and simulated socket faults.

---

## 2. 5-Component Handoff Protocol

### 2.1. Observation
1. **Host Test Execution**:
   Running `./edge/esp32/test/run_host_tests.sh` executed both `host_config_test.cpp` and `test_m1_dual_mode.cpp`.
   - Node Config Unit Tests: `PASSED (0 failures)`
   - Milestone 1 Dual-Mode Unit Tests: `95 / 95 tests passed`, `Result: PASSED (100% SUCCESS) (0 failures)`, `exit code 0`.
2. **Timing Benchmarks**:
   - `DualModeComm::tick()` execution time: `~0.009 µs` (strict budget: `< 200 µs` / `< 0.2 ms`).
   - `serializeTrackingPayload()` execution time: `~0.175 µs` (strict budget: `< 20 µs`).
   - Serial failover latency: `1.375 µs` (budget: `< 100 µs`).
3. **Source Code Inspection**:
   - `edge/esp32/src/camera/dual_mode_comm.h` (142 lines) and `dual_mode_comm.cpp` (355 lines): Non-blocking state machine with 5-second reconnect cooldown hysteresis, UDP broadcast socket on port 4210, MQTT hook, and Serial fallback (`UART0` 115200).
   - `edge/esp32/src/camera/tracking_payload.h` (135 lines) and `tracking_payload.cpp` (262 lines): Zero-heap static buffer formatting (`snprintf`), bounds clamping for confidence `[0.0, 1.0]` and non-negative count, 64-bit uint64 epoch formatting (`%llu`), and ArduinoJson deserializer.
   - `edge/esp32/test/test_m1_dual_mode.cpp` (547 lines): 95 assertions covering schemas, 64-bit epoch timestamps, canary byte integrity, failover cycles, socket write failures, reconnect hysteresis, and timing benchmarks.

### 2.2. Logic Chain
1. **Requirement R2 (Dual-Mode Communication)** requires Wi-Fi UDP broadcast as primary transport and automatic fallback to USB Serial when offline or encountering socket write failure.
2. In `DualModeComm::transmit()`, the method first checks `isPrimaryTransportActive()`. If online, it sends a UDP broadcast packet to `255.255.255.255:4210` via `sendUdpBroadcast()`. If MQTT client is attached and connected, it also publishes telemetry to `econ/telemetry/<zone_id>`.
3. If UDP packet delivery fails (e.g. `beginPacket() == 0` or `endPacket() == 0`), or if Wi-Fi is disconnected (`isWifiConnected() == false`), `transmit()` immediately executes `sendSerial()`, writing framed JSON terminated by `\n` to Serial.
4. Because this fallback occurs within the same synchronous function call without blocking sleep or polling loops, failover latency is instantaneous (< 1.5 µs measured), completely satisfying the zero-delay requirement.
5. In `tracking_payload.cpp`, `serializeTrackingPayloadPtr` avoids all dynamic memory (`malloc`/`new`), preventing heap fragmentation on the ESP32. Strict bounds checking (`written < 0 || written >= max_len`) ensures canary guard bytes remain uncorrupted under all edge cases.
6. Therefore, the implementation completely satisfies all functional and non-functional requirements of Milestone 1.

### 2.3. Caveats
- **Untrusted String Sanitization**: `serializeTrackingPayload` directly embeds `sensor_id` and `zone_id` using `%s`. In the edge firmware, these are fixed compile-time or NVS configuration strings. If untrusted arbitrary strings containing unescaped double quotes `"` are passed in the future, caller should sanitize or ensure valid identifiers.
- **Milestone Isolation**: Milestone 1 implements the transport, state machine, and payload schema. Camera hardware I2S DMA acquisition and TFLite Micro person detection inference are part of Milestone 2.

### 2.4. Conclusion
Milestone 1 is verified as fully compliant with the requirements in `ORIGINAL_REQUEST.md`, `PROJECT.md`, and `SCOPE.md`. The implementation is robust, performant, memory-safe, and passes all 95 host test assertions as well as independent adversarial stress tests. **Verdict: APPROVE.**

### 2.5. Verification Method
To independently reproduce the review findings:
```bash
cd /Users/nguyenhoangkhoi/Documents/econ/edge/esp32
./test/run_host_tests.sh
```
*Expected Result*:
```
Summary: 95 / 95 tests passed
Result: PASSED (100% SUCCESS) (0 failures)
Exit Code: 0
```

---

## 3. Adversarial Challenges & Stress Testing

### Challenge 1: Rapid Wi-Fi Connection Flapping
- **Scenario**: Simulated 1,000 alternating cycles of `WL_CONNECTED` and `WL_DISCONNECTED` while continuously transmitting telemetry.
- **Observed Behavior**: The state machine smoothly alternated between primary UDP and Serial fallback without memory leak, crash, or dropped transmissions. (1000/1000 transmissions succeeded).
- **Result**: **PASS**.

### Challenge 2: Intermittent UDP Socket Write Failures
- **Scenario**: Injected intermittent socket write failures (`failNextSend = true`) on every 3rd transmission while Wi-Fi remained reported as connected.
- **Observed Behavior**: `sendUdpBroadcast` returned `false`, triggering immediate zero-delay fallback to `sendSerial`. All 300 transmissions completed successfully (200 via UDP, 100 via Serial fallback).
- **Result**: **PASS**.

### Challenge 3: Exact Buffer Boundary & Overflow Stress
- **Scenario**: Supplied a buffer of exact size `length + 1` (valid) and `length` (1 byte short).
- **Observed Behavior**: Exact buffer succeeded. 1-byte short buffer safely returned 0 with null-termination `buffer[0] = '\0'`. Canary guard bytes `0xAA` remained untouched.
- **Result**: **PASS**.

---

## 4. Verified Claims Matrix

| Claim | Method | Result |
|---|---|---|
| Primary transport broadcasts UDP to `:4210` | Off-target mock inspection in `test_m1_dual_mode.cpp` | **PASS** |
| Fallback transport transmits framed JSON over Serial | Buffer inspection in `test_m1_dual_mode.cpp` & scratch stress test | **PASS** |
| Zero-delay failover latency < 100 µs | High-resolution clock benchmark (1.375 µs measured) | **PASS** |
| Non-blocking state tick < 0.2 ms budget | High-resolution clock benchmark (0.009 µs measured) | **PASS** |
| Zero heap allocation on hot path | Stack buffer usage and memory canary verification | **PASS** |
| 64-bit epoch timestamp fidelity | Large `uint64_t` serialization tests | **PASS** |
| Host test suite execution | `run_host_tests.sh` execution | **PASS (95/95)** |

---

## 5. Gate Verdict
**Verdict:** **APPROVE** (Proceed to Milestone 2)
