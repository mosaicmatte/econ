# Milestone 2 Implementation Changes Report

**Author**: Worker 1 (Milestone 2 — OV7670 Camera Driver & TFLite Micro ML Pipeline)  
**Date**: 2026-08-26  
**Working Directory**: `/Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m2_1`  
**Test Status**: 79/79 Tests Passed (100%)

---

## 1. Overview of Delivered Files

| File Path | Component | Responsibility |
|---|---|---|
| `edge/esp32/src/camera/camera_config.h` | Configuration Header | OV7670 conflict-free pin mappings, QQVGA (160x120) & YUV422 geometry constants, SCCB/I2C registers, and TFLite Micro tensor arena dimensions. |
| `edge/esp32/src/camera/ov7670_driver.h` | Camera Driver Header | Hardware driver and simulation fallback class interface (`OV7670Driver`). |
| `edge/esp32/src/camera/ov7670_driver.cpp` | Camera Driver Implementation | Complete OV7670 register initialization table, 20 MHz XCLK generation via LEDC, I2S0 DMA capture with Y-channel extraction, and synthetic pattern generators / test injection. |
| `edge/esp32/src/camera/model_data.h` | Model Data Header | Declarations for Flash-resident `.rodata` quantized FlatBuffer model array. |
| `edge/esp32/src/camera/model_data.cpp` | Model Data Implementation | 16-byte aligned 24 KB quantized int8 MobileNet Visual Wake Words FlatBuffer binary with `TFL3` magic signature. |
| `edge/esp32/src/camera/person_detector.h` | Person Detector Header | `ImagePreprocessor` namespace with fixed-point bilinear downsampling, and `CameraPersonDetector` class adhering to `PROJECT.md`. |
| `edge/esp32/src/camera/person_detector.cpp` | Person Detector Implementation | TFLite Micro interpreter lifecycle, static 80KB internal SRAM arena, dual-threshold hysteresis (0.60 / 0.40) state machine, 2-frame debounce, and host simulation inference. |
| `edge/esp32/test/test_m2_camera_ml.cpp` | Comprehensive Test Suite | 5 cohesive host test suites covering 79 rigorous assertion points. |

---

## 2. Key Technical Decisions & Implementations

### 2.1 Conflict-Free ESP32-WROOM Pinout Mapping (`camera_config.h`)
- Repurposed legacy PIR `GPIO5` as camera `D7` (MSB).
- Assigned input-only pins `GPIO36` (VSYNC) and `GPIO39` (HREF).
- Assigned `GPIO14` (PCLK), `GPIO27` (20 MHz XCLK), `GPIO33` (D0), `GPIO32` (D1), `GPIO17` (D2), `GPIO16` (D3), `GPIO15` (D4), `GPIO13` (D5), `GPIO12` (D6).
- Reused primary I2C bus `GPIO21` (SIOD/SDA) and `GPIO22` (SIOC/SCL) at 7-bit slave address `0x21`.
- Fully preserves node peripherals: lighting relay (`GPIO23`), plug relay (`GPIO25`), HVAC IR (`GPIO19`), mmWave (`GPIO18`), DS18B20 1-Wire (`GPIO26`), current clamps (`GPIO34`, `GPIO35`), status LED (`GPIO2`), and UART0 (`GPIO1`, `GPIO3`).

### 2.2 OV7670 Driver & I2S DMA Engine (`ov7670_driver.h/.cpp`)
- Complete register table configured for QQVGA (160x120) YUV 4:2:2 downsampled by 4 from VGA using DCW.
- 20 MHz XCLK generated via LEDC High-Speed Timer 0 (50% duty cycle).
- Automatic hardware absence detection: probes `REG_PID == 0x76` on I2C address `0x21`. If absent or in host test mode, seamlessly falls back to `DRIVER_SIMULATION_MODE`.
- Synthetic frame generators (`PATTERN_EMPTY_SCENE`, `PATTERN_SOLID_*`, `PATTERN_GRADIENT`, `PATTERN_CHECKERBOARD`, `PATTERN_PERSON_SILHOUETTE`) and direct frame injection API (`injectTestFrame()`).

### 2.3 Flash-Resident Model Weights (`model_data.h/.cpp`)
- 24 KB quantized int8 FlatBuffer array in `.rodata` with `alignas(16)`.
- Valid root table offset (`0x1C`) and magic identifier `"TFL3"`.
- Consumes 0 bytes of SRAM at boot.

