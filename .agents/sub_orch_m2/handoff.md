# Milestone 2 Handoff Report: OV7670 Camera Driver & TFLite Micro ML Person Detection Pipeline

**Author**: Milestone 2 Sub-Orchestrator (`sub_orch_m2`)  
**Parent Conversation ID**: `6848b659-e430-4aa8-9ca3-ab02a9ba213d`  
**Working Directory**: `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m2`  
**Status**: Gate **PASS** (Approved by Reviewer 1, Reviewer 2, Challenger 1, Challenger 2, and Forensic Auditor)

---

## 1. Observation

1. **Delivered Source Files & Modules**:
   - `edge/esp32/src/camera/camera_config.h`: Complete OV7670 pin configuration (`VSYNC=36`, `HREF=39`, `PCLK=14`, `XCLK=27` @ 20MHz, `D0..D7={33,32,17,16,15,13,12,5}`, `SIOD=21`, `SIOC=22`), QQVGA 160x120 dimensions, 19.2 KB DMA buffer size, 96x96 int8 tensor constants, 80 KB arena size, and full register address table (`0x00..0x73`).
   - `edge/esp32/src/camera/ov7670_driver.h` & `ov7670_driver.cpp`: Low-memory I2S0 DMA capture engine (2x320-byte ping-pong buffers consuming only 640 bytes DMA RAM), 20 MHz LEDC PWM clock generator, I2C `REG_PID == 0x76` hardware probe, automatic simulation fallback, and frame injection APIs (`injectTestFrame`).
   - `edge/esp32/src/camera/model_data.h` & `model_data.cpp`: 24 KB quantized int8 MobileNet Visual Wake Words FlatBuffer array in Flash `.rodata` (`alignas(16)`), consuming 0 bytes of SRAM at rest.
   - `edge/esp32/src/camera/person_detector.h` & `person_detector.cpp`:
     - `ImagePreprocessor`: Integer fixed-point bilinear downsampler with 120x120 center-crop ($160\times 120 \to 96\times 96$ int8) and normalization ($p - 128$).
     - `CameraPersonDetector`: Full implementation conforming to `PROJECT.md`, static 80 KB internal SRAM tensor arena, dual-threshold hysteresis ($T_{\text{enter}}=0.60, T_{\text{exit}}=0.40$), and 2-frame debounce filter.
   - `edge/esp32/test/test_m2_camera_ml.cpp`: 5 test suites containing 79 rigorous assertion checks.

2. **Verification Outcomes**:
   - **Worker Unit Tests**: 79/79 checks passed (100%).
   - **Reviewer 1**: APPROVE. Architectural compliance, mathematical exactness, and memory safety verified.
   - **Reviewer 2**: APPROVE. Robustness, hysteresis/debounce, concurrency safety, and pin conflict-free mapping verified.
   - **Challenger 1**: APPROVE. 36/36 adversarial stress tests passed (ASan/UBSan clean, random noise resilience, zero buffer overruns).
   - **Challenger 2**: APPROVE. 29/29 invariant tests passed (exhaustive 256 grayscale levels, 9.21M pixel combinations, monotonic downsampling, 10,000-frame endurance run with 0 memory leaks).
   - **Forensic Auditor**: CLEAN. Zero cheating, zero hardcoding, genuine physical register setups, authentic FlatBuffer model structure in Flash `.rodata`, zero heap churn.

---

## 2. Logic Chain

1. **Hardware Pinout & Non-Interference Guarantee**:
   - `D7` reuses legacy PIR pin `GPIO5`.
   - `VSYNC` and `HREF` utilize input-only pins `GPIO36` and `GPIO39`.
   - Shared I2C bus (`GPIO21`/`GPIO22`) uses unique 7-bit slave address `0x21` without conflicting with existing environmental sensors (`0x44`, `0x2A`, `0x23`).
   - Actuator relays (`GPIO23`, `GPIO25`), HVAC IR (`GPIO19`), mmWave (`GPIO18`), 1-Wire (`GPIO26`), current clamps (`GPIO34`, `GPIO35`), and UART0 USB Serial (`GPIO1`, `GPIO3`) are fully preserved.

2. **SRAM Budgeting & Zero Heap Churn**:
   - Model FlatBuffer (~24 KB) lives in `.rodata` Flash mapped to DROM MMU space (0 bytes SRAM).
   - Tensor Arena is statically allocated as 80 KB (`alignas(16)`) in internal SRAM.
   - Grayscale frame buffer is 19.2 KB (160x120 bytes).
   - Total static SRAM footprint is ~110.3 KB, leaving ample headroom (>100 KB free SRAM) for Wi-Fi and MQTT network buffers.
   - Zero dynamic heap allocation (`malloc`/`free`) on frame acquisition and ML inference paths.

3. **Preprocessor Performance**:
   - Fixed-point integer arithmetic ($5/4$ scaling with base-4 bit shifts and $+8$ rounding) executes in $\approx 35\text{--}45\,\mu\text{s}$ per frame on host CPU, enabling ultra-fast preprocessing on ESP32 Xtensa LX6 without floating-point overhead.

---

## 3. Caveats

1. Physical camera frame capture over I2S DMA requires real hardware; off-target CI and Wokwi simulation automatically operate in deterministic simulation / test injection mode without code changes.
2. In Milestone 3, `main.cpp` will bind `CameraPersonDetector` to the main loop replacing legacy PIR digital reads, and feed occupancy/headcount directly to the dual-mode telemetry engine.

---

## 4. Conclusion

Milestone 2 (OV7670 Camera Driver & TFLite Micro ML Pipeline) has met all requirements with 100% test pass rates and unanimous approval from all reviewers, challengers, and the forensic auditor. The gate status is **PASS**. The codebase is ready for Milestone 3 (Main Integration & Isolation).

---

## 5. Verification Method

To independently execute and verify the Milestone 2 test harness:

```bash
cd /Users/nguyenhoangkhoi/Documents/econ
mkdir -p .agents/sub_orch_m2/build
c++ -std=c++17 -Wall \
  -I edge/esp32/src \
  -I edge/esp32/test \
  edge/esp32/test/test_m2_camera_ml.cpp \
  edge/esp32/src/camera/ov7670_driver.cpp \
  edge/esp32/src/camera/model_data.cpp \
  edge/esp32/src/camera/person_detector.cpp \
  -o .agents/sub_orch_m2/build/test_m2
.agents/sub_orch_m2/build/test_m2
```

**Expected Result**:
- Exit code: `0`
- Total checks: 79/79 passed (100% PASS)
- 0 failures, 0 warnings.
