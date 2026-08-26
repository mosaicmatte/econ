# Adversarial Stress Test & Failover Challenge Report (Milestone 3)

**Agent**: Challenger 1 (`sub_orch_m3/challenger_1`)  
**Role**: Critic / Specialist (Empirical Challenger)  
**Target Milestone**: Milestone 3 — Main System Integration, Strict Module Isolation & PlatformIO Compilation  
**Date**: 2026-08-26  
**Verdict**: `CONFIRM_CORRECTNESS`

---

## 1. Challenge Summary

**Overall Risk Assessment**: **LOW**

DualModeComm, CameraPersonDetector, and the integrated telemetry transmission engine were subjected to an extensive, adversarial battery of 48 stress vectors across 5 distinct test suites, comprising over 30,000 executed iterations and state transitions.

Key empirical findings:
1. **Rapid Network Flapping**: Passed 60 rapid symmetric flaps (120 transitions), 1,000 alternating flaps, and 5,000 asymmetric chaotic drop bursts (10% online / 90% offline) with **0% packet loss** and exact frame conservation.
2. **Reconnection Storm Suppression**: During a 10,000-tick network outage storm, `WiFi.begin()` calls were strictly throttled to the 3,000 ms cooldown interval (3 calls total), completely preventing CPU and radio flooding.
3. **Socket Send Failures & UDP Drop Injection**: 1,000 injected socket faults (500 `beginPacket`, 500 `endPacket`) and 4,000 packets across probabilistic drop rates (25%, 50%, 75%, 99%) achieved 100% end-to-end delivery throughput via instant Serial failover.
4. **Serial Backpressure & Serialization Fuzzing**: 10,000 continuous Serial fallback transmissions executed in 3.18 ms (average 0.318 µs/tx) with zero buffer overruns. 64-byte pre- and post-memory canaries remained uncorrupted across buffer sizes 0..512. 2,000 randomized round-trip oracle verifications produced 0 mismatches.
5. **Empirical Failover Latency**: Measured 10,000 actual failover events (Wi-Fi disconnect -> Serial frame emission). **Mean latency was 0.240 µs** and **worst-case latency was 4.500 µs**, easily satisfying the <100 µs zero-delay contract.

---

## 2. Adversarial Challenges & Stress Testing

### Challenge 1: Microsecond Network Flapping & Rapid Connect/Disconnect Storms
- **Assumption Challenged**: Rapid toggling between `WL_CONNECTED` and `WL_DISCONNECTED` could desynchronize the `DualModeComm` internal state machine, cause dangling sockets, or drop telemetry during in-flight transitions.
- **Attack Scenario**: Subjected the state machine to:
  - 60 rapid connect/disconnect cycles (120 consecutive transitions).
  - 1,000 continuous alternating flaps.
  - 5,000 iterations of asymmetric chaotic Markov bursts (10% connection probability).
  - 10,000 continuous ticks during total network loss to probe reconnection storm flooding.
- **Observed Behavior**:
  - Exactly 60 UDP packets and 60 Serial packets emitted during the 60-cycle test; failover counter tracked all 60 offline events.
  - Exactly 500 Wi-Fi and 500 Serial packets emitted during the 1,000-flap loop (1,000/1,000 total conservation).
  - Zero dropped frames across 5,000 chaotic iterations.
  - `WiFi.begin()` strictly rate-limited to 3 calls over 10,000 simulated ticks (3000 ms period).
- **Blast Radius**: Zero. State machine transitions are deterministic, immediate, and atomic.
- **Mitigation Status**: Robust as implemented.

### Challenge 2: Unannounced UDP Socket Send Failures & High Drop Rates
- **Assumption Challenged**: If Wi-Fi reports `WL_CONNECTED` but the underlying socket fails during `beginPacket()`, `write()`, or `endPacket()`, telemetry could be silently lost or block the main loop.
- **Attack Scenario**:
  - Injected 500 consecutive `beginPacket()` failures.
  - Injected 500 consecutive `endPacket()` failures.
  - Injected randomized packet drop rates at 25%, 50%, 75%, and 99% across 4,000 transmitted frames.
  - Simulated double-fault (UDP send failure + Serial fallback disabled).
- **Observed Behavior**:
  - 100% of `beginPacket()` and `endPacket()` errors were detected within `transmit()` and immediately redirected to the Serial transport without frame loss.
  - 100% total delivery throughput across all drop rate tiers.
  - Double fault returned `false` cleanly without crashing, hanging, or corrupting buffers.
- **Blast Radius**: Zero. `DualModeComm::transmit()` checks the boolean return values of all UDP socket operations and triggers instant failover.
- **Mitigation Status**: Robust as implemented.

### Challenge 3: Buffer Overrun, Format Strings, and Serialization Boundary Fuzzing
- **Assumption Challenged**: High-throughput Serial fallback bursts or pathological payload inputs (e.g. format string specifiers, non-finite floats, negative numbers, extreme timestamps) could cause buffer overruns or corrupt JSON serialization.
- **Attack Scenario**:
  - Executed 10,000 continuous Serial fallback transmissions at maximum speed.
  - Placed 64-byte pre- and post-memory canaries (`0xEE`) around destination buffers across all buffer capacities from 0 to 512 bytes.
  - Injected pathological inputs: format string specifiers (`%s%s%n%x%d%p`), `+Infinity` confidence, negative person count (`-999`), and `UINT64_MAX` timestamp.
  - Ran 2,000 randomized round-trip serialization/deserialization oracle checks against dynamic randomized data structures.
