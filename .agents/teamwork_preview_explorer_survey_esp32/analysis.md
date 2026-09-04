# Comprehensive Technical Analysis: ESP32 C++ Firmware Survey for Requirement R1

**Target Component**: `edge/esp32` C++ Firmware  
**Requirement R1**: "Update the C++ firmware in `edge/esp32` to read the ACS712 analog sensor on GPIO 35. Apply a new `stripCalAPerV` calibration multiplier, calculate the RMS power, and append `stripW` to the MQTT telemetry JSON payload."  
**Investigator**: `teamwork_preview_explorer_survey_esp32`  
**Date**: 2026-09-04  

---

## 1. Codebase Overview & Structure (`edge/esp32`)

The `edge/esp32` directory contains an Arduino-ESP32 C++ firmware project managed with PlatformIO. It acts as an IoT edge node binding to a physical building zone.

### File Inventory
| File | Size | Role / Description |
|---|---|---|
| `platformio.ini` | 8.8 KB | PlatformIO environment config (`[env:esp32dev]`), build flags, library dependencies, pinout documentation, and runtime configuration documentation. |
| `src/main.cpp` | 46.0 KB | Core firmware (1021 lines). Initializes hardware pins, WiFi, MQTT PubSubClient, runs sampling loops, publishes telemetry (`readAndPublish()`), processes commands (`handleCommand()`), and handles runtime config updates (`handleConfig()`). |
| `src/node_config.h` | 15.8 KB | Runtime configuration subsystem (310 lines). Defines `struct NodeConfig`, defaults, validation ranges, NVS flash persistence via ESP32 `Preferences`, JSON config updates, and `/state` serialization. |
| `src/wifi_secrets.h` | 99 B | Site WiFi and MQTT credentials (`WIFI_SSID`, `WIFI_PASS`, `MQTT_HOST`). |
| `src/wifi_secrets.example.h` | 916 B | Template for site credentials. |
| `test/host_config_test.cpp` | 6.0 KB | Host-side C++ unit tests verifying `node_config.h` defaults, validation rules, JSON deserialization/application, rejection of out-of-bounds values, and factory reset. |
| `test/run_host_tests.sh` | 840 B | Shell script to compile and run host config tests on Linux/macOS. |
| `test/arduino_shim.h`, `Arduino.h`, `Preferences.h` | ~2.2 KB | Host-side shims mimicking Arduino and ESP32 Preferences for offline testing. |
| `esp32_emulator.py` | 5.9 KB | Python MQTT software twin emulating the telemetry wire contract without physical hardware. |
| `wokwi.toml` & `diagram.json` | ~7.1 KB | Wokwi simulation configuration for virtual breadboard testing. |
| `README.md` | 7.8 KB | Firmware guide covering sensors, pins, flashing, topics, and operation. |

---

## 2. Current Sensor Reading Logic & RMS Power Calculation

### 2.1 Sensors Currently Supported
The firmware enables sensors modularly using `#ifndef` compile-time macros:
- `USE_SHT30` (I2C SDA 21 / SCL 22, addr 0x44): Temperature & humidity (default: 1).
- `USE_DHT` (GPIO 4): Fallback DHT22 temperature & humidity (default: disabled if SHT30 active).
- `USE_CO2` (I2C SDA 21 / SCL 22, addr 0x2A): ASAIR ACD1200 NDIR CO2 ppm (default: 1).
- `USE_PIR` (GPIO 5): Passive infrared motion detector.
- `USE_MMWAVE` (GPIO 18): 24 GHz FMCW radar (Rd-03 / HLK-LD2410C) for stationary presence.
- `USE_TOUCH_PRESENCE` (GPIO 32 / T9): Capacitive touch presence demo.
- `USE_PLUG` (GPIO 34 ADC1_CH6): SCT-013 split-core current clamp for plug circuit (default: 1).
- `USE_AC_CLAMP` (GPIO 35 ADC1_CH7): SCT-013 current clamp on AC compressor supply (default: 0).
- `USE_SUPPLY_TEMP` (GPIO 26): DS18B20 1-Wire probe for AC discharge louvre.
- `USE_LUX` (I2C addr 0x23): BH1750 ambient light sensor.

### 2.2 Existing Current & True-RMS Calculation Algorithm
Current sensing for `USE_PLUG` (lines 545–560 in `main.cpp`) and `USE_AC_CLAMP` (lines 567–582) uses True-RMS sampling:

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

