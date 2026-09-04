# Adversarial Verification & Challenge Report: JSON Serialization Limits & Config Fuzzing

**Agent**: `teamwork_preview_challenger_m1_2`  
**Working Directory**: `d:\ECON1\econ\.agents\teamwork_preview_challenger_m1_2`  
**Handoff Type**: Hard (Empirical challenge complete)  
**Verdict**: **APPROVE** (with architectural type-coercion finding documented)

---

## 1. Observation

### Target Under Review
1. **JSON Serialization in Telemetry**:
   - `edge/esp32/src/main.cpp` line 725: `StaticJsonDocument<384> doc;`
   - `edge/esp32/src/main.cpp` lines 809–816:
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
   - `edge/esp32/src/main.cpp` line 848: `char buf[384];`
   - `edge/esp32/src/main.cpp` lines 849–851:
     ```cpp
     size_t n = serializeJson(doc, buf);
     client.publish(TELEMETRY_TOPIC, buf, n);
     Serial.printf("[mqtt] pub %s -> %s\n", TELEMETRY_TOPIC, buf);
     ```

2. **Configuration Validation & Deserialization**:
   - `edge/esp32/src/node_config.h` lines 163–164:
     ```cpp
     if (!(c.stripCalAPerV >= 1.0f && c.stripCalAPerV <= 500.0f))
       return cfgFail("stripCalAPerV %.3f outside 1..500 A/V", (double)c.stripCalAPerV);
     ```
   - `edge/esp32/src/node_config.h` line 257:
     ```cpp
     if (doc.containsKey("stripCalAPerV")) next.stripCalAPerV = doc["stripCalAPerV"];
     ```

### Empirical Test Harnesses Created & Executed
Two empirical test suites were authored in `edge/esp32/test/`:
1. `edge/esp32/test/empirical_payload_test.cpp`:
   - Harness with memory guard canary bytes (`0x5A` before, `0xA5` after `char buf[384]`).
   - Evaluates all 17 telemetry features active simultaneously under nominal, maximum realistic, extreme float (`1e6`, `1e-4`, negative, `.1` recurring, `NaN`, `+Infinity`, `-Infinity`), and theoretical worst-case string representations.
   - Evaluates boundary truncation and overflow behavior in ArduinoJson.

2. `edge/esp32/test/empirical_fuzz_test.cpp`:
   - Fuzzer exercising `stripCalAPerV` over extreme negative values (`-100`, `-100.0`, `-0.001`, `-1e20`), zero (`0`, `0.0`, `-0.0`), sub-threshold boundary (`0.99`, `0.99999`), valid boundaries (`1.0`, `500.0`), super-threshold values (`500.01`, `501.0`, `1000.0`, `1e10`, `1e38`), IEEE-754 special floats (`NaN`, `Inf`, `-Inf` both via JSON string and direct programmatic injection), invalid types (`false`, `null`, `[]`, `{}`, `[15.0]`, non-numeric strings, long strings up to 1000 chars), and multi-field atomic rejection.

