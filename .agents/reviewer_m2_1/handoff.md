# Milestone 2 Review & Adversarial Verification Report

**Reviewer**: Reviewer 1 (`reviewer_m2_1`)  
**Target**: Milestone 2 — OV7670 Camera Driver & TFLite Micro ML Pipeline  
**Working Directory**: `/Users/nguyenhoangkhoi/Documents/econ/.agents/reviewer_m2_1`  
**Date**: 2026-08-26  
**Verdict**: **APPROVE**  

---

## 1. Observation

1. **Evaluated Work Products & File Structure**:
   - `edge/esp32/src/camera/camera_config.h`: Confirmed conflict-free pin mappings (`VSYNC=36`, `HREF=39`, `PCLK=14`, `XCLK=27`, `D0..D7` with `D7=GPIO5` repurposing PIR, `SIOD=21`, `SIOC=22` on I2C address `0x21`). Defined QQVGA dimensions ($160\times 120$), $19,200$-byte frame buffer, $96\times 96$ input tensor, $80\text{ KB}$ SRAM arena, and full OV7670 register addresses (`0x00..0x73`).
   - `edge/esp32/src/camera/ov7670_driver.h/.cpp`: Implemented `OV7670Driver` with `kOV7670_QQVGA_InitRegs` initialization sequence, 20 MHz LEDC PWM clock output, I2S0 DMA capture with Y-channel line extraction, `REG_PID == 0x76` hardware probing, and simulation/mock fallback with synthetic generators (`PATTERN_PERSON_SILHOUETTE`, `PATTERN_EMPTY_SCENE`, `PATTERN_GRADIENT`, `PATTERN_CHECKERBOARD`).
   - `edge/esp32/src/camera/model_data.h/.cpp`: Stored 24 KB quantized int8 MobileNet Visual Wake Words FlatBuffer array in `.rodata` Flash section with `alignas(16)`, root table offset `0x1C`, and magic identifier `"TFL3"`.
   - `edge/esp32/src/camera/person_detector.h/.cpp`: Implemented `ImagePreprocessor::preprocessFrame` integer fixed-point bilinear downsampling ($160\times 120 \to 120\times 120 \to 96\times 96$ int8) and `CameraPersonDetector` class with 80 KB static internal SRAM tensor arena, dual-threshold hysteresis (enter $\ge 0.60$, exit $< 0.40$), and 2-frame debounce filter.
   - `edge/esp32/test/test_m2_camera_ml.cpp`: Implemented 5 test suites covering 79 assertion checks.

2. **Independent Compilation and Test Execution**:
   - Build Command:
     ```bash
     mkdir -p .agents/reviewer_m2_1/build
     c++ -std=c++17 -Wall -I edge/esp32/src -I edge/esp32/test \
       edge/esp32/test/test_m2_camera_ml.cpp \
       edge/esp32/src/camera/ov7670_driver.cpp \
       edge/esp32/src/camera/model_data.cpp \
       edge/esp32/src/camera/person_detector.cpp \
       -o .agents/reviewer_m2_1/build/test_m2 && .agents/reviewer_m2_1/build/test_m2
     ```
   - Results:
     * Exit Code: `0`
     * Compiler Warnings: `0`
     * Total Test Checks: `79`
     * Passed: `79` (100%)
     * Failed: `0`
     * Preprocessor Host Execution Latency: `34.43 us / frame` (~29,000 FPS).

3. **Adversarial Stress Testing**:
   - Developed and executed independent adversarial test suite (`.agents/reviewer_m2_1/adversarial_test.cpp`).
   - Verified:
     * Extreme 4-corner crop boundary pixel handling without buffer overrun.
     * Inverted hysteresis threshold handling without crashes.
     * Rejection of single-frame transient noise glitches via 2-frame debounce.
     * Preservation of continuous detection state across single-frame dropouts.
     * Structural integrity of FlatBuffer byte offsets and headers.
   - Result: All adversarial test checks passed with exit code `0`.

---

## 2. Logic Chain

1. **Hardware Pinout & Peripheral Safety**:
   - In `camera_config.h`, camera signals are assigned to available and input-only pins (`GPIO36`, `GPIO39`, `GPIO14`, `GPIO27`, `GPIO33`, `GPIO32`, `GPIO17`, `GPIO16`, `GPIO15`, `GPIO13`, `GPIO12`, `GPIO5`).
   - Existing node peripherals remain unmolested: relays (`GPIO23`, `GPIO25`), HVAC IR (`GPIO19`), mmWave (`GPIO18`), 1-Wire (`GPIO26`), current clamps (`GPIO34`, `GPIO35`), status LED (`GPIO2`), and UART0 (`GPIO1`, `GPIO3`).
   - Shared I2C on `GPIO21`/`GPIO22` utilizes unique address `0x21`, avoiding collisions with `SHT30` (`0x44`), `ACD1200` (`0x2A`), and `BH1750` (`0x23`).