#### Key Mechanics:
1. **Sampling Duration**: 100 ms (~5 full cycles of 50 Hz AC mains).
2. **Sampling Rate**: Executes tight loop `while (millis() - start < 100)`. On an ESP32 at 240 MHz, this collects several thousand samples per window.
3. **Window Starvation Guard**: `if (n < 100) return -1;` — if interrupted or starved, returns `-1` to trigger field omission.
4. **DC Offset Removal**: The algorithm calculates the window mean:
   $$\mu = \frac{1}{n} \sum_{i=1}^n v_i$$
   The AC component variance is:
   $$\sigma^2 = \frac{1}{n} \sum_{i=1}^n v_i^2 - \mu^2$$
   Taking $\text{rmsCounts} = \sqrt{\max(0.0, \sigma^2)}$ isolates the true AC RMS amplitude regardless of DC bias level!
5. **ADC Voltage Conversion**: Multiplies `rmsCounts` by $\frac{3.3\text{ V}}{4095.0}$ (12-bit ADC full-scale 0–4095 counts across 3.3V).
6. **Calibration Multiplier**: Multiplies by `gCfg.plugCalAPerV` (Amperes per Volt at the pin).
7. **Noise Floor Cutoff**: If `amps < 0.10`, clamps to `0.0f`.
8. **Apparent Power (Watts) Calculation**:
   $$\text{plugW} = \text{round}(\text{amps} \times \text{plugMainsV} \times 10) / 10.0$$
   Nominal mains voltage `gCfg.plugMainsV` defaults to 230.0 V (Vietnam single-phase).

### 2.3 ACS712 Sensor vs SCT-013
- **SCT-013**: Split-core current transformer. Requires an external burden resistor (33 $\Omega$ for -000) and an external $10\text{k}\Omega / 10\text{k}\Omega$ voltage divider to bias the AC signal to 1.65 V.
- **ACS712**: Integrated Hall-effect current sensor IC connected *inline* with the load. Powered by 5 V, it outputs an analog voltage centered at $V_{CC}/2 = 2.5\text{ V}$ when zero current flows. AC current causes sinusoidal swing around 2.5 V (sensitivity: 66 mV/A for 30A module, 100 mV/A for 20A, 185 mV/A for 5A).
- **Adaptation to Firmware**: Because the firmware's RMS calculation dynamically computes `mean = sum / n` and calculates the variance $\sigma^2 = \frac{\sum v^2}{n} - \mu^2$, **the DC midpoint of the ACS712 (2.5 V or resistor-divided 1.65 V) is automatically subtracted!**
- RMS voltage is converted to current using `stripCalAPerV` (A/V), and multiplied by mains voltage to yield RMS Power `stripW`.

---

## 3. GPIO 35 Configuration and Reading

### 3.1 Hardware Characteristics of GPIO 35
- **ADC Unit**: ADC1, Channel 7 (`ADC1_CHANNEL_7`).
- **Input-Only Pin**: GPIO 35 (along with 34, 36, 39) is input-only on the ESP32. It cannot be configured as an output, eliminating risk of driving the sensor line.
- **ADC1 Advantage**: ADC1 functions reliably while WiFi and Bluetooth are operational. (ADC2 cannot be used when WiFi is active due to RF calibration multiplexing).
- **Pull-up/down**: GPIO 35 has no internal pull-up or pull-down resistors. The sensor directly drives this high-impedance analog input.

### 3.2 Existing Codebase State
In `src/main.cpp` lines 140–167, GPIO 35 was conditionally defined for `USE_AC_CLAMP` (which defaults to 0):
```cpp
#if USE_AC_CLAMP
  #ifndef AC_CLAMP_PIN
    #define AC_CLAMP_PIN 35
  #endif
...
```
In `platformio.ini` line 64 and `WIRING.md`, GPIO 35 was historically documented as reserved for the AC clamp.

### 3.3 Required Configuration for ACS712 Strip Metering
To implement R1:
1. Define feature flag:
   ```cpp
   #ifndef USE_STRIP
     #define USE_STRIP 1
   #endif
   ```
2. Define ADC pin:
   ```cpp
   #ifndef STRIP_ADC_PIN
     #define STRIP_ADC_PIN 35
   #endif
   ```
