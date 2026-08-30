import React from 'react';
import ForecastChart from './ForecastChart';

// The evidence behind one recommendation.
//
// /api/recommendations has always carried the reasoning — the learned mean and σ, the
// z-score, how many samples back the baseline, which hour bucket answered, the predicted
// and equilibrium values, the time to breach — and both dashboards threw all of it away
// and rendered the prose message alone. That left every card unfalsifiable: an operator
// could read "6.2σ above normal" but had no way to see what normal was, how much data was
// behind it, or how far the reading actually sat.
//
// This renders that payload verbatim. Nothing here computes a new number or fills a gap:
// a field the engine did not send is a row that does not appear.

// σ-position strip: where the current reading sits against the learned band. The shaded
// span is ±2σ around the learned mean; the tick is the reading. Both are placed from the
// engine's own baseline/sigma, so the picture and the sentence cannot disagree.
function SigmaStrip({ value, baseline, sigma, unit, deviation }) {
  if (!(sigma > 0)) return null;
  // Four σ either side, clamped — enough to show a serious excursion without compressing
  // the normal band to a line.
  const span = 4;
  // The ENGINE's z-score, not one recomputed here. Recomputing from the rounded baseline
  // and σ in the payload lands a tenth of a σ away from the figure the card's own sentence
  // quotes, and a picture that disagrees with the text beside it is worse than no picture.
  const z = Number.isFinite(deviation) && deviation !== 0
    ? deviation
    : (value - baseline) / sigma;
  const pos = Math.max(0, Math.min(100, ((Math.max(-span, Math.min(span, z)) + span) / (2 * span)) * 100));
  const bandLo = ((-2 + span) / (2 * span)) * 100;
  const bandHi = ((2 + span) / (2 * span)) * 100;
  const clipped = Math.abs(z) > span;
  return (
    <div style={{ marginTop: '6px' }}>
      <div style={{ position: 'relative', height: '8px', background: 'rgba(255,255,255,0.05)', borderRadius: '4px' }}>
        <div
          title={`learned normal ±2σ: ${(baseline - 2 * sigma).toFixed(1)}–${(baseline + 2 * sigma).toFixed(1)} ${unit || ''}`}
          style={{ position: 'absolute', left: `${bandLo}%`, width: `${bandHi - bandLo}%`, top: 0, height: '100%', background: 'rgba(0,163,224,0.25)', borderRadius: '4px' }}
        />
        <div style={{ position: 'absolute', left: '50%', top: '-2px', width: '1px', height: '12px', background: 'rgba(255,255,255,0.45)' }} />
        <div
          title={`reading ${value.toFixed(1)} ${unit || ''} — ${z.toFixed(1)}σ`}
          style={{
            position: 'absolute', left: `calc(${pos}% - 1px)`, top: '-3px', width: '3px', height: '14px',
            background: Math.abs(z) >= 3 ? 'var(--accent-red)' : 'var(--accent-yellow)', borderRadius: '1px',
          }}
        />
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '8px', color: 'var(--text-muted)', marginTop: '3px' }}>
        <span>−4σ</span>
        <span>learned normal {baseline.toFixed(1)}±{sigma.toFixed(1)} {unit}</span>
        <span>{clipped ? `+4σ (off scale: ${z.toFixed(1)}σ)` : '+4σ'}</span>
      </div>
    </div>
  );
}

