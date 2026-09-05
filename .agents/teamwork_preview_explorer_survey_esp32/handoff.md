# Handoff Report: ESP32 C++ Firmware Survey (Requirement R1)

**Agent**: `teamwork_preview_explorer_survey_esp32`  
**Working Directory**: `d:\ECON1\econ\.agents\teamwork_preview_explorer_survey_esp32`  
**Handoff Type**: Hard (Investigation complete)  
**Reference Report**: `d:\ECON1\econ\.agents\teamwork_preview_explorer_survey_esp32\analysis.md`  

---

## 1. Observation

1. **Requirement R1**: `d:\ECON1\econ\.agents\ORIGINAL_REQUEST.md`:
   - Line 18–19: *"Update the C++ firmware in `edge/esp32` to read the ACS712 analog sensor on GPIO 35. Apply a new `stripCalAPerV` calibration multiplier, calculate the RMS power, and append `stripW` to the MQTT telemetry JSON payload."*
   - Line 30–31: Acceptance criteria: *"ESP32 firmware compiles and successfully flashes via `python -m platformio run -t upload` in the `edge/esp32` directory. ESP32 serial output confirms successful JSON MQTT publish including the `"stripW"` field."*

2. **Existing Current & Power Calculation**: `edge/esp32/src/main.cpp`:
   - Lines 545–560 (`readPlugAmps()`):
     ```cpp
     float readPlugAmps() {
       double sum = 0, sumSq = 0;
       int n = 0;
       unsigned long start = millis();
       while (millis() - start < 100) {
         int v = analogRead(PLUG_ADC_PIN);
         sum += v;
         sumSq += (double)v * v;
         n++;
       }
       if (n < 100) return -1;
       double mean = sum / n;
       double rmsCounts = sqrt(fmax(0.0, sumSq / n - mean * mean));
       float amps = (float)(rmsCounts * (3.3 / 4095.0) * gCfg.plugCalAPerV);
       return amps < 0.10 ? 0.0f : amps;  // below the clamp's noise floor = genuinely off
     }
     ```
   - Lines 767–775:
     ```cpp
     #if USE_PLUG
       float amps = readPlugAmps();
       if (amps >= 0) {
         doc["plugW"] = round(amps * gCfg.plugMainsV * 10) / 10.0;  // measured, engine-side model yields
       } else {
         Serial.println("[plug] ADC window starved -> omitted (engine keeps modelling)");
       }
       doc["plug"] = plugOn ? "ON" : "OFF";
     #endif
     ```

3. **Current GPIO 35 Assignment**: `edge/esp32/src/main.cpp`:
   - Lines 157–167:
     ```cpp
     #if USE_AC_CLAMP
       #ifndef AC_CLAMP_PIN
         #define AC_CLAMP_PIN 35
       #endif
       ...
     #endif
     ```
   - `USE_AC_CLAMP` defaults to `0` in line 145 (`#define USE_AC_CLAMP 0`), leaving GPIO 35 inactive by default.

4. **Runtime Configuration Pattern**: `edge/esp32/src/node_config.h`:
   - Lines 58–63:
     ```cpp
     #ifndef PLUG_CAL_A_PER_V
       #define PLUG_CAL_A_PER_V 60.6
     #endif
     #ifndef PLUG_MAINS_V
       #define PLUG_MAINS_V 230.0
     #endif
     ```
   - Lines 90–103 (`struct NodeConfig`): contains `plugCalAPerV`, `plugMainsV`, `acCalAPerV`, `acMainsV`.
   - Line 154–155: Validation check `if (!(c.plugCalAPerV >= 1.0f && c.plugCalAPerV <= 500.0f))`.
   - Line 246: Deserialization `if (doc.containsKey("plugCalAPerV")) next.plugCalAPerV = doc["plugCalAPerV"];`.
   - Line 298: State serialization `if (gCfg.plugCalAPerV != d.plugCalAPerV) ov.add("plugCalAPerV");`.

5. **JSON Document Buffer Sizing**: `edge/esp32/src/main.cpp`:
   - Line 692: `StaticJsonDocument<256> doc;`
   - Line 807: `char buf[288];`
   - Serial publishing: `Serial.printf("[mqtt] pub %s -> %s\n", TELEMETRY_TOPIC, buf);`

6. **Tool Commands and Results**:
   - `python -m platformio run -e esp32dev`: Exited 0 with `SUCCESS Took 45.31 seconds`.
   - `g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/host_config_test.cpp -o test/host_config_test.exe && .\test\host_config_test.exe`: Exited 0 with `PASSED (0 failures)`.

---

## 2. Logic Chain

1. **Hardware Mapping (GPIO 35)**:
   - *Premise*: Requirement R1 mandates reading the ACS712 analog sensor on GPIO 35.
   - *Evidence*: `main.cpp:157–167` allocated GPIO 35 to `AC_CLAMP_PIN` under `USE_AC_CLAMP`, which defaults to 0 (`main.cpp:145`). GPIO 35 is an ESP32 ADC1 channel (ADC1_CH7) that is input-only and remains functional while WiFi is active.
   - *Inference*: Reconfiguring GPIO 35 for `STRIP_ADC_PIN 35` under a new `#define USE_STRIP 1` macro is safe, avoids any ADC2/WiFi conflict, and does not break default builds.