3. In `setup()`:
   - Ensure `analogReadResolution(12)` is called (already present at lines 934 and 951 in `main.cpp`).
   - Optionally declare `pinMode(STRIP_ADC_PIN, INPUT);`.
   - Add initialization diagnostic log:
     ```cpp
     Serial.printf("[strip] ACS712 on GPIO%d (cal %.1f A/V) — power strip metering\n",
                   STRIP_ADC_PIN, (double)gCfg.stripCalAPerV);
     ```
4. Read routine:
   ```cpp
   float readStripAmps() {
     double sum = 0, sumSq = 0;
     int n = 0;
     unsigned long start = millis();
     while (millis() - start < 100) {
       int v = analogRead(STRIP_ADC_PIN);
       sum += v;
       sumSq += (double)v * v;
       n++;
     }
     if (n < 100) return -1;
     double mean = sum / n;
     double rmsCounts = sqrt(fmax(0.0, sumSq / n - mean * mean));
     float amps = (float)(rmsCounts * (3.3 / 4095.0) * gCfg.stripCalAPerV);
     return amps < 0.10 ? 0.0f : amps;
   }
   ```

---

## 4. Where `stripCalAPerV` Should Be Defined & Configured

In the ECON edge node architecture, tunables are governed by `src/node_config.h`:
> *"The compile-time flags become the DEFAULTS, and each one may be overridden at runtime over MQTT and persisted to NVS."*

### 4.1 Changes in `src/node_config.h`
1. **Compile-time Default Macro**:
   ```cpp
   #ifndef STRIP_CAL_A_PER_V
     #define STRIP_CAL_A_PER_V 15.0f  // Default for ACS712 30A (~66 mV/A -> 15.15 A/V)
   #endif
   ```
2. **Add to `struct NodeConfig`**:
   ```cpp
   struct NodeConfig {
     char     zoneLabel[32];
     uint32_t publishIntervalMs;
     float    plugCalAPerV;
     float    plugMainsV;
     float    acCalAPerV;
     float    acMainsV;
     float    stripCalAPerV;         // ACS712 strip sensor: amps per volt at ADC
     uint8_t  touchEnterPct;
     uint8_t  touchExitPct;
     uint8_t  touchOccupants;
     float    setpointMinC;
     float    setpointMaxC;
     uint32_t cfgRev;
   };
   ```
3. **Initialize in `cfgDefaults()`**:
   ```cpp
   c.stripCalAPerV = (float)STRIP_CAL_A_PER_V;
   ```
4. **Validation in `cfgValidate()`**:
   ```cpp
   if (!(c.stripCalAPerV >= 1.0f && c.stripCalAPerV <= 500.0f))
     return cfgFail("stripCalAPerV %.3f outside 1..500 A/V", (double)c.stripCalAPerV);
   ```
5. **Runtime JSON Ingestion in `cfgApplyJson()`**:
   ```cpp
   if (doc.containsKey("stripCalAPerV")) next.stripCalAPerV = doc["stripCalAPerV"];
   ```
   If updated, `next.cfgRev` is incremented, persisted to NVS flash via `cfgSave()`, and reported over `econ/config/<zone>/state`.
6. **State Reporting in `cfgSerializeState()`**:
   ```cpp
   out["stripCalAPerV"] = gCfg.stripCalAPerV;
   if (gCfg.stripCalAPerV != d.stripCalAPerV) ov.add("stripCalAPerV");
   ```

### 4.2 Mains Voltage for Strip
The power strip operates on the same 230 V single-phase circuit as `plugMainsV`. RMS power calculation can use:
$$\text{stripW} = \text{amps} \times \text{gCfg.plugMainsV}$$
Alternatively, a dedicated `stripMainsV` (defaulting to 230.0 V) could be provided if independent voltage tuning is desired, but using `gCfg.plugMainsV` avoids redundant schema expansion.

### 4.3 Documentation in `platformio.ini`
Add commentary to `platformio.ini`:
```ini
;   -DSTRIP_CAL_A_PER_V=15.0             ACS712 strip sensor A/V (runtime range 1..500)
```

### 4.4 Unit Testing in `test/host_config_test.cpp`
Add test assertions:
```cpp
check(std::abs(d.stripCalAPerV - 15.0f) < 1e-3, "strip calibration defaults to 15.0 A/V");
check(apply("{\"stripCalAPerV\":15.2}"), "strip calibration accepted");
check(!apply("{\"stripCalAPerV\":0.5}"), "stripCalAPerV below range refused");
```

