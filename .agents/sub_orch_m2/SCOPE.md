# Scope: Milestone 2 — OV7670 Camera Driver & TFLite Micro ML Person Detection Pipeline

## Objective
Implement a robust, production-grade OV7670 camera driver with I2S DMA frame capture, SCCB I2C configuration, and 20 MHz XCLK generation, combined with a TFLite Micro ML Person Detection inference engine on ESP32 WROOM. Operating in QQVGA (160x120) grayscale mode with 96x96 int8 frame preprocessing, flash-resident model weights, SRAM tensor arena (~80KB), and full host testability with hardware simulation fallback.

## Exclusively Owned Files
- `edge/esp32/src/camera/camera_config.h`
- `edge/esp32/src/camera/ov7670_driver.h`
- `edge/esp32/src/camera/ov7670_driver.cpp`
- `edge/esp32/src/camera/model_data.h`
- `edge/esp32/src/camera/model_data.cpp`
- `edge/esp32/src/camera/person_detector.h`
- `edge/esp32/src/camera/person_detector.cpp`
- `edge/esp32/test/test_m2_camera_ml.cpp`

## Requirements
1. **OV7670 Camera Driver**:
   - I2S DMA parallel byte capture in 8-bit mode.
   - SCCB / I2C register configuration for OV7670 (registers: CLKRC, COM7, COM3, COM14, SCALING_*, etc.).
   - 20 MHz PWM/LEDC clock generation for camera XCLK.
   - QQVGA (160x120) 8-bit grayscale mode (19.2 KB DMA buffer).
   - Robust hardware error handling and graceful fallback / mock mode when camera hardware is unattached or running in simulator/host tests.

2. **Frame Preprocessing Pipeline**:
   - Efficient bilinear or area-average downsampling / cropping from QQVGA (160x120) grayscale to 96x96 int8 input tensor.
   - Pixel normalization mapping uint8 [0, 255] to int8 [-128, 127] as expected by quantized TFLite models.

3. **TFLite Micro ML Pipeline & Weights**:
   - Quantized int8 person detection (Visual Wake Words) model array stored in `.rodata` (Flash).
   - Static tensor arena (~80 KB) allocated in internal SRAM.
   - Person detection inference: extracts detection probability/confidence (0.0 to 1.0) and person count.

4. **Integration Interface**:
   - Conform to `CameraPersonDetector` interface in `PROJECT.md`.
   - Thread-safe, non-blocking frame acquisition and inference cycles.

5. **Testing & Verification**:
   - Host test runner in `test_m2_camera_ml.cpp` verifying driver lifecycle, fallback modes, frame preprocessing math, model data integrity, inference engine, and threshold logic.
