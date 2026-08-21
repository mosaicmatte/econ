// A readable view of a 30 Hz stream.
//
// The engine broadcasts at ~30 fps and every reading carries measurement noise, which is
// correct: that is what a real sensor does. But a display is not a data logger, and two
// things follow from rendering the raw stream directly.
//
// The values become unreadable. A temperature carrying ±0.08 °C of noise, printed to one
// decimal thirty times a second, is a digit that never settles — there is no rate at which
// a person can read it, and nothing is gained by trying.
//
// Worse, ORDER becomes unstable. The profiler sorts worst-first, which is right when
// something is wrong and actively harmful when nothing is: with every room inside its
// deadband, the sort key is dominated by noise, so the rows permanently reshuffle. The
// reader sees motion and reads it as change, when the building is doing nothing at all.
//
// So the view is sampled, not the data:
//   - values refresh on a fixed cadence a person can follow;
//   - a row's position is decided by its STATE, and within a state by a deviation
//     quantised well above the noise floor, so noise cannot reorder anything;
//   - a state change — a room entering alarm — bypasses the cadence entirely, because
//     "you may wait a second to see it" is not a trade worth making for a fault.

import { useEffect, useRef, useState } from 'react';

// How often the numbers refresh. Fast enough to feel live, slow enough to read.
const REFRESH_MS = 1000;

// The sort key is quantised to this many °C. It must sit clearly above the stream's noise
// (±0.08 °C) or ordering jitters; too coarse and genuinely different rooms tie.
const DEV_BUCKET_C = 0.5;

const RANK = { alarm: 0, struggling: 1, starved: 2, overcooled: 3, healthy: 4 };

// Ordering: state first, then how far out of band (bucketed), then name. Name last is what
// makes it deterministic — two rooms in the same state and the same bucket keep their
// relative order for as long as that remains true.
function order(rows) {
  return [...rows].sort((a, b) => {
    const r = (RANK[a.state] ?? 9) - (RANK[b.state] ?? 9);
    if (r !== 0) return r;
    const qa = Math.floor(Math.abs(a.dev) / DEV_BUCKET_C);
    const qb = Math.floor(Math.abs(b.dev) / DEV_BUCKET_C);
    if (qa !== qb) return qb - qa;
    return String(a.name).localeCompare(String(b.name));
  });
}

// A cheap signature of what would change the ORDER or the urgency — not of the values.
function stateSignature(rows) {
  return order(rows).map((r) => `${r.id}:${r.state}`).join('|');
}

export default function useSteadyRows(rows) {
  const [view, setView] = useState(() => order(rows || []));
  const latest = useRef(rows || []);
  const shownSig = useRef(stateSignature(rows || []));

  latest.current = rows || [];

  // Immediate: any change of state, in either direction. A room entering alarm must not
  // wait for the next tick, and a room leaving it should not keep the red rail.
  const sig = stateSignature(latest.current);
  useEffect(() => {
    if (sig !== shownSig.current) {
      shownSig.current = sig;
      setView(order(latest.current));
    }
  }, [sig]);

  // Otherwise refresh the values on the cadence.
  useEffect(() => {
    const id = setInterval(() => {
      shownSig.current = stateSignature(latest.current);
      setView(order(latest.current));
    }, REFRESH_MS);
    return () => clearInterval(id);
  }, []);

  return view;
}

export { REFRESH_MS, DEV_BUCKET_C };
