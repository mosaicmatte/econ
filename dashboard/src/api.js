// Backend host resolution.
//
// The dashboard is frequently opened from another device on the LAN — a phone at
// http://192.168.1.254:5173, for instance. Hard-coding "localhost:8080" breaks that
// case: on the phone, "localhost" is the phone itself, so the WebSocket/API calls hit
// nothing and every live metric reads 0 while the (statically bundled) 3D shell still
// renders. Derive the backend host from the page URL instead, so the API and WebSocket
// follow wherever the page was served from. VITE_BACKEND_HOST overrides for split
// deployments (frontend and Go engine on different machines).
const backendHost =
  import.meta.env.VITE_BACKEND_HOST ||
  (typeof window !== 'undefined' && window.location.hostname) ||
  'localhost';

const BACKEND_PORT = import.meta.env.VITE_BACKEND_PORT || '8080';

export const API_BASE = `http://${backendHost}:${BACKEND_PORT}`;
export const WS_URL = `ws://${backendHost}:${BACKEND_PORT}/ws`;

// Operator token (ECON_ADMIN_TOKEN on the engine). Reading telemetry needs nothing; every
// command — a zone override, the Auto-Pilot switch, a plug-policy change — needs this.
//
// It lives in localStorage rather than in a bundled env var on purpose: baking it into
// the build would ship the building's control credential to anyone who can fetch the
// JavaScript, and would make rotating it a redeploy. The operator pastes it once per
// browser. That is the same trade the REST panels already make with X-Admin-Token, and
// it is only appropriate because this is a console an operator signs into, not a public
// page.
//
// An engine with no token set (demo mode) accepts commands without one, so leaving this
// empty is the correct configuration for the demo and the wrong one for a real building.
const TOKEN_KEY = 'econ.adminToken';

export function getAdminToken() {
  try {
    return localStorage.getItem(TOKEN_KEY) || '';
  } catch {
    return ''; // private mode / storage disabled: behave as unauthenticated
  }
}

export function setAdminToken(token) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* nothing to do — the caller still holds it for this session */
  }
}
