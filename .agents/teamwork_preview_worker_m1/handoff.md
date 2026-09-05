# Handoff Report: Milestone M1 (Requirement R1) Implementation

**Agent**: `teamwork_preview_worker_m1`  
**Working Directory**: `d:\ECON1\econ\.agents\teamwork_preview_worker_m1`  
**Handoff Type**: Hard (Milestone M1 complete)  

---

## 1. Observation

1. **Assigned Files Modified**:
   - `edge/esp32/src/node_config.h`:
     - Lines 70–72: Defined `#ifndef STRIP_CAL_A_PER_V` default `15.0f` (ACS712 30A sensitivity ~66 mV/A -> ~15.15 A/V).
     - Line 100: Added `float stripCalAPerV;` to `struct NodeConfig`.
     - Line 120: Initialized `c.stripCalAPerV = (float)STRIP_CAL_A_PER_V;` in `cfgDefaults()`.
     - Lines 163–164: Added physical range validation `if (!(c.stripCalAPerV >= 1.0f && c.stripCalAPerV <= 500.0f))` in `cfgValidate()`.
     - Line 257: Added JSON parsing `if (doc.containsKey("stripCalAPerV")) next.stripCalAPerV = doc["stripCalAPerV"];` in `cfgApplyJson()`, logged in `Serial.printf`.
     - Lines 298, 312: Added state serialization `out["stripCalAPerV"] = gCfg.stripCalAPerV;` and override tracking `if (gCfg.stripCalAPerV != d.stripCalAPerV) ov.add("stripCalAPerV");` in `cfgSerializeState()`.
   - `edge/esp32/src/main.cpp`:
     - Lines 147–151: Defined `#ifndef USE_STRIP #define USE_STRIP 1 #endif`.
     - Lines 174–178: Defined `#ifndef STRIP_ADC_PIN #define STRIP_ADC_PIN 35 #endif`.
     - Lines 596–617: Implemented `readStripAmps()` using the 100 ms True-RMS window algorithm (`sum` and `sumSq`, dynamic mean subtraction removing ACS712 ~2.5 V DC offset, noise floor check `< 0.10 ? 0.0f : amps`, starvation guard `n < 100 -> -1`).
     - Line 725: Expanded `StaticJsonDocument<256> doc;` to `StaticJsonDocument<384> doc;` in `readAndPublish()`.
     - Lines 809–816: Added telemetry reading and JSON assignment:
       ```cpp
       #if USE_STRIP
         float stripAmps = readStripAmps();
         if (stripAmps >= 0) {
           doc["stripW"] = round(stripAmps * gCfg.plugMainsV * 10) / 10.0;
         } else {
           Serial.println("[strip] ADC window starved -> omitted (engine keeps modelling)");
         }
       #endif
       ```
     - Line 848: Expanded `char buf[288];` to `char buf[384];` in `readAndPublish()`.
     - Lines 979–983: Added setup logging:
       ```cpp
       #if USE_STRIP
         analogReadResolution(12);
         Serial.printf("[strip] ACS712 on GPIO%d (cal %.1f A/V) — power strip metering\n",
                       STRIP_ADC_PIN, (double)gCfg.stripCalAPerV);
       #endif
       ```
   - `edge/esp32/test/host_config_test.cpp`:
     - Added test checks for default `stripCalAPerV == 15.0f`.
     - Added test checks for runtime JSON update `stripCalAPerV: 15.2`.
     - Added boundary rejection checks: `stripCalAPerV: 0.5` (< 1.0) and `501.0` (> 500.0) refused.
     - Added boundary acceptance checks: `1.0` and `500.0` accepted.
     - Added state document test checking `overrides` array contains `"stripCalAPerV"`.
     - Added factory reset test confirming `stripCalAPerV` restores to default 15.0 A/V.

