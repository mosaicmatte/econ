import React from 'react';

// Whether what is on screen is the building right now, or the last thing it did before the
// stream stopped.
//
// Every streamed figure in this dashboard — temperatures, load, savings, occupancy, fault
// counts — is the most recent websocket frame, and when the engine goes away that frame
// simply stays there. useDigitalTwin's reconnect logic names the failure exactly: "polls
// keep refreshing so the page LOOKS alive while every streamed number is stale". It fixed
// the socket; nothing told the operator. The result is a dashboard that reads as normal
// operation through an engine outage, indefinitely, and reads that way most convincingly
// when the building is doing something the operator would want to know about.
//
// So: silent while the stream is healthy, and explicit the moment it is not. It reports the
// age rather than merely "disconnected", because a two-second gap during a reconnect and a
// twenty-minute gap during an outage are different situations and only one of them is worth
// acting on.

// How far behind the stream may fall before the numbers stop being "now". Frames arrive at
// 30 fps, so several seconds of silence is already well outside normal operation; the
// threshold is generous enough that a routine hiccup does not flash a warning.
const STALE_MS = 6000;

export function streamIsStale(streamOpen, streamAgeMs) {
  if (streamAgeMs == null) return !streamOpen; // nothing received yet
  return !streamOpen || streamAgeMs > STALE_MS;
}

export function streamAgeLabel(ms) {
  if (ms == null) return 'no telemetry received yet';
  const s = Math.round(ms / 1000);
  if (s < 90) return `${s}s ago`;
  const m = Math.round(s / 60);
  if (m < 90) return `${m} min ago`;
  return `${Math.round(m / 60)} h ago`;
}

/** Desktop banner. Renders nothing at all while the stream is healthy. */
export default function StreamStatus({ streamOpen, streamAgeMs, style }) {
  if (!streamIsStale(streamOpen, streamAgeMs)) return null;
  const never = streamAgeMs == null;
  return (
    <div
      role="status"
      style={{
        display: 'flex', alignItems: 'center', gap: '8px',
        padding: '6px 12px', borderRadius: '8px',
        background: 'rgba(255,59,48,0.12)', border: '1px solid rgba(255,59,48,0.45)',
        color: 'var(--accent-red, #FF3B30)', fontSize: '11px', fontWeight: 600,
        letterSpacing: '0.03em', pointerEvents: 'none',
        ...style,
      }}
    >
      <span style={{ width: 7, height: 7, borderRadius: '50%', background: 'currentColor', flexShrink: 0 }} />
      {never
        ? 'NO TELEMETRY — waiting for the engine'
        : `TELEMETRY STALE — these are the last values received, ${streamAgeLabel(streamAgeMs)}`}
    </div>
  );
}
