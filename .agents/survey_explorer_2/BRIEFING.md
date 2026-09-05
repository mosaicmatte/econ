# BRIEFING — 2026-08-26T04:04:45Z

## Mission
Investigate OV7670 camera driver integration, lightweight ML person detection (TensorFlow Lite for Microcontrollers), memory constraints (ESP32 WROOM no PSRAM), pinout, inference pipeline, and PlatformIO configuration.

## 🔒 My Identity
- Archetype: Specification Miner / Teamwork Specialist
- Roles: Survey Spec Miner 2
- Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_2
- Original parent: 6848b659-e430-4aa8-9ca3-ab02a9ba213d
- Milestone: Exploration / Survey Phase Completed

## 🔒 Key Constraints
- ESP32 WROOM without external PSRAM (strict SRAM limit ~320KB, typical heap ~200-280KB).
- Model and tensor arena must fit comfortably in internal RAM (e.g. 96x96 grayscale int8, tensor arena ~70-100KB).
- OV7670 camera integration: pinout mapping, SCCB/I2C, DMA/XCLK clock generation.
- Dual-mode communication (Wi-Fi broadcast fallback to USB Serial) and minimal changes outside camera module.
- Read-only miner: do NOT implement code in src/, only produce survey_report.md, handoff.md, progress.md.

## Current Parent
- Conversation ID: 6848b659-e430-4aa8-9ca3-ab02a9ba213d
- Updated: 2026-08-26T04:04:45Z

## Task Summary
- **What to build**: Comprehensive survey & architecture spec for OV7670 + TFLite Micro person detection on ESP32 WROOM.
- **Success criteria**: Detailed analysis of camera driver, pinout, clock, DMA, TFLite Micro model, memory budgets, inference pipeline, PlatformIO dependencies, and Wokwi simulation support.
- **Interface contracts**: ORIGINAL_REQUEST.md, survey_report.md, handoff.md.

## Key Decisions Made
- Confirmed QQVGA 160x120 Grayscale (19.2 KB DMA buffer) + 96x96 int8 TFLM model (~80 KB tensor arena) fits comfortably in internal DRAM (>100 KB free headroom).
- Specified `huge_app.csv` partition (3.14 MB) in `platformio.ini` to prevent flash overflow.
- Defined `CameraPersonDetector` modular interface for strict isolation (R1) and dual-mode reporting for Wi-Fi/Serial fallback (R2).

## Artifact Index
- `.agents/survey_explorer_2/survey_report.md` — Detailed survey report
- `.agents/survey_explorer_2/handoff.md` — Self-contained 5-component handoff report
- `.agents/survey_explorer_2/progress.md` — Liveness & heartbeat log
- `.agents/survey_explorer_2/DISPATCH.md` — Original task dispatch record