### Verbatim Tool Execution Outputs
1. **Payload Test Output (`empirical_payload_test.exe`)**:
   ```
   === EMPIRICAL CHALLENGE: JSON SERIALIZATION BUFFER LIMITS ===

   --- Test Scenario 1: Nominal Telemetry (All 17 Features) ---
     [PASS] Nominal document contains all 17 fields
     Serialized: {"zone":"Level 4","source":"esp32","cfgRev":1,"temperature":24.5,"humidity":60.2,"tempReal":true,"occupancy":3,"co2":850,"plugW":185.4,"plug":"ON","stripW":230.5,"supplyC":14.5,"acW":1199.9,"lux":450,"lights":"ON","setpoint":24,"acReal":true}
     Length: 242 bytes (measured: 242 bytes, buf capacity: 384)
     [PASS] No memory corruption before/after char buf[384]
     [PASS] serializeJson return matches measureJson
     [PASS] Nominal payload length is well below 384 bytes
     [PASS] Headroom in buf[384] is at least 50 bytes

   --- Test Scenario 2: Maximum Realistic Limits ---
     [PASS] Max-realistic document contains all 17 fields
     Serialized: {"zone":"ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ","source":"esp32","cfgRev":4294967295,"temperature":99.9,"humidity":100,"tempReal":false,"occupancy":50,"co2":40000,"plugW":26000,"plug":"OFF","stripW":26000,"supplyC":99.9,"acW":26000,"lux":65535,"lights":"OFF","setpoint":35,"acReal":false}
     Length: 282 bytes (measured: 282 bytes, buf capacity: 384)
     [PASS] No memory corruption under max realistic values
     [PASS] serializeJson return matches measureJson
     [PASS] Max-realistic payload strictly fits in buf[384]
     Headroom remaining in char buf[384]: 102 bytes
     [PASS] Headroom remaining is >= 20 bytes

   --- Test Scenario 3: Extreme Float Values ---
     [PASS] [All Zero] guard intact & payload fits (248 bytes)
     [PASS] serializeJson matches measureJson without truncation
     [PASS] Extreme float payload strictly fits within 384 bytes
     [PASS] [Negative floats] guard intact & payload fits (265 bytes)
     [PASS] serializeJson matches measureJson without truncation
     [PASS] Extreme float payload strictly fits within 384 bytes
     [PASS] [Fractional precision (.1 recurring)] guard intact & payload fits (275 bytes)
     [PASS] serializeJson matches measureJson without truncation
     [PASS] Extreme float payload strictly fits within 384 bytes
     [PASS] [Large floats (1e6)] guard intact & payload fits (286 bytes)
     [PASS] serializeJson matches measureJson without truncation
     [PASS] Extreme float payload strictly fits within 384 bytes
     [PASS] [Very small floats (1e-4)] guard intact & payload fits (253 bytes)
     [PASS] serializeJson matches measureJson without truncation
     [PASS] Extreme float payload strictly fits within 384 bytes
     [PASS] [NaN values] guard intact & payload fits (272 bytes)
     [PASS] serializeJson matches measureJson without truncation
     [PASS] Extreme float payload strictly fits within 384 bytes
     [PASS] [Infinity values] guard intact & payload fits (272 bytes)
     [PASS] serializeJson matches measureJson without truncation
     [PASS] Extreme float payload strictly fits within 384 bytes
     [PASS] [-Infinity values] guard intact & payload fits (272 bytes)
     [PASS] serializeJson matches measureJson without truncation
     [PASS] Extreme float payload strictly fits within 384 bytes

   --- Test Scenario 4: Worst-Case Theoretical String Length ---
     Worst-Case Serialized: {"zone":"WWWWWWWWWWWWWWWWWWWWWWWWWWWWWWW","source":"esp32","cfgRev":4294967295,"temperature":-99999.9,"humidity":100,"tempReal":false,"occupancy":65535,"co2":65535,"plugW":-999999.9,"plug":"OFF","stripW":-999999.9,"supplyC":-99999.9,"acW":-999999.9,"lux":65535,"lights":"OFF","setpoint":-99999.9,"acReal":false}
     Worst-Case Length: 311 bytes (buf size: 384 bytes)
     [PASS] Theoretical worst-case length strictly < 384 bytes
     [PASS] Guard bytes untouched
     Guaranteed buffer margin: 73 bytes remaining
     [PASS] Guaranteed safety margin >= 15 bytes

   --- Test Scenario 5: Buffer Boundary Safety Verification ---
     [PASS] Guard bytes intact before/after buf[384]
     [PASS] Payload is safely null-terminated in buf[384]
     [PASS] Null terminator exists at buffer position n
     [PASS] Payload plus null-terminator fits within 384 bytes
     [PASS] ArduinoJson write strictly bounded by buffer capacity

   Payload Limit Tests Completed: PASSED (0 failures)
   ```