// A predicted trajectory the operator can check against the clock. Every point is the
// closed-form first-order response the engine identified for THIS room — not a cosmetic
// ease between two endpoints. It is drawn only when the engine sent both an equilibrium
// and a time constant for the room, because those two are what define the curve.
function TrajectoryStrip({ now, equilibrium, tauMin, etaSec, limit, unit }) {
  if (!(tauMin > 0) || !Number.isFinite(equilibrium) || !Number.isFinite(now)) return null;
  const horizonMin = Math.max(5, Math.min(60, ((etaSec || 0) / 60) * 1.6 || 30));
  const pts = [];
  for (let i = 0; i <= 24; i++) {
    const tMin = (i / 24) * horizonMin;
    // First-order step response: v(t) = eq + (now − eq)·e^(−t/τ).
    pts.push({ t: tMin, v: equilibrium + (now - equilibrium) * Math.exp(-tMin / tauMin) });
  }
  const vals = pts.map((p) => p.v).concat(Number.isFinite(limit) ? [limit] : []);
  const lo = Math.min(...vals), hi = Math.max(...vals);
  const rng = hi - lo || 1;
  const W = 100, H = 34;
  const path = pts.map((p, i) => `${i === 0 ? 'M' : 'L'}${((p.t / horizonMin) * W).toFixed(2)},${(H - ((p.v - lo) / rng) * H).toFixed(2)}`).join(' ');
  const limY = Number.isFinite(limit) ? H - ((limit - lo) / rng) * H : null;
  const etaX = etaSec > 0 ? Math.min(W, ((etaSec / 60) / horizonMin) * W) : null;
  return (
    <div data-testid="forecast-chart" className="forecast-chart-container forecast-chart" style={{ marginTop: '6px' }}>
      <div style={{ fontSize: '8px', color: 'var(--text-muted)', marginBottom: '2px', letterSpacing: '0.04em' }}>
        IDENTIFIED RESPONSE · τ {tauMin.toFixed(0)} MIN · NEXT {horizonMin.toFixed(0)} MIN
      </div>
      <svg className="forecast-chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" style={{ width: '100%', height: '34px', display: 'block', overflow: 'visible' }}>
        {limY !== null && (
          <line x1="0" y1={limY} x2={W} y2={limY} stroke="var(--accent-red)" strokeWidth="0.6" strokeDasharray="2 2" vectorEffect="non-scaling-stroke" />
        )}
        {etaX !== null && (
          <line x1={etaX} y1="0" x2={etaX} y2={H} stroke="var(--accent-yellow)" strokeWidth="0.6" strokeDasharray="1 2" vectorEffect="non-scaling-stroke" />
        )}
        <path d={path} fill="none" stroke="var(--accent-blue)" strokeWidth="1.4" vectorEffect="non-scaling-stroke" />
      </svg>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: '8px', color: 'var(--text-muted)', marginTop: '2px' }}>
        <span>now {now.toFixed(1)} {unit}</span>
        {Number.isFinite(limit) && <span style={{ color: 'var(--accent-red)' }}>limit {limit.toFixed(1)}</span>}
        <span>settles {equilibrium.toFixed(1)} {unit}</span>
      </div>
    </div>
  );
}

function Row({ k, v, tone }) {
  return (
    <>
      <span style={{ fontSize: '10px', color: 'var(--text-muted)' }}>{k}</span>
      <span style={{ fontSize: '10px', fontFamily: 'monospace', color: tone || 'var(--text-primary)', textAlign: 'right' }}>{v}</span>
    </>
  );
}

// hourLabel mirrors the engine's own rendering of which learned bucket answered.
const hourLabel = (h) => (h === 24 || h == null ? 'all-hours' : `${String(h).padStart(2, '0')}:00`);

