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
