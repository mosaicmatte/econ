// Local model matching: measure THIS machine, ask the server which model tier it should run.
//
// The download card used to offer one bundle and let the operator guess. But the right
// bundle is a hardware question — a dependency-free scorer runs on a decade-old laptop,
// while TimesFM on an accelerator does not — and the browser is the only thing that knows
// what machine it is on. So the client measures, the server decides (server/modelcatalog.go),
// and the card explains the decision.
//
// What a browser will actually tell you is limited, and this hook is careful not to
// overstate it:
//   - navigator.deviceMemory is absent on Safari/Firefox. Where it exists the spec caps it
//     at 8 (so "8" means "8 or more"), though some browsers report the true figure.
//   - hardwareConcurrency is widely available but privacy modes can clamp it.
//   - The GPU is only visible as a vendor-formatted renderer string behind a WebGL debug
//     extension, which some browsers withhold entirely.
// Every one of those gaps is reported as absent rather than guessed, and the server labels
// its estimate accordingly.

import { useCallback, useEffect, useState } from 'react';
import { API_BASE } from './api';

// probeGpu reads the unmasked WebGL renderer. Wrapped in try/catch and disposed of
// immediately: creating a WebGL context is the one part of this that can genuinely fail
// (blocked contexts, headless, exhausted context limits) and it must never break the panel.
function probeGpu() {
  try {
    const canvas = document.createElement('canvas');
    const gl = canvas.getContext('webgl') || canvas.getContext('experimental-webgl');
    if (!gl) return '';
    const dbg = gl.getExtension('WEBGL_debug_renderer_info');
    const renderer = dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER);
    const lose = gl.getExtension('WEBGL_lose_context');
    if (lose) lose.loseContext();
    return typeof renderer === 'string' ? renderer : '';
  } catch {
    return '';
  }
}

// probePlatform prefers the modern userAgentData hint and falls back to the deprecated
// navigator.platform, which is still the only signal some browsers give.
function probePlatform() {
  const uaPlatform = navigator.userAgentData?.platform;
  if (uaPlatform) return uaPlatform;
  const p = navigator.platform || '';
  if (/mac/i.test(p)) return 'macOS';
  if (/win/i.test(p)) return 'Windows';
  if (/linux/i.test(p)) return 'Linux';
  return p;
}

function probeArch() {
  // There is no reliable architecture API. Apple Silicon is inferable from the GPU string
  // and nothing else is worth guessing at, so this stays deliberately thin.
  const gpu = probeGpu();
  if (/apple m\d/i.test(gpu)) return 'arm64';
  return '';
}

export function profileHardware() {
  return {
    cores: navigator.hardwareConcurrency || 0,     // 0 = not reported
    memoryGb: navigator.deviceMemory || 0,          // 0 = not reported (Safari/Firefox)
    platform: probePlatform(),
    arch: probeArch(),
    gpuRenderer: probeGpu(),
    hasWebGpu: typeof navigator.gpu !== 'undefined',
  };
}

export function useLocalModel() {
  const [profile, setProfile] = useState(null);
  const [rec, setRec] = useState(null);       // ModelRecommendation from the server
  const [error, setError] = useState(null);
  const [override, setOverride] = useState(null); // operator's manual tier choice

  const run = useCallback(() => {
    let p;
    try {
      p = profileHardware();
    } catch (e) {
      setError('could not profile this machine');
      return;
    }
    setProfile(p);
    fetch(`${API_BASE}/api/model/recommend`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(p),
    })
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => { if (d) { setRec(d); setError(null); } else setError('recommendation unavailable'); })
      .catch(() => setError('recommendation unavailable'));
  }, []);

  useEffect(() => { run(); }, [run]);

  // The operator can always overrule the recommendation — the server's pick is advice
  // grounded in what the browser would admit to, not a verdict. A machine that reports
  // nothing is exactly the machine whose owner knows better than we do.
  const selected = override || rec?.recommended || null;
  const tier = rec?.tiers?.find((t) => t.id === selected) || null;

  // The export is tailored: the tier decides what is in the zip, and the measured worker
  // count is written into its MANIFEST so the local runtime parallelises to this machine.
  const exportUrl = selected
    ? `${API_BASE}/api/model/export?tier=${encodeURIComponent(selected)}&workers=${rec?.workers || 1}`
    : `${API_BASE}/api/model/export`;

  return { profile, rec, tier, selected, setOverride, exportUrl, error, reload: run };
}
