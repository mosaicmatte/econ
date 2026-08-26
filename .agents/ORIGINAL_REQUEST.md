# Original User Request

## 2026-08-26T04:01:19Z

Update the software for an ESP32 WROOM to replace a PIR motion sensor with an OV7670 camera. The goal is to track people in real time to feed into a topology/BIM model, ensuring the architecture is extensible for future features.

Working directory: /Users/nguyenhoangkhoi/Documents/econ/edge/esp32
Integrity mode: development

## Requirements

### R1. Camera-Based Person Detection Module
Implement a people detection module using the OV7670 camera and a lightweight Machine Learning model (e.g., TensorFlow Lite) suitable for the ESP32 WROOM. Ensure changes are strictly isolated to this module without modifying other parts of the existing software.

### R2. Dual-Mode Communication
The module must broadcast real-time tracking data over Wi-Fi as its primary method. It must automatically fall back to transmitting data over the USB Serial connection if Wi-Fi is unavailable or disconnected.

## Verification Resources
The project contains a PlatformIO environment (`platformio.ini`) and Wokwi simulator configurations (`wokwi.toml`).

## Acceptance Criteria

### Compilation
- [ ] Code compiles successfully via PlatformIO for the ESP32 target without errors.
- [ ] Firmware fits within the available flash and RAM limits of the ESP32 WROOM.

### Architecture
- [ ] No files outside of the camera module's scope are modified.

### Agent-as-Judge
- [ ] An independent agent reviews the codebase and confirms that real-time Wi-Fi broadcasting is implemented.
- [ ] An independent agent confirms that the system falls back to Serial output when Wi-Fi is disconnected.
- [ ] An independent agent confirms the ML person detection model is properly initialized and processes camera frames.
