# Handoff Report: Frontend Dashboard Survey (Requirement R3)

**Agent**: `teamwork_preview_explorer_survey_frontend`  
**Working Directory**: `d:\ECON1\econ\.agents\teamwork_preview_explorer_survey_frontend`  
**Handoff Type**: Hard (Investigation complete)  
**Reference Report**: `d:\ECON1\econ\.agents\teamwork_preview_explorer_survey_frontend\analysis.md`  

---

## 1. Observation

1. **Framework & Architecture**:
   - `dashboard/package.json` (lines 1–35):
     The project is named `econ-dashboard` and depends on `vite` (`^5.0.0`), `@vitejs/plugin-react` (`^4.2.0`), `react` (`^18.2.0`), `react-dom` (`^18.2.0`), `@react-three/fiber` (`^8.17.10`), `flatbuffers` (`^25.9.23`), and `lucide-react` (`^0.294.0`).
     There are no Next.js packages (`next`), no `next.config.js`, no `app/` router, and no `pages/` router. The app is a client-side SPA built with Vite.
   - `dashboard/src/main.jsx` (lines 12–19):
     Mounts the application via `bootBuilding().finally(async () => { const { default: Root } = await import('./Root.jsx'); ReactDOM.createRoot(...).render(<Root />); })`.
   - `dashboard/src/Root.jsx` (lines 27–31):
     Renders `<HardwareInspector />` if the query string contains `?inspector`, `<MobileApp />` if `window.innerWidth < 768`, or `<App />` for desktop.

2. **Telemetry Ingestion via WebSocket**:
   - `dashboard/src/api.js` (lines 17–18):
     `export const API_BASE = 'http://${backendHost}:${BACKEND_PORT}';`
     `export const WS_URL = 'ws://${backendHost}:${BACKEND_PORT}/ws';`
   - `dashboard/src/useDigitalTwin.js` (lines 190–192, 226–269):
     Opens `const ws = new WebSocket(WS_URL); ws.binaryType = 'arraybuffer';`.
     Parses incoming binary messages using Google FlatBuffers:
     `const buf = new flatbuffers.ByteBuffer(new Uint8Array(event.data));`
     `const state = SimState.getRootAsSimState(buf);`
     Iterates `state.zones(i)` (`ZoneData`), extracting `temp`, `load`, `occupants`, `plugW`, `plugShed`, etc. into `newSimData.zones[id]`.
     Iterates `state.global()` (`GlobalData`), extracting `buildingLoadMw`, `coolingOutputMw`, etc. into `newSimData`.

3. **Telemetry Ingestion via REST Polling**:
   - `dashboard/src/App.jsx` (lines 415–424):
     Polls `${API_BASE}/api/hardware` every 5000 ms:
     `fetch(`${API_BASE}/api/hardware`).then(res => res.json()).then(list => setHardwareNodes(Object.fromEntries(list.map(n => [n.zoneId, n]))))`
     Passes `hardwareNodes` down as a prop to `GlobalMetricsPanel` (line 933).

4. **Existing Power Metric Cards**:
   - `dashboard/src/GlobalMetricsPanel.jsx`:
     - Lines 66–98: Defines `DeltaCard` component with title, icon, value, unit, delta badge, and Recharts `Sparkline`.
     - Lines 212–215: Renders `TOTAL LOAD` card using `DeltaCard`:
       `title="TOTAL LOAD" icon={Zap} value={splitPowerMw(bldgLoad).value} unit={splitPowerMw(bldgLoad).unit}`
     - Lines 224–240: Renders `ENERGY SAVED` card with `Zap` icon in green and cost savings.
     - Lines 320–346: Renders `BESS` card with `Zap` icon, battery SoC %, and charging/discharging draw.
     - Lines 463–471: In zone drilldown mode (`selectedNode?.type === 'zone'`), renders `BulletGraph` for `Plug Draw` (`(selectedNode.data.plugW ?? 0) / 1000 kW`).
   - `dashboard/src/HardwareInspector.jsx` (lines 24–33):
     Defines `FIELDS` registry containing `plugW: { unit: 'W', flag: 'USE_PLUG' }` and `acW: { unit: 'W', flag: 'USE_AC_CLAMP' }`.

5. **Build and Verification**:
   - Executed `npm run build` in `dashboard`:
     Exited with code 0 (`built in 51.00s`, `dist/assets/index-*.js`, `dist/assets/Root-*.js`).

---

## 2. Logic Chain

