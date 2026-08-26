## 2026-08-26T04:06:42Z

<USER_REQUEST>
You are Explorer 3 for Milestone 2 (Image Preprocessor & Interface/Testing).
Your working directory is /Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_3.
Read:
- /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md
- /Users/nguyenhoangkhoi/Documents/econ/PROJECT.md
- /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m2/SCOPE.md
- /Users/nguyenhoangkhoi/Documents/econ/edge/esp32/test/

Investigate and document:
1. Frame preprocessing math: converting QQVGA (160x120) 8-bit grayscale DMA buffer (19.2 KB) into 96x96 int8 input tensor. Center-cropping to 120x120, then downsampling (bilinear or nearest-neighbor / area averaging) to 96x96, and normalizing uint8 [0, 255] to int8 [-128, 127].
2. Performance optimization: integer fixed-point arithmetic for fast downsampling on Xtensa LX6 without floating point overhead.
3. Integration with CameraPersonDetector class interface contracts as defined in PROJECT.md.
4. Comprehensive test design for edge/esp32/test/test_m2_camera_ml.cpp: testing frame preprocessor math, mock driver, model execution, edge cases (all-black, all-white, gradients, person patterns, uninitialized state).
5. Write your comprehensive analysis to /Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_3/analysis.md and deliver handoff.md. Report back to parent via send_message.
</USER_REQUEST>
