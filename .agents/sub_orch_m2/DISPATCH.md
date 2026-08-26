# Dispatch Log

## 2026-08-26T04:06:05Z

You are the Sub-Orchestrator for Milestone 2: OV7670 Camera Driver & TFLite Micro ML Person Detection Pipeline.
Your working directory is /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m2.
Your parent conversation ID is 6848b659-e430-4aa8-9ca3-ab02a9ba213d.

MANDATORY: First read /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md and /Users/nguyenhoangkhoi/Documents/econ/PROJECT.md.

Scope & Exclusively Owned Files:
- edge/esp32/src/camera/camera_config.h
- edge/esp32/src/camera/ov7670_driver.h
- edge/esp32/src/camera/ov7670_driver.cpp
- edge/esp32/src/camera/model_data.h
- edge/esp32/src/camera/model_data.cpp
- edge/esp32/src/camera/person_detector.h
- edge/esp32/src/camera/person_detector.cpp
- edge/esp32/test/test_m2_camera_ml.cpp

Requirements (R1):
1. OV7670 camera driver with I2S DMA frame capture, SCCB/I2C configuration, and 20 MHz XCLK generation.
2. Operating in QQVGA (160x120) grayscale mode (19.2 KB DMA buffer) with graceful fallback/mock for simulation/unattached hardware.
3. Quantized int8 TFLite Micro person detection model weights in Flash (.rodata) and tensor arena (~80 KB) allocated in internal SRAM.
4. Frame preprocessing pipeline: cropping/downsampling QQVGA to 96x96 int8 input tensor with normalization.
5. TFLite Micro inference engine running on ESP32 Xtensa LX6 extracting person detection probability and headcount.
6. Execute the iteration loop (Explorer -> Worker -> Reviewer -> Challenger -> Auditor) with strict verification and unit testing.
7. When complete and gated with PASS, deliver handoff.md and send completion message to parent.
