# ECON Edge Node Hardware Architecture (Verified)

## 1. ESP32 Edge Node (WROOM-32)

### Actuators
* **Lighting Relay (G3MB-202P SSR)**:
  * **Code**: `edge/esp32/src/main.cpp` (L75) sets `RELAY_PIN = 13`. Driven active HIGH.
  * **Wiring Contradiction**: `edge/WIRING.md` (L184) claims this is on GPIO23. `main.cpp` error checks (L237) also assume GPIO23, but the actual pin assignment is GPIO13.
* **Plug Socket Relay (G3MB-202P SSR)**:
  * **Code**: `edge/esp32/src/main.cpp` (L210) sets `PLUG_RELAY_PIN = 27`. Boots energized (HIGH).
  * **Wiring Contradiction**: `edge/WIRING.md` (L185) claims this is on GPIO25. `main.cpp` error checks (L243) assume GPIO27.
* **HVAC IR Emitter**:
  * **Code**: `edge/esp32/src/main.cpp` (L79) sets `IR_PIN = 25`. Uses `IRremoteESP8266` library to send frames (`USE_IR_AC=1`).
  * **Wiring Contradiction**: `edge/WIRING.md` (L186) claims this is on GPIO19.
* **Status LED (Onboard)**:
  * **Code**: `edge/esp32/src/main.cpp` (L80) sets `STATUS_LED = 2`. Displays MQTT connection state.

### Sensors
* **SHT30-IIC (Temp/Humidity)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L227-231). Shares I2C bus on `I2C_SDA = 21`, `I2C_SCL = 22` at address `0x44`. 3.3V logic.
* **ACD1200 NDIR (CO2)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L331-333). Shares I2C bus on GPIO21/22 at address `0x2A`. ⚠️ Requires BSS138 Level Shifter as its I2C lines are internally pulled up to 5V.
* **BH1750 (Lux)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L192-194). Shares I2C bus on GPIO21/22 at address `0x23`.
* **Rd-03/LD2410C (mmWave Radar)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L318). GPIO18 (`MMWAVE_PIN`). 3.3V logic logic out. OR'ed with PIR if both exist.
* **HC-SR501 (PIR)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L302). GPIO5 (`PIR_PIN`). 3.3V logic.
* **DS18B20 (Supply Temp)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L183). GPIO26 (`SUPPLY_TEMP_PIN`). 1-Wire interface, requires 4.7kΩ pull-up to 3.3V.
* **SCT-013 (Plug Current Clamp)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L204). GPIO34 (`PLUG_ADC_PIN`), ADC1_CH6. Input-only. Requires 33Ω burden and 1.65V bias network.
* **SCT-013 (AC Clamp) & ACS712 (Strip Power)**:
  * **Code/Wiring**: Both share GPIO35 (`AC_CLAMP_PIN` and `STRIP_ADC_PIN`) on ADC1_CH7. `main.cpp` L164 & L176. These are physically mutually exclusive on a single board, yet the code permits both to be enabled, leading to overlapping reads on the same physical pin.
* **Capacitive Touch (Presence Demo)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L399). GPIO32 (`TOUCH_PIN = T9`). Used when no physical presence sensor is installed.
* **DHT11/22 (Fallback Temp/Humidity)**:
  * **Code/Wiring**: `edge/esp32/src/main.cpp` (L291). GPIO4 (`DHT_PIN`). Uses 10kΩ pull-up.
* **Camera Detector (AI Module)**:
  * **Code**: `edge/esp32/src/main.cpp` (L911-L919). Supports a camera detector (`USE_CAMERA`) integration which tracks person count and provides confidence metrics.

## 2. Raspberry Pi Pico / Pico W
* **Onboard LED**: Simulated Light, toggled via software.
* **RP2040 Internal Die Temp**: ADC 4 (-4.0°C estimated offset). Used as a fallback if no SHT30 or DHT is present.
* **SHT30 (Temp/Humidity)**: `edge/pico/main.py` (L36-L38). GP4 (SDA) and GP5 (SCL), I2C0 0x44.
* **DHT11/22**: GP15.
* **BOOTSEL Button**: Polled at 4 Hz to simulate presence toggle.
* **Wired Jumper**: GP16. Pulled up; grounds for presence active state.
