# RD-04 mmWave Radar Implementation Plan

This document outlines the detailed implementation plan for integrating the RD-04 (mmWave Radar) sensor, incorporating the necessary corrections identified in the previous review round. 

## Key Corrections Addressed
1. **File Location**: This plan is explicitly written to `/Users/nguyenhoangkhoi/Documents/econ/implementation_plan.md`.
2. **Blocking C++ Code**: All `delay()` functions are replaced with non-blocking `millis()` based state checks.
3. **Missing WiFi Reconnect Logic**: Implemented an automatic WiFi reconnection loop in the C++ firmware.
4. **3.3V Power Requirement**: Corrected the wiring diagrams to strictly use 3.3V for the RD-04 VCC, as 5V can damage the sensor or cause unstable readings.
5. **Python Serial Blocking**: The Python script for serial reading uses a non-blocking approach with timeouts to ensure the main thread does not hang.

---

## Plan A: Direct ESP32 Edge Node Integration (Recommended)
This plan integrates the RD-04 directly into the ESP32 using C++ and publishes the detection data over MQTT.

### 1. Wiring (ESP32)
**CRITICAL**: The RD-04 operates on 3.3V logic and requires a 3.3V power supply. Do **not** connect it to the 5V/VIN pin.

* **RD-04 VCC** -> **ESP32 3.3V**
* **RD-04 GND** -> **ESP32 GND**
* **RD-04 OUT (GPIO)** -> **ESP32 GPIO 18** (or an available input pin)

### 2. C++ Code Implementation (Non-blocking & WiFi Reconnect)
Update `main.cpp` (or the respective sensor module) with the following logic:

```cpp
#include <WiFi.h>

const int MMWAVE_PIN = 18;
unsigned long lastCheckTime = 0;
const unsigned long checkInterval = 100; // Check every 100ms
unsigned long previousMillis = 0;
unsigned long interval = 30000;

void setupWiFi() {
  WiFi.begin("YOUR_SSID", "YOUR_PASSWORD");
  // Non-blocking WiFi setup can be managed in loop()
}

void checkWiFiConnection() {
  unsigned long currentMillis = millis();
  // if WiFi is down, try reconnecting
  if ((WiFi.status() != WL_CONNECTED) && (currentMillis - previousMillis >= interval)) {
    Serial.println("Reconnecting to WiFi...");
    WiFi.disconnect();
    WiFi.begin("YOUR_SSID", "YOUR_PASSWORD");
    previousMillis = currentMillis;
  }
}

void setup() {
  Serial.begin(115200);
  pinMode(MMWAVE_PIN, INPUT);
  setupWiFi();
}

void loop() {
  checkWiFiConnection();
  
  // Non-blocking sensor read
  if (millis() - lastCheckTime >= checkInterval) {
    lastCheckTime = millis();
    int presence = digitalRead(MMWAVE_PIN);
    
    // Only publish on state change or periodic heartbeat
    // publishToMQTT("telemetry/presence", presence);
    Serial.print("Presence: ");
    Serial.println(presence);
  }
}
```

---

## Plan B: PC / Gateway Integration via Serial (Python)
This plan assumes the RD-04 is connected to a serial bridge (like an FTDI adapter) or an ESP32 acting as a passthrough, and a host PC reads the data using Python.

### 1. Wiring (USB-to-Serial FTDI)
**CRITICAL**: Ensure the FTDI adapter jumper is set to 3.3V.

* **RD-04 VCC** -> **FTDI 3.3V**
* **RD-04 GND** -> **FTDI GND**
* **RD-04 TX** -> **FTDI RX**
* **RD-04 RX** -> **FTDI TX**

### 2. Python Code Implementation (Non-blocking)
Update the Python reader script to use timeouts so `readline()` doesn't block indefinitely.

```python
import serial
import time
import sys

SERIAL_PORT = '/dev/ttyUSB0'
BAUD_RATE = 115200

def main():
    try:
        # Crucial correction: Use timeout=0.1 to make readline non-blocking
        ser = serial.Serial(SERIAL_PORT, BAUD_RATE, timeout=0.1)
        print(f"Listening on {SERIAL_PORT}...")
    except serial.SerialException as e:
        print(f"Failed to open port {SERIAL_PORT}: {e}")
        sys.exit(1)

    try:
        while True:
            # The script can perform other tasks here (e.g., UI updates, sending heartbeats)
            
            # Non-blocking read
            if ser.in_waiting > 0:
                line = ser.readline().decode('utf-8').strip()
                if line:
                    print(f"RD-04 Data: {line}")
                    # Process the data
            
            # Prevent 100% CPU usage
            time.sleep(0.01)
            
    except KeyboardInterrupt:
        print("\nExiting...")
    finally:
        ser.close()

if __name__ == '__main__':
    main()
```
