import React, { useEffect, useRef, useState } from 'react';
import { UploadCloud, FileCheck2, AlertTriangle, Building2, Loader2, History, KeyRound } from 'lucide-react';
import { API_BASE } from './api';

// Blueprint -> twin, in two explicit steps so nothing deploys sight-unseen:
//   1) DIGITIZE: upload a DXF / PDF / image (or, on a phone, photograph the paper
//      drawing) and review what the pipeline found — zone count, types, method.
//   2) DEPLOY: swap the running engine onto it and reload the app, which refetches
//      geometry at boot (buildingStore) so every panel rebuilds on the new building.
// Shared by desktop (modal) and mobile (full-screen sheet) — one flow, one truth.

const ACCEPT = '.dxf,.pdf,.png,.jpg,.jpeg,.webp,.bmp,.tif,.tiff';

export default function BlueprintImportPanel({ onClose, mobile = false }) {
  const [file, setFile] = useState(null);
  const [floors, setFloors] = useState(1);
  const [fw, setFw] = useState(60);
  const [fd, setFd] = useState(40);
  const [busy, setBusy] = useState(false);
  const [deploying, setDeploying] = useState(false);
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  const [dragOver, setDragOver] = useState(false);
  // Operational guardrails: the server may require an admin token for anything that
  // replaces the running building (ECON_ADMIN_TOKEN). We only ask for it after a 401,
  // so the demo flow stays frictionless and the commercial flow stays gated.
  const [token, setToken] = useState('');
  const [needToken, setNeedToken] = useState(false);
  const [backups, setBackups] = useState([]);
  const [restoring, setRestoring] = useState(null);
  const inputRef = useRef(null);

  useEffect(() => {
    fetch(`${API_BASE}/api/building/backups`)
      .then((r) => (r.ok ? r.json() : []))
      .then((list) => setBackups(Array.isArray(list) ? list : []))
      .catch(() => {});
  }, []);

  const adminHeaders = token ? { 'X-Admin-Token': token } : {};
  const handleAuthFailure = () => {
    setNeedToken(true);
    setError('This server requires an admin token to replace the running building. Enter it below and retry.');
  };

  const pick = (f) => {
    if (!f) return;
    setFile(f);
    setResult(null);
    setError(null);
  };

  const digitize = async () => {
    if (!file || busy) return;
    setBusy(true);
    setError(null);
    setResult(null);
    try {
      const form = new FormData();
      form.append('file', file);
      form.append('floors', String(floors));
      form.append('footprint', `${fw}x${fd}`);
      const r = await fetch(`${API_BASE}/api/digitize`, { method: 'POST', body: form });
      const text = await r.text();
      if (!r.ok) {
        let msg = text;
        try { msg = JSON.parse(text).detail || text; } catch { /* raw text */ }
        throw new Error(msg);
      }
      setResult(JSON.parse(text));
    } catch (e) {
      setError(String(e.message || e));
    } finally {
      setBusy(false);
    }
  };

  const deploy = async () => {
    if (!result || deploying) return;
    setDeploying(true);
    setError(null);
    try {
      const r = await fetch(`${API_BASE}/api/building`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...adminHeaders },
        body: JSON.stringify({
          buildingData: result.buildingData,
          ontology: result.ontology,
          source: file?.name || 'unknown', // lands in the server's audit log
        }),
      });
      if (r.status === 401) { handleAuthFailure(); setDeploying(false); return; }
      if (!r.ok) throw new Error(await r.text());
      // The engine is already running the new building; a full reload makes the
      // frontend refetch geometry at boot and rebuild everything on it.
      window.location.reload();
    } catch (e) {
      setError(String(e.message || e));
      setDeploying(false);
    }
  };

  const restore = async (name) => {
    if (restoring) return;
    setRestoring(name);
    setError(null);
    try {
      const r = await fetch(`${API_BASE}/api/building/rollback`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', ...adminHeaders },
        body: JSON.stringify({ name }),
      });
      if (r.status === 401) { handleAuthFailure(); setRestoring(null); return; }
      if (!r.ok) throw new Error(await r.text());
      window.location.reload();
    } catch (e) {
      setError(String(e.message || e));
      setRestoring(null);
    }
  };

  const s = result?.stats;

  const shell = mobile
    ? { display: 'flex', flexDirection: 'column', gap: '16px', color: '#fff' }
    : {
        position: 'absolute', inset: 0, zIndex: 200, display: 'flex', alignItems: 'center',
        justifyContent: 'center', background: 'rgba(0,0,0,0.65)', backdropFilter: 'blur(4px)',
      };
  const card = mobile
    ? {}
    : {
        width: 'min(560px, 92vw)', maxHeight: '88vh', overflowY: 'auto',
        background: 'rgba(12,14,18,0.97)', border: '1px solid var(--border-glass)',
        borderRadius: '12px', padding: '20px', color: 'var(--text-primary)',
        display: 'flex', flexDirection: 'column', gap: '16px',
      };
  const label = { fontSize: '10px', letterSpacing: '1px', color: mobile ? 'rgba(255,255,255,0.6)' : 'var(--text-secondary)', fontWeight: 'bold' };
  const input = {
    width: '100%', boxSizing: 'border-box', background: 'rgba(255,255,255,0.06)',
    border: '1px solid rgba(255,255,255,0.15)', borderRadius: '6px', color: 'inherit',
    padding: '8px 10px', fontSize: '13px',
  };

  return (
    <div style={shell} onClick={mobile ? undefined : onClose}>
      <div style={card} onClick={(e) => e.stopPropagation()}>
        {!mobile && (
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', borderBottom: '1px solid var(--border-glass)', paddingBottom: '10px' }}>
            <h3 style={{ margin: 0, fontSize: '14px', letterSpacing: '1px', display: 'flex', alignItems: 'center', gap: '8px' }}>
              <Building2 size={16} /> IMPORT BLUEPRINT
            </h3>
            <button onClick={onClose} style={{ background: 'transparent', border: '1px solid var(--text-secondary)', borderRadius: '4px', padding: '4px 10px', color: 'var(--text-secondary)', cursor: 'pointer', fontSize: '10px', fontWeight: 'bold' }}>CLOSE</button>
          </div>
        )}

        {/* Drop zone / file picker */}
        <div
          onDragOver={(e) => { e.preventDefault(); setDragOver(true); }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => { e.preventDefault(); setDragOver(false); pick(e.dataTransfer.files?.[0]); }}
          onClick={() => inputRef.current?.click()}
          style={{
            border: `2px dashed ${dragOver ? '#3DDC84' : 'rgba(255,255,255,0.25)'}`,
            borderRadius: '10px', padding: '22px 16px', textAlign: 'center', cursor: 'pointer',
            background: dragOver ? 'rgba(61,220,132,0.06)' : 'rgba(255,255,255,0.02)',
          }}
        >
          <UploadCloud size={26} style={{ opacity: 0.7 }} />
          <div style={{ fontSize: '13px', marginTop: '8px', fontWeight: 600 }}>
            {file ? file.name : mobile ? 'Choose a file or photograph the drawing' : 'Drop a blueprint, or click to choose'}
          </div>
          <div style={{ fontSize: '11px', marginTop: '4px', opacity: 0.55 }}>
            DXF (AutoCAD) · PDF · PNG/JPG scan or photo — up to 40 MB
          </div>
          <input
            ref={inputRef} type="file" accept={ACCEPT} style={{ display: 'none' }}
            capture={mobile ? 'environment' : undefined}
            onChange={(e) => pick(e.target.files?.[0])}
          />
        </div>

        {/* Parameters */}
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: '10px' }}>
          <div>
            <div style={label}>FLOORS</div>
            <input style={input} type="number" min="1" max="200" value={floors}
              onChange={(e) => setFloors(Math.max(1, Math.min(200, Number(e.target.value) || 1)))} />
          </div>
          <div>
            <div style={label}>WIDTH (M)</div>
            <input style={input} type="number" min="5" value={fw} onChange={(e) => setFw(Number(e.target.value) || 60)} />
          </div>
          <div>
            <div style={label}>DEPTH (M)</div>
            <input style={input} type="number" min="5" value={fd} onChange={(e) => setFd(Number(e.target.value) || 40)} />
          </div>
        </div>
        <div style={{ fontSize: '11px', opacity: 0.55, lineHeight: 1.5, marginTop: '-8px' }}>
          Footprint scales images and unitless DXFs; a DXF drawn in real units overrides it.
          Photos work best flat and straight-on. AutoCAD .dwg must be exported to .dxf first
          (SAVEAS → DXF).
        </div>

        {/* Actions */}
        <button
          onClick={digitize} disabled={!file || busy}
          style={{
            width: '100%', padding: '12px', borderRadius: '8px', border: 'none', cursor: file && !busy ? 'pointer' : 'default',
            background: file && !busy ? '#2C6FB5' : 'rgba(255,255,255,0.08)', color: '#fff', fontWeight: 'bold',
            fontSize: '13px', letterSpacing: '1px', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '8px',
          }}
        >
          {busy ? (<><Loader2 size={16} className="spin" style={{ animation: 'spin 1s linear infinite' }} /> DIGITIZING…</>) : 'DIGITIZE'}
        </button>

        {error && (
          <div style={{ display: 'flex', gap: '8px', alignItems: 'flex-start', background: 'rgba(255,59,48,0.12)', border: '1px solid rgba(255,59,48,0.4)', borderRadius: '8px', padding: '10px 12px', fontSize: '12px', lineHeight: 1.5 }}>
            <AlertTriangle size={15} color="#FF6B5E" style={{ flexShrink: 0, marginTop: '1px' }} />
            <span>{error}</span>
          </div>
        )}

        {needToken && (
          <div>
            <div style={{ ...label, display: 'flex', alignItems: 'center', gap: '6px' }}><KeyRound size={11} /> ADMIN TOKEN</div>
            <input
              style={input} type="password" value={token} placeholder="X-Admin-Token"
              onChange={(e) => setToken(e.target.value)} autoComplete="off"
            />
          </div>
        )}

        {/* Review — everything the pipeline found, before anything changes */}
        {s && (
          <div style={{ background: 'rgba(61,220,132,0.06)', border: '1px solid rgba(61,220,132,0.35)', borderRadius: '8px', padding: '12px', fontSize: '12px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', fontWeight: 'bold', color: '#3DDC84' }}>
              <FileCheck2 size={15} /> DIGITIZED — REVIEW BEFORE DEPLOY
            </div>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '4px 12px', fontFamily: 'monospace', fontSize: '11.5px' }}>
              <span>zones / floor</span><span>{s.zonesPerFloor}</span>
              <span>floors</span><span>{s.floors} ({s.totalZones} zones total)</span>
              <span>footprint</span><span>{s.footprintM[0].toFixed(1)} × {s.footprintM[1].toFixed(1)} m</span>
              <span>method</span><span>{s.method}</span>
            </div>
            {s.unitNote && <div style={{ fontSize: '11px', opacity: 0.7 }}>{s.unitNote}</div>}
            <div style={{ fontSize: '11px', opacity: 0.8 }}>
              {Object.entries(s.zoneTypes).map(([t, n]) => `${n} ${t}`).join(' · ')}
            </div>
            <button
              onClick={deploy} disabled={deploying}
              style={{ width: '100%', padding: '12px', borderRadius: '8px', border: 'none', cursor: deploying ? 'default' : 'pointer', background: '#3DDC84', color: '#08110b', fontWeight: 'bold', fontSize: '13px', letterSpacing: '1px', marginTop: '4px' }}
            >
              {deploying ? 'DEPLOYING…' : 'DEPLOY TO TWIN (replaces current building)'}
            </button>
            <div style={{ fontSize: '10.5px', opacity: 0.6, lineHeight: 1.5 }}>
              Deploy swaps the live engine onto this building and reloads the app. The
              current building is backed up server-side first and the change is written
              to the audit log. Zone telemetry history starts fresh; edge nodes re-bind
              on their next message.
            </div>
          </div>
        )}

        {/* Restore — the automatic pre-deploy backups, newest first */}
        {backups.length > 0 && (
          <div style={{ borderTop: `1px solid ${mobile ? 'rgba(255,255,255,0.12)' : 'var(--border-glass)'}`, paddingTop: '12px' }}>
            <div style={{ ...label, display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '8px' }}>
              <History size={11} /> PREVIOUS BUILDINGS ({backups.length})
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: '6px', maxHeight: '150px', overflowY: 'auto' }}>
              {backups.map((b) => (
                <div key={b.name} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', fontSize: '11.5px', fontFamily: 'monospace', background: 'rgba(255,255,255,0.03)', borderRadius: '6px', padding: '7px 10px' }}>
                  <span>{b.name}</span>
                  <span style={{ opacity: 0.6 }}>{b.floors} fl · {b.zones} zones</span>
                  <button
                    onClick={() => restore(b.name)} disabled={!!restoring}
                    style={{ background: 'rgba(255,255,255,0.08)', border: '1px solid rgba(255,255,255,0.2)', borderRadius: '4px', color: 'inherit', padding: '3px 10px', fontSize: '10px', fontWeight: 'bold', cursor: restoring ? 'default' : 'pointer' }}
                  >
                    {restoring === b.name ? 'RESTORING…' : 'RESTORE'}
                  </button>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
