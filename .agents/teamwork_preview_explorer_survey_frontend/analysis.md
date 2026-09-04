# Frontend Dashboard Investigation & Survey Report (Requirement R3)

**Target**: `dashboard` directory  
**Requirement**: R3 ("Update the Next.js/React frontend in the dashboard directory to parse stripW from the WebSocket/API and display it as a new 'Power Strip' card on the dashboard, alongside the existing Power metrics.")  
**Investigator**: `teamwork_preview_explorer_survey_frontend`  
**Date**: 2026-09-04  

---

## 1. Executive Summary

A comprehensive investigation of the `dashboard` directory was conducted to plan the implementation of requirement R3: ingesting and rendering the new telemetry metric `stripW` (Power Strip Watts from the ESP32 ACS712 sensor).

Key findings:
1. **Framework Clarification**: The request references a *"Next.js/React frontend"*; however, the codebase in `dashboard` is actually built with **Vite 5 (`vite@^5.0.0`)** and **React 18 (`react@^18.2.0`)** as a Single Page Application (SPA). There are no Next.js files (`next.config.js`, App Router `app/`, or Pages Router `pages/`). All build scripts (`npm run dev`, `npm run build`) use Vite.
2. **Dual Telemetry Ingestion Architecture**:
   - **Real-time binary streaming via WebSocket** (`ws://${backendHost}:${BACKEND_PORT}/ws` in `src/useDigitalTwin.js`): Uses Google FlatBuffers binary protocol (`flatbuffers@^25.9.23`) to deserialize `SimState`, extracting per-zone metrics (`ZoneData`) and whole-building metrics (`GlobalData`).
   - **REST Polling** (`http://${backendHost}:${BACKEND_PORT}/api/hardware` in `src/App.jsx`): Polled every 5 seconds, returning an array of physical `HardwareNode` objects with live sensor readings from physical edge devices (ESP32 / Raspberry Pi Pico).
3. **Existing Power Metrics**:
   - Located primarily in `src/GlobalMetricsPanel.jsx` (the right-side HUD dock `hud-dock-right`), alongside `src/PlugLoadPanel.jsx` and `src/MobileEnergyScreen.jsx`.
   - The primary power cards on the global dashboard are **TOTAL LOAD** (`DeltaCard` with `Zap` icon and sparkline), **ENERGY SAVED** (card with `Zap` icon in green), and **BESS** (Battery Energy Storage System card with `Zap` icon).
4. **Proposed "Power Strip" Card Placement**:
   - In `src/GlobalMetricsPanel.jsx` under the global view (`!selectedNode`), positioned alongside `TOTAL LOAD`, `ENERGY SAVED`, and `BESS`.
   - In `src/GlobalMetricsPanel.jsx` under the zone diagnostics view (`selectedNode?.type === 'zone'`), rendering `stripW` alongside `Plug Draw`.
   - In `src/HardwareInspector.jsx`, registering `stripW` under `FIELDS` with unit `'W'` and build flag `'USE_STRIP'`.
5. **Build Verification**:
   - `npm run build` executed successfully without errors (`built in 51.00s`).

---

## 2. Framework, Dependencies & Directory Structure

### 2.1 Dependency Overview (`dashboard/package.json`)
```json
{
  "name": "econ-dashboard",
  "private": true,
  "version": "0.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite --host",
    "build": "vite build",
    "preview": "vite preview --host",
    "dev:local": "vite"
  },
  "dependencies": {
    "@react-three/drei": "^9.106.0",
    "@react-three/fiber": "^8.17.10",
    "@xyflow/react": "^12.11.0",
    "flatbuffers": "^25.9.23",
    "lucide-react": "^0.294.0",
    "puppeteer": "^25.1.0",
    "react": "^18.2.0",
    "react-dom": "^18.2.0",
    "react-is": "^19.2.7",
    "recharts": "^3.8.1",
    "three": "^0.184.0",
    "three-bvh-csg": "^0.0.18"
  },
  "devDependencies": {
    "@types/react": "^18.2.37",
    "@types/react-dom": "^18.2.15",
    "@vitejs/plugin-react": "^4.2.0",
    "vite": "^5.0.0"
  }
}
```

### 2.2 Entry Points & Routing
- `dashboard/index.html`: Contains `<div id="root"></div>` and loads `<script type="module" src="/src/main.jsx"></script>`.
- `dashboard/src/main.jsx`:
  Calls `bootBuilding()` from `src/buildingStore.js` to fetch the live building geometry from `/api/building` (or bundled fallback), then dynamically imports and mounts `<Root />`.
- `dashboard/src/Root.jsx`:
  Selects view mode based on URL and viewport:
  - If URL contains `?inspector` or `#inspector` -> `<HardwareInspector />` (hardware bring-up dashboard).
  - If viewport width `< 768px` -> `<MobileApp />` (mobile-optimized dashboard).
  - Otherwise -> `<App />` (cinematic 3D desktop digital twin).

