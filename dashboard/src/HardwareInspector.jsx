// TEMPORARY MODULE — hardware bring-up and troubleshooting.
//
// Reached at ?inspector (or #inspector) instead of the main console, deliberately: when
// you are chasing a loose wire you do not want a 3D building loading over the top of the
// numbers, and a separate page means this can be deleted without touching App.jsx.
//
// Pairs with server/devices.go. Read-only — it issues no commands, so it needs no
// operator token and cannot change the building.
//
// To remove: delete this file and the three lines in Root.jsx that reference it.

// React is imported by name as well as by hook: the project's other entry components
// (Root.jsx) do the same, and it keeps this file working under the classic JSX transform
// as well as the automatic one. Without it a stale Vite dep cache surfaces as
// "ReferenceError: React is not defined" from this file rather than from the cache.
import React, { useState, useEffect, useCallback } from 'react';
import { API_BASE } from './api';

const POLL_MS = 2000;

// Field metadata: units, and which build flag puts the field on the wire. The flag is here
// because the single most common bring-up question is "why is this field missing" and the
// answer is usually that the firmware was not built with it, not that the sensor failed.
const FIELDS = {
  temperature: { unit: '°C', flag: 'USE_SHT30 (or USE_DHT)' },
  humidity:    { unit: '%',  flag: 'USE_SHT30 (or USE_DHT)' },
  co2:         { unit: 'ppm', flag: 'USE_CO2' },
  occupancy:   { unit: '',   flag: 'USE_MMWAVE / USE_PIR' },
  plugW:       { unit: 'W',  flag: 'USE_PLUG' },
  supplyC:     { unit: '°C', flag: 'USE_SUPPLY_TEMP' },
  acW:         { unit: 'W',  flag: 'USE_AC_CLAMP' },
  lux:         { unit: 'lx', flag: 'USE_LUX' },
};

const fmt = (v, unit) =>
  v === null || v === undefined || Number.isNaN(v) ? '—' : `${(+v).toFixed(2)}${unit ? ' ' + unit : ''}`;
const ago = (s) => (s < 1 ? 'now' : s < 60 ? `${s.toFixed(0)}s` : `${(s / 60).toFixed(1)}m`);

