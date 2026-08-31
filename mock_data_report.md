# Codebase Scan Report: Unimplemented Features, Hardcoded Values & Mock Data

> **Scan Date**: August 2026  
> **Workspace Root**: `/Users/nguyenhoangkhoi/Documents/econ`  
> **Scope**: Frontend (`dashboard/`), Backend Engine (`server/`), AI Modules (`ai_modules/`), Forecasting (`backend/`), and Edge Firmware (`edge/`).

---

## Executive Summary

A comprehensive automated and manual code audit was conducted across the ECON digital twin repository. The codebase follows strong design principles (such as *omission over fabrication* for physical sensor data and data-driven ontology binding), but contains several categorized instances of:
1. **Mock Data & Offline Fallbacks**: Synthetic camera frame generators, bundled fallback geometry fixtures, and offline weather models.
2. **Hardcoded Values & Physical Constants**: Regional tariff rates, cohort sustainability benchmarks, default setpoints/deadbands, and baseline plant efficiencies.
3. **Unimplemented & Orphaned Modules**: Earlier iterations of 3D airflow vector field solvers, unwired YOLOv11 symbol detection pipelines, and an entirely superseded legacy Python backend (`backend/core_engine/`).

---

## 1. Frontend (`dashboard/`) Findings

### 1.1 Mock Data & Fallback Fixtures
- **Bundled Building Geometry Fallbacks**:
  - **Location**: `dashboard/src/building-data.json`, `dashboard/src/building-data-home.json`, `dashboard/src/buildingStore.js` (lines 13–14, 58–75)
  - **Description**: Bundled JSON fixtures serve as offline fallbacks when the live Go backend (`/api/building-data`) is unreachable. `buildingIsLive()` flags whether data is streamed or fallback.
- **Initial State Seeding**:
  - **Location**: `dashboard/src/useDigitalTwin.js` (`getInitialSimData()`, lines 27–68)
  - **Description**: Seeds default values (`plantCop = 3.2`, `co2 = 450`, `humidity = 50`, `ahuPressure = 0`, `temp = 24.0`) prior to the first binary FlatBuffers frame arrival over WebSocket.

### 1.2 Hardcoded Values & Magic Constants
- **EVN Electricity Tariff Constants**:
  - **Location**: `dashboard/src/tariff.js` (lines 1–45)
  - **Description**: Hardcoded 2025–2026 Vietnam EVN "Kinh doanh" ≥22 kV Time-Of-Use (TOU) rates under Decision 1279/QĐ-BCT & Decision 963/QĐ-BCT (`NORMAL = 2887`, `OFF_PEAK = 1609`, `PEAK = 5025` VND/kWh; peak window: 17:30–22:30 Mon–Sat; 26 charging days/month).
- **Sustainability Benchmarks & Grid Emission Factors**:
  - **Location**: `dashboard/src/sustainability.js` (lines 1–75)
  - **Description**: Hardcoded benchmark cohort values from ICEC 2021 office surveys (`EUI_BENCHMARK.hcmc = 116.4`, `hanoi = 105.9`, `both = 109.6` kWh/m²·yr, Vietnam grid carbon intensity factor `0.6766` kg CO₂/kWh).
- **Geographic Fallback Coordinates**:
  - **Location**: `dashboard/src/LiveWeatherBackground.jsx` (lines 12–15), `dashboard/src/App.jsx` (lines 444–445), `dashboard/src/MobileApp.jsx` (lines 67–68)
  - **Description**: Fallback coordinates for solar zenith and sky rendering default to Ho Chi Minh City (`lat = 10.8231, lon = 106.6297`) when `/api/weather` is unavailable.
- **Geometric Bounding Box Fallback**:
  - **Location**: `dashboard/src/floorGeometry.js` (lines 10–25)
  - **Description**: Fallback building footprint defaults to commercial tower plate dimensions (`60m x 40m`, center `(30, 20)`) when building geometry lacks explicit bounds.