2. **Fuzz Test Output (`empirical_fuzz_test.exe`)**:
   ```
   === EMPIRICAL CHALLENGE: stripCalAPerV CONFIGURATION FUZZING ===

   Baseline configuration:
     gCfg.stripCalAPerV: 15.000
     gCfg.cfgRev: 0

   --- Fuzz Test 1: Extreme and Negative Values ---
   [config] REJECTED: stripCalAPerV -100.000 outside 1..500 A/V
     [PASS] Refused: {"stripCalAPerV": -100} (err: stripCalAPerV -100.000 outside 1..500 A/V)
     [PASS]   stripCalAPerV strictly unmodified
     [PASS]   cfgRev strictly unchanged
     [PASS]   Rejection error recorded
   ...
   [config] REJECTED: stripCalAPerV 0.000 outside 1..500 A/V
     [PASS] Refused: {"stripCalAPerV": 0} (err: stripCalAPerV 0.000 outside 1..500 A/V)
   ...
   [config] REJECTED: stripCalAPerV 0.990 outside 1..500 A/V
     [PASS] Refused: {"stripCalAPerV": 0.99} (err: stripCalAPerV 0.990 outside 1..500 A/V)
   ...
   [config] REJECTED: stripCalAPerV 500.010 outside 1..500 A/V
     [PASS] Refused: {"stripCalAPerV": 500.01} (err: stripCalAPerV 500.010 outside 1..500 A/V)
   ...

   --- Fuzz Test 2: Valid Boundaries Acceptance ---
   [config] applied -> rev 1 (interval 5000ms, plug 60.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 1.00 A/V, touch 62/82%, setpoint 16.0..30.0 C)
     [PASS] Boundary min 1.0f accepted
     [PASS] stripCalAPerV updated to 1.0f
     [PASS] cfgRev incremented to 1
   [config] applied -> rev 2 (interval 5000ms, plug 60.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 500.00 A/V, touch 62/82%, setpoint 16.0..30.0 C)
     [PASS] Boundary max 500.0f accepted
     [PASS] stripCalAPerV updated to 500.0f
     [PASS] cfgRev incremented to 2
   [config] applied -> rev 3 (interval 5000ms, plug 60.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 15.00 A/V, touch 62/82%, setpoint 16.0..30.0 C)
     [PASS] Reset to nominal 15.0f
     [PASS] cfgRev incremented to 3

   --- Fuzz Test 3: NaN and Infinity Injection ---
     [PASS] Refused NaN/Inf string: {"stripCalAPerV": NaN}
     [PASS] Refused NaN/Inf string: {"stripCalAPerV": "NaN"}
     [PASS] Refused NaN/Inf string: {"stripCalAPerV": Infinity}
     [PASS] Refused NaN/Inf string: {"stripCalAPerV": "Infinity"}
     [PASS] Refused NaN/Inf string: {"stripCalAPerV": -Infinity}
     [PASS] Refused NaN/Inf string: {"stripCalAPerV": "-Infinity"}
   [config] REJECTED: stripCalAPerV nan outside 1..500 A/V
     [PASS] Programmatic quiet_NaN refused in cfgApplyJson
   [config] REJECTED: stripCalAPerV inf outside 1..500 A/V
     [PASS] Programmatic +Infinity refused in cfgApplyJson
   [config] REJECTED: stripCalAPerV -inf outside 1..500 A/V
     [PASS] Programmatic -Infinity refused in cfgApplyJson

   --- Fuzz Test 4: Type Confusion and String Injection ---
   [config] REJECTED: stripCalAPerV 0.000 outside 1..500 A/V
     [PASS] Cleanly rejected invalid type: {"stripCalAPerV": "invalid_string"}
   [config] REJECTED: stripCalAPerV 0.000 outside 1..500 A/V
     [PASS] Cleanly rejected invalid type: {"stripCalAPerV": ""}
   [config] REJECTED: stripCalAPerV 0.000 outside 1..500 A/V
     [PASS] Cleanly rejected invalid type: {"stripCalAPerV": false}
   [config] REJECTED: stripCalAPerV 0.000 outside 1..500 A/V
     [PASS] Cleanly rejected invalid type: {"stripCalAPerV": null}
   [config] REJECTED: stripCalAPerV 0.000 outside 1..500 A/V
     [PASS] Cleanly rejected invalid type: {"stripCalAPerV": []}
   [config] REJECTED: stripCalAPerV 0.000 outside 1..500 A/V
     [PASS] Cleanly rejected invalid type: {"stripCalAPerV": {}}
   [config] REJECTED: stripCalAPerV 0.000 outside 1..500 A/V
     [PASS] Cleanly rejected invalid type: {"stripCalAPerV": "AAAA..."}
     [PASS] Cleanly rejected invalid type: {"stripCalAPerV": "CCCC..."}
   [config] applied -> rev 4 (interval 5000ms, plug 60.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 1.00 A/V, touch 62/82%, setpoint 16.0..30.0 C)
     [PASS] Boolean true coerces to 1.0f in ArduinoJson and passes validation
     [PASS] stripCalAPerV set to 1.0f from boolean true
     [PASS] cfgRev bumped on boolean true conversion
     [NOTE] Finding documented: boolean true coerces to 1.0f without explicit .is<float>() check

   --- Fuzz Test 5: String Numeric Values ---
   [config] REJECTED: stripCalAPerV 0.500 outside 1..500 A/V
     [PASS] String "0.5" outside 1..500 rejected
   [config] REJECTED: stripCalAPerV 9999.000 outside 1..500 A/V
     [PASS] String "9999.0" outside 1..500 rejected

   --- Fuzz Test 6: Multi-Field Atomic Rejection ---
   [config] REJECTED: stripCalAPerV -100.000 outside 1..500 A/V
     [PASS] Multi-field payload with bad stripCalAPerV rejected as whole
     [PASS] publishIntervalMs kept previous value
     [PASS] plugCalAPerV kept previous value
     [PASS] stripCalAPerV kept previous value
     [PASS] cfgRev unchanged

   --- Fuzz Test 7: Post-Fuzz Health and Recovery ---
   [config] applied -> rev 6 (interval 5000ms, plug 60.60 A/V @ 230 V, ac 60.60 A/V @ 220 V, strip 22.50 A/V, touch 62/82%, setpoint 16.0..30.0 C)
     [PASS] Valid configuration accepted after extreme fuzzing
     [PASS] stripCalAPerV cleanly set to 22.5f
     [PASS] cfgRev properly incremented on valid config
   [config] factory reset -> rev 7
     [PASS] Factory reset restores default 15.0f
     [PASS] cfgRev bumped on factory reset

   === Fuzz Test Summary: PASSED (0 failures) ===
   ```

