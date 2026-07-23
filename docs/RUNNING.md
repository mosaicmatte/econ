# Running ECON

Every command needed to bring the twin up, in the order they depend on each other. Split
out of the README so that file can stay an orientation document.

## Bringing it up

### Prerequisites
Before starting, ensure you have the following installed on your system:
- **Docker & Docker Compose:** Required to run the Go backend and TimescaleDB containers.
- **Node.js (v18+):** Required to build and run the React/Vite frontend.
- **npm or yarn:** Package manager for Node.js.

### 1. Starting the Backend (Go Simulation Engine)
The backend runs the thermodynamic simulation, serves the building data, and streams WebSocket telemetry.

```bash
# Navigate to the server directory
cd server

# Build and start the Go backend and PostgreSQL database in detached mode
docker-compose up -d --build

# Verify the containers are running
docker ps
# You should see four containers: 'server-server-1' (engine, :8080), 'server-db-1'
# (TimescaleDB, :5432), 'server-mqtt-1' (Mosquitto broker, :1883) and
# 'server-forecasting-1' (Python LSTM service, :8000)

# (Optional) Follow the backend logs to see incoming WebSocket connections
docker logs -f server-server-1
```
*Note: The Go backend runs on `http://localhost:8080`. The WebSocket endpoint is at `ws://localhost:8080/ws`.*

### 2. Starting the Frontend (React Dashboard)
The frontend connects to the local Go backend to render the 3D map and interactive topology.

```bash
# Open a new terminal and navigate to the dashboard directory
# 🚨 CRITICAL: Ensure you are inside the econ/dashboard folder before running this!
cd econ/dashboard

# Install all Node.js dependencies (use --legacy-peer-deps to bypass Three.js peer conflicts)
npm ci

# Start the Vite development server
npm run dev
```

### 3. Accessing the Mobile Dashboard
To allow the frontend server to be accessible from a mobile device on your local network:

**Step 1: Start the Go Backend**
Since the backend sends the live data to your frontend, it needs to be running.
```bash
cd server
docker-compose up -d --build
```

**Step 2: Start the React Frontend**
`npm run dev` now binds the local network by default (`vite --host`), so no extra flag is
needed. Use `npm run dev:local` if you want the old loopback-only behaviour.
```bash
# 🚨 CRITICAL: Make sure you are in the dashboard folder!
cd econ/dashboard
npm ci
npm run dev
```

**Step 3: Open it on your Mobile Phone**
1. Make sure your mobile phone is connected to the **same Wi-Fi network** as your computer.
2. Look at the terminal where you ran `npm run dev`. Vite will output something like this:
   ```text
   ➜  Local:   http://localhost:5188/
   ➜  Network: http://192.168.x.x:5188/   <--- Use this one!
   ```
3. Open Safari or Chrome on your phone, and type in that exact **Network URL** (e.g., `http://192.168.1.5:5188`).

On viewports ≤ 820 px the site automatically serves the live mobile **Impact screen**
instead of the WebGL-heavy desktop stack: an autonomous-savings donut (streamed
`energySavedMw`), current load against the LSTM's predicted peak, occupancy by level,
and any physical edge nodes — all fed by the same WebSocket stream as the desktop twin.

*(Note: You can also preview it on desktop by right-clicking → Inspect and toggling the "Device Emulation" icon, or simply narrowing the browser window below 820 px).*

### 4. Testing the Backend via CLI Dashboard
For backend testing and telemetry debugging, you can use the standalone Go CLI Dashboard (htop-style).
```bash
# Open a new terminal and navigate to the CLI directory
cd server/cli

# Run the CLI dashboard
go run dashboard.go
```
*Note: The CLI dashboard dynamically sorts the hottest thermal zones to the top of your terminal and natively reads the high-speed FlatBuffers binary stream.*

### 5. Running the Autonomous DeepFloorplan Scanner
The `ai_modules/branch_b_digitization/deepfloorplan` directory contains a Streamlit app to autonomously digitize any architectural blueprint.
```bash
# Navigate to the DeepFloorplan directory
cd ai_modules/branch_b_digitization/deepfloorplan

# Install dependencies (if you haven't already)
pip install streamlit opencv-python networkx

# Run the Streamlit UI
streamlit run app.py
```
*Note: The AI will autonomously grid-search OpenCV parameters to mathematically derive the Space Syntax Dual Graph of the blueprint.*

**From blueprint to live twin (headless pipeline):** the Streamlit app is exploratory — the production path turns any floorplan image into the exact `building-data.json` the engine and dashboard consume:

