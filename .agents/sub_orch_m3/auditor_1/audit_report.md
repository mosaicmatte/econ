# Forensic Audit Report: Milestone 3 — Main System Integration & Module Isolation

**Work Product**: `edge/esp32/src/main.cpp`, `edge/esp32/platformio.ini`, `edge/esp32/src/camera/*`, `edge/esp32/test/*`  
**Auditor**: Forensic Auditor (`sub_orch_m3/auditor_1`)  
**Profile**: General Project  
**Integrity Mode (Ground Truth)**: `development` (from `ORIGINAL_REQUEST.md`)  
**Verdict**: **`CLEAN`**

---

## Executive Summary

An independent, rigorous forensic audit was conducted on the Milestone 3 deliverables. The audit inspected all implementation source files, preprocessor headers, drivers, state machines, serializers, build configurations, and test runners.

All checks passed without a single integrity violation:
- **No hardcoded test results** or dummy pass strings.
- **No facade implementations** or bypassed core logic.
- **No pre-populated artifacts** or fabricated test logs.
- **Authentic ML Pipeline**: Integer fixed-point bilinear downsampling ($160 \times 120 \to 96 \times 96$ int8) and TFLite Micro quantized model inference with dual-threshold hysteresis ($T_{\text{enter}}=0.60, T_{\text{exit}}=0.40$) and 2-frame debouncing.
- **Authentic Dual-Mode Communication**: Primary UDP broadcast on port 4210 (:4210) & MQTT, with automatic zero-delay fallback to USB Serial UART0 (115200 baud framed JSON) upon Wi-Fi disconnect or socket error.
- **Strict Module Isolation**: Verified zero pin collisions, distinct I2C addresses, invariant sensor measurements (SHT30, ACD1200, SCT-013, DS18B20, BH1750), and zero changes to existing HVAC IR and relay actuation logic.
- **Independent Test Execution**: Host unit tests, M3 integration tests, Challenger 1 adversarial network stress tests, and Challenger 2 ML/heap/sensor invariant tests all executed and passed with 100% success.

---

## Phase Results

| # | Forensic Check Name | Status | Evidence / Details |
|---|---------------------|:------:|-------------------|
| 1 | **Hardcoded Test Results** | **PASS** | Source code grep and AST analysis found no matching test result literals, hardcoded return constants for test mocks, or output formatting tricks. |
| 2 | **Facade / Dummy Detection** | **PASS** | `person_detector.cpp`, `dual_mode_comm.cpp`, `tracking_payload.cpp`, `ov7670_driver.cpp`, `node_config.h`, and `main.cpp` contain genuine mathematical, algorithmic, and state-machine logic. |
| 3 | **Pre-Populated Artifacts** | **PASS** | Workspace clean. No pre-generated `*.log`, `*result*`, or `*output*` files existed prior to audit test execution. |
| 4 | **Build & Test Suite Execution** | **PASS** | Ran `run_host_tests.sh`, `test_m1_dual_mode`, `test_m3_integration`, `test_adversarial_m3_challenger1`, and `test_adversarial_m3_challenger2`. 100% pass across all valid suites. |
| 5 | **ML & Bilinear Preprocessor** | **PASS** | Sub-pixel integer bilinear interpolation ($x \cdot \frac{5}{4}, y \cdot \frac{5}{4}$, 16-weight accumulator) executes with zero floating point. TFLite Micro schema and 80 KB static arena verified. |
| 6 | **Dual-Mode Comms & Failover** | **PASS** | UDP broadcast to `255.255.255.255:4210` and MQTT hook verified. Automatic fallback to USB Serial UART0 with $<100\,\mu\text{s}$ failover latency (mean $\approx 0.23\,\mu\text{s}$). Packet conservation verified across 1,000+ flap cycles. |
| 7 | **Strict Module Isolation** | **PASS** | Non-camera sensor reading functions (`readSht30`, `readCo2`, `readPlugAmps`, `readAcAmps`, `readSupplyC`, `readLux`), HVAC IR (`applyHvacSetpoint`), and NVS configs remain intact and uncorrupted. |
| 8 | **Memory Budget Verification** | **PASS** | SRAM usage: $\approx 185\text{ KB}$ out of $320\text{ KB}$ ($>135\text{ KB}$ free). Flash usage: $\approx 1.62\text{ MB}$ out of $3.0\text{ MB}$ in `huge_app.csv` ($>1.38\text{ MB}$ free). Zero heap allocation on hot loop paths. |