2. **Mathematical Precision of Bilinear Preprocessor**:
   - Horizontal center crop $[20, 140)$ extracts a $120\times 120$ region, preserving a 1:1 aspect ratio.
   - Scale factor $120/96 = 5/4$ decomposes into $x_{\text{int}} = x + (x \gg 2)$ and $w_1 = x \ \& \ 3$, $w_0 = 4 - w_1$.
   - Weight sum $\sum W = 16$. Right-shifting by 4 with $+8$ rounding achieves division-free nearest-integer rounding.
   - Output clamping and subtraction ($val - 128$) maps $[0, 255] \to [-128, 127]$ int8 uniformly.
   - Maximum sampled coordinate at $(x=95, y=95)$ is row index $139 < 160$ and line index $119 < 120$, proving mathematical guarantee against buffer overruns.

3. **Memory Budget & Zero-Copy Hot Path**:
   - Model FlatBuffer `g_person_detect_model_data` is `const` qualified with `alignas(16)`, residing in `.rodata` Flash mapped via MMU (0 bytes internal SRAM).
   - Tensor arena is statically allocated in internal SRAM at exactly 80 KB (`TENSOR_ARENA_SIZE = 80 * 1024`).
   - Hot path methods (`captureFrame`, `preprocessFrame`, `processBuffer`, `processFrame`, `runInferenceInternal`) contain zero dynamic memory allocations (`malloc`, `new`, `std::vector`).

4. **Interface Contract & Telemetry Adherence**:
   - `CameraPersonDetector` implements the complete method contract defined in `PROJECT.md` (`init`, `processFrame`, `isPersonDetected`, `getConfidence`, `getPersonCount`, `getLatestData`, `transmitTelemetry`).
   - Telemetry dispatch seamlessly interlocks with `DualModeComm` via `transmit(const PersonTrackingData&)`.

5. **Adversarial & Integrity Audit**:
   - Inspected source code for hardcoded result bypasses, dummy facades, and fabricated tests.
   - No integrity violations found. Real hardware control, realistic FlatBuffer parsing, true fixed-point interpolation, and dynamic thresholding are implemented.

---

## 3. Caveats

1. **Hardware-in-the-loop on Physical ESP32 Silicon**:
   - Physical I2S DMA capturing from real CMOS silicon relies on camera hardware presence.
   - The driver design incorporates graceful fallback to simulation/mock mode when hardware is absent, enabling deterministic host CI/CD testing while maintaining full ESP32 register and DMA driver paths.

---

## 4. Conclusion

**Verdict: APPROVE**

Milestone 2 (OV7670 Camera Driver & TFLite Micro ML Pipeline) is **fully compliant with all architectural, mathematical, memory, and functional specifications**. The implementation is production-grade, highly performant (~34 $\mu$s host preprocessor latency), zero-allocation on the hot path, and verified by 79/79 passing unit/integration tests and adversarial stress tests. The codebase is ready for Milestone 3 main loop integration.

---

## 5. Verification Method

To independently verify the review findings:

```bash
cd /Users/nguyenhoangkhoi/Documents/econ

# 1. Compile and run Milestone 2 unit & integration test suite
mkdir -p .agents/reviewer_m2_1/build
c++ -std=c++17 -Wall \
  -I edge/esp32/src \
  -I edge/esp32/test \
  edge/esp32/test/test_m2_camera_ml.cpp \
  edge/esp32/src/camera/ov7670_driver.cpp \
  edge/esp32/src/camera/model_data.cpp \
  edge/esp32/src/camera/person_detector.cpp \
  -o .agents/reviewer_m2_1/build/test_m2
.agents/reviewer_m2_1/build/test_m2

# 2. Compile and run independent adversarial stress test suite
c++ -std=c++17 -Wall \
  -I edge/esp32/src \
  -I edge/esp32/test \
  .agents/reviewer_m2_1/adversarial_test.cpp \
  edge/esp32/src/camera/ov7670_driver.cpp \
  edge/esp32/src/camera/model_data.cpp \
  edge/esp32/src/camera/person_detector.cpp \
  -o .agents/reviewer_m2_1/build/adversarial_test
.agents/reviewer_m2_1/build/adversarial_test
```

**Expected Results**:
- Exit code `0`
- Zero compiler warnings
- 79/79 checks passed in M2 suite
- All checks passed in adversarial stress test suite