2. **True-RMS Math Compatibility**:
   - *Premise*: ACS712 outputs an analog signal centered at $V_{CC}/2 \approx 2.5\text{ V}$ (or 1.65 V with voltage divider).
   - *Evidence*: `main.cpp:545–560` computes the DC offset `mean = sum / n` over a 100 ms window and calculates AC RMS counts as $\sqrt{\frac{\sum v_i^2}{n} - \text{mean}^2}$.
   - *Inference*: This algorithm automatically subtracts the 2.5 V DC offset of the ACS712 without requiring any code-level hardcoded offset subtraction. Scaling `rmsCounts * (3.3 / 4095.0) * stripCalAPerV` accurately calculates $I_{\text{RMS}}$, which when multiplied by mains voltage ($230\text{ V}$) yields active RMS Power $P = I_{\text{RMS}} \times V_{\text{mains}}$.

3. **Calibration Architecture (`stripCalAPerV`)**:
   - *Premise*: Tunable calibration constants in this repository are defined in `node_config.h` with compile-time defaults and runtime MQTT overrides persisted to NVS.
   - *Evidence*: `node_config.h:1–39` enforces that installation properties (such as clamp/burden constants) must not require firmware reflashes and must bump `cfgRev` when changed.
   - *Inference*: Adding `stripCalAPerV` to `NodeConfig`, `cfgDefaults()`, `cfgValidate()` (range 1.0–500.0 A/V), `cfgApplyJson()`, and `cfgSerializeState()` preserves system design consistency and enables over-the-air recalibration without reflashing.

4. **Telemetry JSON Payload Contract (`stripW`)**:
   - *Premise*: Telemetry must append `"stripW"` and publish to `econ/telemetry/<zone>` while echoing to serial.
   - *Evidence*: `main.cpp:767–775` appends `"plugW"` as `round(amps * gCfg.plugMainsV * 10) / 10.0`. `main.cpp:692,807` currently allocates `StaticJsonDocument<256>` and `char buf[288]`.
   - *Inference*: Appending `doc["stripW"] = round(stripAmps * gCfg.plugMainsV * 10) / 10.0` conforms to the existing contract. Because the JSON document now contains up to 17 fields, expanding `StaticJsonDocument<384>` and `char buf[384]` is necessary to avoid silent buffer overflow or string truncation.

5. **Serial Monitor / USB Bridge Compatibility**:
   - *Premise*: Acceptance criterion requires serial output confirmation of `"stripW"`.
   - *Evidence*: `bridge.py:40,57–59` uses regex `r"\[mqtt\] pub (.+?) -> (\{.*\})"` to match `Serial.printf("[mqtt] pub %s -> %s\n", TELEMETRY_TOPIC, buf);`.
   - *Inference*: The standard serial output already in `main.cpp:810` will immediately emit `"stripW"` in the printed JSON string, satisfying the test and bridge without extra logging logic.

---

## 3. Caveats

1. **Hardware Zero-Offset Drift**: The ACS712 output is nominally $V_{CC}/2$ (2.5 V at 5 V supply). Because the ESP32 ADC full-scale range is 3.3 V (with 11 dB attenuation), a 5 V ACS712 module without a voltage divider could clip positive peaks above 3.3 V if measuring high currents (>12 A on a 30A module). However, for a typical power strip load (<2000 W, ~8.7 A RMS, ~12.3 A peak), signal stays within range or can use a simple 2:3 resistor divider. The True-RMS software logic accommodates either arrangement via `stripCalAPerV`.
2. **Unconnected Floating Pin**: If GPIO 35 has no physical sensor connected during testing on a bare devkit, analog noise could register a small reading. The `amps < 0.10 ? 0.0f : amps` noise gate suppresses low-level noise.
3. **Mains Voltage Source**: We recommend using `gCfg.plugMainsV` (default 230.0 V) for the strip power calculation, as a power strip is plugged into the socket/plug circuit. An independent `stripMainsV` can be added if distinct mains tracking is needed.

---

## 4. Conclusion

Requirement R1 is clear, straightforward, and architecturally aligned with the existing codebase:
1. **Pin Assignment**: Configure GPIO 35 as `STRIP_ADC_PIN` under `#define USE_STRIP 1`.
2. **Calibration**: Add `stripCalAPerV` (default ~15.0 A/V for ACS712 30A) to `node_config.h` with 1.0–500.0 A/V validation, JSON parsing, and NVS persistence.
3. **Sampling Logic**: Add `readStripAmps()` replicating the 100 ms True-RMS window with DC offset variance subtraction.
4. **Telemetry Serialization**: Expand `StaticJsonDocument<384>` and `char buf[384]`, and append `doc["stripW"] = round(stripAmps * gCfg.plugMainsV * 10) / 10.0;`.
5. **Contract**: Emits `"stripW"` (float in Watts, 1 decimal place, omitted on starvation) on `econ/telemetry/<zone>` and serial log `[mqtt] pub ...`.

---

## 5. Verification Method

1. **Firmware Compilation**:
   ```powershell
   python -m platformio run -e esp32dev
   ```
   *Expected*: Clean compilation with 0 errors and output `.pio/build/esp32dev/firmware.bin`.

2. **Host Configuration Unit Test**:
   ```powershell
   g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/host_config_test.cpp -o test/host_config_test.exe
   .\test\host_config_test.exe
   ```
   *Expected*: `PASSED (0 failures)` validating default `stripCalAPerV`, boundary checks, and JSON updates.

3. **Flashing & Hardware Serial Verification**:
   ```powershell
   python -m platformio run -t upload
   python -m platformio device monitor -b 115200
   ```
   *Expected*: Serial output logs:
   `[strip] ACS712 on GPIO35 (cal 15.0 A/V) — power strip metering`
   `[mqtt] pub econ/telemetry/zone_1 -> {...,"stripW":...}`

4. **Invalidation Conditions**:
   - Compilation failure under PlatformIO.
   - `"stripW"` missing from serial `[mqtt] pub` line.
   - Buffer overflow in `StaticJsonDocument` or `char buf[]` truncating JSON output.
