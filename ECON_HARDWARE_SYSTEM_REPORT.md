# ECON Hardware & System Architecture Report

This report documents the hardware components, system architecture, and inner workings of the ECON Digital Twin project. It is designed to serve as a comprehensive reference for offline building, development, and debugging when disconnected from the physical hardware.

---

## 1. System Architecture Overview

ECON is a high-performance Building Digital Twin platform that bridges BIM data with real-time SCADA/HVAC telemetry.

### Software Stack
*   **Backend Physics Engine (Go):** Located in `server/`. Implements a continuous lumped-capacitance 2R1C (2-Resistor / 1-Capacitor) thermodynamic model for each zone, integrating against live weather data (Open-Meteo). Solves building airflow via the Hardy Cross fluid network method.
*   **Sustainability & Decarbonization Module (`server/carbon.go`):** Tracks Scope 2 Operational Carbon emissions, calculates predictive maintenance diagnostics, monitors space utilization efficiency, and integrates a live carbon market client for offsets.
*   **Frontend Dashboard (React / Three.js / WebGL):** Located in `dashboard/`. Uses Brick Schema ontologies and React Three Fiber to render a 3D isometric view of the building. State is streamed from the backend at 30 FPS using Google FlatBuffers over WebSockets to avoid browser GC stalls.
*   **Message Broker (MQTT):** Mosquitto MQTT broker hosts the telemetry stream between edge nodes, AI services, and the Go engine.
*   **Time-Series Database (TimescaleDB):** Persists telemetry (temperature, CO2, plug load, AFDD residuals, etc.) into 5-minute continuous aggregates for 90-day retention and raw 1-second buckets for 7-day retention.
*   **AI Forecaster & Operations (Python):** Located in `backend/forecasting`. Employs Google TimesFM (zero-shot) and supervised LSTMs for building load forecasting. Includes an offline standard-library recommender bundle.
*   **Computer Vision (Edge AI):** YOLOv8/YOLOv11 + ByteTrack for privacy-preserving semantic room segmentation, symbol detection (CubiCasa5K), and human occupancy tracking without sending video frames over the network.

---

## 2. Edge Hardware Ecosystem

The physical deployment consists of Gateway(s) and numerous Sensor/Actuator Edge Nodes. 

### 2.1 The Gateway (One per site)
*   **Hardware:** Raspberry Pi 5 2GB (or Pi Zero 2 W for budget setups).
*   **Function:** Hosts the Mosquitto broker and runs `gateway.py`, providing a local failsafe policy to turn off lights/HVAC in vacant zones if the main Go engine goes offline.

### 2.2 The Edge Nodes (ESP32 WROOM-32 / NodeMCU-32S)
The primary hardware node is the ESP32 (or a Pi Pico W). It utilizes a 5V/2A USB power supply. To prevent brownouts, all 5V components are run directly off the VIN rail rather than the ESP32's 3.3V onboard regulator (which provides ~500mA max).

#### **Sensing Loadout (The Inputs)**
1.  **Temperature & Humidity (`USE_SHT30`):** SHT30-IIC sensor (±0.2°C) on I2C (GPIO 21/22). Powered via 3.3V. Crucial for the precision needed to identify room thermal time constants.
2.  **CO2 (`USE_CO2`):** ASAIR ACD1200 NDIR sensor. 5V VCC, requiring a **BSS138 Level Shifter** to step down to the ESP32's 3.3V I2C bus. Floating SET pin for I2C.
3.  **Presence/Occupancy (`USE_MMWAVE`):** Ai-Thinker Rd-03 24GHz mmWave radar (detects stationary presence). Runs on 3.3V on GPIO 18.
4.  **Plug-Load Metering (`USE_PLUG`):** SCT-013 split-core current transformer (100A/50mA current-output). Clamped on the LIVE wire only. Uses a 33Ω burden resistor + a 1.65V mid-rail bias voltage divider. Read by ADC1 on **GPIO 34**.
5.  **AC Supply Temperature (`USE_SUPPLY_TEMP`):** DS18B20 waterproof probe in the AC discharge louvre on **GPIO 26** (1-Wire).
6.  **AC Power Metering (`USE_AC_CLAMP`):** A second SCT-013 on the AC supply line, utilizing identical analog front-end circuitry on **GPIO 35** (ADC1).
7.  **Ambient Light (`USE_LUX`):** BH1750 on I2C (GPIO 21/22). Used for solar gain approximations.

#### **Actuation Loadout (The Outputs)**
1.  **Infrared AC Control (`USE_IR_AC`):** 940nm IR LED driven by a 2N2222 (or S8050) NPN transistor. The base is fed by **GPIO 19** via a 1kΩ resistor. *Note: Cannot be placed on GPIO 22 to avoid corrupting the I2C clock.*
2.  **Lighting & Socket Relays (`USE_PLUG`):** 2-channel 5V Solid State Relay (G3MB-202P, zero-cross). Driven directly by TTL High Level on **GPIO 23** (Lights) and **GPIO 25** (Sockets). The socket relay boots fail-energized (HIGH).

---

## 3. Dynamic Identification & "Learned" Operations

A defining feature of ECON is its transition from static thresholds to real-time physical identification:
*   **Recursive Least Squares (RLS):** Every 5 minutes, the engine dynamically fits a zone's thermal time constant (θ₀), cooling authority (θ₁), per-occupant gain (θ₂), and measured air-change rate (φ₁) based on recent telemetry.
*   **Predictive Recommendations:** By resolving the exact physical properties of a room, ECON calculates realistic ETA for cooling loops, sets dynamic setback ceilings, and triggers AFDD (Automated Fault Detection and Diagnostics) residual alarms if a room's physics deviate from the real-time simulation.

---

## 4. Offline Development & Commissioning Workflows

When disconnected from the physical building or sensors, developers can utilize the following workflows to simulate hardware and test system boundaries:

### 4.1 Zero-Hardware Fast Path
You can spin up the whole Digital Twin and Simulation without hardware:
```bash
# 1. Start Broker & TimescaleDB
cd econ/server && docker compose up -d

# 2. Run Backend Engine
go run .

# 3. Run Frontend Dashboard
cd ../dashboard && npm ci && npm run dev
```

### 4.2 Building Fixture Generation
To simulate different building topologies offline:
```bash
python3 tools/officeize_fixture.py --write
rm -f server/data/room-dynamics.json && go run .
```

### 4.3 Simulation & Fallbacks
*   **Graceful Missing Sensors:** If an ESP32 is booted without a physical sensor attached (e.g., SHT30 missing), it will accurately report `tempReal: false` or omit the payload entirely (for CO2). The Go engine gracefully handles these omissions by relying on physical default models (e.g., 30°C design day defaults) or pure simulated values (VAV physics over measured clamp physics).
*   **Local Models Export:** Operators can pull trained offline LSTM / AI models from `/api/model/export` which contains a `recommender.py` capable of scoring telemetry entirely offline using the Python standard library.