### 2.3 Styling Architecture
- **No Tailwind CSS**: Despite modern React conventions, Tailwind is not installed.
- **CSS Custom Properties**: Defined in `dashboard/src/index.css`:
  - `--bg-obsidian`: `#000000`
  - `--bg-panel`: `#1e1e1e`
  - `--border-glass`: `#333333`
  - `--text-primary`: `#FFFFFF`
  - `--text-secondary`: `#AAAAAA`
  - `--text-muted`: `#666666`
  - `--accent-blue`: `#00A3E0`
  - `--accent-green`: `#00FF00`
  - `--accent-red`: `#FF0000`
  - `--accent-yellow`: `#FFFF00`
- **Inline Styles**: Components use scoped inline styling (`style={{ ... }}`) combined with CSS classes (`hud-container`, `hud-dock-right`, `mono`).
- **Icons**: Provided by `lucide-react` (e.g. `Zap`, `Plug`, `Activity`, `Users`, `Thermometer`, `PowerOff`).

---

## 3. Telemetry Ingestion Pipeline

Telemetry reaches the frontend through two decoupled paths:

```
+-------------------------------------------------------------------+
|                            ESP32 Node                             |
|  Reads ACS712 on GPIO 35 -> calculates RMS watts -> MQTT "stripW" |
+---------------------------------+---------------------------------+
                                  | MQTT econ/telemetry/<zone>
                                  v
+-------------------------------------------------------------------+
|                            Go Backend                             |
|  Unmarshals JSON { "stripW": 142.5, ... }                         |
|  Writes to TimescaleDB telemetry table (strip_w)                  |
|  Binds to zone & hardware state                                   |
+-------------------+-----------------------------------------------+
                    |                               |
     (1) WebSocket  |                (2) REST API   |
     Binary Flatbuf |                JSON Polling   |
                    v                               v
+--------------------------------+  +-------------------------------+
| dashboard/src/useDigitalTwin.js|  | dashboard/src/App.jsx         |
| new WebSocket(WS_URL)          |  | fetch('/api/hardware', 5s)    |
| SimState.getRootAsSimState()   |  | setHardwareNodes(map)         |
| extracts z.stripW()            |  | extracts node.stripW          |
+---------------+----------------+  +---------------+---------------+
                |                                   |
                +-----------------+-----------------+
                                  |
                                  v
+-------------------------------------------------------------------+
|               dashboard/src/GlobalMetricsPanel.jsx                |
|                    <PowerStripCard stripW={...} />                |
|     Rendered alongside TOTAL LOAD, ENERGY SAVED, BESS cards       |
+-------------------------------------------------------------------+
```

### 3.1 Path 1: WebSocket Binary Stream (`src/useDigitalTwin.js`)
- URL derived via `src/api.js`: `ws://${backendHost}:${BACKEND_PORT}/ws` (binary type: `arraybuffer`).
- In `src/useDigitalTwin.js` lines 226–270:
  ```javascript
  const buf = new flatbuffers.ByteBuffer(new Uint8Array(event.data));
  const state = SimState.getRootAsSimState(buf);
  
  const zonesLen = state.zonesLength();
  for (let i = 0; i < zonesLen; i++) {
    const z = state.zones(i);
    const id = z.id();
    if (newSimData.zones[id]) {
      newSimData.zones[id] = {
        ...newSimData.zones[id],
        temp: z.temp(),
        load: z.load(),
        occupancy: z.occupants(),
        lightsOn: z.lightsOn(),
        humidity: z.humidity(),
        co2: z.co2(),
        alert,
        plugW: z.plugW(),
        plugShed: z.plugShed(),
        supplyC: z.supplyC(),
        supplyReal: z.supplyReal(),
        // NEW: stripW: typeof z.stripW === 'function' ? z.stripW() : 0,
      };
    }
  }
  ```

### 3.2 Path 2: Polled REST API (`src/App.jsx`)
- Polled every 5 seconds (`src/App.jsx` lines 415–424):
  ```javascript
  useEffect(() => {
    let alive = true;
    const load = () => fetch(`${API_BASE}/api/hardware`)
      .then(res => res.json())
      .then(list => { if (alive) setHardwareNodes(Object.fromEntries((list || []).map(n => [n.zoneId, n]))); })
      .catch(() => {});
    load();
    const id = setInterval(load, 5000);
    return () => { alive = false; clearInterval(id); };
  }, []);
  ```
- The Go backend `HardwareStatus()` returns JSON objects with:
  ```json
  {
    "zoneId": "zone-101",
    "topic": "econ/telemetry/zone-101",
    "source": "esp32",
    "online": true,
    "plugW": 28.5,
    "stripW": 142.3
  }
  ```
