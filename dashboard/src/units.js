// Power, energy and rate formatting — one set of rules, so a panel cannot show the same
// quantity two different ways.
//
// This exists because the overview did exactly that: TOTAL LOAD read "0.01 MW" while the
// utilization bar two rows below read "25 kW", and the BESS read "discharging 0.01 MW".
// Megawatts are the right unit for the tower this dashboard was built against and the
// wrong one for a house, where every headline figure rounds to 0.01 or to zero. The unit
// should follow the magnitude, not the building the code was first written for.

// powerMw renders a power given in MEGAWATTS at a sensible scale.
//   0.0000  -> "0 W"      (nothing, not "0.00 MW")
//   0.00012 -> "120 W"
//   0.0118  -> "11.8 kW"
//   1.42    -> "1.42 MW"
export function powerMw(mw, { digits } = {}) {
  const v = Number(mw);
  if (!Number.isFinite(v)) return '—';
  const w = v * 1e6;
  const abs = Math.abs(w);
  if (abs < 0.5) return '0 W';
  if (abs < 1000) return `${Math.round(w)} W`;
  if (abs < 1e6) {
    const kw = w / 1000;
    const d = digits ?? (Math.abs(kw) < 10 ? 1 : 0);
    return `${kw.toFixed(d)} kW`;
  }
  return `${v.toFixed(digits ?? 2)} MW`;
}

// powerKw is the same rule for a value already in kilowatts.
export function powerKw(kw, opts) {
  return powerMw((Number(kw) || 0) / 1000, opts);
}

// splitPowerMw returns { value, unit } for callers that style the number and the unit
// differently — the overview's big readouts do.
export function splitPowerMw(mw) {
  const s = powerMw(mw);
  const i = s.lastIndexOf(' ');
  return { value: s.slice(0, i), unit: s.slice(i + 1) };
}

// energyKwh renders an energy in kWh, stepping up to MWh where kWh stops being readable.
export function energyKwh(kwh) {
  const v = Number(kwh);
  if (!Number.isFinite(v)) return '—';
  if (Math.abs(v) >= 1000) return `${(v / 1000).toFixed(2)} MWh`;
  return `${v.toFixed(1)} kWh`;
}