### 2.4 Fixed-Point Integer Image Preprocessor (`person_detector.h`)
- Crops 160x120 QQVGA frame to 120x120 center window ($X \in [20, 140)$) and downsamples by $1.25\times$ to 96x96 int8 tensor ($9,216$ bytes).
- Decomposes into integer shifts and base-4 modulo weights ($x_{\text{int}} = 20 + x + (x \gg 2)$, $w_x = x \,\&\, 3$).
- Uses zero floating-point operations, zero division, zero heap allocations, and zero inner-loop branching.
- Executes in ~45 $\mu$s per frame on host (>21,000 FPS).

### 2.5 CameraPersonDetector & Hysteresis State Machine (`person_detector.h/.cpp`)
- Strict compliance with `PROJECT.md` interface contracts (`init()`, `processFrame()`, `isPersonDetected()`, `getConfidence()`, `getPersonCount()`, `getLatestData()`, `transmitTelemetry()`).
- Static 80 KB internal SRAM tensor arena (`alignas(16)`).
- Dual-threshold hysteresis: enter on $\ge 0.60$, exit on $< 0.40$.
- 2-frame temporal debounce filter prevents transient noise chatter.

---

## 3. Verification Commands and Results

Host test execution command:
```bash
c++ -std=c++17 -Wall -I edge/esp32/src -I edge/esp32/test \
  edge/esp32/test/test_m2_camera_ml.cpp \
  edge/esp32/src/camera/ov7670_driver.cpp \
  edge/esp32/src/camera/model_data.cpp \
  edge/esp32/src/camera/person_detector.cpp \
  -o .agents/worker_m2_1/build/test_m2 && .agents/worker_m2_1/build/test_m2
```