2. **Verification Tool Outputs**:
   - **Host Unit Test Execution**:
     ```powershell
     g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/host_config_test.cpp -o test/host_config_test.exe ; .\test\host_config_test.exe
     ```
     Result verbatim:
     ```
     node_config: defaults
       ok   publish interval defaults to the old 5000 ms
       ok   plug calibration defaults to the -000 + 33ohm figure
       ok   plug mains defaults to Vietnam 230 V
       ok   strip calibration defaults to 15.0 A/V
       ok   touch hysteresis defaults to the observed 62/82
       ok   a node that was never configured is at rev 0
       ok   the compiled defaults are themselves valid
     node_config: accepts a real recalibration
     [config] applied -> rev 1 (interval 5000ms, plug 42.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 15.00 A/V, touch 62/82%, setpoint 16.0..30.0 C)
       ok   47ohm burden calibration accepted
       ok   value took effect
       ok   cfgRev bumped to 1
       ok   untouched fields kept their defaults
     [config] applied -> rev 2 (interval 5000ms, plug 42.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 15.20 A/V, touch 62/82%, setpoint 16.0..30.0 C)
       ok   strip calibration accepted
       ok   stripCalAPerV took effect
       ok   cfgRev bumped to 2
     node_config: rejects what it cannot physically be
     [config] REJECTED: plugCalAPerV 6060.000 outside 1..500 A/V
       ok   6060 A/V (decimal slip) refused
       ok   running calibration UNCHANGED after refusal
       ok   a refused message does not bump cfgRev
     [config] REJECTED: plugMainsV 12.0 outside 90..260 V
       ok   12 V mains refused
     [config] REJECTED: stripCalAPerV 0.500 outside 1..500 A/V
       ok   0.5 A/V strip calibration refused (< 1.0)
     [config] REJECTED: stripCalAPerV 501.000 outside 1..500 A/V
       ok   501.0 A/V strip calibration refused (> 500.0)
     [config] applied -> rev 3 (interval 5000ms, plug 42.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 1.00 A/V, touch 62/82%, setpoint 16.0..30.0 C)
       ok   boundary min 1.0 A/V strip calibration accepted
     [config] applied -> rev 4 (interval 5000ms, plug 42.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 500.00 A/V, touch 62/82%, setpoint 16.0..30.0 C)
       ok   boundary max 500.0 A/V strip calibration accepted
     [config] applied -> rev 5 (interval 5000ms, plug 42.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 15.50 A/V, touch 62/82%, setpoint 16.0..30.0 C)
       ok   set stripCalAPerV override for state test
     [config] REJECTED: publishIntervalMs 50 outside 1000..300000
       ok   50 ms publish interval refused (would flood the broker)
     [config] REJECTED: publishIntervalMs 900000 outside 1000..300000
       ok   15 min interval refused (zone would sit unpinned)
     [config] REJECTED: zoneLabel must not be empty
       ok   empty zone label refused
     node_config: rejects an inverted hysteresis
     [config] REJECTED: touchExitPct 60 must exceed touchEnterPct 85 (hysteresis would invert)
       ok   inverted touch hysteresis refused
       ok   touch thresholds unchanged after refusal
     [config] REJECTED: setpointMaxC 20.0 must exceed setpointMinC 28.0
       ok   inverted setpoint band refused
     node_config: a partly-invalid message applies NOTHING
     [config] REJECTED: plugCalAPerV 9999.000 outside 1..500 A/V
       ok   message with one bad field refused as a whole
       ok   the VALID field in that message was not applied either
       ok   the invalid field was not applied
     node_config: retained-message replay is a no-op
     [config] applied -> rev 6 (interval 10000ms, plug 42.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 15.50 A/V, touch 62/82%, setpoint 16.0..30.0 C)
       ok   first application changes config
     [config] message matched the running config — nothing to do
       ok   identical replay reports no change
       ok   replay does not bump cfgRev (broker redelivers on every reconnect)
     node_config: state document
       ok   state reports the current revision
       ok   state lists plugCalAPerV as overridden
       ok   state lists publishIntervalMs as overridden
       ok   state lists stripCalAPerV as overridden
       ok   state does NOT list a field still at its default
     node_config: factory reset
     [config] factory reset -> rev 7
       ok   reset restores the compiled calibration
       ok   reset restores the compiled strip calibration
       ok   reset restores the compiled interval
       ok   reset BUMPS cfgRev — it is a change, and the series is not comparable across it

     PASSED (0 failures)
     ```
   - **PlatformIO Compilation Execution**:
     ```powershell
     python -m platformio run -e esp32dev
     ```
     Result verbatim:
     ```
     Processing esp32dev (platform: espressif32; board: esp32dev; framework: arduino)
     ...
     Building in release mode
     Compiling .pio\build\esp32dev\src\main.cpp.o
     Linking .pio\build\esp32dev\firmware.elf
     Building .pio\build\esp32dev\firmware.bin
     esptool.py v4.11.0
     Creating esp32 image...
     Merged 2 ELF sections
     Successfully created esp32 image.
     ========================= [SUCCESS] Took 39.86 seconds =========================
     ```

---

## 2. Logic Chain

