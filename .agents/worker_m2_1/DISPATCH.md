## 2026-08-26T04:09:22Z
You are Worker 1 for Milestone 2: OV7670 Camera Driver & TFLite Micro ML Person Detection Pipeline.
Your working directory is /Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m2_1.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Scope & Exclusively Owned Files:
- edge/esp32/src/camera/camera_config.h
- edge/esp32/src/camera/ov7670_driver.h
- edge/esp32/src/camera/ov7670_driver.cpp
- edge/esp32/src/camera/model_data.h
- edge/esp32/src/camera/model_data.cpp
- edge/esp32/src/camera/person_detector.h
- edge/esp32/src/camera/person_detector.cpp
- edge/esp32/test/test_m2_camera_ml.cpp

Required Reference Files:
- /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md
- /Users/nguyenhoangkhoi/Documents/econ/PROJECT.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m2/SCOPE.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_1/analysis.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2/analysis.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_3/analysis.md
- /Users/nguyenhoangkhoi/Documents/econ/edge/esp32/test/arduino_shim.h

Tasks:
1. Implement edge/esp32/src/camera/camera_config.h:
   - Full pin mappings (VSYNC, HREF, PCLK, XCLK=27 @ 20MHz, D0-D7 with D7 on GPIO5, SIOD=21, SIOC=22).
   - QQVGA resolution definitions (160x120), YUV422 format constants, grayscale frame buffer size (19200 bytes).
2. Implement edge/esp32/src/camera/ov7670_driver.h and ov7670_driver.cpp:
   - Complete OV7670 register initialization table (CLKRC, COM7, COM3, COM14, SCALING_*, etc.).
   - 20 MHz XCLK generation via LEDC (under #ifdef ESP32).
   - I2S0 DMA capture engine with line-by-line Y-channel extraction to 19.2 KB grayscale buffer.
   - Robust detection & simulation / mock fallback mode if camera hardware is missing or in host test mode (REG_PID check, synthetic frame generator, injectTestFrame API).
3. Implement edge/esp32/src/camera/model_data.h and model_data.cpp:
   - Quantized int8 TFLite Micro person detection model data array in Flash (.rodata) with alignas(16).
4. Implement edge/esp32/src/camera/person_detector.h and person_detector.cpp:
   - ImagePreprocessor: Fast integer fixed-point bilinear downsampling from 160x120 (120x120 center crop) to 96x96 int8 tensor with normalization (p - 128).
   - CameraPersonDetector class adhering strictly to PROJECT.md interface contracts:
     - init(), processFrame(), isPersonDetected(), getConfidence(), getPersonCount(), getLatestData(), transmitTelemetry(DualModeComm&).
   - Static internal SRAM tensor arena (~80 KB, alignas(16)).
   - Dual-threshold hysteresis (0.60 / 0.40) and temporal debounce filter.
   - Mock inference & frame injection for host testing.
5. Implement edge/esp32/test/test_m2_camera_ml.cpp:
   - 5 complete test suites (Preprocessor math/bounds, OV7670 driver/simulation, Model data integrity, PersonDetector inference/hysteresis/debounce, Integration/telemetry).
6. Build and Test Verification:
   - Run the host test suite: compile with c++ -std=c++17 -Wall -I edge/esp32/src -I edge/esp32/test edge/esp32/test/test_m2_camera_ml.cpp and execute binary. Ensure 100% tests pass.
   - Run PlatformIO verification (or check compile compatibility).
7. Document all changes and test outputs in /Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m2_1/changes.md and deliver /Users/nguyenhoangkhoi/Documents/econ/.agents/worker_m2_1/handoff.md. Report back to parent via send_message.
