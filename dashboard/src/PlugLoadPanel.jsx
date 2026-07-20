import { useState } from 'react';
import { Plug, PowerOff, ShieldAlert, Gauge, KeyRound, Moon } from 'lucide-react';
import { usePlugs } from './usePlugs';
import { money, TARIFF } from './tariff';
import { GRID_EF_KG_PER_KWH, IS_IT_DOMINATED } from './sustainability';

// Plug-load management (APLC) — the end use a conventional BMS neither meters nor
// controls. In the Hanoi office-tower case study this project benchmarks against
// (Luong et al. 2025), that blind spot made plug loads the single largest end use:
// 26.4% of energy and 35.3% of CO₂, in a building that already ran a full BMS. This
// panel is the control room for the loop the twin closes: modelled (or clamp-measured)
// per-zone plug draw, and an after-hours sweep that sheds switchable sockets in
// verifiably empty zones and restores them the instant presence returns.

const CASE_STUDY_SHARE = 26.4; // % of total energy, Luong et al. 2025 (doi:10.54772/jomc.v15i02.1190)

const S = {
  section: { marginBottom: '16px', padding: '12px', background: 'rgba(0,0,0,0.3)', borderRadius: '8px', border: '1px solid var(--border-glass)' },
  label: { fontSize: '9px', color: 'var(--text-secondary)', letterSpacing: '0.08em', textTransform: 'uppercase' },
  big: { fontFamily: 'monospace', fontWeight: 'bold', fontSize: '20px', color: 'var(--text-primary)' },
  row: { display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' },
  input: { width: '52px', background: 'rgba(0,0,0,0.5)', border: '1px solid var(--border-glass)', borderRadius: '4px', color: 'var(--text-primary)', fontFamily: 'monospace', fontSize: '11px', padding: '4px 6px' },
};

export default function PlugLoadPanel({ simData }) {
  const { status, needToken, saving, setToken, updateConfig } = usePlugs();
  const [tokenInput, setTokenInput] = useState('');
  const [draft, setDraft] = useState(null); // schedule edits before SAVE

  // Live stream numbers (engine-computed; the poll below adds policy + leaderboard).
  const plugKw = simData?.plugKw ?? 0;
  const standbyKw = simData?.plugStandbyKw ?? 0;
  const shedKw = simData?.plugShedKw ?? 0;
  const savedKwh = simData?.plugSavedKwh ?? 0;
  const loadKw = (simData?.buildingLoadMw ?? 0) * 1000;
  const sharePct = loadKw > 0 ? (plugKw / loadKw) * 100 : 0;

  const cfg = draft ?? status?.config ?? null;
  const edit = (patch) => setDraft({ ...(draft ?? status.config), ...patch });

  const save = async () => {
    if (!cfg) return;
    if (await updateConfig(cfg)) setDraft(null);
  };

  return (
    <div style={{ fontFamily: 'Inter, sans-serif' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '4px' }}>
        <Plug size={16} color="var(--accent-yellow)" />
        <span style={{ fontSize: '11px', fontWeight: 'bold', color: 'var(--text-primary)', letterSpacing: '0.06em' }}>PLUG LOADS · APLC</span>
        {status?.armed && (
          <span style={{ display: 'flex', alignItems: 'center', gap: '4px', fontSize: '9px', fontWeight: 'bold', color: 'var(--accent-blue)', border: '1px solid var(--accent-blue)', borderRadius: '4px', padding: '2px 6px' }}>
            <Moon size={10} /> SWEEP ARMED
          </span>
        )}
      </div>
      <p style={{ fontSize: '10px', color: 'var(--text-secondary)', lineHeight: 1.5, margin: '0 0 12px 0' }}>
        The load a BMS can't see: sockets. The Hanoi tower case study measured plug loads as its <b>largest</b> end
        use — {CASE_STUDY_SHARE}% of energy — despite a full BMS. This building runs occupancy-verified socket
        control instead.
      </p>

      {/* Live picture */}
      <div style={S.section}>
        <div style={S.row}>
          <span style={S.label}>Plug draw now</span>
          <span style={S.big}>{plugKw.toFixed(0)} kW</span>
        </div>
        <div style={S.row}>
          <span style={S.label}>Share of building load</span>
          <span style={{ fontFamily: 'monospace', fontWeight: 'bold', fontSize: '13px', color: sharePct > CASE_STUDY_SHARE ? 'var(--accent-yellow)' : 'var(--accent-green)' }}>
            {sharePct.toFixed(1)}% <span style={{ color: 'var(--text-muted)', fontWeight: 'normal', fontSize: '10px' }}>(office case study: {CASE_STUDY_SHARE}%)</span>
          </span>
        </div>
        {IS_IT_DOMINATED && (
          <div style={{ fontSize: '8px', color: 'var(--text-muted)', marginBottom: '6px' }}>
            This building's load is server-room-dominated, so its plug share reads low against the office cohort.
          </div>
        )}
        <div style={S.row}>
          <span style={S.label}>Always-on standby (phantom)</span>
          <span style={{ fontFamily: 'monospace', fontWeight: 'bold', fontSize: '13px', color: 'var(--accent-yellow)' }}>{standbyKw.toFixed(1)} kW</span>
        </div>
        <div style={{ ...S.row, marginBottom: 0 }}>
          <span style={S.label}>Swept off right now</span>
          <span style={{ fontFamily: 'monospace', fontWeight: 'bold', fontSize: '13px', color: shedKw > 0 ? 'var(--accent-green)' : 'var(--text-muted)' }}>
            {shedKw.toFixed(1)} kW · {status?.shedZones ?? 0} zones
          </span>
        </div>
      </div>

      {/* Savings — the reportable numbers */}
      <div style={S.section}>
        <div style={S.row}>
          <span style={S.label}>Energy avoided (cumulative)</span>
          <span style={{ ...S.big, color: 'var(--accent-green)' }}>{savedKwh.toFixed(1)} kWh</span>
        </div>
        <div style={S.row}>
          <span style={S.label}>≈ cost avoided</span>
          <span style={{ fontFamily: 'monospace', fontWeight: 'bold', fontSize: '13px', color: 'var(--accent-green)' }}>{money(savedKwh * TARIFF.normalPerKwh)}</span>
        </div>
        <div style={{ ...S.row, marginBottom: 0 }}>
          <span style={S.label}>≈ CO₂ avoided (VN grid)</span>
          <span style={{ fontFamily: 'monospace', fontWeight: 'bold', fontSize: '13px', color: 'var(--accent-green)' }}>{(savedKwh * GRID_EF_KG_PER_KWH).toFixed(1)} kg</span>
        </div>
        <div style={{ fontSize: '8px', color: 'var(--text-muted)', marginTop: '6px' }}>
          Switchable standby × time shed, wall-clock. Priced at the normal EVN rate; grid factor {GRID_EF_KG_PER_KWH} kgCO₂/kWh.
        </div>
      </div>

      {/* Sweep policy */}
      {cfg && (
        <div style={S.section}>
          <div style={{ ...S.row, marginBottom: '10px' }}>
            <span style={{ ...S.label, display: 'flex', alignItems: 'center', gap: '6px' }}>
              <PowerOff size={11} /> After-hours sweep
            </span>
            <button
              onClick={() => edit({ enabled: !cfg.enabled })}
              style={{
                background: cfg.enabled ? 'rgba(46,204,113,0.15)' : 'rgba(0,0,0,0.5)',
                border: `1px solid ${cfg.enabled ? 'var(--accent-green)' : 'var(--border-glass)'}`,
                color: cfg.enabled ? 'var(--accent-green)' : 'var(--text-secondary)',
                borderRadius: '4px', padding: '4px 10px', fontSize: '10px', fontWeight: 'bold', cursor: 'pointer',
              }}
            >
              {cfg.enabled ? 'ENABLED' : 'DISABLED'}
            </button>
          </div>
          <div style={S.row}>
            <span style={S.label}>Work hours (sweep disarmed)</span>
            <span style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
              <input type="number" min="0" max="23" value={cfg.workStartHour} onChange={(e) => edit({ workStartHour: +e.target.value })} style={S.input} />
              <span style={{ color: 'var(--text-muted)', fontSize: '10px' }}>–</span>
              <input type="number" min="0" max="23" value={cfg.workEndHour} onChange={(e) => edit({ workEndHour: +e.target.value })} style={S.input} />
            </span>
          </div>
          <div style={S.row}>
            <span style={S.label}>Vacancy grace (minutes)</span>
            <input type="number" min="0" max="240" value={cfg.graceMinutes} onChange={(e) => edit({ graceMinutes: +e.target.value })} style={S.input} />
          </div>
          <div style={{ ...S.row, marginBottom: 0 }}>
            <span style={{ fontSize: '8px', color: 'var(--text-muted)' }}>
              Never swept: {(cfg.criticalTypes || []).join(', ') || '—'}
            </span>
            {draft && (
              <button onClick={save} disabled={saving} style={{ background: 'var(--accent-blue)', border: 'none', color: '#fff', borderRadius: '4px', padding: '4px 12px', fontSize: '10px', fontWeight: 'bold', cursor: 'pointer', opacity: saving ? 0.6 : 1 }}>
                {saving ? 'SAVING…' : 'SAVE POLICY'}
              </button>
            )}
          </div>
          {needToken && (
            <div style={{ marginTop: '10px', paddingTop: '10px', borderTop: '1px solid var(--border-glass)' }}>
              <span style={{ ...S.label, display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '6px' }}>
                <KeyRound size={11} color="var(--accent-yellow)" /> Admin token required for policy changes
              </span>
              <input
                type="password"
                value={tokenInput}
                placeholder="X-Admin-Token"
                onChange={(e) => { setTokenInput(e.target.value); setToken(e.target.value); }}
                style={{ ...S.input, width: '100%', boxSizing: 'border-box', marginTop: '4px' }}
              />
            </div>
          )}
        </div>
      )}

      {/* Phantom-load leaderboard */}
      <div style={S.section}>
        <div style={{ ...S.label, display: 'flex', alignItems: 'center', gap: '6px', marginBottom: '8px' }}>
          <Gauge size={11} /> Top standby (phantom) zones
        </div>
        {(status?.topStandby ?? []).map((z) => (
          <div key={z.zoneId} style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '4px 0', borderBottom: '1px solid rgba(255,255,255,0.04)' }}>
            <span style={{ fontSize: '10px', fontFamily: 'monospace', color: 'var(--text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '55%' }}>
              {z.zoneId}
            </span>
            <span style={{ display: 'flex', gap: '5px', alignItems: 'center' }}>
              {z.critical && <span title="critical type — never swept"><ShieldAlert size={10} color="var(--accent-red)" /></span>}
              {z.shed && <span style={{ fontSize: '8px', fontWeight: 'bold', color: 'var(--accent-green)' }}>SHED</span>}
              {z.measured && <span style={{ fontSize: '8px', fontWeight: 'bold', color: 'var(--accent-blue)' }}>CLAMP</span>}
              <span style={{ fontSize: '10px', fontFamily: 'monospace', fontWeight: 'bold', color: 'var(--accent-yellow)' }}>{z.standbyW.toFixed(0)} W</span>
            </span>
          </div>
        ))}
        <div style={{ fontSize: '8px', color: 'var(--text-muted)', marginTop: '8px' }}>
          {status?.meteredZones
            ? `${status.meteredZones} zone(s) metered by a live SCT-013 clamp; the rest are modelled from area + occupancy.`
            : 'No power clamp reporting — figures are modelled (1.2 W/m² standby + 65 W per present occupant, coincidence-weighted). Fit an SCT-013 node for measured watts.'}
        </div>
      </div>
    </div>
  );
}