```bash
cd ai_modules/branch_b_digitization

# 1. Rooms: segment the blueprint and assemble floors + thermal zones + hvacMapping
#    (schema documented in LAYOUT_SCHEMA.md)
python floorplan_to_buildingdata.py --image deepfloorplan/real_floorplan.png \
       --out /tmp/building-data.json --floors 15 --footprint 60x40

# 2. Symbols (optional): run the trained SkeySpot YOLO detector over the same sheet
#    (lights, thermostats, doors/windows — 69.5% mAP@50, paper §3.2) and enrich the
#    building + Brick ontology with the detected assets/netlist. Defaults operate on
#    the module-local building-data.json / brick-ontology.json:
python skeyspot_pipeline.py --image deepfloorplan/real_floorplan.png

# 3. Deploy as the twin's single source of truth (the engine image bakes data at build time)
cp /tmp/building-data.json ../../server/data/building-data.json
cp /tmp/building-data.json ../../dashboard/src/building-data.json
cd ../../server && docker compose up -d --build server
```

The dashboard hot-reloads onto the new geometry and the engine restart re-runs physics on it. The currently deployed building (15 floors / 1350 zones) came out of exactly this pipeline.

### 6. Training the YOLOv11 Computer Vision Models
If you wish to retrain the YOLOv11 models (either for Occupancy Detection in Branch A or Floorplan Semantic Segmentation in Branch B) rather than using the pre-trained weights:

```bash
# Navigate to the relevant AI module (e.g., Branch B Digitization)
cd ai_modules/branch_b_digitization/skeyspot

# Ensure Ultralytics is installed
pip install ultralytics

# Start the training job (modify data.yaml and epochs as needed)
# Note on Hardware Acceleration:
# - For Windows/Linux with NVIDIA GPU: append `device=0`
# - For Mac Apple Silicon (M1/M2/M3): append `device=mps`
# - For CPU only: omit the device flag
yolo task=detect mode=train model=yolo11n.pt data=data.yaml epochs=100 imgsz=640 batch=16 device=0
```
*Once training is complete, copy the output weights from `runs/detect/train/weights/best.pt` into the module's root directory before running the tracker.*

### 7. Testing the End-to-End Hardware Integration
To verify the complete physics loop (YOLO -> MQTT -> Go Engine -> ESP32), you can run the mock hardware emulator alongside the computer vision tracker:

**Terminal 1: Start the ESP32 Hardware Emulator**
```bash
cd edge/esp32
python3 esp32_emulator.py
```
*This script subscribes to the `econ/commands/+` MQTT wildcard and listens for thermodynamic actuation triggers (e.g., `LIGHTS_OFF`, `SETPOINT=26.0`).*

**Terminal 2: Start the YOLO Tracker (with local video bypass)**
```bash
cd ai_modules/branch_a_occupancy/yolo_bytetrack
pip install ultralytics opencv-python paho-mqtt
python3 yolo_tracker.py                       # demo footage (people-detection.mp4)
python3 yolo_tracker.py --source 0            # or a LIVE webcam pointed at a doorway
```
*As YOLO detects people in the sample video, it publishes telemetry to the Go Engine. When the occupancy drops, the Engine mathematically determines the required setback and fires an MQTT actuation command back to Terminal 1, audibly simulating a physical hardware relay click!*

**Real boards (ESP32 + Raspberry Pi Pico):** the same loop runs on physical hardware — full flash-and-demo guides live in [`edge/esp32/README.md`](edge/esp32/README.md) and [`edge/pico/README.md`](edge/pico/README.md). The 30-second version:

```bash
# ESP32 (WiFi node): copy src/wifi_secrets.example.h → src/wifi_secrets.h, fill it in, then
cd edge/esp32 && pio run -t upload
#   → touch GPIO32: the zone shows occupied in <0.2 s and the engine commands LIGHTS_ON back

# Raspberry Pi Pico (USB node): flash MicroPython, `mpremote cp main.py :main.py`, then
cd edge/pico && pip install -r requirements.txt && python bridge.py
#   → press BOOTSEL (~half a second): presence toggles, the onboard LED follows the engine
#   → hold a fingertip on the RP2040 chip: the zone's dashboard temperature climbs live

# Watch the engine bind each board to its own office zone, then inspect live state:
curl localhost:8080/api/hardware
docker exec server-mqtt-1 mosquitto_sub -t 'econ/#' -v     # raw MQTT bus
```

