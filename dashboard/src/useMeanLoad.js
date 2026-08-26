import { useEffect, useRef, useState } from 'react';

const STORAGE_KEY = 'econ.meanLoad.v1';

/**
 * Running mean of the building's electrical load, and how long it has been observed for.
 *
 * Annual energy intensity is mean load x 8760 by definition, so a mean is the only correct
 * basis for anything shown next to an annual benchmark. The instantaneous load is not:
 * an office at 09:00 with three thousand people in it sits far above its own annual
 * average, and the same figure sampled at 03:00 sits far below. Comparing either to a
 * cohort EUI produces a number that looks authoritative and means nothing.
 *
 * Accumulates in localStorage so the window survives a page reload — the observation is a
 * property of the building, not of the browser tab, and a demo that reloads should not
 * silently restart its own evidence.
 */
export default function useMeanLoad(loadMw) {
  const acc = useRef(null);
  const [state, setState] = useState({ meanMw: 0, hours: 0, samples: 0, peakMw: 0 });

  // Restore any accumulated window once, on mount.
  if (acc.current === null) {
    let restored = null;
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw) {
        const p = JSON.parse(raw);
        if (Number.isFinite(p.sumMwMs) && Number.isFinite(p.ms) && p.ms >= 0) restored = p;
      }
    } catch {
      // A corrupt or unavailable store just means the window starts now.
    }
    acc.current = restored || { sumMwMs: 0, ms: 0, samples: 0, peakMw: 0 };
    if (typeof acc.current.peakMw !== 'number') acc.current.peakMw = 0;
    acc.current.last = null;
  }

  useEffect(() => {
    if (!Number.isFinite(loadMw) || loadMw < 0) return;
    const now = Date.now();
    const a = acc.current;

    if (a.last !== null) {
      // Trapezoidal over the real elapsed interval, so an irregular websocket cadence
      // does not weight a burst of fast frames more heavily than a slow one.
      const dt = now - a.lastAt;
      // Ignore a gap longer than five minutes: the tab was backgrounded or the socket
      // dropped, and the building's load during that time is unobserved, not constant.
      if (dt > 0 && dt <= 5 * 60 * 1000) {
        a.sumMwMs += ((a.last + loadMw) / 2) * dt;
        a.ms += dt;
        a.samples += 1;
      }
    }
    a.last = loadMw;
    a.lastAt = now;
    // Highest load this building has actually been seen at. It is the only honest
    // denominator for a "plant utilization" bar: a design nameplate would have to be
    // synthesized from coefficients, and the panel's previous attempt at one — a fixed
    // 3.6 MW floor — read 0.3% on a house and would have read 0.3% straight through a
    // total plant failure. An observed peak is a fact, and it says how long it took to
    // observe, so a reader can discount it while the window is short.
    if (loadMw > a.peakMw) a.peakMw = loadMw;

    const hours = a.ms / 3_600_000;
    const meanMw = a.ms > 0 ? a.sumMwMs / a.ms : loadMw;
    setState({ meanMw, hours, samples: a.samples, peakMw: a.peakMw });

    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify({
        sumMwMs: a.sumMwMs, ms: a.ms, samples: a.samples, peakMw: a.peakMw,
      }));
    } catch {
      // Storage full or blocked; the in-memory window still works for this session.
    }
  }, [loadMw]);

  return state;
}
