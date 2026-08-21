// Shared plug-load (APLC) state: one hook, both dashboards.
//
// Polls GET /api/plugs for the sweep policy, phantom-load leaderboard and savings, and
// exposes updateConfig() for policy changes. Policy changes are operational (they decide
// when a building's sockets switch off), so the backend may demand the admin token —
// the hook surfaces that as needToken and retries with whatever token the UI collects,
// the same UX contract as the blueprint deploy flow.

import { useCallback, useEffect, useRef, useState } from 'react';
import { API_BASE, getAdminToken, setAdminToken } from './api';

export function usePlugs(pollMs = 10000) {
  const [status, setStatus] = useState(null); // /api/plugs snapshot, null until first load
  const [needToken, setNeedToken] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState(null);
  // Seed from the shared operator token (api.js). This hook used to start with an empty
  // token of its own, so on a token-protected engine an operator who had already signed
  // in for the websocket still got a silent 401 the first time they touched the sweep —
  // updateConfig returned false and callers that ignored the result showed nothing at all.
  const tokenRef = useRef(getAdminToken());

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

  // Persist through the shared store so a token entered here also authorizes the
  // websocket, and vice versa — one credential per browser, not one per panel.
  const setToken = (t) => { tokenRef.current = t; setAdminToken(t); };

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
        setError('This engine requires an operator token to change the sweep policy.');
        return false;
      }
      if (res.ok) {
        setNeedToken(false);
        setError(null);
        const s = await res.json();
        setStatus(s);
        return true;
      }
      setError(`The engine refused the change (HTTP ${res.status}).`);
      return false;
    } catch (e) {
      setError(`Could not reach the engine: ${e.message}`);
      return false;
    } finally {
      setSaving(false);
    }
  }, [status]);

  // error is surfaced so a caller that does not check updateConfig's return value still
  // has something to render. A control that silently does nothing is worse than one that
  // says why it could not.
  return { status, needToken, saving, error, setToken, updateConfig, reload: load };
}
