## 2026-09-04T06:19:33Z

DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

You are teamwork_preview_worker_m1.
Your working directory is d:\ECON1\econ\.agents\teamwork_preview_worker_m1.

Read these documents before doing any work:
- d:\ECON1\econ\.agents\ORIGINAL_REQUEST.md
- d:\ECON1\econ\PROJECT.md
- d:\ECON1\econ\.agents\teamwork_preview_explorer_survey_esp32\handoff.md
- d:\ECON1\econ\.agents\teamwork_preview_explorer_survey_esp32\analysis.md

Your exclusive write ownership:
- edge/esp32/src/main.cpp
- edge/esp32/src/node_config.h
- edge/esp32/test/host_config_test.cpp
DO NOT modify any other files outside edge/esp32 and your working directory.

Milestone M1 Objective:
Implement requirement R1:
1. In edge/esp32/src/node_config.h:
   - Define STRIP_CAL_A_PER_V default (15.0f for ACS712 30A).
   - Add stripCalAPerV to struct NodeConfig.
   - Set default in cfgDefaults().
   - Add range validation (1.0f to 500.0f) in cfgValidate().
   - Add JSON parsing in cfgApplyJson().
   - Add serialization in cfgSerializeState().
2. In edge/esp32/src/main.cpp:
   - Define USE_STRIP 1 and STRIP_ADC_PIN 35 (safe ADC1 pin).
   - Implement readStripAmps() using the 100ms True-RMS window algorithm (sum and sumSq, mean subtraction for ACS712 ~2.5V DC offset elimination, noise floor check < 0.10A -> 0.0f).
   - In readAndPublish(): expand StaticJsonDocument<256> to StaticJsonDocument<384> and char buf[288] to char buf[384] to prevent buffer overflow.
   - In readAndPublish(): call readStripAmps() and append doc["stripW"] = round(stripAmps * gCfg.plugMainsV * 10) / 10.0 if amps >= 0.
   - In setup(): log sensor initialization message: Serial.printf("[strip] ACS712 on GPIO%d (cal %.1f A/V) — power strip metering\n", STRIP_ADC_PIN, (double)gCfg.stripCalAPerV);
3. In edge/esp32/test/host_config_test.cpp:
   - Add test coverage for stripCalAPerV defaults, boundary validation, and JSON updates.

Verification:
- Compile firmware via: python -m platformio run -e esp32dev (in edge/esp32)
- Run host config tests:
  g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/host_config_test.cpp -o test/host_config_test.exe ; .\test\host_config_test.exe
- Verify both exit with 0 errors.
- Write handoff.md in your working directory with all commands and verification results.
- Send a message to parent orchestrator (3d053cc7-022e-47ba-9164-0325863f09a2) with your completion report.
