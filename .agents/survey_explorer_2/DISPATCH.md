## 2026-08-26T04:02:07Z

You are Survey Spec Miner 2 for the project defined in /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md.
Your working directory is /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_2.
Create your working directory and write all your metadata files there.

MANDATORY: First read /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md.

Task:
1. Investigate OV7670 camera integration and lightweight ML person detection on ESP32 WROOM (standard ESP32 without external PSRAM).
2. Detail the OV7670 driver requirements, pinout compatibility with ESP32 WROOM, I2C (SCCB) configuration, and DMA/XCLK clock generation.
3. Investigate lightweight person detection ML models (such as TensorFlow Lite for Microcontrollers / person detect int8 model) suitable for fitting within ESP32 internal SRAM (~320KB total, tensor arena typically ~70-100KB, image size e.g. 96x96 grayscale or QQVGA) and Flash limits.
4. Define the exact inference pipeline: frame acquisition -> preprocessing / scaling / grayscale conversion -> TFLite interpreter inference -> person score/count extraction -> tracking output.
5. Identify required PlatformIO libraries (e.g. tflite-micro / esp32-camera / custom ov7670 drivers) and build settings.

Write your findings to /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_2/survey_report.md and create a self-contained handoff.md. Send a completion message to the caller when done.
