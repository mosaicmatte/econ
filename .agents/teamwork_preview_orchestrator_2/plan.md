# Execution Plan — Project Orchestrator 2

## Mission
Resume and complete the end-to-end integration of the `stripW` telemetry metric across the Go Backend & TimescaleDB (Milestone 2), Next.js Dashboard (Milestone 3), and complete E2E verification (Milestone 4).

## Milestone Overview & Status
1. **Milestone 1: ESP32 Firmware Update**
   - Status: **DONE** (Passed PlatformIO build, host tests, dual reviews, dual challenges, forensic audit in Orchestrator 1).
2. **Milestone 2: Go Backend & TimescaleDB Update**
   - Status: **IN_PROGRESS**
   - Scope:
     - TimescaleDB `telemetry` schema update with non-destructive `ALTER TABLE`.
     - Go MQTT server structs parsing `stripW`.
     - Go SQL batch insert handling 7 columns including `strip_w`.
     - Backend simulation engine and FlatBuffers relay.
     - Go tests passing with 0 errors and docker logs clean.
3. **Milestone 3: Frontend Dashboard Update**
   - Status: **PLANNED**
   - Scope:
     - Parse `stripW` in FlatBuffers unpacker and REST polling.
     - Add new "Power Strip" card in `GlobalMetricsPanel.jsx`.
     - Register `stripW` in `HardwareInspector.jsx`.
     - Ensure Vite build (`npm run build`) and dev (`npm run dev`) pass.
4. **Milestone 4: E2E Integration & Verification**
   - Status: **PLANNED**
   - Scope:
     - End-to-end telemetry flow verification from edge/MQTT to DB to frontend.
     - Acceptance criteria verification (Go backend compiles/runs, DB retains historical data, frontend renders card).
     - Adversarial testing and final forensic audit.

---

## Detailed Milestone 2 Execution Cycle
1. **Investigation & Strategy**:
   - Spawn Explorers to assess the current state of `server/` files and TimescaleDB, and recommend exact implementation steps.
2. **Implementation**:
   - Spawn `teamwork_preview_worker` with Explorer recommendations and mandatory integrity warning.
   - Worker implements schema migration (`ALTER TABLE`), Go structs (`stripW`), batch SQL insert, and engine/FlatBuffers relay.
   - Worker runs Go test suite and verifies Docker container logs.
3. **Independent Review**:
   - Spawn 2 independent `teamwork_preview_reviewer` agents.
4. **Empirical Verification & Stress Testing**:
   - Spawn 2 independent `teamwork_preview_challenger` agents.
5. **Forensic Audit**:
   - Spawn `teamwork_preview_auditor` to verify implementation authenticity and absence of hardcoded facades.
6. **Milestone 2 Gate Check**:
   - Record verdicts in `GATE_STATUS.md`. Gate passes only if all reviewers APPROVE, challengers confirm correctness, and auditor reports CLEAN.

---

## Detailed Milestone 3 Execution Cycle
1. **Investigation**:
   - Spawn Explorers to verify dashboard telemetry hooks and card component layout.
2. **Implementation**:
   - Spawn Worker to implement `GlobalMetricsPanel.jsx`, `useDigitalTwin.js`, `zone-data.ts`, `HardwareInspector.jsx`.
   - Worker verifies `npm run build` and `npm run dev`.
3. **Review & Challenge**:
   - Spawn 2 Reviewers, 2 Challengers, and 1 Forensic Auditor.
4. **Milestone 3 Gate Check**:
   - Record verdicts and evaluate pass criteria.

---

## Milestone 4 & Final Acceptance
1. Spawn E2E test verification subagents.
2. Verify all acceptance criteria from `ORIGINAL_REQUEST.md`.
3. Synthesize final report and notify Sentinel via `send_message`.