---

## 2. Logic Chain

1. **JSON Buffer Sizing in `readAndPublish()`**:
   - Worker M1 expanded `StaticJsonDocument<256>` to `StaticJsonDocument<384>` and `char buf[288]` to `char buf[384]`.
   - On the 32-bit ESP32 architecture (`sizeof(void*) == 4`), `sizeof(VariantSlot)` is exactly 16 bytes.
   - An object containing 17 fields requires 17 `VariantSlot`s (272 bytes).
   - The only non-literal string copied into the document pool is `gCfg.zoneLabel`, which is bounded to 31 chars + 1 null terminator = 32 bytes (`char zoneLabel[32]`).
   - Total maximum memory pool consumed on ESP32 is $272 + 32 = 304\text{ bytes}$, leaving $384 - 304 = 80\text{ bytes}$ of margin. `doc.overflowed()` is guaranteed false.
   - Empirical serialized string length under nominal telemetry is 242 bytes.
   - Empirical serialized string length under maximum realistic operating parameters is 282 bytes.
   - Under extreme combinatorial limits (negative numbers, maximum integer widths, 6-digit wattages, and 31-char zone labels), the maximum theoretical serialized length is 311 bytes.
   - Because $311 < 384$, the serialized string fits with an absolute guaranteed headroom of at least 73 bytes. Buffer overflow and truncation are physically impossible in `char buf[384]`.
   - In contrast, under the previous `char buf[288]`, the 311-byte worst-case payload would have truncated at byte 287, resulting in invalid JSON output and stack over-read during `Serial.printf("%s", buf)`. The worker's expansion to 384 bytes was therefore strictly necessary and mathematically sufficient.

2. **Configuration Fuzzing & Validation Safety**:
   - Negative values (`-100`, `-0.001`), zero (`0.0`), sub-threshold (`0.99`), super-threshold (`500.01`), and out-of-range (`1000.0`) are immediately caught by `!(c.stripCalAPerV >= 1.0f && c.stripCalAPerV <= 500.0f)`.
   - In IEEE-754 arithmetic, all comparisons with `NaN` evaluate to `false`. Hence `(NaN >= 1.0f && NaN <= 500.0f)` is `false`, and the negation `!(...)` is `true`, guaranteeing rejection.
   - `+Infinity <= 500.0f` is `false`; `-Infinity >= 1.0f` is `false`. Both evaluate to `true` under `!(...)` and are rejected.
   - Malformed JSON strings (such as unquoted `NaN` or `Infinity` or oversized strings > 512 bytes) fail in `deserializeJson()`, recording a `malformed JSON: InvalidInput` or `NoMemory` error without touching `gCfg`.
   - In multi-field payloads, a single invalid field prevents application of the entire message, preserving complete atomic integrity.