// `limit` is the comfort ceiling this prediction was measured against. The caller passes
// it because only the caller knows the zone's live setpoint — the engine uses
// setpoint + 1 °C for the thermal pass and the ASHRAE guideline for CO₂. When the caller
// cannot supply it the reference line is simply not drawn; a guessed threshold on a chart
// is worse than no threshold at all.
// `horizonMin` is how far ahead the engine integrates its predictions (its
// predictHorizonSec, reported as model.horizonMin). It is what makes the identified time
// constant recoverable from the predicted value below, so it must be the engine's figure
// and not a number assumed here.
export default function RecommendationEvidence({ rec, model, matureAfter, limit, horizonMin = 30, forecast = null }) {
  if (!rec) return null;
  const unit = rec.unit || '';
  const learned = rec.basis === 'learned' && rec.sigma > 0;
  const rows = [];

  rows.push(['Reading now', `${(rec.value ?? 0).toFixed(1)} ${unit}`]);

  if (learned) {
    rows.push(['Learned normal', `${rec.baseline.toFixed(1)} ± ${rec.sigma.toFixed(1)} ${unit}`]);
    rows.push(['Deviation', `${rec.deviation >= 0 ? '+' : ''}${rec.deviation.toFixed(1)} σ`]);
    rows.push(['Bucket', `${hourLabel(rec.hour)} · ${rec.samples} samples`]);
  } else if (rec.basis === 'standard') {
    rows.push(['Basis', 'recognized fixed standard']);
    rows.push(['Baseline maturity', `${rec.samples}/${matureAfter || '—'} samples — still learning`]);
  }

  if (rec.kind === 'prediction') {
    if (rec.etaSec > 0) rows.push(['Time to breach', rec.etaSec >= 5400 ? `${(rec.etaSec / 3600).toFixed(1)} h` : `${Math.round(rec.etaSec / 60)} min`]);
    if (rec.predicted) rows.push(['At the horizon', `${rec.predicted.toFixed(1)} ${unit}`]);
    if (rec.equilibrium) rows.push(['Settles at', `${rec.equilibrium.toFixed(1)} ${unit}`]);
  }
  if (rec.kind === 'capability' && rec.equilibrium) {
    rows.push(['Equilibrium at full flow', `${rec.equilibrium.toFixed(1)} ${unit}`]);
  }

  // The identified model behind the call, when the room has one. This is the part that
  // tells an operator how much the prediction is worth.
  if (model) {
    if (rec.metric === 'co2' && model.co2Ready) {
      rows.push(['Measured air change', `${model.achPerHour.toFixed(1)} ACH · ${model.co2Samples} samples`]);
      rows.push(['Fit residual', `${model.co2Residual.toFixed(0)} ppm/h`]);
    } else if (model.thermalReady) {
      rows.push(['Time constant', `${model.timeConstantMin.toFixed(0)} min · ${model.thermalSamples} samples`]);
      rows.push(['Fit residual', `${model.thermalResidual.toFixed(2)} °C/h`]);
      // Provenance: a cooling authority fitted against the design supply temperature
      // carries that assumption's error, and saying so is the difference between a
      // model an engineer can audit and one they have to take on faith.
      const frac = model.supplyMeasuredFrac || 0;
      rows.push([
        'Supply-air basis',
        frac > 0 ? `${(frac * 100).toFixed(0)}% measured probe` : 'design value (no probe)',
        frac > 0 ? 'var(--accent-green)' : 'var(--accent-yellow)',
      ]);
    }
  }

  // Time constant for the predicted trajectory.
  //
  // Preferred source is the room's identified model. When that is not to hand, tau can be
  // recovered EXACTLY from the numbers the engine already sent, because the prediction is
  // a first-order step response evaluated at a known horizon:
  //
  //     predicted = eq + (now - eq)*exp(-T/tau)
  //     =>  tau = -T / ln((predicted - eq)/(now - eq))
  //
  // So the curve drawn is the engine's own curve, algebraically inverted — not a shape
  // chosen to look plausible. Degenerate cases (already at equilibrium, or a ratio outside
  // (0,1) from rounding) yield no tau, and then no curve is drawn at all.
  const recoveredTau = (() => {
    const { value: now, predicted, equilibrium: eq } = rec;
    if (rec.kind !== 'prediction') return 0;
    if (!Number.isFinite(predicted) || !Number.isFinite(eq) || !Number.isFinite(now)) return 0;
    const num = predicted - eq, den = now - eq;
    if (Math.abs(den) < 1e-9) return 0;
    const ratio = num / den;
    if (!(ratio > 1e-6 && ratio < 1 - 1e-6)) return 0;
    const tau = -horizonMin / Math.log(ratio);
    return Number.isFinite(tau) && tau > 0 ? tau : 0;
  })();
  const tauMin = (model?.thermalReady && rec.metric === 'temp' ? model.timeConstantMin : 0) || recoveredTau;

  const isLoadRec = rec.metric === 'buildingLoadMw' || rec.metric === 'load' || rec.zone === 'GLOBAL';

  return (
    <div style={{ marginTop: '8px', paddingTop: '8px', borderTop: '1px solid var(--border-glass)' }}>
      <div style={{ display: 'grid', gridTemplateColumns: '1fr auto', rowGap: '3px', columnGap: '10px' }}>
        {rows.map(([k, v, tone]) => <Row key={k} k={k} v={v} tone={tone} />)}
      </div>
      {learned && <SigmaStrip value={rec.value} baseline={rec.baseline} sigma={rec.sigma} deviation={rec.deviation} unit={unit} />}
      {rec.kind === 'prediction' && (
        <TrajectoryStrip now={rec.value} equilibrium={rec.equilibrium} tauMin={tauMin} etaSec={rec.etaSec} limit={limit} unit={unit} />
      )}
      {(isLoadRec && (forecast?.available || (forecast?.seriesData && forecast.seriesData.length > 0))) && (
        <div style={{ marginTop: '8px', paddingTop: '6px', borderTop: '1px solid var(--border-glass)' }}>
          <div style={{ fontSize: '8px', color: 'var(--text-muted)', marginBottom: '3px', letterSpacing: '0.04em' }}>
            PREDICTIVE LOAD TRAJECTORY & PEAK FORECAST
          </div>
          <ForecastChart
            series={forecast?.seriesData || forecast?.series}
            upperBand={forecast?.upperBand}
            upperQuantile={forecast?.upperQuantile || 'q9'}
            peakUpperMw={forecast?.peakUpperMw}
            lstmPeakMw={forecast?.lstmPeakMw}
            stepMinutes={forecast?.stepMinutes || 5}
            engine={forecast?.engine || 'timesfm'}
            liveLoadMw={isLoadRec ? rec.value : null}
            plausible={forecast?.plausible}
            height={90}
            compact={true}
            showLegend={true}
          />
        </div>
      )}
    </div>
  );
}

export { SigmaStrip, TrajectoryStrip };