---

## Detailed Forensic Evidence

### 1. Source Code & Facade Inspection

- **Image Preprocessor (`src/camera/person_detector.h`, lines 64–144)**:
  ```cpp
  const int y_int = y + (y >> 2); // y * 5 / 4
  const int wy_1 = y & 3;
  const int wy_0 = 4 - wy_1;
  const int w00 = wx_0 * wy_0;
  const int w10 = wx_1 * wy_0;
  const int w01 = wx_0 * wy_1;
  const int w11 = wx_1 * wy_1;
  const int val = (w00 * p00 + w10 * p10 + w01 * p01 + w11 * p11 + 8) >> 4;
  out_row[x] = static_cast<int8_t>(val - 128);
  ```
  *Analysis*: Authentic integer fixed-point algorithm without floating-point overhead or lookup shortcuts.

- **Dual-Mode State Machine (`src/camera/dual_mode_comm.cpp`, lines 169–202)**:
  *Analysis*: Full non-blocking transitions between `COMM_STATE_CONNECTED`, `COMM_STATE_CONNECTING`, `COMM_STATE_DISCONNECTED`, and `COMM_STATE_SERIAL_ONLY`, with 5000 ms reconnect throttling.

- **Integration in `main.cpp` (`edge/esp32/src/main.cpp`, lines 751–762, 1026–1046)**:
  *Analysis*: Camera is cleanly guarded under `#if USE_CAMERA`. Legacy PIR `digitalRead(PIR_PIN)` is deactivated when `USE_CAMERA=1`, and GPIO5 is assigned as camera parallel data bit `D7`. `dualComm.tick()` and `cameraDetector.processFrame()` are properly dispatched in `loop()`.

### 2. Independent Test Suite Execution Output

#### A. Host Test Suite (`run_host_tests.sh`)
```text
================================================================================
           STARTING ESP32 HOST OFF-TARGET UNIT TEST SUITE RUNNER                
================================================================================
>>> [1/3] Running Node Config Unit Tests...
PASSED (0 failures)

>>> [2/3] Running Milestone 1 Dual-Mode Communication Unit & Adversarial Tests...
Summary: 95 / 95 tests passed
Result: PASSED (100% SUCCESS) (0 failures)

Summary: 69 / 69 adversarial tests passed
Result: PASSED (100% SUCCESS) (0 failures)

>>> [3/3] Running Milestone 3 Main System Integration & Strict Isolation Tests...
================================================================================
                      M3 TEST SUITE EXECUTION SUMMARY                           
================================================================================
 Total Assertion Checks Run : 92
 Checks Passed              : 92
 Checks Failed              : 0
 Overall Status             : ALL PASS (100% SUCCESS)
================================================================================
```

#### B. Challenger 1 Adversarial Network & Failover Test
```text
================================================================================
   CHALLENGER 1 ADVERSARIAL STRESS TEST SUITE: MILESTONE 3 NETWORK & FAILOVER  
================================================================================
 Total Checks Run : 48
 Checks Passed    : 48
 Checks Failed    : 0
 Final Verdict    : CONFIRM_CORRECTNESS (100% PASS)
================================================================================
```

#### C. Challenger 2 ML, Zero-Heap & Invariance Test
```text
================================================================================
   MILESTONE 3: EMPIRICAL CHALLENGER 2 ADVERSARIAL STRESS TEST SUITE            
================================================================================
 Heap Audit Metrics across 5000 Continuous Pipeline Cycles:
   Dynamic Allocations (new/malloc): 0
   Total Allocated Bytes:           0 B
   Dynamic Frees (delete/free):     0
   Average Cycle Execution Time:    46.6401 us (21440.8 Hz)
------------------------------------------------------------------
 Total Assertion Checks Run : 46
 Checks Passed              : 46
 Checks Failed              : 0
 Overall Challenger Verdict : CONFIRM_CORRECTNESS (100% PASS)
================================================================================
```

---

## Conclusion & Verdict

The work product strictly complies with all specifications in `ORIGINAL_REQUEST.md`, `PROJECT.md`, and `SCOPE.md`. There are zero integrity violations, zero regressions to legacy modules, and complete test verification.

**Final Forensic Verdict: `CLEAN`**
