# ECON Center (formerly ECON) - Project Context

## Project Overview
ECON Center is a Web UI for Facility Managers featuring a cutting-edge 3D isometric digital twin of a 9-story office building. It visualizes real-time occupancy, HVAC energy usage, cooling output, and provides predictive insights.

## Core Technologies
- **Frontend Framework:** React (Vite)
- **3D Rendering:** React Three Fiber (R3F), Three.js
- **Styling:** Inline CSS, CSS modules (focus on Apple/Tesla-style glassmorphism and modern UI)
- **State Management:** React hooks

## Key Features & Architecture
### 1. 3D Digital Twin (`BuildingModel.jsx`)
- **Geometry:** Procedurally generated isometric 9-story building using `ExtrudeGeometry`.
- **Aesthetic:** Translucent, glowing green glass surfaces matching Tesla Energy's premium aesthetic.
- **Interactivity:** 
  - Users can tap on individual floors to zoom in.
  - Camera system permanently sits at the highest altitude (`y=50`) and looks down at the active floor, ensuring a steep, top-down isometric angle.
  - Uses `MeshPhysicalMaterial` for premium glass effects.

### 2. HUD & UI (`MobileApp.jsx`)
- **Metrics Display:** Four global metrics in the corners (Total HVAC Load, Cooling Output, Plant Efficiency, Grid Power) featuring sleek 1px vertical drop-lines and glowing dots.
- **Glassmorphism Overlay:** A dark, frosted glass bottom sheet for navigation menus (Energy, Impact, Analytics, System Logs).
- **Branding:** Rebranded from ECON to **ECON Center** per latest design requirements.

### 3. Environment & Background (`LiveWeatherBackground.jsx`)
- **Dynamic Background:** Uses a pure, edge-to-edge dark vector sky gradient.
- **Alignment:** Background is fixed to `top right` to ensure elements like the moon are perfectly visible on mobile screens.
- **Atmosphere:** Deep navy to pitch-black gradient to seamlessly merge with the 3D WebGL background space, eliminating horizon lines.

## Recent Updates (June 19, 2026)
- **3D Graphics Overhaul:** Transitioned from generic 3D shapes to a highly polished translucent architectural model.
- **Tesla Aesthetic:** Implemented dark mode, vertical metric connector lines, and pure black gradients.
- **Camera Rigging:** Fixed the `OrbitControls` camera target and position to lock at a steep downward viewing angle from the roof.
- **Asset Pipeline:** Generated and integrated new square-format, zero-margin vector sky backgrounds.
- **GitHub Sync:** Code is fully committed and pushed to `origin/main`.

## Current Focus
The project is in a stable, highly polished state regarding its front-end 3D visualization and UI layout. The architectural foundation is ready for backend telemetry integration, advanced scenario controls, or further UI refinements.