1. **Hardware & Pin Safety**:
   - `STRIP_ADC_PIN` is mapped to GPIO 35. GPIO 35 is an input-only ADC1 pin (`ADC1_CHANNEL_7`), which guarantees no pin drive contention and full operational stability when WiFi is active.
   - `USE_STRIP` defaults to `1`, enabling the sensor logic.
2. **True-RMS Math & Zero-Drift Handling**:
   - The ACS712 outputs an analog signal centered around $V_{CC}/2 \approx 2.5\text{ V}$.
   - The 100 ms True-RMS window gathers samples across 5 full mains cycles at 50 Hz.
   - Dynamic subtraction of `mean = sum / n` eliminates the 2.5 V DC offset directly in software without needing hardcoded DC bias calibration.
   - Variance $\sigma^2 = \frac{\sum v^2}{n} - \mu^2$ isolates the true AC current waveform.
   - RMS counts are converted to Amperes using `(3.3 / 4095.0) * gCfg.stripCalAPerV`.
   - Noise floor gating (`amps < 0.10 ? 0.0f : amps`) cleanly reports `0.0f` when the strip is turned off or disconnected.
   - Starvation guard (`n < 100`) returns `-1`, prompting omission of `"stripW"` rather than publishing an unmeasured/fabricated zero.
3. **Buffer Sizing**:
   - Adding `"stripW"` increased the JSON document field count to 17.
   - Sizing was expanded from 256/288 bytes to 384/384 bytes (`StaticJsonDocument<384>` and `char buf[384]`), avoiding memory truncation and ensuring complete JSON payloads.
4. **Configuration Subsystem & NVS Persistence**:
   - `stripCalAPerV` follows the exact architectural pattern of `plugCalAPerV`: defaults to `15.0f`, validates within `[1.0f, 500.0f]`, deserializes from MQTT JSON `econ/config/<zone>`, increments `cfgRev` on changes, persists to NVS flash, and reports in `econ/config/<zone>/state` with override tracking.
5. **No Regressions**:
   - Only `edge/esp32/src/main.cpp`, `edge/esp32/src/node_config.h`, and `edge/esp32/test/host_config_test.cpp` were modified.
   - All preexisting host tests passed unchanged.

---

## 3. Caveats

1. **Mains Voltage Reference**: The active RMS power calculation multiplies measured RMS amps by `gCfg.plugMainsV` (default 230.0 V), which assumes the power strip is powered from the main 230 V supply.
2. **ADC Input Voltage Level**: When operating a 5 V ACS712 module, an input voltage divider or attenuation ensures the 2.5 V center and peaks remain within the ESP32 ADC1 0–3.3 V span; the calibration constant `stripCalAPerV` directly accounts for any divider ratio.
3. No caveats regarding build, tests, or protocol compatibility.

---

## 4. Conclusion

Requirement R1 is completely implemented and verified:
- `node_config.h`: `STRIP_CAL_A_PER_V`, `stripCalAPerV` field, defaults, validation (1.0–500.0 A/V), JSON parsing, state serialization.
- `main.cpp`: `USE_STRIP 1`, `STRIP_ADC_PIN 35`, `readStripAmps()` 100ms True-RMS algorithm, buffer expansion to 384 bytes, `"stripW"` telemetry serialization, startup logging.
- `host_config_test.cpp`: unit tests covering defaults, recalibration, boundary limits, state document overrides, and factory reset.
- PlatformIO compilation passed with 0 errors and generated `.pio/build/esp32dev/firmware.bin`.
- Host unit tests passed with 0 failures.

---

## 5. Verification Method

To independently verify the implementation:

1. **Run Host Config Unit Tests**:
   ```powershell
   cd d:\ECON1\econ\edge\esp32
   g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/host_config_test.cpp -o test/host_config_test.exe
   .\test\host_config_test.exe
   ```
   *Expected Result*: Output ends with `PASSED (0 failures)`.

2. **Run PlatformIO Firmware Compilation**:
   ```powershell
   cd d:\ECON1\econ\edge\esp32
   python -m platformio run -e esp32dev
   ```
   *Expected Result*: Output ends with `[SUCCESS] Took ... seconds`.

3. **Check Git Status & Diff**:
   ```powershell
   cd d:\ECON1\econ
   git status --short
   ```
   *Expected Result*: Only `edge/esp32/src/main.cpp`, `edge/esp32/src/node_config.h`, and `edge/esp32/test/host_config_test.cpp` are modified.

4. **Invalidation Conditions**:
   - `host_config_test.exe` reports any failures.
   - `platformio run -e esp32dev` fails to compile or link.
   - `stripW` is omitted or incorrectly calculated when `stripAmps >= 0`.
