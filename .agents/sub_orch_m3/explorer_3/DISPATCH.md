## 2026-08-26T04:20:41Z
You are Explorer 3 for Milestone 3 (Integration Testing & Verification Strategy).
Your working directory is /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_3.
Your parent conversation ID is 25b89dd0-edb1-4020-a99b-5de00d21e502.

MANDATORY FIRST STEP:
Read /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md, /Users/nguyenhoangkhoi/Documents/econ/PROJECT.md, /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/SCOPE.md, /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m1/handoff.md, and /Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m2/handoff.md.

TASK:
1. Examine existing tests in `edge/esp32/test/` (e.g. M1 unit tests, M2 comms tests).
2. Formulate a comprehensive integration test suite `test_m3_integration.cpp` (runnable via native test runner and Unity/PlatformIO test framework) covering:
   - Camera person detection occupancy replacement for legacy PIR.
   - Dual-mode comms state machine (Wi-Fi online -> UDP broadcast + MQTT; Wi-Fi offline -> USB Serial fallback).
   - Strict isolation check: mock verification that sensor readings (temperature, humidity, CO2, energy) and HVAC commands remain unaltered and unaffected by camera/comms operations.
   - Telemetry payload formatting and timing.
3. Detail the exact test cases and assertions required.
4. Write your comprehensive analysis and recommendations to `/Users/nguyenhoangkhoi/Documents/econ/.agents/sub_orch_m3/explorer_3/analysis.md` and deliver `handoff.md`.
5. Send a message to your parent when done.
