// Shared plug-load (APLC) state: one hook, both dashboards.
//
// Polls GET /api/plugs for the sweep policy, phantom-load leaderboard and savings, and
// exposes updateConfig() for policy changes. Policy changes are operational (they decide
// when a building's sockets switch off), so the backend may demand the admin token —
// the hook surfaces that as needToken and retries with whatever token the UI collects,
// the same UX contract as the blueprint deploy flow.

import { useCallback, useEffect, useRef, useState } from 'react';
import { API_BASE } from './api';

export function usePlugs(pollMs = 10000) {
  const [status, setStatus] = useState(null); // /api/plugs snapshot, null until first load
  const [needToken, setNeedToken] = useState(false);
  const [saving, setSaving] = useState(false);
  const tokenRef = useRef('');

  const load = useCallback(() => {
    fetch(`${API_BASE}/api/plugs`)
      .then((r) => (r.ok ? r.json() : null))
      .then((s) => { if (s) setStatus(s); })
      .catch(() => {});
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, pollMs);
    return () => clearInterval(id);
  }, [load, pollMs]);

  const setToken = (t) => { tokenRef.current = t; };

  // updateConfig POSTs a full policy (merge over the current one) and refreshes.
  // Resolves true on success; flips needToken on a 401 instead of throwing.
  const updateConfig = useCallback(async (patch) => {
    if (!status) return false;
    setSaving(true);
    try {
      const headers = { 'Content-Type': 'application/json' };
      if (tokenRef.current) headers['X-Admin-Token'] = tokenRef.current;
      const res = await fetch(`${API_BASE}/api/plugs`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ ...status.config, ...patch }),
      });
      if (res.status === 401) {
        setNeedToken(true);
        return false;
      }
      if (res.ok) {
        setNeedToken(false);
        const s = await res.json();
        setStatus(s);
        return true;
      }
      return false;
    } catch {
      return false;
    } finally {
      setSaving(false);
    }
  }, [status]);

  return { status, needToken, saving, setToken, updateConfig, reload: load };
}