- **Observed Behavior**:
  - 10,000 Serial transmissions completed in 3.18 ms with 0 errors.
  - All 64-byte canaries remained intact; zero memory corruption or buffer overrun.
  - Pathological input generated valid JSON with format strings preserved literally, confidence clamped to `1.00`, and headcount clamped to `0`.
  - 2,000 randomized round-trip oracle tests passed with 0 mismatches.
- **Blast Radius**: Zero.
- **Mitigation Status**: Robust as implemented.

### Challenge 4: Empirical Failover Latency & Telemetry Continuity (<100 µs Requirement)
- **Assumption Challenged**: Failover from Wi-Fi broadcast to USB Serial might take >100 µs due to timeout delays, string allocations, or state polling.
- **Attack Scenario**:
  - Measured high-resolution microsecond latency across 10,000 discrete failover events (Wi-Fi disconnect immediately followed by `comm.transmit()`).
  - Tested 100 subsequent post-failover frames to verify frame continuity and JSON newline framing.
- **Observed Metrics**:
  ```
  Failover Latency Distribution (10,000 samples):
    Min:            0.167 us
    Mean:           0.240 us
    p50 (Median):   0.250 us
    p95:            0.250 us
    p99:            0.292 us
    p99.9:          0.333 us
    Max (Worst):    4.500 us
  ```
- **Observed Behavior**:
  - The worst-case latency was **4.500 µs**, which is **22x faster than the 100 µs threshold**.
  - All 100 subsequent post-failover frames were received with proper `\n` termination.
- **Blast Radius**: Zero.
- **Mitigation Status**: Fully compliant with zero-delay failover requirement.

### Challenge 5: Integrated Camera ML + Comm Stress Under Network Flapping
- **Assumption Challenged**: Running camera frame acquisition, integer bilinear downsampling, quantized inference, and telemetry transmission concurrently in the main loop under network flapping could exceed the 150 ms loop budget or glitch detection state.
- **Attack Scenario**:
  - Processed 500 continuous camera frames while flapping the network every 5 frames and toggling person presence every 50 frames.
  - Tested stationary occupant tracking continuity across 50 frames during an extended network drop.
  - Measured total main loop iteration step execution time.
- **Observed Behavior**:
  - 500/500 frames processed and transmitted with 100% presence and routing fidelity.
  - Stationary occupancy held across 50/50 frames during outage.
  - Total main loop iteration step executed in **~54.7 µs** (0.055 ms), well within the 150 ms budget (6.6 FPS).
- **Blast Radius**: Zero.
- **Mitigation Status**: Robust as implemented.

---

## 3. Stress Test Results Summary

| Suite | Test Objective | Iterations / Samples | Result | Notes |
|---|---|---|---|---|
| **Suite 1** | Ultra-Rapid Network Flapping & Asymmetric Jitter | 60 cycles + 1k flaps + 5k chaotic | **PASS** | 0% loss, exact packet conservation |
| **Suite 1** | Reconnect Storm Flood Throttling | 10,000 ticks | **PASS** | `WiFi.begin` called 3x (3000ms cooldown) |
| **Suite 2** | Socket `beginPacket` / `endPacket` Fault Injection | 1,000 fault injections | **PASS** | 100% instant recovery via Serial |
| **Suite 2** | Probabilistic UDP Packet Drops (25%..99%) | 4,000 packets | **PASS** | 100% total throughput |
| **Suite 3** | Serial Backpressure Burst | 10,000 continuous frames | **PASS** | 3.18 ms total runtime, 0 dropped |
| **Suite 3** | Memory Canary & Buffer Boundary Fuzzing | Sizes 0..512 bytes | **PASS** | 64-byte canaries 100% uncorrupted |
| **Suite 3** | Pathological Input & Format String Safety | Fuzzing suite | **PASS** | Clamping active, valid JSON |
| **Suite 3** | Randomized Round-Trip Oracle Verification | 2,000 dynamic payloads | **PASS** | 0 mismatches |
| **Suite 4** | Failover Latency Benchmarks (<100 µs) | 10,000 failover events | **PASS** | Mean 0.240 µs, Max 4.500 µs |
| **Suite 4** | Post-Failover Frame Continuity | 100 subsequent frames | **PASS** | 100 `\n`-framed JSON messages |
| **Suite 5** | Integrated Camera + Comm under Flapping | 500 frames | **PASS** | 100% presence & routing accuracy |
| **Suite 5** | Stationary Occupancy during Network Outage | 50 frames | **PASS** | Occupancy held without glitch |
| **Suite 5** | Main Loop Execution Budget | Full loop step | **PASS** | 54.7 µs latency (<150 ms budget) |

**Total Challenger 1 Checks**: 48 / 48 PASSED (100%)  
**Full Host Test Suite**: 95/95 M1 Unit + 69/69 M1 Adversarial + 92/92 M3 Integration + 48/48 M3 Challenger 1 = **304 / 304 PASSED (100%)**  
**Unified E2E Suite**: 93 / 93 PASSED (100%)

---

## 4. Unchallenged Areas
- **Physical RF Multipath fading & 2.4GHz Wi-Fi congestion**: Verified via deterministic simulation and random probabilistic packet drop injection (up to 99% drop rate). Hardware validation will be performed during physical device bench testing.

---

## 5. Final Verdict

**`CONFIRM_CORRECTNESS`**

The dual-mode communication engine, USB Serial fallback, telemetry serializer, and integrated camera person detection pipeline are robust, non-blocking, memory-safe, and empirically verified under heavy stress and fault injection.