A node reporting a genuinely measured temperature (`tempReal:true` — DHT22, RP2040 die sensor) **pins its zone's physics to the sensor**. Hardware-bound zones carry a ⚡ LIVE HARDWARE badge in the zone micro-HUD, appear in the Enterprise Overview's EDGE HARDWARE list and the AI Insights hardware card, and visibly go dark in the 3D view when the engine cuts their lights.

**Physics-grounded AFDD (no training data):** every pinned zone also runs a sensor-free *shadow twin* of the 2R1C physics; sustained divergence between measurement and model is the fault signal. Fake a failing room:

```bash
# Seed the pin at a plausible temperature, then hold the "room" 6 °C hotter than physics allows:
docker exec server-mqtt-1 mosquitto_pub -t econ/telemetry/pico_1 \
  -m '{"zone":"Pico Lab","occupancy":2,"temperature":27.4,"source":"pico","tempReal":true}'
for i in 1 2 3 4 5 6; do docker exec server-mqtt-1 mosquitto_pub -t econ/telemetry/pico_1 \
  -m '{"zone":"Pico Lab","occupancy":2,"temperature":33.4,"source":"pico","tempReal":true}'; sleep 5; done
curl -s localhost:8080/api/hardware        # → "residual" climbs past 2.0 and "afddAlert": true
```

A red **AFDD: Physics Divergence** card appears in AI Insights and the node's row shows its live Δ residual.

**Forecast-driven pre-cooling:** click **ACTIVATE PRE-COOLING** on the High Grid Demand insight card (or `curl -X POST localhost:8080/api/precool`) — for 20 minutes every occupied zone is commanded 1.5 °C below its setpoint (watch `SETPOINT=22.5` go out on the MQTT bus), charging thermal mass ahead of the peak. A background poller opens the same window automatically whenever the LSTM's predicted peak crosses `PRECOOL_TRIGGER_MW` (default 2.0 MW).

### 8. Testing the Full Digital Twin Functionality
Once the Backend (Step 1) and Frontend (Step 2) are running, open your browser to `http://localhost:5173` to explore the complete feature set:

1. **3D Heatmap, Lights & Airflow:** Navigate the 3D model — zones color by live temperature deviation and **go dark when the engine cuts their lights** (energy-saving setback or manual veto). Open the **AIRFLOW** window for the volumetric per-floor flow field: instanced heat-colored arrows, diffuser cones whose color gradient encodes supply rate, and layer chips (ARROWS / WALLS / HVAC / WINDOWS / PEOPLE / POWER).
2. **Fault Injection (AI Auto-Pilot):** On the bottom control bar, pick a target and click **Inject**. Watch the zone run away thermally (red, pulsing) while the AI Auto-Pilot detects the anomaly and aggressively reroutes cooling to stabilize the building.
3. **Telemetry Profiler & Insights:** In the left dock, **Profiler** shows the live thermodynamic scatter (Power vs CO₂) with history sparklines persisted in TimescaleDB. **AI Insights** diagnoses faults, charts the LSTM forecast inline (VIEW PREDICTIONS), exposes real model metrics, and — when physical boards are connected — lists them with click-to-fly-to-zone.
4. **Riser Topology:** The **MAP LEVEL TOPOLOGY** panel renders the floor as a professional equipment schedule: the AHU on top feeding one terminal-unit card per zone (status LED, temp vs setpoint, occupancy, VAV airflow bar). Clicking a card selects the zone and flies the 3D camera to it.
5. **Manual Veto Overrides:** In the zone micro-HUD, click **FORCE OFF** or **MAX COOL**. The engine latches your human-in-the-loop override for 15 minutes (superseding the optimizer) and publishes the command to the zone's physical board if one is bound.
6. **Pre-Cooling & AFDD:** On the **High Grid Demand** card, click **ACTIVATE PRE-COOLING** — the engine opens a 20-minute window that drives every occupied zone 1.5 °C below setpoint (the LSTM poller opens the same window automatically ahead of predicted peaks). And if a hardware-bound room's measurement diverges from its calibrated physics, a red **AFDD: Physics Divergence** card names the zone and its live residual — fault detection with zero training data.

### Troubleshooting
- **Frontend isn't receiving data?** Ensure the backend is running and port `8080` is not blocked. Check the browser console (F12) for WebSocket connection errors.
- **Docker port conflict?** If port `8080` or `5432` is already in use by another application on your machine, stop the conflicting application or map different ports in the `docker-compose.yml` file.
---

# ECON: An Occupancy-Aware Digital Twin for Autonomous HVAC Optimization