### 1.3 Unimplemented & Orphaned Components
- **Orphaned Airflow Visualization Components**:
  - **Location**:
    - `dashboard/src/ConstrainedAirflow.jsx` (2D planar airflow solver)
    - `dashboard/src/AirflowField.jsx` (particle-based floor airflow)
    - `dashboard/src/AirflowVectorField.jsx` (curl-noise 3D vector field)
    - `dashboard/src/VectorFieldFlow.jsx` (vector field helper)
    - `dashboard/src/WindSimulation.jsx` (procedural wind overlay)
  - **Status**: **BUILT, UNWIRED / SUPERSEDED**. Retained in codebase for documentation/history, but superseded by `flowfield3d.js` masked Poisson volumetric solver in `ConstrainedAirflow3D.jsx`.
- **Uninstantiated Floorplan Node**:
  - **Location**: `dashboard/src/App.jsx` (`FloorplanNode`, lines 155–168)
  - **Description**: Registered in ReactFlow `nodeTypes` but never instantiated by `buildTopologyFromSim()`.
- **Diagnostic-Only Modules**:
  - **Location**: `dashboard/src/HardwareInspector.jsx`
  - **Description**: Diagnostic standalone tool accessible only via `?inspector` query parameter for raw MQTT inspection.

---

## 2. Backend & Go Physics Engine (`server/`) Findings

### 2.1 Mock Drivers & Offline Fallbacks
- **Climatological Weather Fallback**:
  - **Location**: `server/weather.go` (lines 35–65)
  - **Description**: Generates a deterministic 30.0°C–34.0°C diurnal sinusoidal temperature curve when Open-Meteo external weather API fails or is offline.
- **TimescaleDB Fallback Handling**:
  - **Location**: `server/db.go` (lines 15–40)
  - **Description**: If PostgreSQL/TimescaleDB is absent (`DB_URL` unset), historical metrics degrade gracefully without halting the live physics engine.

### 2.2 Hardcoded Values & Magic Constants
- **Baseline Chiller Plant COP**:
  - **Location**: `server/simulation/engine.go` (lines 80–110)
  - **Description**: Baseline COP defaults to `3.0`–`3.2` when physical AC electrical current clamps (`acW`) are not connected.
- **Solar Irradiance Multipliers**:
  - **Location**: `server/simulation/engine.go` (lines 145–160)
  - **Description**: Uses static façade orientation multipliers (`solarGainMultiplier * 10000 W`) rather than dynamic hourly solar flux integration.
- **Default Port & Admin Fallback**:
  - **Location**: `server/main.go` (lines 181–185), `server/auth.go`
  - **Description**: Default listening port `8080`; demo mode grants automatic authorization when `ECON_ADMIN_TOKEN` is unset.

### 2.3 Unimplemented & Specified Features
- **BESS Degradation & Round-Trip Losses**:
  - **Location**: `server/simulation/bess.go`
  - **Description**: Battery state of charge (SoC) integrates purely against nominal capacity (kWh) and inverter power limits; cycle ageing, degradation curves, and round-trip thermal losses are not implemented.
- **CO₂ Demand-Controlled Ventilation (DCV)**:
  - **Location**: `server/simulation/dynamics.go`, `server/simulation/engine.go`
  - **Description**: NDIR CO₂ values are ingested, σ-scored, and used for room air-change identification, but no automated actuator loop modulates VAV fresh-air dampers based on CO₂.
- **Latent Heat Humidity Coupling**:
  - **Location**: `server/simulation/engine.go`
  - **Description**: Humidity is ingested and logged, but the 2R1C thermal model is sensible-heat only (latent moisture loads are omitted).

---

## 3. AI Modules (`ai_modules/`) Findings

### 3.1 Unwired Machine Learning Pipelines
- **YOLOv11 Symbol Detector**:
  - **Location**: `ai_modules/branch_b_digitization/branch_b/skeyspot/detector.py`, `skeyspot_pipeline.py`
  - **Description**: Trained YOLOv11 model (mAP@50 = 69.5%) and OCR graph netlist builder are functional in standalone scripts, but **unwired** to the deployed `digitizer/app.py` service.
- **Netlist Graph Builder & Geometry Merger**:
  - **Location**: `ai_modules/branch_b_digitization/branch_b/ocr_graph_search/netlist_builder.py`, `geometry_merge.py`
  - **Description**: Built as part of blueprint symbol parsing, but not imported by production digitization runtime.

