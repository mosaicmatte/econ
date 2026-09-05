# Empirical Adversarial Challenge & Verification Report: Frontend BIM Model Switching

## 1. Observation

### Codebase & Verification Scripts Examined
- **`dashboard/verify_bim_switching.js`**:
  - Validates pure model invariants:
    - Multi-Level Commercial Tower: 15 floors, 1350 zones, ~42,036 m² area, `bldg-econ-digitized`, `IS_IT_DOMINATED = true`.
    - Domestic House: 1 floor, 5 zones, ~72.3 m² area, `bldg-econ-house-hcmc`, `IS_IT_DOMINATED = false`.
  - Evaluates Puppeteer E2E tests against the built Vite production bundle (`dist/index.html`):
    - Interacts with `[data-testid="building-model-toggle"]`, `[data-testid="toggle-domestic-home"]`, `[data-testid="toggle-multilevel"]`.
    - Asserts level button count reduces from 15 (`level-btn-1`..`15`) to 1 (`level-btn-1`).
    - Asserts selected level and desktop stepper display `L1`.
    - Asserts React Flow P&ID topology updates from 91 nodes to 6 nodes (1 AHU + 5 Domestic House zones: Kitchen, Office, Living, Passage, Bathroom).
    - Asserts GlobalMetricsPanel telemetry reflects 5 zones (`5Z`) and ~72 m² conditioned area.
    - Asserts boundary clamping on level stepper under Domestic House model.
    - Asserts clean restoration of Office model (15 floor buttons, 90+ zones, >35,000 m² area).
    - Asserts zone selection reset on BIM switch.
    - Asserts mobile viewport (390x844) responsive rendering.

### Stress Test Findings & Hardening
- **Static Server Port Contention**:
  - When port 5193/5194 was busy due to previous runner processes, `createStaticServer` encountered `EADDRINUSE`.
  - **Hardened**: Updated `createStaticServer` in `verify_level_toggle.js` and `verify_bim_switching.js` to catch `EADDRINUSE` and dynamically fallback to ephemeral port `0`.
- **Puppeteer Mount Timing**:
  - Replaced fixed `setTimeout(1500)` with `await page.waitForSelector('[data-testid="building-model-toggle"]', { timeout: 10000 })` to ensure deterministic execution under varying CPU/sandbox scheduling.
- **Created Dedicated Adversarial Stress Harness (`dashboard/verify_adversarial_bim.js`)**:
  - 20 consecutive rapid model toggle switches at 50ms intervals.
  - Boundary clamp oracle: L15 -> domestic home L1 -> Step Next 10x -> Step Prev 10x (asserting hard clamp at L1).
  - Zone selection reset: Selecting a zone prior to switch clears orphan pointers and reverts right HUD dock to `ENTERPRISE OVERVIEW`.
  - Telemetry & shoelace area calculus validation.
  - Viewport boundary stress across 4K UHD (3840x2160), Tablet (768x1024), and Small Mobile (320x568).

### Test Execution Results
All test suites executed empirically in `dashboard/`:
1. `node verify_bim_switching.js`:
   - **Result**: `11 Total | 11 Passed | 0 Failed (14607ms)`
2. `node verify_level_toggle.js`:
   - **Result**: `13 Total | 13 Passed | 0 Failed (18885ms)`
3. `node verify_ai_actions.js`:
   - **Result**: `20 Total | 20 Passed | 0 Failed (6353ms)`
4. `node verify_adversarial_bim.js`:
   - **Result**: `7 Total | 7 Passed | 0 Failed (18879ms)`
- **Total Tests Passed Across All Suites**: 51 / 51 (100% Pass Rate).

---

## 2. Logic Chain

1. **Authenticity Assessment**:
   - `verify_bim_switching.js` does NOT use tautological assertions or mock proxies. It boots a headless Chromium browser instance via Puppeteer, serves the production bundle from `dashboard/dist`, uses request interception for asset dispatch, queries real DOM nodes (`.react-flow__node`, `[data-testid="building-model-toggle"]`, `[data-testid^="level-btn-"]`), reads computed inner text, and verifies CSS transitions and layout state.
2. **State & Geometry Invariance**:
   - Switching between `multi-level` and `domestic-home` triggers synchronous updates across `buildingStore.js` and `sustainability.js`.
   - The shoelace formula in `sustainability.js` derives the real polygon area from digitized zone coordinates (72.3 m² for domestic home vs 42,036.6 m² for the commercial tower).
3. **DOM & Reactive Synchronization**:
   - In `App.jsx`, `subscribeBuildingChange` listens for model changes and updates `currentBuilding`, `buildingModelType`, `activeFloor` (clamping to default floor or L1), and clears `selectedZone`.
   - `GlobalMetricsPanel.jsx` receives `availableFloors` from the active building (`bld.floors.map(f => f.level)`), rendering exactly 1 button for the domestic home and 15 for the commercial tower.
   - React Flow P&ID diagram constructs nodes from `simState.zones` filtered by the active floor zones, ensuring that when switching to domestic home, exactly 6 nodes (1 AHU + 5 zone units) are mounted with proper labels.
4. **Boundary Clamping & Stepper Logic**:
   - Stepper buttons (`level-step-prev` and `level-step-next`) clamp floor navigation within `[floors[0], floors[floors.length - 1]]`. On a 1-floor model, `floors.length === 1`, ensuring `activeFloor` cannot deviate from L1.
5. **Adversarial Resilience**:
   - Under 20 rapid consecutive toggles (50ms interval), zero uncaught exceptions or React boundary errors were emitted, and the DOM remained completely coherent.

---

## 3. Caveats

- Tests run against the production Vite build (`dashboard/dist`). Any future changes to `dashboard/src/` require running `npm run build` prior to test execution.
- In sandboxed environments without graphical display, Puppeteer runs in `--headless=true` mode with `--no-sandbox` flags, which is standard for CI/E2E pipelines.

---

## 4. Conclusion

The Frontend BIM Model Switching implementation and its Puppeteer verification script (`dashboard/verify_bim_switching.js`) are **genuine, mathematically sound, robust, and free of facades**. The system flawlessly handles rapid switching, edge viewports, boundary clamping, zone resets, and telemetry re-binding across all tested scenarios.

---

## 5. Verification Method

To independently reproduce and verify all findings, execute:

```bash
cd /Users/nguyenhoangkhoi/Documents/econ/dashboard
npm run build
node verify_bim_switching.js
node verify_level_toggle.js
node verify_ai_actions.js
node verify_adversarial_bim.js
```

All 51 tests across the 4 suites will execute and pass with exit code `0`.
