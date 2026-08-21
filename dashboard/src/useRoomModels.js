// Identified room models (GET /api/rooms/models).
//
// This endpoint has existed, complete and correct, with no consumer. It is the twin's
// actual differentiator: for every room the engine has watched long enough, the physical
// constants it recovered from that room's OWN history — its thermal time constant, the
// cooling its VAV really delivers per unit of flow, its measured air-change rate, and how
// well the fit reproduces what the room then did.
//
// The AI panel was showing conclusions ("this room breaches in 14 min") with no way to see
// the model that produced them. That is the difference between a dashboard an engineer
// trusts and one they learn to ignore, so the panel now renders the identity behind each
// prediction — including its provenance, which is the part that decides how much the
// number is worth:
//
//   supplyMeasuredFrac is the share of the fit that was referenced to a MEASURED discharge
//   temperature rather than the library's design value. A room at 0.0 is still a real fit,
//   but its cooling authority inherits whatever error that assumption carries, and an
//   operator deciding whether to dispatch someone deserves to know which they are looking
//   at. Nothing here is smoothed or defaulted: a room the engine has not identified simply
//   does not appear.

import { useCallback, useEffect, useState } from 'react';
import { API_BASE } from './api';

export function useRoomModels(pollMs = 30000) {
  const [state, setState] = useState({ rooms: [], identified: 0, learning: 0, matureAfter: 0, loaded: false });

  const load = useCallback(() => {
    fetch(`${API_BASE}/api/rooms/models`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!d) return;
        setState({
          rooms: Array.isArray(d.rooms) ? d.rooms : [],
          identified: d.identified ?? 0,
          learning: d.learning ?? 0,
          matureAfter: d.matureAfter ?? 0,
          loaded: true,
        });
      })
      .catch(() => {});
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, pollMs);
    return () => clearInterval(id);
  }, [load, pollMs]);

  // byZone lets a recommendation card look up the model that produced it without the
  // caller re-scanning the list on every render.
  const byZone = {};
  state.rooms.forEach((r) => { byZone[r.zone] = r; });

  return { ...state, byZone, reload: load };
}

export default useRoomModels;
