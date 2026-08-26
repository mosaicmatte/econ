# E2E Test Infra: ESP32 WROOM OV7670 Person Detection Module

## Test Philosophy
- **Opaque-Box & Requirement-Driven**: Tests are derived strictly from the requirements specified in `ORIGINAL_REQUEST.md` and interface contracts in `PROJECT.md`.
- **Methodology**: Category-Partition + Boundary Value Analysis (BVA) + Pairwise Combinatorial Testing + Real-World Workload Testing.
- **Host-Executable & Deterministic**: High-fidelity host runner in C++17 with comprehensive mocks/shims for hardware DMA, radio stacks, and sensor loops to enable rapid, hermetic verification on the build host with clear pass/fail exit codes (0 on success).

## Feature Inventory & Test Mapping
| # | Feature | Source (Requirement) | Tier 1 (Coverage) | Tier 2 (Boundary) | Tier 3 (Pairwise) | Tier 4 (Real-World) |
|---|---------|----------------------|:-----------------:|:-----------------:|:-----------------:|:-------------------:|
| 1 | Dual-Mode Comm Engine (Wi-Fi UDP:4210 & MQTT) | ORIGINAL_REQUEST R2 | 5 tests | 5 tests | ✓ | ✓ |
| 2 | Serial Fallback Engine (UART0 failover) | ORIGINAL_REQUEST R2 | 5 tests | 5 tests | ✓ | ✓ |
| 3 | Tracking Payload Schema (Topology/BIM JSON) | ORIGINAL_REQUEST R1, R2 | 5 tests | 5 tests | ✓ | ✓ |
| 4 | OV7670 Camera Driver & Ingestion | ORIGINAL_REQUEST R1 | 5 tests | 5 tests | ✓ | ✓ |
| 5 | TFLite Micro ML Person Detection Pipeline | ORIGINAL_REQUEST R1 | 5 tests | 5 tests | ✓ | ✓ |
| 6 | Frame Preprocessor (QQVGA to 96x96 int8) | ORIGINAL_REQUEST R1 | 5 tests | 5 tests | ✓ | ✓ |
| 7 | Main System Integration (PIR Replacement) | ORIGINAL_REQUEST R1 | 5 tests | 5 tests | ✓ | ✓ |
| 8 | Strict Module Isolation & Memory Constraints | ORIGINAL_REQUEST Arch, Comp | 5 tests | 5 tests | ✓ | ✓ |

## Test Architecture
- **Test Runner**: `edge/esp32/test/run_all_e2e_tests.sh`
- **Host Test Harness**: `edge/esp32/test/test_e2e_opaque_box.cpp`
- **Pass/Fail Semantics**: Returns exit code 0 if 100% of tests pass; non-zero if any assertion fails. Prints detailed per-tier and per-feature execution logs.
- **Directory Layout**:
  - `edge/esp32/test/arduino_shim.h`: Host-side shims for Arduino/ESP32 APIs (Serial, Preferences, millis, WiFi, UDP).
  - `edge/esp32/test/test_e2e_opaque_box.cpp`: Full 4-tier E2E opaque-box test suite.
  - `edge/esp32/test/run_all_e2e_tests.sh`: Shell runner compiling and executing all host tests.

## Tier Breakdown & Scenarios

### Tier 1 — Feature Coverage (≥5 tests per feature)
1. **Dual-Mode Comm**: Wi-Fi broadcast UDP initialization, broadcast packet transmission, MQTT telemetry publishing, connection state queries, auto-reconnect triggering.
2. **Serial Fallback**: Serial port initialization, automatic failover when Wi-Fi down, formatted UART frame output, zero-delay switching, fallback status telemetry.
3. **Tracking Payload Schema**: Serialization of presence flag, confidence score formatting (0.00-1.00), headcount field serialization, ISO8601/timestamp formatting, zone/sensor ID metadata validation.
4. **OV7670 Camera Driver**: SCCB register initialization sequence, clock generation (XCLK), I2S DMA frame buffer allocation, frame capture trigger, frame acquisition validation.
5. **TFLite Micro ML Pipeline**: Model weights array loading, tensor arena initialization (~80KB SRAM), input tensor quantization mapping, inference execution step, output score dequantization.
6. **Frame Preprocessor**: Grayscale extraction, downsampling/cropping from 160x120 QQVGA to 96x96, int8 value scaling (-128 to 127), aspect ratio preservation, invalid frame buffer rejection.
7. **Main System Integration**: PIR replacement with person detection boolean, detection polling loop, telemetry transmission dispatch, non-blocking execution, state transition notification.
8. **Module Isolation**: No modification to existing sensor drivers, isolated config namespace, flash/RAM footprint verification, clean header encapsulation, compile-time guard verification.

