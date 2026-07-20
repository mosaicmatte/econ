// Live operational signals the AI layer reasons over, polled from the Go engine:
//
//   /api/precool — is a forecast/manual pre-cool window open right now, and until when
//   /api/weather — what outdoor temperature the 2R1C envelope is integrating against,
//                  and whether it is live Open-Meteo data or the climatological fallback
//
// One hook, both dashboards — the desktop AI Insights panel and the mobile AI screen
// must never disagree about whether the building is pre-cooling.

import { useCallback, useEffect, useState } from 'react';
import { API_BASE } from './api';

export function useOpsStatus(pollMs = 15000) {
  const [precool, setPrecool] = useState(null); // {active, until} | null until first load
  const [weather, setWeather] = useState(null); // {outdoorC, live, ageSec} | null

  const load = useCallback(() => {
    fetch(`${API_BASE}/api/precool`)
      .then((r) => (r.ok ? r.json() : null))
      .then((s) => { if (s) setPrecool(s); })
      .catch(() => {});
    fetch(`${API_BASE}/api/weather`)
      .then((r) => (r.ok ? r.json() : null))
      .then((s) => { if (s) setWeather(s); })
      .catch(() => {});
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, pollMs);
    return () => clearInterval(id);
  }, [load, pollMs]);

  return { precool, weather, reload: load };
}

// Formats a precool `until` timestamp as a wall-clock label ("01:23"), or ''.
export function untilLabel(until) {
  if (!until) return '';
  const d = new Date(until);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
