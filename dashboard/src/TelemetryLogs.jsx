import React, { useEffect, useRef, useState } from 'react';
import { Terminal } from 'lucide-react';

// A telemetry log — that is, a record of things that HAPPENED.
//
// What this was: on every websocket frame it rebuilt one line per zone, stamped every line
// with the time the rebuild happened, and rendered the result under the heading "RAW
// TELEMETRY STREAM". So it looked like a scrolling log and was a snapshot table, redrawn
// thirty times a second. Nothing accumulated, every timestamp was identical and always
// "now", and the whole panel flickered — the same defect the profiler had, in a panel whose
// entire purpose is history.
//
// A log records transitions. Temperature drifting by a hundredth of a degree is not an
// event; a zone entering alarm, its lights being actuated, its sockets being swept, or a
// setpoint moving are. Those are logged when they occur, with the time they occurred, and
// they stay on screen afterwards — which is what makes the panel answerable to a question
// like "when did that room go into alarm?".

const MAX_LINES = 300;

// Rounded so ordinary sensor noise cannot register as a change. The stream carries ±0.08 °C
// of noise, so a setpoint watched to two decimals would "change" continuously.
const round1 = (v) => (typeof v === 'number' ? Math.round(v * 10) / 10 : v);

// The facts worth remembering about a zone. Anything not in here is live state, and the
// live panels already show it better than a log can.
function snapshot(z) {
  return {
    alert: z.alert === true ? 'ALARM' : z.alert === 'REMEDIATING' ? 'REMEDIATING' : 'ok',
    lightsOn: z.lightsOn !== false,
    plugShed: !!z.plugShed,
    setpoint: round1(z.setpoint),
    occupied: (z.occupancy || 0) > 0,
  };
}

const EVENTS = [
  { key: 'alert', text: (p, n) => `state ${p} -> ${n}`, level: (p, n) => (n === 'ALARM' ? 'alarm' : n === 'ok' ? 'clear' : 'warn') },
  { key: 'lightsOn', text: (p, n) => `lighting ${n ? 'ON' : 'OFF'}`, level: () => 'info' },
  { key: 'plugShed', text: (p, n) => (n ? 'sockets SWEPT (non-critical circuits off)' : 'sockets RESTORED'), level: () => 'info' },
  { key: 'setpoint', text: (p, n) => `setpoint ${p}°C -> ${n}°C`, level: () => 'info' },
  { key: 'occupied', text: (p, n) => (n ? 'became occupied' : 'became vacant'), level: () => 'info' },
];

const COLOUR = {
  alarm: 'var(--accent-red)',
  warn: 'var(--accent-yellow)',
  clear: 'var(--accent-green)',
  info: 'var(--text-secondary)',
};

export default function TelemetryLogs({ simData }) {
  const [lines, setLines] = useState([]);
  const prev = useRef(null);

  useEffect(() => {
    const zones = simData?.zones;
    if (!zones) return;

    // First frame establishes the baseline. Emitting an "event" for every zone's initial
    // state would fill the log with things that did not happen.
    if (prev.current === null) {
      const base = {};
      for (const [id, z] of Object.entries(zones)) base[id] = snapshot(z);
      prev.current = base;
      return;
    }

    const stamp = new Date().toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
    const fresh = [];
    for (const [id, z] of Object.entries(zones)) {
      const now = snapshot(z);
      const was = prev.current[id];
      if (!was) { prev.current[id] = now; continue; }
      for (const ev of EVENTS) {
        if (was[ev.key] !== now[ev.key]) {
          fresh.push({
            id: `${id}:${ev.key}:${stamp}:${now[ev.key]}`,
            t: stamp,
            zone: z.label || id,
            msg: ev.text(was[ev.key], now[ev.key]),
            level: ev.level(was[ev.key], now[ev.key]),
          });
        }
      }
      prev.current[id] = now;
    }
    if (fresh.length) setLines((cur) => [...fresh.reverse(), ...cur].slice(0, MAX_LINES));
  }, [simData]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', gap: '1rem' }}>
      <div style={{ fontSize: '10px', fontWeight: 'bold', color: 'var(--text-secondary)', letterSpacing: '1px', marginBottom: '4px', display: 'flex', alignItems: 'center', gap: '6px' }}>
        <Terminal size={12} color="var(--accent-blue)" /> EVENT LOG
        <span style={{ marginLeft: 'auto', fontWeight: 'normal', letterSpacing: 0, color: 'var(--text-muted)' }}>
          {lines.length ? `${lines.length} event${lines.length === 1 ? '' : 's'} this session` : ''}
        </span>
      </div>
      <div style={{ flex: 1, background: 'rgba(0,0,0,0.6)', border: '1px solid var(--border-glass)', borderRadius: '8px', padding: '12px', overflowY: 'auto', fontFamily: 'monospace', fontSize: '10px', color: 'var(--text-secondary)', display: 'flex', flexDirection: 'column', gap: '4px' }}>
        {lines.length === 0 ? (
          <div style={{ color: 'var(--text-muted)', lineHeight: 1.6 }}>
            Nothing has changed since this panel opened. Alarms, lighting and socket actuation,
            setpoint moves and occupancy transitions are recorded here as they happen — a
            quiet log means a quiet building, not a broken panel.
          </div>
        ) : (
          lines.map((l) => (
            <div key={l.id} style={{ borderBottom: '1px solid rgba(255,255,255,0.05)', paddingBottom: '4px' }}>
              <span style={{ color: 'var(--accent-green)' }}>&gt; </span>
              <span style={{ color: 'var(--text-muted)' }}>[{l.t}]</span>{' '}
              <span style={{ color: 'var(--text-primary)' }}>{l.zone}</span>{' '}
              <span style={{ color: COLOUR[l.level] || 'var(--text-secondary)' }}>{l.msg}</span>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