### Tier 2 — Boundary & Corner Cases (≥5 tests per feature)
1. **Comm Boundaries**: MTU buffer boundary (512B/1024B), rapid intermittent Wi-Fi drops, socket write failure, unconfigured SSID/broker, broadcast address subnet limits.
2. **Serial Boundaries**: Buffer overflow protection during high-frequency detection, baud rate boundary timing, corrupted character framing rejection, simultaneous Wi-Fi restoration during active serial write, null terminator integrity.
3. **Payload Boundaries**: Confidence at exact extremes (0.000, 1.000), headcount at boundary values (0, 1, 255, negative guard), maximum zone_id string length, empty payload buffer handling, JSON special character escaping.
4. **Camera Boundaries**: Completely black frame (all 0x00), saturated bright frame (all 0xFF), frame DMA timeout, partial/corrupted scanlines, high-frequency frame capture requests.
5. **ML Pipeline Boundaries**: Ambiguous detection threshold (score = 0.50), minimum score (0.00), maximum score (1.00), uninitialized tensor arena invocation, corrupted model data header.
6. **Preprocessor Boundaries**: Zero-dimension frame buffer, non-standard stride, odd dimension clipping, identical uniform pixel matrix, extreme brightness gradients.
7. **Integration Boundaries**: Sensor poll timeout, rapid person state toggling (presence flicker), camera frame drop during main loop cycle, memory exhaustion recovery, emergency restart trigger.
8. **Isolation Boundaries**: Multiple includes of camera headers without conflict, namespace collision defense, NVS preference key collision avoidance, stack depth limits under maximum load, zero memory leaks across 1000 cycles.

### Tier 3 — Cross-Feature Combinations (Pairwise Coverage)
1. **Wi-Fi Drop during Active High-Confidence Detection**: Verifies that when a person is detected (confidence > 0.85) and Wi-Fi drops mid-transmission, the payload is immediately rerouted to USB Serial without dropping the frame event.
2. **Camera Sensor DMA Glitch during Serial Fallback**: Verifies robust error recovery when camera DMA times out while the system is operating in serial fallback mode.
3. **Rapid Network Flapping with Continuous Inference**: Verifies stability when Wi-Fi connects/disconnects every 50ms while TFLite inference is running continuously.
4. **Payload Serializer Buffer Exhaustion under Dual Broadcast**: Verifies safe truncation or error return when serialized telemetry exceeds transport buffer.
5. **Model Re-initialization during Active Telemetry Stream**: Verifies that hot-reloading model parameters does not deadlock communication or corrupt active packets.
6. **Simultaneous Serial Command Ingestion and Wi-Fi Telemetry Out**: Verifies bi-directional stability when incoming commands arrive on Serial while Wi-Fi broadcasting is active.

### Tier 4 — Real-World Application Scenarios
1. **Scenario A: Continuous Room Occupancy Simulation**: Simulates a 24-hour office room cycle: empty room (0 people, low confidence), single worker entry (presence=true, count=1, conf=0.92), meeting group arrival (count=4), worker exit, and return to empty room. Verifies end-to-end topology/BIM tracking accuracy.
2. **Scenario B: Dynamic Network Degraded Mode Transition**: Simulates node startup with full Wi-Fi, gradual signal degradation, total AP disconnect, fallback to USB serial data collector, and subsequent Wi-Fi recovery with automatic reconnection and seamless broadcast resumption.
3. **Scenario C: Harsh Lighting & Visual Perturbation Scenario**: Simulates office light switching, sun glare, shadows, and darkness transitions to verify preprocessor stability, model inference confidence degradation gracefully handling false positives, and robust telemetry tagging.
4. **Scenario D: High-Throughput Topology BIM Event Burst**: Simulates rapid headcount fluctuations at an entryway threshold with high-frequency telemetry generation, validating message queue bounds, zero heap fragmentation, and guaranteed event delivery.
5. **Scenario E: Extended Long-Run Stability & Zero Memory Leakage**: Runs 10,000 continuous capture-inference-serialize-transmit cycles on the host harness, verifying constant memory allocation and zero leaked bytes.

## Coverage Thresholds
- **Tier 1**: ≥ 40 test cases (8 features × 5 tests)
- **Tier 2**: ≥ 40 test cases (8 features × 5 tests)
- **Tier 3**: ≥ 8 pairwise integration test cases
- **Tier 4**: ≥ 5 real-world workload scenarios
- **Total Minimum Target**: ≥ 93 comprehensive opaque-box test cases
