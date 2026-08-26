## 2026-08-26T04:02:07Z

You are Survey Explorer 3 for the project defined in /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md.
Your working directory is /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_3.
Create your working directory and write all your metadata files there.

MANDATORY: First read /Users/nguyenhoangkhoi/Documents/econ/.agents/ORIGINAL_REQUEST.md.

Task:
1. Investigate Dual-Mode Communication requirements (R2: Wi-Fi real-time broadcast + automatic USB Serial fallback) in /Users/nguyenhoangkhoi/Documents/econ/edge/esp32.
2. Analyze the current communication / telemetry implementation in the codebase.
3. Design the dual-mode communication mechanism:
   - Wi-Fi broadcasting (e.g. UDP broadcast, MQTT, WebSocket, or HTTP stream) when Wi-Fi is connected.
   - Automatic fallback to USB Serial (Serial.printf / JSON / framed telemetry) when Wi-Fi is unavailable or disconnected.
   - Reconnection state machine and transition handling without blocking or crashing the main detection loop.
   - Data payload schema for feeding real-time person tracking data into the topology/BIM model (e.g. timestamp, sensor_id, person_detected, confidence, count/bounding box if available).
4. Verify architecture isolation rules so the dual-mode communication integrates cleanly into the camera module interface.

Write your findings to /Users/nguyenhoangkhoi/Documents/econ/.agents/survey_explorer_3/survey_report.md and create a self-contained handoff.md. Send a completion message to the caller when done.