/** Inline sparkline. No chart library: one <path>, and this module is meant to be deleted. */
function Spark({ points, width = 520, height = 90 }) {
  if (!points || points.length < 2) {
    return <div style={S.empty}>no history yet — needs a few samples</div>;
  }
  const vs = points.map((p) => p.v);
  const lo = Math.min(...vs), hi = Math.max(...vs);
  const span = hi - lo || 1;
  const t0 = new Date(points[0].t).getTime();
  const t1 = new Date(points[points.length - 1].t).getTime();
  const tspan = t1 - t0 || 1;
  const d = points
    .map((p, i) => {
      const x = ((new Date(p.t).getTime() - t0) / tspan) * width;
      const y = height - ((p.v - lo) / span) * (height - 8) - 4;
      return `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
  // Colour by provenance: measured rows are evidence, modelled rows are the simulation.
  const measured = points.filter((p) => p.q === 'measured').length;
  const stroke = measured === points.length ? '#4ade80' : measured === 0 ? '#f59e0b' : '#60a5fa';
  return (
    <div>
      <svg
        viewBox={`0 0 ${width} ${height}`}
        preserveAspectRatio="none"
        style={{ width: '100%', height, display: 'block' }}
      >
        <path d={d} fill="none" stroke={stroke} strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
      </svg>
      <div style={S.sparkMeta}>
        <span>min {lo.toFixed(2)}</span>
        <span>max {hi.toFixed(2)}</span>
        <span>{points.length} pts</span>
        <span style={{ color: stroke }}>
          {measured === points.length ? 'all measured' : measured === 0 ? 'all modelled' : `${measured}/${points.length} measured`}
        </span>
      </div>
    </div>
  );
}

function DeviceCard({ dev, staleAfter }) {
  const [metric, setMetric] = useState(null);
  const [series, setSeries] = useState(null);
  const [minutes, setMinutes] = useState(60);
  const [showRaw, setShowRaw] = useState(false);

  const loadSeries = useCallback(async (m, mins) => {
    setSeries(null);
    try {
      const r = await fetch(`${API_BASE}/api/devices/series?device=${encodeURIComponent(dev.id)}&metric=${m}&minutes=${mins}`);
      const j = await r.json();
      setSeries(j.points || []);
    } catch { setSeries([]); }
  }, [dev.id]);

  useEffect(() => { if (metric) loadSeries(metric, minutes); }, [metric, minutes, loadSeries]);

  const live = dev.online;
  const reporting = Object.entries(dev.fields || {}).filter(([, f]) => f.count > 0);
  const silent = Object.entries(dev.fields || {}).filter(([, f]) => f.count === 0 && f.omitted > 0);

  return (
    <div style={{ ...S.card, borderLeft: `3px solid ${live ? '#4ade80' : '#ef4444'}` }}>
      <div style={S.cardHead}>
        <div>
          <span style={S.devId}>{dev.id}</span>
          <span style={S.badge(live ? '#4ade80' : '#ef4444')}>{live ? 'ONLINE' : 'OFFLINE'}</span>
          {!dev.bound && <span style={S.badge('#f59e0b')}>UNBOUND</span>}
          {dev.tempReal === false && <span style={S.badge('#f59e0b')}>tempReal:false</span>}
          {dev.acKnown && !dev.acReal && <span style={S.badge('#f59e0b')}>acReal:false</span>}
        </div>
        <div style={S.dim}>
          {dev.source || '?'} · {dev.zone || 'no label'} · last {ago(dev.ageSec)} · {dev.rateHz.toFixed(2)} Hz · {dev.messages} msgs
          {dev.malformed > 0 && <span style={{ color: '#ef4444' }}> · {dev.malformed} malformed</span>}
        </div>
      </div>

      {!dev.bound && (
        <div style={S.warn}>
          Publishing, but the engine has not bound it to a zone. Usually the topic suffix does
          not match any zone id and no <code>zone</code> label in the payload matches either.
        </div>
      )}
      {!live && (
        <div style={S.warn}>
          Silent for more than {staleAfter}s. Check power and USB first, then WiFi, then whether
          the broker is reachable from the node.
        </div>
      )}

      <table style={S.table}>
        <thead>
          <tr><th style={S.th}>field</th><th style={S.th}>last</th><th style={S.th}>age</th>
              <th style={S.th}>n</th><th style={S.th}>omitted</th><th style={S.th}>range</th><th style={S.th}></th></tr>
        </thead>
        <tbody>
          {reporting.map(([name, f]) => {
            const meta = FIELDS[name] || {};
            const fage = (Date.now() - new Date(f.at).getTime()) / 1000;
            const stale = fage > staleAfter;
            return (
              <tr key={name}>
                <td style={S.td}>{name}</td>
                <td style={{ ...S.td, color: stale ? '#f59e0b' : '#e5e7eb', fontWeight: 600 }}>{fmt(f.last, meta.unit)}</td>
                <td style={{ ...S.td, color: stale ? '#f59e0b' : '#9ca3af' }}>{ago(fage)}</td>
                <td style={S.td}>{f.count}</td>
                <td style={{ ...S.td, color: f.omitted > 0 ? '#f59e0b' : '#4b5563' }}>{f.omitted}</td>
                <td style={S.td}>{f.min.toFixed(1)} … {f.max.toFixed(1)}</td>
                <td style={S.td}>
                  <button style={S.btn(metric === name)} onClick={() => setMetric(metric === name ? null : name)}>
                    {metric === name ? 'hide' : 'history'}
                  </button>
                </td>
              </tr>
            );
          })}
          {silent.map(([name, f]) => (
            <tr key={name}>
              <td style={{ ...S.td, color: '#6b7280' }}>{name}</td>
              <td colSpan={5} style={{ ...S.td, color: '#6b7280' }}>
                never reported — {f.omitted} messages without it
                {FIELDS[name]?.flag && <> · needs <code>{FIELDS[name].flag}</code></>}
              </td>
              <td style={S.td}></td>
            </tr>
          ))}
        </tbody>
      </table>

      {metric && (
        <div style={S.chartBox}>
          <div style={S.chartHead}>
            <strong>{metric}</strong>
            <span>
              {[15, 60, 360, 1440].map((m) => (
                <button key={m} style={S.btn(minutes === m)} onClick={() => setMinutes(m)}>
                  {m < 60 ? `${m}m` : `${m / 60}h`}
                </button>
              ))}
            </span>
          </div>
          {series === null ? <div style={S.empty}>loading…</div> : <Spark points={series} />}
        </div>
      )}

      <button style={S.linkBtn} onClick={() => setShowRaw(!showRaw)}>
        {showRaw ? 'hide' : 'show'} last raw payload
      </button>
      {showRaw && <pre style={S.pre}>{dev.lastJson || '(none)'}</pre>}
    </div>
  );
}

function SustainabilityTab() {
  const [data, setData] = useState(null);
  const [err, setErr] = useState(null);

  useEffect(() => {
    const fetchSus = async () => {
      try {
        const res = await fetch(`${API_BASE}/api/sustainability`);
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        setData(await res.json());
        setErr(null);
      } catch (e) {
        setErr(e.message);
      }
    };
    fetchSus();
    const interval = setInterval(fetchSus, 5000);
    return () => clearInterval(interval);
  }, []);

  if (err) return <div style={S.error}>Error fetching sustainability data: {err}</div>;
  if (!data) return <div style={S.dim}>Loading sustainability data...</div>;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
      <div style={S.card}>
        <h2 style={{marginTop: 0, color: '#4ade80'}}>Scope 2 Carbon Accounting</h2>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: '16px' }}>
          <div>
            <div style={S.dim}>Instantaneous Power</div>
            <div style={{fontSize: 24, fontWeight: 'bold'}}>{data.carbonAccounting.instantaneousPowerW.toFixed(1)} W</div>
          </div>
          <div>
            <div style={S.dim}>Emission Rate</div>
            <div style={{fontSize: 24, fontWeight: 'bold'}}>{data.carbonAccounting.instantaneousEmissionRateKgPerHour.toFixed(3)} kgCO2e/h</div>
          </div>
          <div>
            <div style={S.dim}>Grid Factor</div>
            <div style={{fontSize: 24, fontWeight: 'bold'}}>{data.carbonAccounting.gridEmissionFactorKgPerKwh.toFixed(2)} kg/kWh</div>
          </div>
          <div>
            <div style={S.dim}>Cumulative Emissions</div>
            <div style={{fontSize: 24, fontWeight: 'bold'}}>{data.carbonAccounting.cumulativeEmissionsKgCO2e.toFixed(3)} kgCO2e</div>
          </div>
        </div>
      </div>

      <div style={S.card}>
        <h2 style={{marginTop: 0, color: '#60a5fa'}}>Carbon Credit Recommendations (Live Market)</h2>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div>
            <div style={{marginBottom: 8}}><strong>Status:</strong> {data.carbonCreditRecommendations.recommendation}</div>
            {data.carbonCreditRecommendations.overBudget && (
              <>
                <div style={{color: '#f87171', marginBottom: 4}}>Deficit: {data.carbonCreditRecommendations.deficitKgCO2e.toFixed(2)} kgCO2e</div>
                <div style={{marginBottom: 4}}>Credits Needed: {data.carbonCreditRecommendations.wholeCertificatesNeeded} (approx {data.carbonCreditRecommendations.creditsNeededMetricTons?.toFixed(3)} MT)</div>
                <div style={{color: '#fbbf24', fontSize: 18, fontWeight: 'bold', marginTop: 8}}>Estimated Cost: ${data.carbonCreditRecommendations.estimatedCostUSD.toFixed(2)}</div>
              </>
            )}
          </div>
          <div style={{textAlign: 'right', background: '#111827', padding: '12px', borderRadius: '4px', border: '1px solid #374151'}}>
            <div style={{fontSize: 10, color: '#9ca3af', textTransform: 'uppercase'}}>Live Quote Source</div>
            <div style={{fontWeight: 'bold'}}>{data.carbonCreditRecommendations.marketQuote.source}</div>
            <div style={{color: '#4ade80', fontSize: 18}}>${data.carbonCreditRecommendations.marketQuote.spotPricePerMetricTonUSD.toFixed(6)} / MT</div>
            <div style={{fontSize: 10, color: '#9ca3af', marginTop: 4}}>Updated: {new Date(data.carbonCreditRecommendations.marketQuote.fetchedAt).toLocaleTimeString()}</div>
          </div>
        </div>
      </div>

      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '16px' }}>
        <div style={S.card}>
          <h2 style={{marginTop: 0, color: '#fbbf24'}}>Predictive Maintenance</h2>
          {data.predictiveMaintenance.activeAlertsCount === 0 ? (
            <div style={{color: '#4ade80'}}>All systems operating within normal parameters.</div>
          ) : (
            <div style={{display: 'flex', flexDirection: 'column', gap: '8px'}}>
              {data.predictiveMaintenance.warnings.map((w, i) => (
                <div key={i} style={{background: '#7f1d1d30', border: '1px solid #ef444450', padding: '8px', borderRadius: '4px'}}>
                  <div style={{color: '#f87171', fontWeight: 'bold', fontSize: 11, marginBottom: 4}}>[{w.type.toUpperCase()}] {w.equipmentId}</div>
                  <div style={{fontSize: 12}}>{w.message}</div>
                </div>
              ))}
            </div>
          )}
        </div>

        <div style={S.card}>
          <h2 style={{marginTop: 0, color: '#a78bfa'}}>Space Utilization</h2>
          <div style={{marginBottom: 12}}>
            <span style={{fontSize: 24, fontWeight: 'bold'}}>{data.spaceUtilization.overallEfficiencyPercent.toFixed(1)}%</span>
            <span style={S.dim}> Overall Efficiency</span>
          </div>
          <table style={{width: '100%', borderCollapse: 'collapse'}}>
            <thead>
              <tr>
                <th style={S.th}>Zone</th>
                <th style={S.th}>Occupants</th>
                <th style={S.th}>Capacity</th>
                <th style={S.th}>Efficiency</th>
              </tr>
            </thead>
            <tbody>
              {data.spaceUtilization.zones.map((z, i) => (
                <tr key={i}>
                  <td style={S.td}>{z.zoneId}</td>
                  <td style={S.td}>{z.liveOccupants}</td>
                  <td style={S.td}>{z.designCapacity}</td>
                  <td style={S.td}>
                    <span style={{color: z.efficiencyPercent > 100 ? '#f87171' : z.efficiencyPercent > 50 ? '#4ade80' : '#fbbf24'}}>
                      {z.efficiencyPercent.toFixed(0)}%
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </div>
  );
}

function UsbTab() {
  const [port, setPort] = useState(null);
  const [log, setLog] = useState('');
  const [input, setInput] = useState('');
  
  const [ssid, setSsid] = useState('');
  const [password, setPassword] = useState('');

  const connect = async () => {
    if (!('serial' in navigator)) {
      setLog(l => l + 'WebSerial API not supported in this browser.\n');
      return;
    }
    try {
      const p = await navigator.serial.requestPort();
      await p.open({ baudRate: 115200 });
      setPort(p);
      readLoop(p);
    } catch (e) {
      setLog(l => l + `Error: ${e.message}\n`);
    }
  };

  const readLoop = async (p) => {
    const textDecoder = new TextDecoderStream();
    p.readable.pipeTo(textDecoder.writable).catch(e => setLog(l => l + `Pipe Error: ${e.message}\n`));
    const reader = textDecoder.readable.getReader();
    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        setLog(l => l + value);
      }
    } catch (e) {
      setLog(l => l + `Read Error: ${e.message}\n`);
    } finally {
      reader.releaseLock();
      setPort(null);
    }
  };

  const send = async (text) => {
    if (!port) return;
    try {
      const encoder = new TextEncoder();
      const writer = port.writable.getWriter();
      await writer.write(encoder.encode(text + '\r\n'));
      writer.releaseLock();
      setLog(l => l + `> ${text}\n`);
    } catch (e) {
      setLog(l => l + `Write Error: ${e.message}\n`);
    }
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '16px' }}>
      <div style={S.card}>
        <h2 style={{marginTop: 0, color: '#3b82f6'}}>USB Serial Console</h2>
        <div style={{ marginBottom: 12 }}>
          {!port ? (
            <button style={{...S.btn(true), padding: '6px 14px'}} onClick={connect}>Connect to USB Device</button>
          ) : (
            <span style={S.badge('#4ade80')}>CONNECTED</span>
          )}
        </div>
        <div style={{...S.pre, height: 300, overflowY: 'auto', whiteSpace: 'pre-wrap', wordBreak: 'break-all'}}>
          {log || 'No data'}
        </div>
        
        <div style={{ marginTop: 12, display: 'flex', gap: 8 }}>
          <input style={S.input} value={input} onChange={e => setInput(e.target.value)} placeholder="Send command..." onKeyDown={e => e.key === 'Enter' && (send(input), setInput(''))} />
          <button style={{...S.btn(true), padding: '6px 14px'}} onClick={() => { send(input); setInput(''); }}>Send</button>
        </div>
      </div>

      <div style={S.card}>
        <h2 style={{marginTop: 0, color: '#a855f7'}}>WiFi Provisioning (USB)</h2>
        <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
          <button style={{...S.btn(false), padding: '6px 14px'}} onClick={() => { setSsid('homewifi'); setPassword(''); }}>Preset: homewifi</button>
          <button style={{...S.btn(false), padding: '6px 14px'}} onClick={() => { setSsid('wifi chua'); setPassword(''); }}>Preset: wifi chua</button>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <input style={S.input} placeholder="SSID" value={ssid} onChange={e => setSsid(e.target.value)} />
          <input style={S.input} placeholder="Password" value={password} onChange={e => setPassword(e.target.value)} />
          <button style={{...S.btn(true), padding: '6px 14px'}} onClick={() => send(`WIFI ${ssid} ${password}`)}>Send WiFi Config</button>
        </div>
      </div>
    </div>
  );
}

function ConfigTab() {
  const [cmd, setCmd] = useState('');
  const [status, setStatus] = useState(null);

  const sendCmd = async (e) => {
    e.preventDefault();
    if (!cmd.trim()) return;
    setStatus({ msg: 'Sending...', ok: true });
    try {
      const res = await fetch(`${API_BASE}/api/command`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: `CONFIG:${cmd}`, zone: 'all' })
      });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      setStatus({ msg: 'Command sent successfully', ok: true });
      setCmd('');
    } catch (err) {
      setStatus({ msg: err.message, ok: false });
    }
  };

  return (
    <div style={S.card}>
      <h2 style={{marginTop: 0, color: '#f59e0b'}}>HARDWARE CONFIGURATION</h2>
      <p style={S.dim}>Broadcast configuration to all listening hardware nodes via <code>POST /api/command</code>.</p>
      <form onSubmit={sendCmd} style={{ display: 'flex', gap: 8, marginTop: 12 }}>
        <input style={S.input} value={cmd} onChange={e => setCmd(e.target.value)} placeholder="e.g. SET_REPORT_INTERVAL 5000" />
        <button type="submit" style={{...S.btn(true), padding: '6px 14px'}}>Send CONFIG</button>
      </form>
      {status && (
        <div style={{ marginTop: 12, color: status.ok ? '#4ade80' : '#ef4444', fontSize: 12 }}>
          {status.msg}
        </div>
      )}
    </div>
  );
}

export default function HardwareInspector() {
  const [data, setData] = useState(null);
  const [events, setEvents] = useState(null);
  const [quality, setQuality] = useState(null);
  const [qMinutes, setQMinutes] = useState(60);
  const [err, setErr] = useState(null);
  const [tab, setTab] = useState('devices');

  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const r = await fetch(`${API_BASE}/api/devices`);
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        const j = await r.json();
        if (alive) { setData(j); setErr(null); }
      } catch (e) {
        // Distinguish "the engine is down" from "the engine is up but predates this
        // module". Both arrive here as an opaque TypeError: Go's default mux answers an
        // unknown route with a bare 404 that carries no Access-Control-Allow-Origin, so
        // the browser refuses to let us read it and fetch rejects exactly as it would for
        // a refused connection. Reporting both as "cannot reach the engine — start it"
        // sends you to restart a process that is already running and answering.
        //
        // /api/hardware has existed far longer than the inspector, so it is the probe:
        // if it answers, the engine is alive and simply lacks registerDeviceRoutes.
        let kind = 'down';
        try {
          const probe = await fetch(`${API_BASE}/api/hardware`);
          if (probe.ok) kind = 'stale';
        } catch { /* genuinely unreachable */ }
        if (alive) setErr({ kind, detail: e.message });
      }
    };
    tick();
    const id = setInterval(tick, POLL_MS);
    return () => { alive = false; clearInterval(id); };
  }, []);

  useEffect(() => {
    if (tab !== 'events') return;
    setEvents(null);
    fetch(`${API_BASE}/api/devices/events`).then((r) => r.json()).then((j) => setEvents(j.events || [])).catch(() => setEvents([]));
  }, [tab]);

  // The quality roll-up groups over the raw sample table, which is large — a full
  // building persists ~1500 rows/s. Without an explicit loading state a slow window
  // renders as an empty table, which reads as "no data" rather than "still counting".
  useEffect(() => {
    if (tab !== 'quality') return;
    setQuality(null);
    fetch(`${API_BASE}/api/devices/quality?minutes=${qMinutes}`)
      .then((r) => r.json()).then(setQuality).catch(() => setQuality({ error: true }));
  }, [tab, qMinutes]);

  const devices = data?.devices || [];
  const online = devices.filter((d) => d.online).length;

  return (
    <div style={S.page}>
      <div style={S.header}>
        <div>
          <h1 style={S.h1}>Hardware Inspector</h1>
          <div style={S.sub}>
            Raw MQTT view, one level below <code>/api/hardware</code>. Read-only.
            <span style={S.tempTag}>TEMPORARY BRING-UP MODULE</span>
          </div>
        </div>
        <div style={S.counts}>
          <div><span style={{ color: '#4ade80', fontSize: 24, fontWeight: 700 }}>{online}</span> online</div>
          <div><span style={{ color: '#9ca3af', fontSize: 24, fontWeight: 700 }}>{devices.length}</span> seen</div>
        </div>
      </div>

      <div style={S.tabs}>
        {['devices', 'events', 'quality', 'forecast', 'sustainability', 'usb', 'config'].map((t) => (
          <button key={t} style={S.tab(tab === t)} onClick={() => setTab(t)}>{t}</button>
        ))}
        <a href={window.location.pathname} style={S.back}>← back to console</a>
      </div>

      {err && (
        <div style={S.error}>
          {err.kind === 'stale' ? (
            <>
              The engine at <code>{API_BASE}</code> is running, but it does not serve
              <code> /api/devices</code> — it was built before the hardware inspector existed.
              <br />Restart it onto a current build: <code>cd econ/server &amp;&amp; go run .</code>
              {' '}(learned models persist to <code>data/</code>, so a restart keeps them),
              or point the dashboard at one that has the routes with
              {' '}<code>VITE_BACKEND_PORT=&lt;port&gt;</code>.
            </>
          ) : (
            <>
              Cannot reach the engine at <code>{API_BASE}</code> — {err.detail}.
              <br />Start it with <code>cd econ/server &amp;&amp; go run .</code> (and the broker with <code>docker compose up -d mqtt</code>).
            </>
          )}
        </div>
      )}

      {tab === 'devices' && (
        devices.length === 0 && !err ? (
          <div style={S.empty}>
            No node has published yet. The engine is up but nothing has arrived on
            <code> econ/telemetry/+</code>. Check the node is powered, on WiFi, and pointed at this broker.
          </div>
        ) : devices.map((d) => <DeviceCard key={d.id} dev={d} staleAfter={data?.staleAfter || 20} />)
      )}

      {tab === 'events' && (
        <div style={S.card}>
          {events === null && <div style={S.empty}>loading…</div>}
          <table style={S.table}>
            <thead><tr><th style={S.th}>time</th><th style={S.th}>device</th><th style={S.th}>event</th><th style={S.th}>detail</th></tr></thead>
            <tbody>
              {events?.length === 0 && <tr><td colSpan={4} style={S.td}>no events recorded</td></tr>}
              {(events || []).map((e, i) => (
                <tr key={i}>
                  <td style={S.td}>{new Date(e.t).toLocaleString()}</td>
                  <td style={S.td}>{e.device}</td>
                  <td style={{ ...S.td, color: e.event === 'offline' ? '#ef4444' : '#4ade80' }}>{e.event}</td>
                  <td style={S.td}>{e.detail}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {tab === 'forecast' && <ForecastTab />}
      {tab === 'sustainability' && <SustainabilityTab />}
      {tab === 'usb' && <UsbTab />}
      {tab === 'config' && <ConfigTab />}

      {tab === 'quality' && (
        <div style={S.card}>
          <div style={S.chartHead}>
            <p style={{ ...S.dim, margin: 0, maxWidth: 620, lineHeight: 1.6 }}>
              How much of stored history is evidence rather than simulation. A chart drawn over
              mostly <strong>modelled</strong> rows describes the twin, not the building.
            </p>
            <span>
              {[15, 60, 360, 1440].map((m) => (
                <button key={m} style={S.btn(qMinutes === m)} onClick={() => setQMinutes(m)}>
                  {m < 60 ? `${m}m` : `${m / 60}h`}
                </button>
              ))}
            </span>
          </div>
          {quality === null && (
            <div style={S.empty}>
              counting rows over the last {qMinutes < 60 ? `${qMinutes}m` : `${qMinutes / 60}h`}…
              <br /><span style={{ fontSize: 11 }}>the engine writes ~1500 rows/s, so a wide window takes a while</span>
            </div>
          )}
          {quality?.error && <div style={S.warn}>query failed — is the database up?</div>}
          {quality?.totals && (
            <div style={S.totals}>
              {Object.entries(quality.totals).map(([k, v]) => (
                <div key={k} style={S.total}>
                  <div style={{ fontSize: 22, fontWeight: 700,
                    color: k === 'measured' ? '#4ade80' : k === 'modelled' ? '#f59e0b' : '#60a5fa' }}>
                    {v.toLocaleString()}
                  </div>
                  <div style={S.dim}>{k} rows</div>
                </div>
              ))}
            </div>
          )}
          <table style={S.table}>
            <thead><tr><th style={S.th}>quality</th><th style={S.th}>device</th><th style={S.th}>metric</th><th style={S.th}>rows</th><th style={S.th}>span</th></tr></thead>
            <tbody>
              {(quality?.breakdown || []).map((r, i) => (
                <tr key={i}>
                  <td style={{ ...S.td, color: r.quality === 'measured' ? '#4ade80' : r.quality === 'modelled' ? '#f59e0b' : '#9ca3af' }}>{r.quality}</td>
                  <td style={S.td}>{r.device}</td>
                  <td style={S.td}>{r.metric}</td>
                  <td style={S.td}>{r.rows.toLocaleString()}</td>
                  <td style={S.td}>{new Date(r.from).toLocaleTimeString()} → {new Date(r.to).toLocaleTimeString()}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

const mono = 'ui-monospace, SFMono-Regular, Menlo, monospace';
/**
 * Sparkline for a FORECAST horizon: evenly-spaced future values, no timestamps and no
 * provenance. Deliberately not the Spark component above — that one colours its stroke by
 * measured-vs-modelled, which is a statement about evidence that no forecast can make.
 */
function ForecastSpark({ values, colour, width = 300, height = 60 }) {
  if (!values || values.length < 2) return null;
  const lo = Math.min(...values), hi = Math.max(...values);
  const span = hi - lo || 1;
  const d = values
    .map((v, i) => {
      const x = (i / (values.length - 1)) * width;
      const y = height - ((v - lo) / span) * (height - 8) - 4;
      return `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
  return (
    <svg
      viewBox={`0 0 ${width} ${height}`}
      preserveAspectRatio="none"
      style={{ width: '100%', height, display: 'block' }}
    >
      <path d={d} fill="none" stroke={colour} strokeWidth="1.5" strokeDasharray="3 2" vectorEffect="non-scaling-stroke" />
    </svg>
  );
}

/**
 * Both forecasters, live, over the same instant.
 *
 * The LSTM is supervised and only knows this building once train.py has had real history
 * to learn from; TimesFM is a pretrained foundation model that forecasts a series it has
 * never seen. They answer the same question with opposite trade-offs, and until this panel
 * nothing in the system ever put their answers next to each other.
 *
 * The number that matters most here is not the forecast — it is `realSamples`. The LSTM's
 * 12-step window is left-padded while the engine warms up, so a confident-looking MW figure
 * can be standing on one real sample. Showing the peak without showing what backs it is how
 * a forecast gets trusted more than it has earned.
 */
function ForecastTab() {
  const [data, setData] = useState(null);
  const [pending, setPending] = useState(true);

  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const r = await fetch(`${API_BASE}/api/forecast/compare?horizon=12`);
        const j = await r.json();
        if (alive) { setData(j); setPending(false); }
      } catch (e) {
        if (alive) { setData({ fetchError: e.message }); setPending(false); }
      }
    };
    tick();
    // Slower than the device poll: each call runs two model inferences, and the first
    // TimesFM call may be downloading a multi-gigabyte checkpoint.
    const id = setInterval(tick, 30000);
    return () => { alive = false; clearInterval(id); };
  }, []);

  const engines = [
    { key: 'lstm', name: 'LSTM', kind: 'supervised · trained on this building', colour: '#60a5fa' },
    { key: 'timesfm', name: 'TimesFM', kind: 'zero-shot · pretrained foundation model', colour: '#a78bfa' },
  ];

  if (pending) return <div style={S.card}><div style={S.empty}>asking both forecasters…</div></div>;
  if (data?.fetchError) {
    return <div style={S.card}><div style={S.warn}>could not reach the engine: {data.fetchError}</div></div>;
  }

  const agree = data?.agreement || {};

  return (
    <div style={S.card}>
      <p style={{ ...S.dim, margin: '0 0 14px', maxWidth: 700, lineHeight: 1.6 }}>
        Both engines, same instant, predicted <strong>peak building load</strong>. Neither is ground
        truth until it is compared against measured outturn — what to weigh them by is
        <strong> real samples</strong>, below.
      </p>

      <div style={{ display: 'flex', gap: 14, flexWrap: 'wrap', marginBottom: 16 }}>
        {engines.map(({ key, name, kind, colour }) => {
          const e = data?.[key] || {};
          const thin = e.available && e.windowLen && e.realSamples < e.windowLen;
          return (
            <div key={key} style={{ ...S.chartBox, flex: '1 1 300px', marginTop: 0 }}>
              <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, flexWrap: 'wrap' }}>
                <span style={{ fontSize: 15, fontWeight: 700, color: colour }}>{name}</span>
                <span style={S.badge(e.available ? '#4ade80' : '#ef4444')}>
                  {e.available ? 'LIVE' : 'UNAVAILABLE'}
                </span>
              </div>
              <div style={{ ...S.dim, fontSize: 11, marginTop: 2 }}>{kind}</div>

              {e.available ? (
                <>
                  <div style={{ fontSize: 30, fontWeight: 700, margin: '10px 0 2px' }}>
                    {(+e.peakMw).toFixed(3)} <span style={{ fontSize: 14, color: '#9ca3af' }}>MW</span>
                  </div>
                  <div style={{ ...S.dim, fontSize: 11 }}>
                    backed by <strong style={{ color: thin ? '#f59e0b' : '#4ade80' }}>
                      {e.realSamples}{e.windowLen ? ` / ${e.windowLen}` : ''}
                    </strong> real sample{e.realSamples === 1 ? '' : 's'}
                    {thin && ' — the rest of the window is padding'}
                  </div>
                  {e.series?.length > 0 && (
                    <div style={{ marginTop: 10 }}>
                      <ForecastSpark values={e.series} colour={colour} />
                      <div style={S.sparkMeta}>
                        <span>horizon {data.horizonMinutes} min</span>
                        <span>step {data.stepMinutes} min</span>
                        <span>dashed = forecast, not history</span>
                      </div>
                    </div>
                  )}
                </>
              ) : (
                <div style={{ ...S.dim, marginTop: 10, lineHeight: 1.6, fontSize: 12 }}>{e.error}</div>
              )}
            </div>
          );
        })}
      </div>

      {agree.comparable ? (
        <div style={agree.relativeDiff > 0.2 ? S.warn : { ...S.dim, lineHeight: 1.6 }}>
          The two disagree by <strong>{(agree.relativeDiff * 100).toFixed(1)}%</strong>
          {' '}({(+agree.deltaMw).toFixed(3)} MW), {agree.higher.toUpperCase()} higher.
          {agree.relativeDiff > 0.2 &&
            ' A gap this wide usually means one of them is forecasting from far less real history than the other — compare the sample counts above before believing either.'}
        </div>
      ) : (
        <div style={S.dim}>{agree.note}</div>
      )}
    </div>
  );
}