- This data is passed into `GlobalMetricsPanel` as the `hardwareNodes` prop.

---

## 4. TypeScript & Schema Definitions

### 4.1 FlatBuffers TypeScript Classes (`dashboard/src/telemetry/zone-data.ts`)
The schema in `server/schema/telemetry.fbs` defines:
```flatbuffers
table ZoneData {
  id: string;
  temp: float;
  occupants: int;
  load: float;
  lightsOn: bool = true;
  humidity: float = 0;
  co2: float = 0;
  plugW: float = 0;
  plugShed: bool = false;
  supplyC: float = 0;
  supplyReal: bool = false;
  stripW: float = 0; // Appended field for R3
}
```

In `dashboard/src/telemetry/zone-data.ts`:
- Vtable offset for field index 11 (`stripW`):
  `offset = 26` (since index 0 is at offset 4, index 1 is at offset 6, ..., index 10 is at offset 24, index 11 is at offset 26).
- Accessor method:
  ```typescript
  stripW(): number {
    const offset = this.bb!.__offset(this.bb_pos, 26);
    return offset ? this.bb!.readFloat32(this.bb_pos + offset) : 0.0;
  }
  
  static addStripW(builder: flatbuffers.Builder, stripW: number) {
    builder.addFieldFloat32(11, stripW, 0.0);
  }
  ```

### 4.2 Hardware Inspector Field Registry (`dashboard/src/HardwareInspector.jsx`)
In `src/HardwareInspector.jsx` lines 24–33:
```javascript
const FIELDS = {
  temperature: { unit: '°C', flag: 'USE_SHT30 (or USE_DHT)' },
  humidity:    { unit: '%',  flag: 'USE_SHT30 (or USE_DHT)' },
  co2:         { unit: 'ppm', flag: 'USE_CO2' },
  occupancy:   { unit: '',   flag: 'USE_MMWAVE / USE_PIR' },
  plugW:       { unit: 'W',  flag: 'USE_PLUG' },
  supplyC:     { unit: '°C', flag: 'USE_SUPPLY_TEMP' },
  acW:         { unit: 'W',  flag: 'USE_AC_CLAMP' },
  lux:         { unit: 'lx', flag: 'USE_LUX' },
  stripW:      { unit: 'W',  flag: 'USE_STRIP' }, // NEW
};
```

---

## 5. Existing Power Metric Cards Analysis

### 5.1 Card Styles in `src/GlobalMetricsPanel.jsx`
1. **DeltaCard (Lines 66–98)**:
   ```jsx
   function DeltaCard({ title, icon: Icon, value, unit, delta, isGood, historyData, dataKey, sparkColor }) {
     return (
       <div style={{ background: 'rgba(255,255,255,0.02)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '12px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
         <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
           <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '10px', color: 'var(--text-secondary)' }}>
             <Icon size={12} /> {title}
           </div>
           <Sparkline data={historyData} dataKey={dataKey} color={sparkColor} />
         </div>
         <div style={{ display: 'flex', alignItems: 'flex-end', gap: '8px' }}>
           <div style={{ fontSize: '20px', fontWeight: 'bold', color: 'var(--text-primary)', fontFamily: 'monospace', lineHeight: 1 }}>
             {value} <span style={{ fontSize: '10px', color: 'var(--text-secondary)' }}>{unit}</span>
           </div>
           ...
         </div>
       </div>
     );
   }
   ```
2. **TOTAL LOAD Card (Lines 212–215)**:
   Uses `DeltaCard` with `title="TOTAL LOAD"`, `icon={Zap}`, `sparkColor="var(--accent-yellow)"`.
3. **ENERGY SAVED Card (Lines 226–240)**:
   Custom 12px padded card, `border: '1px solid rgba(34, 197, 94, 0.3)'`, `Zap` icon in green, value in monospace 16px.
4. **BESS (Battery) Card (Lines 327–346)**:
   Custom 12px padded card, `border: '1px solid var(--border-glass)'`, `Zap` icon in green, shows state of charge + charge/discharge rate.

### 5.2 Power Formatting Helper (`src/units.js`)
`src/units.js` exports:
- `powerMw(mw, { digits })`: Scales megawatts to `W`, `kW`, or `MW`.
- `powerKw(kw, opts)`: Scales kilowatts.
- `splitPowerMw(mw)`: Splits formatted string into `{ value, unit }`.
- For raw Watts (W), formatting directly as `${stripW.toFixed(1)} W` (or switching to `kW` when $\ge 1000\text{ W}$) maintains precision.

---

## 6. Detailed Implementation Design for Requirement R3

### 6.1 Placement of "Power Strip" Card
The new card will be inserted in `src/GlobalMetricsPanel.jsx` in the **Enterprise Overview** section (lines 208–348), placed immediately after the **TOTAL LOAD** and **OCCUPANCY** cards, alongside **ENERGY SAVED** and **BESS**.

