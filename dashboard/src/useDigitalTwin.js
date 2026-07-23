import { useState, useEffect, useRef, useMemo } from 'react';
import * as flatbuffers from 'flatbuffers';
import { SimState } from './telemetry';
import { getBuilding } from './buildingStore';
const buildingData = getBuilding(); // live geometry — fetched before this module evaluates (see main.jsx)
import { API_BASE, WS_URL, getAdminToken } from './api';

const INTEGRATION_BY_TYPE = {
  'server-room': 1.05,
  'corridor': 0.95,
  'lobby': 0.90,
  'mechanical': 0.85,
  'retail': 0.75,
  'conference': 0.70,
  'office': 0.55,
};

// Data-driven fault targets: derive the selectable zones from the loaded building so any
// regenerated building-data.json "just works" (no hard-coded zoneIds to re-wire).
export const FAULT_ZONES = (() => {
  const zones = [];
  buildingData.floors.forEach(f => f.zones.forEach(z => zones.push({ ...z, level: f.level })));
  const servers = zones.filter(z => z.zoneType === 'server-room');
  const pick = (servers.length ? servers : zones).slice();
  return pick.map(z => ({ id: z.zoneId, label: `L${z.level} ${z.name.replace(/ Level \d+$/, '')}`, type: z.zoneType }));
})();
export const DEFAULT_FAULT_TARGET = FAULT_ZONES[0]?.id || '';

export const getInitialSimData = () => {
  const data = { scenario: 'peak', ahuPressure: 500, buildingLoadMw: 0, systemHealth: 100, totalOccupants: 0, coolingOutputMw: 0, plantCop: 0, energySavedMw: 0, bessDischargeMw: 0, bessSocPct: 0, zonesInSetback: 0, autoPilot: true, vavs: {}, zones: {}, logs: [] };
  buildingData.floors.forEach(floor => {
    floor.zones.forEach(z => {
      let cx = 20, cy = 20;
      if (z.centroid) {
        cx = z.centroid.x;
        cy = z.centroid.y;
      }
      
      if (z.hvacMapping) {
        data.vavs[z.hvacMapping.vavId] = { id: z.hvacMapping.vavId, targetZone: z.zoneId, flow: 0 };
      }
      data.zones[z.zoneId] = {
        id: z.zoneId,
        level: floor.level,
        label: z.name,
        type: z.zoneType,
        archetype: z.zoneType === 'server-room' ? 'server_room' : 'office_dcv',
        bim_asset_id: z.bim_asset_id,
        temp: z.thermalProperties?.setpoint || 24.0,
        setpoint: z.thermalProperties?.setpoint || 24.0,
        deadband: z.thermalProperties?.deadband || 2.0,
        alert: false,
        lightsOn: true, // live actuated state arrives from the backend stream
        occupancy: z.thermalProperties?.occupancy || 0, // real occupancy arrives from the backend stream
        integration_score: INTEGRATION_BY_TYPE[z.zoneType] || 0.6,
        baseHeatGain: z.thermalProperties?.internalHeatLoad || 0,
        centroid: { x: cx, y: cy }
      };
    });
  });
  return data;
};