const S = {
  page: { minHeight: '100vh', background: '#0b0f14', color: '#e5e7eb', fontFamily: mono, fontSize: 13, padding: 20 },
  header: { display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 16, marginBottom: 16 },
  h1: { margin: 0, fontSize: 20, fontWeight: 700, letterSpacing: '-0.01em' },
  sub: { color: '#9ca3af', marginTop: 4, display: 'flex', alignItems: 'center', gap: 10, flexWrap: 'wrap' },
  tempTag: { background: '#f59e0b22', color: '#f59e0b', border: '1px solid #f59e0b55', padding: '2px 8px', borderRadius: 3, fontSize: 11, letterSpacing: '0.04em' },
  counts: { display: 'flex', gap: 24, color: '#9ca3af' },
  tabs: { display: 'flex', gap: 8, marginBottom: 16, alignItems: 'center', borderBottom: '1px solid #1f2937', paddingBottom: 8, flexWrap: 'wrap' },
  tab: (on) => ({ background: on ? '#1f2937' : 'transparent', color: on ? '#e5e7eb' : '#9ca3af', border: '1px solid #1f2937', padding: '5px 14px', borderRadius: 3, cursor: 'pointer', fontFamily: mono, fontSize: 12 }),
  back: { marginLeft: 'auto', color: '#60a5fa', textDecoration: 'none', fontSize: 12 },
  card: { background: '#111827', border: '1px solid #1f2937', borderRadius: 4, padding: 14, marginBottom: 12, overflowX: 'auto' },
  cardHead: { display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', flexWrap: 'wrap', gap: 8, marginBottom: 10 },
  devId: { fontSize: 15, fontWeight: 700, marginRight: 10 },
  badge: (c) => ({ background: `${c}22`, color: c, border: `1px solid ${c}55`, padding: '1px 7px', borderRadius: 3, fontSize: 10, marginRight: 6, letterSpacing: '0.04em' }),
  dim: { color: '#9ca3af', fontSize: 12 },
  warn: { background: '#f59e0b11', border: '1px solid #f59e0b44', color: '#fbbf24', padding: '7px 10px', borderRadius: 3, marginBottom: 10, fontSize: 12, lineHeight: 1.5 },
  error: { background: '#ef444411', border: '1px solid #ef444444', color: '#fca5a5', padding: 12, borderRadius: 4, marginBottom: 12, lineHeight: 1.6 },
  table: { width: '100%', borderCollapse: 'collapse', fontSize: 12 },
  th: { textAlign: 'left', color: '#6b7280', fontWeight: 500, padding: '5px 8px', borderBottom: '1px solid #1f2937', whiteSpace: 'nowrap' },
  td: { padding: '5px 8px', borderBottom: '1px solid #161d29', whiteSpace: 'nowrap' },
  btn: (on) => ({ background: on ? '#374151' : 'transparent', color: on ? '#e5e7eb' : '#9ca3af', border: '1px solid #374151', padding: '2px 9px', borderRadius: 3, cursor: 'pointer', fontFamily: mono, fontSize: 11, marginLeft: 4 }),
  linkBtn: { background: 'none', border: 'none', color: '#60a5fa', cursor: 'pointer', fontFamily: mono, fontSize: 11, padding: '8px 0 0', textDecoration: 'underline' },
  pre: { background: '#0b0f14', border: '1px solid #1f2937', borderRadius: 3, padding: 10, fontSize: 11, color: '#9ca3af', overflowX: 'auto', marginTop: 6 },
  chartBox: { marginTop: 12, padding: 10, background: '#0b0f14', border: '1px solid #1f2937', borderRadius: 3 },
  chartHead: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8, flexWrap: 'wrap', gap: 8 },
  sparkMeta: { display: 'flex', gap: 16, color: '#6b7280', fontSize: 11, marginTop: 4, flexWrap: 'wrap' },
  empty: { color: '#6b7280', padding: 16, textAlign: 'center', lineHeight: 1.6 },
  totals: { display: 'flex', gap: 28, margin: '12px 0 16px', flexWrap: 'wrap' },
  total: { minWidth: 100 },
  input: { background: '#1f2937', color: '#e5e7eb', border: '1px solid #374151', padding: '6px 10px', borderRadius: 3, fontFamily: mono, fontSize: 12, flex: 1, outline: 'none' },
};

