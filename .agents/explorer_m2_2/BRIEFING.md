# BRIEFING — 2026-08-26T04:08:50Z

## Mission
Investigate TFLite Micro ML Pipeline (model architecture, int8 quantization, tensor arena SRAM allocation, OpResolver configuration, inference execution loop, person score/confidence extraction, and host test / mock fallback).

## 🔒 My Identity
- Archetype: explorer
- Roles: investigation, analysis, synthesis
- Working directory: /Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2
- Original parent: 9c20399a-d56c-4ec4-96fd-a7c4f6d7a923
- Milestone: Milestone 2 (TFLite Micro ML Pipeline)

## 🔒 Key Constraints
- Read-only investigation — do NOT implement production source code
- Files for content delivery (.agents/explorer_m2_2/*.md), messages for coordination
- Self-contained 5-component handoff report (Observation, Logic Chain, Caveats, Conclusion, Verification Method)

## Current Parent
- Conversation ID: 9c20399a-d56c-4ec4-96fd-a7c4f6d7a923
- Updated: 2026-08-26T04:06:42Z

## Investigation State
- **Explored paths**:
  - `edge/esp32/platformio.ini` (target configuration, dependencies, partition setup)
  - `PROJECT.md` & `SCOPE.md` (contract interfaces, tracking schema, file ownership)
  - `edge/esp32/src/main.cpp` (PIR sensor sampling, telemetry pipeline, event loop)
  - `edge/esp32/test/` (host test infrastructure: `run_host_tests.sh`, `arduino_shim.h`)
  - `.agents/survey_explorer_1/survey_report.md` & `.agents/survey_explorer_2/survey_report.md`
- **Key findings**:
  - Visual Wake Words 96x96 int8 MobileNet model weights (~250-300 KB) fit cleanly in Flash `.rodata`, mapped to DROM with 0 bytes SRAM at rest.
  - Tensor arena of 80 KB (`alignas(16)`) in internal SRAM provides ample headroom for peak activations (~37 KB) and TFLM metadata.
  - `MicroMutableOpResolver<8>` selectively links only necessary CNN operators (Conv2D, DepthwiseConv2D, AveragePool2D, MaxPool2D, Reshape, FullyConnected, Softmax, Add), saving ~450 KB flash over `AllOpsResolver`.
  - Int8 dequantization formula $(q - Z) \times S$ with dual-threshold hysteresis ($T_{\text{enter}}=0.60, T_{\text{exit}}=0.40$) and 2-frame debounce prevents presence flapping.
  - Dual-mode architecture enables native TFLM on ESP32 and deterministic mock engine on host platforms, allowing 100% CI host test execution without hardware dependencies.
- **Unexplored areas**:
  - Camera hardware I2S DMA registers (investigated by Explorer 1)
  - Frame preprocessor bilinear downsampling implementation & test fixture generation (investigated by Explorer 3)

## Key Decisions Made
- Structured complete ML pipeline architecture, memory layout, and C++ interfaces for `model_data.h/.cpp` and `person_detector.h/.cpp`.
- Designed dual-mode implementation allowing mock inference in host testing without losing fidelity on real hardware.

## Artifact Index
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2/BRIEFING.md` — Persistent working memory
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2/progress.md` — Task progress & heartbeat
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2/analysis.md` — Detailed technical analysis report
- `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2/handoff.md` — 5-component handoff report
