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
      <svg width={width} height={height} style={{ maxWidth: '100%', display: 'block' }}>
        <path d={d} fill="none" stroke={stroke} strokeWidth="1.5" />
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
      } catch (e) { if (alive) setErr(e.message); }
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
        {['devices', 'events', 'quality'].map((t) => (
          <button key={t} style={S.tab(tab === t)} onClick={() => setTab(t)}>{t}</button>
        ))}
        <a href={window.location.pathname} style={S.back}>← back to console</a>
      </div>

      {err && (
        <div style={S.error}>
          Cannot reach the engine at <code>{API_BASE}</code> — {err}.
          <br />Start it with <code>cd econ/server &amp;&amp; go run .</code> (and the broker with <code>docker compose up -d mqtt</code>).
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
};