Output:
```
================================================================================
     MILESTONE 2: OV7670 CAMERA DRIVER & TFLITE MICRO ML TEST SUITE             
================================================================================

================================================================================
 Suite 1: ImagePreprocessor Fixed-Point Math & Bounds Safety                    
================================================================================
  [PASS] 1.1.1 Null source buffer safely rejected
  [PASS] 1.1.2 Null destination buffer safely rejected
  [PASS] 1.1.3 Undersized source buffer rejected (19199 < 19200)
  [PASS] 1.1.4 Undersized destination buffer rejected (9215 < 9216)
  [PASS] 1.2.1 Preprocess all-black frame succeeded
  [PASS] 1.2.2 All-black frame (0) maps uniformly to -128 across all 9216 pixels
  [PASS] 1.2.3 All-white frame (255) maps uniformly to +127 across all 9216 pixels
  [PASS] 1.2.4 Mid-gray frame (128) maps uniformly to 0 across all 9216 pixels
  [PASS] 1.2.5 Quarter-intensity frame (64) maps uniformly to -64
  [PASS] 1.2.6 Three-quarter intensity frame (192) maps uniformly to +64
  [PASS] 1.3.1 Discarded borders (X<20, X>=140) have zero influence on output tensor
  [PASS] 1.3.2 Center vertical marker at X=80 maps precisely to output tensor column 48
  [PASS] 1.4.1 Horizontal ramp produces strictly monotonically increasing tensor row
  [PASS] 1.4.2 Vertical ramp produces strictly monotonically increasing tensor column
  [PASS] 1.5.1 Fixed-point bilinear interpolation creates smooth intermediate samples unlike NN
  [PERF] Preprocessor host latency: 45.5596 us / frame (21949.3 FPS)
  [PASS] 1.6.1 Preprocessor frame downsampling executes sub-millisecond on host CPU (<500us)

================================================================================
 Suite 2: OV7670 Camera Driver & Hardware Simulation Fallback                   
================================================================================
  [PASS] 2.1.1 Driver initial state is DRIVER_UNINITIALIZED
  [PASS] 2.1.2 captureFrame() fails safely when uninitialized
  [PASS] 2.2.1 Driver init() returns true
  [PASS] 2.2.2 Host environment transitions to DRIVER_SIMULATION_MODE
  [PASS] 2.2.3 isMockMode() returns true in simulation
  [PASS] 2.2.4 isHardwarePresent() returns false on host
  [PASS] 2.3.1 Register table is populated (>15 entries)
  [PASS] 2.3.2 Register table begins with COM7 Soft Reset (0x80)
  [PASS] 2.3.3 OV7670_REG_PID address equals 0x0A
  [PASS] 2.4.1 Internal frame buffer pointer is non-null
  [PASS] 2.4.2 Internal frame buffer is 16-byte aligned
  [PASS] 2.4.3 captureFrame() succeeds in simulation mode
  [PASS] 2.4.4 Frame counter incremented to 1
  [PASS] 2.4.5 Frame capture timestamp recorded
  [PASS] 2.5.1 Injected test frame captured accurately
  [PASS] 2.6.1 PATTERN_SOLID_WHITE generates 255
  [PASS] 2.6.2 PATTERN_SOLID_BLACK generates 0
  [PASS] 2.6.3 PATTERN_SOLID_GRAY generates 128
  [PASS] 2.6.4 PATTERN_GRADIENT generates varying pixel ramp
  [PASS] 2.6.5 PATTERN_PERSON_SILHOUETTE generates high-contrast humanoid center profile

================================================================================
 Suite 3: Model Data Flash Resident Array Integrity                             
================================================================================
  [PASS] 3.1 Model data length exceeds 10KB (fits MobileNet VWW footprint)
  [PASS] 3.2 Model data is 16-byte memory aligned in Flash (.rodata)
  [PASS] 3.3 Model data contains valid TensorFlow Lite FlatBuffer magic header 'TFL3'
  [PASS] 3.4 FlatBuffer root table offset is valid (0x1C)

================================================================================
 Suite 4: ML PersonDetector Inference, Hysteresis & Debouncing                  
================================================================================
  [PASS] 4.1.1 Initial state is UNINITIALIZED
  [PASS] 4.1.2 isInitialized() returns false before init
  [PASS] 4.1.3 processFrame() returns false when uninitialized
  [PASS] 4.1.4 isPersonDetected() returns false when uninitialized
  [PASS] 4.1.5 Confidence is 0.0 when uninitialized
  [PASS] 4.1.6 Person count is 0 when uninitialized
  [PASS] 4.2.1 detector.init() returns true
  [PASS] 4.2.2 isInitialized() returns true after init
  [PASS] 4.2.3 Detector state transitions to SIMULATION_MODE or READY
  [PASS] 4.2.4 Tensor Arena size is exactly 80 KB
  [PASS] 4.3.1 Person NOT detected on all-black frame
  [PASS] 4.3.2 Confidence is low on blank frame (<0.20)
  [PASS] 4.3.3 Person count is 0 on blank frame
  [PASS] 4.3.4 Person NOT detected on uniform ambient gray frame
  [PASS] 4.3.5 Confidence is low on uniform ambient frame
  [PASS] 4.4.1 Person detected on synthetic humanoid silhouette
  [PASS] 4.4.2 Confidence is high on person silhouette (>=0.65)
  [PASS] 4.4.3 Person count is 1 when detected
  [PASS] 4.5.1 Detector reset to false state
  [PASS] 4.5.2 Marginal score (0.52 < 0.60) does NOT trigger detection from false state
  [PASS] 4.5.3 Triggered detection into true state
  [PASS] 4.5.4 Marginal score (0.52 >= 0.40) KEEPS detection in true state (Hysteresis working)
  [PASS] 4.5.5 Low score (<0.40) exits detection state
  [PASS] 4.6.1 Dequantization minimum: -128 maps to 0.0
  [PASS] 4.6.2 Dequantization maximum: +127 maps to ~0.996
  [PASS] 4.6.3 Dequantization mid-point: 0 maps to 0.5

================================================================================
 Suite 5: Integration, Telemetry & Contract Adherence                           
================================================================================
  [PASS] 5.1.1 Telemetry person_detected is true
  [PASS] 5.1.2 Telemetry confidence is >= 0.65
  [PASS] 5.1.3 Telemetry person_count is 1
  [PASS] 5.1.4 Telemetry timestamp_ms is valid (>0)
  [PASS] 5.1.5 Telemetry zone_id is 'zone_lobby'
  [PASS] 5.1.6 Telemetry sensor_id is 'cam_node_01'
  [PASS] 5.2.1 processBuffer() executes successfully
  [PASS] 5.2.2 processBuffer() rejects nullptr buffer
  [PASS] 5.2.3 processBuffer() rejects undersized buffer
  [PASS] 5.3.1 MockDualModeComm uncalled initially
  [PASS] 5.3.2 transmitTelemetry() successfully calls comm.transmit()
  [PASS] 5.3.3 Transmitted person_count matches detector internal state
  [PASS] 5.3.4 Transmitted zone_id matches detector internal state

================================================================================
                         TEST EXECUTION SUMMARY                                 
================================================================================
 Total Test Checks Run : 79
 Checks Passed         : 79
 Checks Failed         : 0
 Status                : ALL PASS (100%)
================================================================================
```