1. **Framework Alignment**:
   - *Observation*: `dashboard/package.json` specifies Vite 5 and React 18 scripts (`"dev": "vite --host"`, `"build": "vite build"`), without Next.js.
   - *Deduction*: The user prompt mentions "Update the Next.js/React frontend", but all actual modifications belong strictly in the Vite React application located in `dashboard`. Acceptance criterion `npm run dev` and `npm run build` directly map to the Vite dev server and build runner.

2. **Data Pipeline Integration (`stripW`)**:
   - *Observation*: Live telemetry arrives via both binary FlatBuffers WebSocket (`useDigitalTwin.js`) and JSON polling (`/api/hardware` in `App.jsx`).
   - *Deduction*: To make the frontend robust regardless of whether the backend updates FlatBuffers or REST first, `stripW` must be unpacked in `useDigitalTwin.js` (`z.stripW()`), registered in `telemetry/zone-data.ts`, and checked from both `simData` and `hardwareNodes` in `GlobalMetricsPanel.jsx`.

3. **Card Placement & Visual Language**:
   - *Observation*: Power metrics in `GlobalMetricsPanel.jsx` (TOTAL LOAD, ENERGY SAVED, BESS) are styled as 8px rounded glass panels (`background: 'rgba(255,255,255,0.02)'`, `border: '1px solid var(--border-glass)'`, `padding: '12px'`) featuring `lucide-react` `Zap` icons, 10px uppercase labels, 16–20px bold monospace values, and status captions.
   - *Deduction*: The new "Power Strip" card must be placed in `GlobalMetricsPanel.jsx` directly alongside TOTAL LOAD and BESS in the Enterprise Overview section, matching this exact styling, using a `Zap` or `Plug` icon, displaying the live wattage in `W` (or `kW` when $\ge 1000\text{ W}$), and providing a fallback (`— W` / `"NO SENSOR"`) when absent.

4. **Bring-Up Tooling Integration**:
   - *Observation*: `HardwareInspector.jsx` acts as the hardware bring-up and troubleshooting screen (`?inspector`).
   - *Deduction*: Adding `stripW: { unit: 'W', flag: 'USE_STRIP' }` to `FIELDS` in `HardwareInspector.jsx` enables engineers to inspect the ACS712 sensor's live signal, noise floor, min/max values, and sample age without entering the 3D view.

---

## 3. Caveats

1. **Vite vs Next.js**: The repository uses Vite, not Next.js. Implementers must run `npm run build` and `npm run dev`, NOT `next dev` or `next build`.
2. **FlatBuffers TypeScript Generation**: `src/telemetry/zone-data.ts` was originally generated from FlatBuffers, but per repository comments, fields were manually maintained (`flatc` was not run). If field index 11 (`stripW`) is added to FlatBuffers, it must use vtable offset `26` and builder field index `11`.
3. **Sensor Scope**: The ACS712 sensor is wired to a specific ESP32 node (e.g. GPIO 35). In the global overview, `stripW` can display the total active strip draw across bound hardware nodes or the first reporting hardware node. When a zone is clicked, it shows that zone's strip draw.

---

## 4. Conclusion

Requirement R3 is fully mapped, scoped, and ready for implementation.
The changes require updating 4 files in `dashboard/src`:
1. `src/telemetry/zone-data.ts`: Add `stripW(): number` getter and `addStripW` builder method.
2. `src/useDigitalTwin.js`: Extract `stripW` in zone unpacking loop (`z.stripW()`).
3. `src/GlobalMetricsPanel.jsx`: Render the new "Power Strip" card alongside TOTAL LOAD, ENERGY SAVED, and BESS in the Enterprise Overview, plus zone-level display in Node Diagnostics.
4. `src/HardwareInspector.jsx`: Add `stripW` to the `FIELDS` dictionary.

---

## 5. Verification Method

1. **Build Verification**:
   ```powershell
   cd d:\ECON1\econ\dashboard
   npm run build
   ```
   *Expected result*: Process exits with code 0 and outputs production bundles in `dist/`.

2. **Development Server Verification**:
   ```powershell
   cd d:\ECON1\econ\dashboard
   npm run dev
   ```
   *Expected result*: Starts Vite dev server on `http://localhost:5173`.

3. **Runtime Fallback Check**:
   Open `http://localhost:5173` without hardware connected.
   *Expected result*: The "POWER STRIP" card renders with `— W` and `"NO SENSOR"`, without any runtime errors.

4. **Telemetry Ingestion Check**:
   Simulate an MQTT publish or mock `/api/hardware` returning `[{ "zoneId": "zone-1", "stripW": 185.4 }]`.
   *Expected result*: The "POWER STRIP" card dynamically updates to display `185.4 W` with `"ACTIVE DRAW"`.