### 3.2 Superseded & Draft Modules
- **Legacy Occupancy Tracker**:
  - **Location**: `ai_modules/branch_a_occupancy/count_occupancy.py`
  - **Description**: Prototype script with commented-out inference and hardcoded count logic; superseded by `branch_a/yolo_bytetrack/yolo_tracker.py`.
- **Unimplemented Vision Modules**:
  - **Location**: `ai_modules/branch_a_occupancy/density_map/`, `ai_modules/branch_a_occupancy/rgbt_fusion/`
  - **Description**: Directory placeholders with READMEs; no runnable code.

---

## 4. Forecasting & Legacy Backend (`backend/`) Findings

### 4.1 Superseded Architecture (`backend/core_engine/`)
- **Location**: `backend/core_engine/` (FastAPI optimizer, SQLAlchemy models, Pydantic schemas)
- **Status**: **SUPERSEDED / DEAD CODE**.
- **Description**: 100% superseded by the Go engine (`server/`). Contains zero references anywhere in the active codebase.

### 4.2 Forecaster Model Fallbacks
- **Location**: `backend/forecasting/main.py`, `backend/forecasting/timesfm_forecaster.py`
- **Description**: LSTM model trained on single-building profiles; zero-shot TimesFM foundation model serves as automatic fallback to avoid out-of-distribution hallucinations on different building geometries.

---

## 5. Edge Firmware & Gateways (`edge/`) Findings

### 5.1 Host Mock Sensors & Synthetic Frame Generators
- **OV7670 Camera Driver Mock Mode**:
  - **Location**: `edge/esp32/include/camera_driver.h`, `edge/esp32/src/camera_driver.cpp`
  - **Description**: Provides synthetic test patterns (`PATTERN_PERSON_SILHOUETTE`, `PATTERN_EMPTY_SCENE`, `PATTERN_GRADIENT`, `PATTERN_SOLID_GRAY`) when physical OV7670 camera hardware is absent, disconnected, or compiled for host CI tests (`#ifdef HOST_TEST`).
- **Software ESP32 Emulator**:
  - **Location**: `edge/esp32/esp32_emulator.py`
  - **Description**: Software script emulating ESP32 MQTT telemetry for off-device testing.

### 5.2 Hardcoded Credentials & Network Fallbacks
- **Hardcoded Secret Templates**:
  - **Location**: `edge/esp32/include/wifi_secrets.h.example`
  - **Description**: Example configuration file with placeholder WiFi SSID, password, and MQTT broker IPs.

---

## 6. Summary Matrix

| Subsystem | File / Component | Category | Status / Impact |
|---|---|---|---|
| **Dashboard** | `GlobalMetricsPanel.jsx` | Dynamic Level Toggle | **Resolved** — Real per-level telemetry dynamically computed from live zone states. |
| **Dashboard** | `AirflowField.jsx`, `ConstrainedAirflow.jsx`, `VectorFieldFlow.jsx` | Unimplemented / Orphaned | **Dead code** — Superseded by `ConstrainedAirflow3D.jsx`. |
| **Dashboard** | `tariff.js` | Hardcoded Constants | **Static by design** — Fixed 2024–2026 EVN rate structure. |
| **Dashboard** | `sustainability.js` | Hardcoded Constants | **Static by design** — 116.4 kWh/m²·yr regional office cohort benchmark (ICEC 2021). |
| **Server** | `weather.go` | Mock / Fallback | **Resilience fallback** — Climatological model when Open-Meteo is offline. |
| **Server** | `simulation/bess.go` | Unimplemented Physics | **Model simplification** — No battery degradation or cycle ageing. |
| **Server** | `cli/dashboard.go` | Orphaned CLI | **Built, Unwired** — Separate operator terminal tool. |
| **AI Modules** | `branch_b/skeyspot/` | Unwired ML Pipeline | **Built, Unwired** — Standalone symbol detector not connected to digitizer service. |
| **AI Modules** | `branch_a/count_occupancy.py` | Draft / Mock | **Superseded** by `yolo_tracker.py`. |
| **Backend** | `backend/core_engine/` | Superseded Backend | **Dead code** — Replaced by Go engine. |
| **Edge** | `camera_driver.cpp` | Mock / Synthetic Generator | **Test harness fallback** — Active only when camera hardware is disconnected or in host CI. |

---
*Report generated automatically for ECON Digital Twin platform.*
