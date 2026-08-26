import React from 'react';
import { Users, Plug } from 'lucide-react';

// Per-zone profile rows — the profiler's view when the building is small enough to name
// every room.
//
// The scatter this replaces plots (deviation from setpoint) against (cooling delivered)
// and reads the QUADRANTS: hot and cooling hard is a fault, hot and barely cooling is a
// starved box, cold is money burnt. That is a genuinely good chart for a tower, where the
// interesting object is the population — where the mass sits, which corner has outliers,
// how tight the spread is. It is the wrong chart for five rooms. Every point lands within
// half a degree of setpoint, the quadrants are empty, and a reader learns nothing they
// could not read from a list of five numbers — while the axes imply a distribution that
// five points cannot show.
//
// At this scale the useful question is not "how is the population distributed" but "what
// is each room doing, and which one needs me first". So each zone gets a row, sorted
// worst-first, with its temperature drawn as a bullet against its OWN setpoint and its own
// deadband — every room has different ones, which a shared x-axis quietly flattens.

const STATE = {
  alarm:      { label: 'Alarm',      color: 'var(--accent-red)' },
  struggling: { label: 'Struggling', color: 'var(--accent-yellow)' },
  starved:    { label: 'Starved',    color: '#b06bd8' },
  overcooled: { label: 'Overcooled', color: 'var(--accent-blue)' },
  healthy:    { label: 'In band',    color: 'rgba(46,204,113,0.8)' },
};

// One room's temperature against its own setpoint and deadband. The track spans a fixed
// number of deadbands either side, so "how far out of band" is comparable between rooms
// even though their absolute setpoints differ.
function DeviationBullet({ dev, deadband, color }) {
  const span = Math.max(deadband * 2.5, Math.abs(dev) * 1.15, 1);
  const pct = (v) => ((v + span) / (2 * span)) * 100;
  const inBand = Math.abs(dev) <= deadband;
  return (
    <div style={{ position: 'relative', height: '10px', background: 'rgba(255,255,255,0.05)', borderRadius: '5px' }}>
      {/* the comfort band this room is actually controlled to */}
      <div
        title={`deadband ±${deadband.toFixed(1)} °C`}
        style={{
          position: 'absolute', left: `${pct(-deadband)}%`, width: `${pct(deadband) - pct(-deadband)}%`,
          top: 0, height: '100%', background: 'rgba(46,204,113,0.14)',
          border: '1px solid rgba(46,204,113,0.3)', borderRadius: '5px', boxSizing: 'border-box',
        }}
      />
      {/* setpoint */}
      <div style={{ position: 'absolute', left: '50%', top: '-2px', width: '1px', height: '14px', background: 'rgba(255,255,255,0.45)' }} />
      {/* where the room actually is */}
      <div
        title={`${dev >= 0 ? '+' : ''}${dev.toFixed(2)} °C from setpoint`}
        style={{
          position: 'absolute', left: `calc(${Math.max(0, Math.min(100, pct(dev)))}% - 2px)`, top: '-3px',
          width: '4px', height: '16px', background: color, borderRadius: '2px',
          boxShadow: inBand ? 'none' : `0 0 6px ${color}`,
        }}
      />
    </div>
  );
}

export default function ZoneProfileRows({ rows, onSelect, isMobile = false }) {
  if (!rows || rows.length === 0) {
    return (
      <div style={{ padding: '18px', textAlign: 'center', fontSize: '11px', color: 'var(--text-secondary)' }}>
        No zones streaming yet.
      </div>
    );
  }

  // Already ordered and sampled by useSteadyRows — sorting again here would reintroduce
  // exactly the per-frame reshuffle that hook exists to prevent.
  const sorted = rows;
  const maxCool = Math.max(...sorted.map((r) => r.coolKw), 0.001);

  const fs = isMobile ? 12 : 10;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '2px' }}>
      {sorted.map((r) => {
        const st = STATE[r.state] || STATE.healthy;
        return (
          <div
            key={r.id}
            onClick={() => onSelect && onSelect(r)}
            title="Open this zone"
            style={{
              display: 'grid',
              gridTemplateColumns: isMobile ? '1fr' : 'minmax(96px, 1.4fr) minmax(90px, 2fr) auto',
              gap: isMobile ? '4px' : '10px',
              alignItems: 'center',
              padding: isMobile ? '10px 8px' : '7px 8px',
              borderRadius: '6px',
              background: r.state === 'healthy' ? 'rgba(255,255,255,0.02)' : 'rgba(255,255,255,0.04)',
              borderLeft: `3px solid ${st.color}`,
              cursor: onSelect ? 'pointer' : 'default',
            }}
          >
            {/* who */}
            <div style={{ minWidth: 0 }}>
              <div style={{ fontSize: `${fs}px`, color: 'var(--text-primary)', fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                {r.name}
              </div>
              <div style={{ fontSize: `${fs - 1}px`, color: st.color }}>{st.label}</div>
            </div>

            {/* where it sits against its own band */}
            <div>
              <DeviationBullet dev={r.dev} deadband={r.deadband} color={st.color} />
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: `${fs - 2}px`, color: 'var(--text-muted)', marginTop: '3px' }}>
                <span>{r.temp.toFixed(1)} °C</span>
                <span>set {r.setpoint.toFixed(1)} · {r.dev >= 0 ? '+' : ''}{r.dev.toFixed(1)}</span>
              </div>
            </div>

            {/* what it is being given, and who is in it */}
            <div style={{ display: 'flex', alignItems: 'center', gap: isMobile ? '12px' : '8px', justifyContent: isMobile ? 'flex-start' : 'flex-end' }}>
              <div style={{ textAlign: isMobile ? 'left' : 'right' }}>
                <div style={{ fontSize: `${fs}px`, fontFamily: 'monospace', color: 'var(--text-primary)' }}>
                  {r.coolKw < 10 ? r.coolKw.toFixed(2) : r.coolKw.toFixed(0)} <span style={{ color: 'var(--text-muted)' }}>kW</span>
                </div>
                {/* cooling delivered, relative to the busiest room right now */}
                <div style={{ width: isMobile ? '80px' : '52px', height: '3px', background: 'rgba(255,255,255,0.08)', borderRadius: '2px', marginTop: '3px', marginLeft: isMobile ? 0 : 'auto' }}>
                  <div style={{ width: `${Math.max(2, (r.coolKw / maxCool) * 100)}%`, height: '100%', background: 'var(--accent-blue)', borderRadius: '2px' }} />
                </div>
              </div>
              <span title={`${r.occupancy} occupant${r.occupancy === 1 ? '' : 's'}`} style={{ display: 'flex', alignItems: 'center', gap: '3px', fontSize: `${fs - 1}px`, color: r.occupancy > 0 ? 'var(--text-secondary)' : 'var(--text-muted)' }}>
                <Users size={fs} /> {r.occupancy}
              </span>
              {r.plugShed && (
                <span title="non-critical sockets swept off" style={{ display: 'flex', alignItems: 'center', color: 'var(--accent-green)' }}>
                  <Plug size={fs} />
                </span>
              )}
            </div>
          </div>
        );
      })}
    </div>
  );
}
