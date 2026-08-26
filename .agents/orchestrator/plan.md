# Orchestration Plan: ESP32 OV7670 Person Detection Module

## Objective
Implement an OV7670 camera-based person detection module on ESP32 WROOM with dual-mode communication (Wi-Fi broadcasting + Serial fallback) strictly isolated to the camera module scope, compiling cleanly via PlatformIO within flash/RAM constraints, and verified by adversarial and forensic audit agents.

## Phase 0: Survey & Architecture Discovery
1. Spawn 3 Explorers / Spec Miners:
   - Explorer 1: Inspect `/Users/nguyenhoangkhoi/Documents/econ/edge/esp32` existing codebase structure, PIR sensor implementation, build system (`platformio.ini`), dependencies, memory budgets, and pinouts.
   - Explorer 2: Inspect OV7670 camera drivers, ESP32 camera compatibility/pins, and lightweight ML model (TFLite Micro / person detection) on ESP32 WROOM (SRAM / Flash limits).
   - Explorer 3: Inspect communication layer (Wi-Fi broadcasting UDP/MQTT/WebSocket/HTTP vs Serial fallback) and interface integration with existing sensor architecture.
2. Synthesize findings into `/Users/nguyenhoangkhoi/Documents/econ/PROJECT.md` with Feature Inventory and Module Boundaries.

## Phase 1: Milestone Decomposition & Implementation
- Track A: Dual-Mode Communication & Data Format (Wi-Fi broadcast with automatic USB Serial fallback)
- Track B: Camera Driver & ML Person Detection Inference Pipeline (OV7670 + TFLite Micro / person detection)
- Track C: Integration, Isolation Verification & Build/Resource Optimization

## Phase 2: Verification, E2E Testing, & Audit
- Reviewers: Code completeness, isolation, architecture compliance.
- Challengers: Empirical edge case testing, Wi-Fi disconnect/reconnect fallback testing, ML inference verification, memory/resource checks.
- Forensic Auditor: Integrity verification against hardcoded outputs, dummy mocks, and requirement circumvention.

## Phase 3: Final Reporting
- Synthesize all findings and gate results.
- Send completion report to Sentinel parent.