3. **Adversarial Finding — Boolean Type Coercion**:
   - In `cfgApplyJson()`, line 257 executes:
     `if (doc.containsKey("stripCalAPerV")) next.stripCalAPerV = doc["stripCalAPerV"];`
   - ArduinoJson's `VariantData::asFloat()` converts a boolean `true` variant to `1.0f` (and `false` to `0.0f`).
   - Because `1.0f` is the valid lower bound of the calibration range (`[1.0f, 500.0f]`), sending `{"stripCalAPerV": true}` is accepted and updates `stripCalAPerV` to `1.00 A/V`, incrementing `cfgRev`.
   - While `1.0 A/V` is within physical boundaries and does not cause a crash or memory corruption, a boolean is semantically not an analog calibration constant.
   - This same behavior exists across `plugCalAPerV`, `acCalAPerV`, and `touchOccupants`.
   - Mitigation: Guard float deserialization with `doc["stripCalAPerV"].is<float>()` or `doc["stripCalAPerV"].is<JsonNumber>()`.

---

## 3. Caveats

1. **Host Pointer Size Divergence**: On 64-bit host machines (`sizeof(void*) == 8`), `VariantSlot` is 32 bytes (double that of 32-bit ESP32). A document with 17 fields on 64-bit host consumes > 544 bytes, so host-side testing requires sizing `StaticJsonDocument` appropriately for 64-bit pointers when validating string serialization off-target. On target ESP32 hardware, 384 bytes provides 80 bytes of free headroom.
2. **Boolean True Acceptance**: Documented above as an empirical finding. Does not compromise memory safety or crash the node, but permits `1.0 A/V` calibration via boolean `true`.

---

## 4. Conclusion

**Verdict**: **APPROVE**

- **JSON Sizing & Truncation**: `StaticJsonDocument<384>` and `char buf[384]` provide complete protection against buffer truncation, memory exhaustion, and stack buffer overflow. Nominal size is 242 bytes; maximum realistic is 282 bytes; theoretical combinatorial worst-case is 311 bytes. Headroom in `char buf[384]` is at least 73 bytes.
- **Config Fuzzing**: All extreme values (`-100`, `0`, `0.99`, `500.01`, `NaN`, `Inf`, and long strings) are cleanly rejected without state corruption or NVS persistence. Multi-field atomicity is strictly preserved.
- **Finding Reported**: Type coercion of boolean `true` to `1.0f` documented with mitigation recommendation.

---

## 5. Verification Method

To independently reproduce and verify these empirical results:

1. **Run Payload Limits & Buffer Overflow Test**:
   ```powershell
   cd d:\ECON1\econ\edge\esp32
   g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/empirical_payload_test.cpp -o test/empirical_payload_test.exe
   .\test\empirical_payload_test.exe
   ```
   *Expected Output*: Output terminates with `Payload Limit Tests Completed: PASSED (0 failures)`.

2. **Run Comprehensive Configuration Fuzzing Suite**:
   ```powershell
   cd d:\ECON1\econ\edge\esp32
   g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/empirical_fuzz_test.cpp -o test/empirical_fuzz_test.exe
   .\test\empirical_fuzz_test.exe
   ```
   *Expected Output*: Output terminates with `Fuzz Test Summary: PASSED (0 failures)`.

3. **Run Regression Host Tests**:
   ```powershell
   cd d:\ECON1\econ\edge\esp32
   g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/host_config_test.cpp -o test/host_config_test.exe
   .\test\host_config_test.exe
   ```
   *Expected Output*: Output terminates with `PASSED (0 failures)`.

4. **Invalidation Conditions**:
   - Any serialized payload in `readAndPublish()` exceeds 383 characters.
   - Any canary guard byte around `buf[384]` is corrupted.
   - Any of `-100`, `0`, `0.99`, `500.01`, `NaN`, `Inf`, or oversized string is accepted by `cfgApplyJson()`.