---

## 5. MQTT Telemetry JSON Payload Construction (`stripW`)

### 5.1 Location in Codebase
In `src/main.cpp`, telemetry is assembled and published inside `readAndPublish()` (lines 691–811).

### 5.2 Formatting & Appending `stripW`
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

### 5.3 Buffer Size Sizing Analysis
In `readAndPublish()`:
- Current declaration: `StaticJsonDocument<256> doc;`
- Current output buffer: `char buf[288];`

**Capacity Risk Assessment**:
When all sensors (`temperature`, `humidity`, `tempReal`, `occupancy`, `co2`, `plugW`, `plug`, `supplyC`, `acW`, `lux`, `stripW`, `lights`, `setpoint`, `acReal`, `cfgRev`, `source`, `zone`) are active:
- ArduinoJson 6 allocates 16 bytes per key-value pair on 32-bit Xtensa architecture.
- 17 keys $\times$ 16 bytes = 272 bytes. A 256-byte document could silently refuse to append `stripW` due to memory exhaustion!
- The serialized JSON string with 17 fields reaches ~310–330 characters. `char buf[288]` would truncate the payload!

**Recommendation**:
Expand to:
```cpp
StaticJsonDocument<384> doc; // or 512
char buf[384];              // or 512
```

### 5.4 Serial Confirmation
Line 810 executes:
```cpp
Serial.printf("[mqtt] pub %s -> %s\n", TELEMETRY_TOPIC, buf);
```
This is directly matched by `bridge.py`:
```python
regex = re.compile(r"\[mqtt\] pub (.+?) -> (\{.*\})")
```
This fulfills Acceptance Criterion 2:
> *"ESP32 serial output confirms successful JSON MQTT publish including the `"stripW"` field."*

---

## 6. Build, Flash, and Test Verification

### 6.1 Verification Commands
| Action | Verified Command | Status / Observed Result |
|---|---|---|
| **Compile Firmware** | `python -m platformio run -e esp32dev` | **PASSED**: Built release binary in 45.3s (RAM: 14.2%, Flash: 70.8%). |
| **Flash Hardware** | `python -m platformio run -t upload` | Tested command structure; flashes board on auto-detected COM port (or `--upload-port COM9`). |
| **Serial Monitor** | `python -m platformio device monitor -b 115200` | Standard PlatformIO serial monitor. |
| **Host Config Unit Test** | `g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/host_config_test.cpp -o test/host_config_test.exe; .\test\host_config_test.exe` | **PASSED**: 0 failures across all validation, default, and persistence checks. |
| **USB Serial Bridge** | `python bridge.py` | Bridges ESP32 USB serial `[mqtt] pub ...` to local Mosquitto MQTT broker on port 1883. |

---

## 7. System-Wide Interface Contracts

### 7.1 Telemetry JSON Contract
- **MQTT Topic**: `econ/telemetry/<ZONE_TOPIC>` (e.g. `econ/telemetry/zone_1`)
- **Key Name**: `"stripW"`
- **Data Type**: `float` / JSON number (e.g. `142.5`)
- **Unit**: Watts ($W$)
- **Resolution**: 0.1 W (`round(watts * 10) / 10.0`)
- **Noise Suppression**: Values below 0.10 A are clamped to `0.0`
- **Error Handling**: On starved ADC window ($n < 100$) or sensor fault, the field `"stripW"` is **omitted** from the JSON payload (never fabricated as 0).
- **Cadence**: Published every `gCfg.publishIntervalMs` (default 5000 ms), or immediately on presence state transitions and accepted configuration updates.

### 7.2 Downstream Contracts Alignment
- **Go Backend (`server/mqtt.go`)**:
  Add `StripW *float64 `json:"stripW"` ` to `struct telemetryMsg`. Pointer type preserves "omitted vs zero" distinction.
- **Go Engine (`server/simulation/engine.go`)**:
  Store `HwStripW float64` and track provenance.
- **Database (TimescaleDB)**:
  `ALTER TABLE telemetry ADD COLUMN IF NOT EXISTS strip_w DOUBLE PRECISION;`
  Insert statement updated to save `strip_w`.
- **Frontend Dashboard (`dashboard/src/...`)**:
  Parse `stripW` and render a dedicated "Power Strip" card in Watts.