export function useDigitalTwin(onUpdate) {
  const [activeScenario, setActiveScenario] = useState('peak');
  const [autoPilot, setAutoPilotState] = useState(true);
  const autoPilotRef = useRef(true);
  const [faultTarget, setFaultTargetState] = useState(DEFAULT_FAULT_TARGET);
  const faultTargetRef = useRef(DEFAULT_FAULT_TARGET);
  
  const setFaultTarget = (v) => {
    setFaultTargetState(v);
    faultTargetRef.current = v;
  };
  
  const [loadHistory, setLoadHistory] = useState([]);

  // [GEMINI IMPLEMENTATION START]
  // Fetch TimescaleDB history on mount
  useEffect(() => {
    fetch(`${API_BASE}/api/history`)
      .then(res => res.json())
      .then(data => {
        if (data && data.length > 0) {
          setLoadHistory(data);
        }
      })
      .catch(err => console.log('No history DB available:', err));
  }, []);
  // [GEMINI IMPLEMENTATION END]
  
  const initialData = useMemo(() => getInitialSimData(), []);
  const [simData, setSimData] = useState(initialData);
  const simDataRef = useRef(initialData);
  const activeScenarioRef = useRef(activeScenario);
  const lastHistUpdateRef = useRef(0);
  const wsRef = useRef(null);
  // Whether this connection may issue commands. Starts true so a demo engine (no token
  // configured) behaves exactly as before; a real engine flips it false the moment it
  // rejects a token, and the UI can then say so instead of silently dropping commands.
  const [wsAuthorized, setWsAuthorized] = useState(true);

  // Every value here is real: streamed straight from the Go physics engine's GlobalData
  // (buildingLoadMw, coolingOutputMw, plantCop, energySavedMw, totalOccupants) or computed
  // from the live per-zone temperatures. No fabricated ratios.
  const globalMetrics = useMemo(() => {
    const empty = { occupants: 0, avgTemp: 0, buildingLoadMw: 0, coolingOutputMw: 0, plantCop: 0, energySavedMw: 0, gridPowerMw: 0, bessDischargeMw: 0, bessSocPct: 0, hvacElectricalMw: 0, baseLoadMw: 0 };
    if (!simData || !simData.zones) return empty;
    let tempSum = 0;
    const zones = Object.values(simData.zones);
    zones.forEach(z => { tempSum += parseFloat(z.temp) || 24.0; });
    const bldgLoad = simData.buildingLoadMw || 0;
    const bessDischarge = simData.bessDischargeMw || 0;
    const coolMw = simData.coolingOutputMw || 0;
    const cop = simData.plantCop || 0;
    // The plant's electrical draw is the thermal cooling it delivers divided by its live COP;
    // whatever is left of the building load is the non-HVAC (lighting/plug/fan) baseline.
    // Both fall out of the stream, so they track the real plant instead of the ~2 MW constant
    // the panels used to subtract by hand.
    const hvacElectricalMw = cop > 0 ? Math.min(bldgLoad, coolMw / cop) : 0;
    return {
      occupants: simData.totalOccupants || 0,
      avgTemp: zones.length ? (tempSum / zones.length).toFixed(1) : 0,
      buildingLoadMw: bldgLoad,
      coolingOutputMw: coolMw,                        // thermal cooling delivered (MW)
      plantCop: cop,                                  // chiller-plant coefficient of performance
      energySavedMw: simData.energySavedMw || 0,     // saved by occupancy-driven setback
      hvacElectricalMw,                               // MW electrical drawn by the cooling plant
      baseLoadMw: Math.max(0, bldgLoad - hvacElectricalMw), // MW electrical drawn by everything else
      bessDischargeMw: bessDischarge,                 // + discharging to grid, - charging
      bessSocPct: simData.bessSocPct || 0,            // battery state of charge (0..100)
      gridPowerMw: Math.max(0, bldgLoad - bessDischarge), // battery discharge offsets grid draw
    };
  }, [simData]);

  const loadScenario = (key, onFloorJump) => {
    const baseScenario = key.startsWith('fault:') ? 'fault' : key;
    setActiveScenario(baseScenario);
    activeScenarioRef.current = baseScenario;
    
    if (key.startsWith('fault:') && onFloorJump) {
      const zid = key.slice(6);
      const floor = buildingData.floors.find(f => f.zones.some(z => z.zoneId === zid));
      if (floor) {
        onFloorJump(floor.level, zid);
      }
    }
    
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(key);
    }
  };

  // [GEMINI IMPLEMENTATION START]
  // Added by Gemini (Antigravity) on June 2026.
  // Exposes a function for the UI to dispatch manual override JSON payloads
  // via the WebSocket, allowing the user to veto the AI and control edge devices.
  const sendManualOverride = (action, zoneId) => {
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ action, zone: zoneId }));
    }
  };
  // [GEMINI IMPLEMENTATION END]

  // Auto-Pilot is a REAL engine control: toggling it sends {action:"autopilot",value}
  // over the websocket so the engine actually suspends/resumes the optimizer. We update
  // local state optimistically; the engine echoes the authoritative value back on the
  // stream (below), so a rejected or externally-changed state self-corrects.
  const setAutoPilot = (next) => {
    const val = typeof next === 'function' ? next(autoPilotRef.current) : next;
    autoPilotRef.current = val;
    setAutoPilotState(val);
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
      wsRef.current.send(JSON.stringify({ action: 'autopilot', value: val }));
    }
  };

  useEffect(() => {
    // The stream must survive a backend restart: without reconnect logic, redeploying
    // the engine silently freezes every open dashboard on its last frame — polls keep
    // refreshing so the page LOOKS alive while every streamed number is stale. Reconnect
    // with a short backoff until the effect is torn down.
    let alive = true;
    let retryTimer = null;

    const connect = () => {
      if (!alive) return;
      const ws = new WebSocket(WS_URL);
      ws.binaryType = 'arraybuffer';
      wsRef.current = ws;

      ws.onopen = () => {
        // Authorize for control before anything can be sent. Telemetry streams either
        // way, so a viewer with no token still sees the building — it just cannot
        // change it. An engine in demo mode ignores this entirely.
        const token = getAdminToken();
        if (token) ws.send(JSON.stringify({ action: 'auth', token }));
      };

      ws.onclose = () => {
        if (!alive) return;
        wsRef.current = null;
        retryTimer = setTimeout(connect, 3000);
      };

      ws.onmessage = (event) => {
      // Telemetry arrives as binary FlatBuffers; the engine's control replies (auth
      // result, refusals) are text. Feeding a string to ByteBuffer throws inside the
      // stream handler, so the two are separated before any parsing happens.
      if (typeof event.data === 'string') {
        try {
          const ctl = JSON.parse(event.data);
          if (ctl.type === 'auth') {
            setWsAuthorized(!!ctl.ok);
            if (!ctl.ok) console.warn('[econ] control token rejected — commands will be refused');
          } else if (ctl.type === 'error') {
            console.warn('[econ] engine refused a command:', ctl.error);
          }
        } catch {
          console.warn('[econ] unparseable text frame from engine:', event.data);
        }
        return;
      }
      const buf = new flatbuffers.ByteBuffer(new Uint8Array(event.data));
      const state = SimState.getRootAsSimState(buf);
      
      const prevData = simDataRef.current;
      const newSimData = { ...prevData, logs: [] }; // logs handled by TelemetryLogs directly or omitted here if not needed
      newSimData.zones = { ...prevData.zones };
      newSimData.vavs = { ...prevData.vavs };

      const zonesLen = state.zonesLength();
      for(let i = 0; i < zonesLen; i++) {
        const z = state.zones(i);
        const id = z.id();
        if (newSimData.zones[id]) {
            const temp = z.temp();
            let alert = false;
            const isFaultMode = activeScenarioRef.current === 'fault';
            const isRemediatingMode = activeScenarioRef.current === 'remediating';

            // Real thermal alarm: a zone running well above its comfort band (setpoint + deadband)
            // is critical regardless of scenario — this is what surfaces a genuinely hot/cooling-
            // starved room in the 3D model, topology, and the "Active Critical Faults" count.
            const prev = newSimData.zones[id];
            const sp = prev?.setpoint ?? 24;
            const db = prev?.deadband ?? 1;
            const CRITICAL_MARGIN = 5; // °C above the comfort band before a zone is "critical"

            if (isRemediatingMode && id === faultTargetRef.current) {
                alert = 'REMEDIATING';
            } else if ((isFaultMode && id === faultTargetRef.current) || temp > sp + db + CRITICAL_MARGIN) {
                alert = true;
            }
            // humidity/co2 are the zone's own bound sensor, straight off the stream
            // (0 = nothing measuring it, so the UI can fall back to a modelled figure).
            newSimData.zones[id] = {
              ...newSimData.zones[id], temp, load: z.load(), occupancy: z.occupants(),
              lightsOn: z.lightsOn(), humidity: z.humidity(), co2: z.co2(), alert,
              // APLC: live plug draw (clamp if metered, model otherwise) + sweep state.
              plugW: z.plugW(), plugShed: z.plugShed(),
            };
        }
      }

      const vavsLen = state.vavsLength();
      for(let i = 0; i < vavsLen; i++) {
        const v = state.vavs(i);
        const id = v.id();
        if (newSimData.vavs[id]) {
            newSimData.vavs[id] = { ...newSimData.vavs[id], flow: v.airflow() };
        }
      }

      const g = state.global();
      if (g) {
        // All real, computed by the Go engine from the live physics state.
        newSimData.buildingLoadMw = g.buildingLoadMw();
        newSimData.systemHealth = g.systemHealth();
        newSimData.totalOccupants = g.totalOccupants();
        newSimData.coolingOutputMw = g.coolingOutputMw();
        newSimData.plantCop = g.plantCop();
        newSimData.energySavedMw = g.energySavedMw();
        // BESS: + discharging to grid (shaving), - charging. gridPowerMw = load - discharge.
        newSimData.bessDischargeMw = g.bessDischargeMw();
        newSimData.bessSocPct = g.bessSocPct();
        // Engine-computed building CO2 (real sensors preferred; 0 only from a pre-upgrade server).
        newSimData.avgCo2 = g.avgCo2();
        // APLC: the plug-load picture, engine-computed so every client agrees.
        newSimData.plugKw = g.plugKw();
        newSimData.plugStandbyKw = g.plugStandbyKw();
        newSimData.plugShedKw = g.plugShedKw();
        newSimData.plugSavedKwh = g.plugSavedKwh();
        // Autonomous optimizer, straight from the engine: how many zones it is holding in
        // setback right now, and its real on/off state. zonesInSetback is what makes the
        // "autonomous action" cards report a fact instead of a per-card estimate.
        newSimData.zonesInSetback = g.zonesInSetback();
        newSimData.autoPilot = g.autoPilot();
        // The engine is authoritative: if its flag ever disagrees with the local toggle
        // (reconnect, another operator, a rejected send), adopt the engine's truth.
        if (g.autoPilot() !== autoPilotRef.current) {
          autoPilotRef.current = g.autoPilot();
          setAutoPilotState(g.autoPilot());
        }

        const nowMs = Date.now();
        if (nowMs - lastHistUpdateRef.current > 1000) {
          lastHistUpdateRef.current = nowMs;
          setLoadHistory(prev => {
            const timeStr = new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
            const pwrDraw = Number((g.buildingLoadMw() * 1000).toFixed(1)); // kW
            // CO2 comes from the engine, which prefers real NDIR sensors and only falls
            // back to its occupancy estimate when nothing is measuring — recomputing the
            // estimate here would silently overwrite a real 842 ppm with a modelled 450.
            // (avgCo2() reads 0 only against a pre-upgrade server; estimate locally then.)
            const avgCo2 = Math.round(g.avgCo2() > 0 ? g.avgCo2() : 400 + g.totalOccupants() * 0.85);
            // Occupancy travels as its own field: deriving it back out of co2 breaks the
            // moment co2 is sensor-driven rather than the estimate.
            const newHist = [...prev, { time: timeStr, pwr: pwrDraw, co2: avgCo2, occ: g.totalOccupants() }];
            if (newHist.length > 60) newHist.shift();
            return newHist;
          });
        }
      }

      simDataRef.current = newSimData;
      setSimData(newSimData);

      if (onUpdate) {
        onUpdate(newSimData, activeScenarioRef.current);
      }
      };
    };

    connect();

    return () => {
      alive = false;
      if (retryTimer) clearTimeout(retryTimer);
      if (wsRef.current) wsRef.current.close();
      wsRef.current = null;
    };
  }, []); // eslint-disable-line

  const [aiForecast, setAiForecast] = useState(null);

  // [GEMINI IMPLEMENTATION START]
  // Fetch AI Forecast periodically
  useEffect(() => {
    const fetchForecast = () => {
      fetch(`${API_BASE}/api/forecast`)
        .then(res => {
          if (!res.ok) throw new Error('Forecast unavailable');
          return res.json();
        })
        .then(data => {
          if (data && data.predicted_peak_load) {
            setAiForecast(data);
          }
        })
        .catch(err => console.log('Forecast DB/service unavailable', err));
    };

    fetchForecast(); // initial fetch
    const interval = setInterval(fetchForecast, 30000); // every 30s
    return () => clearInterval(interval);
  }, []);
  // [GEMINI IMPLEMENTATION END]

  return {
    simData,
    initialData,
    activeScenario,
    autoPilot,
    setAutoPilot,
    faultTarget,
    setFaultTarget,
    loadHistory,
    globalMetrics,
    loadScenario,
    sendManualOverride,
    aiForecast,
    wsAuthorized
  };
}