In addition, a zone-level readout will be integrated into the **Node Diagnostics** section (`selectedNode?.type === 'zone'`) as a BulletGraph or metric card so operators drilling down into a zone see the live power strip draw.

### 6.2 Data Resolution Logic
In `src/GlobalMetricsPanel.jsx`, extract `stripW` using a tiered resolution strategy:
```javascript
// Resolve stripW with robust fallback across REST hardware nodes, WebSocket simData, and selected zone
const stripW = (() => {
  if (selectedNode?.data?.stripW != null && Number.isFinite(selectedNode.data.stripW)) {
    return selectedNode.data.stripW;
  }
  // Check active hardware nodes from /api/hardware
  const hwNodeWithStrip = Object.values(hardwareNodes || {}).find(n => n.stripW != null && n.stripW >= 0);
  if (hwNodeWithStrip && hwNodeWithStrip.stripW != null) {
    return hwNodeWithStrip.stripW;
  }
  // Check zone telemetry in simData from WebSocket FlatBuffers
  const zoneWithStrip = Object.values(simData?.zones || {}).find(z => z.stripW != null && z.stripW >= 0);
  if (zoneWithStrip && zoneWithStrip.stripW != null) {
    return zoneWithStrip.stripW;
  }
  // Global simulation field if provided
  if (simData?.stripW != null && Number.isFinite(simData.stripW)) {
    return simData.stripW;
  }
  return null; // Sensor offline / not yet reporting
})();
```

### 6.3 Card Component Design (`PowerStripCard`)
```jsx
{/* Power Strip (ACS712) Card */}
<div style={{
  background: 'rgba(255,255,255,0.02)',
  border: '1px solid var(--border-glass)',
  borderRadius: '8px',
  padding: '12px',
  display: 'flex',
  justifyContent: 'space-between',
  alignItems: 'center',
}}>
  <div>
    <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '10px', color: 'var(--text-secondary)' }}>
      <Zap size={12} color="var(--accent-yellow)" /> POWER STRIP
    </div>
    <div style={{ fontSize: '9px', color: 'var(--text-muted)', marginTop: '4px' }}>
      ACS712 · Bench load
    </div>
  </div>
  <div style={{ textAlign: 'right' }}>
    <div style={{ fontFamily: 'monospace', fontWeight: 'bold', color: stripW !== null ? 'var(--text-primary)' : 'var(--text-muted)', fontSize: '16px' }}>
      {stripW !== null ? (stripW >= 1000 ? (stripW / 1000).toFixed(2) : stripW.toFixed(1)) : '—'}
      <span style={{ fontSize: '10px', color: 'var(--text-secondary)', marginLeft: '4px' }}>
        {stripW !== null ? (stripW >= 1000 ? 'kW' : 'W') : 'W'}
      </span>
    </div>
    <div style={{ fontSize: '9px', color: stripW !== null ? 'var(--accent-green)' : 'var(--text-secondary)', marginTop: '2px' }}>
      {stripW !== null ? (stripW > 0 ? 'ACTIVE DRAW' : 'IDLE / OFF') : 'NO SENSOR'}
    </div>
  </div>
</div>
```

---

## 7. Interface Contract Specifications

| Property | Value | Rationale |
|---|---|---|
| **Field Name** | `stripW` | Matches ESP32 JSON payload (`"stripW"`), Go struct tag (`json:"stripW"`), and TimescaleDB column (`strip_w`). |
| **Data Type** | `float32` / `number` | Carries real-valued AC RMS active power. |
| **Units** | Watts (`W`) | Standard SI active power unit; scaled to `kW` when $\ge 1000\text{ W}$. |
| **Expected Range** | `0.0` to `3500.0` W | Standard 230V 10A/16A power strip bench load limit. |
| **Null / Missing Behavior** | Renders `— W` / `"NO SENSOR"` | Avoids displaying `0.0 W` when sensor is disconnected vs genuine zero. |
| **Negative Values** | Clamped to `0.0` W | AC RMS power cannot be negative; protects against sensor noise. |

---

## 8. Build, Execution & Verification Plan

1. **Production Build**:
   ```powershell
   cd d:\ECON1\econ\dashboard
   npm run build
   ```
   Must succeed with zero TypeScript or Rollup/Vite compilation errors.
2. **Development Server**:
   ```powershell
   cd d:\ECON1\econ\dashboard
   npm run dev
   ```
   Must launch Vite on `http://localhost:5173`.
3. **Dynamic Verification**:
   - When the backend or simulator streams telemetry with `"stripW": 150.5`, the "Power Strip" card must dynamically update without full-page refresh.
   - When no `stripW` is present, the card safely displays `— W` without throwing exceptions or causing blank screen crashes.
