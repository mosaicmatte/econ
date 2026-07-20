// EVN time-of-use (TOU) electricity tariff — Vietnamese commercial context.
//
// Vietnam prices commercial power very differently from the US model this dashboard first
// shipped with, and getting it right matters for a domestic audience. Under Bộ Công Thương
// / EVN regulation the retail bill for a commercial site is almost entirely a three-tier
// ENERGY price with a punishing peak (giờ cao điểm) rate — there is no capacity (đ/kW)
// demand charge on the business tariff. (A two-part capacity charge does begin 1 Jul 2026,
// but only for the ~7,000-site manufacturing pilot at ≥22 kV / ≥200,000 kWh·month; "kinh
// doanh" commercial customers are not expected in it until ~2028–2030, so it does not apply
// to this building.) The TOU windows were restructured by Decision 963/QĐ-BCT (implementing
// Circular 60/2025/TT-BCT), effective 22 Apr 2026, which retired the old Thông tư 16/2014
// split-peak schedule and consolidated peak into a single evening block:
//
//   • Giờ cao điểm (peak)      — 17:30–22:30, Mon–Sat (single block; NO peak on Sunday)
//   • Giờ thấp điểm (off-peak) — 00:00–06:00, every day
//   • Giờ bình thường (normal) — 06:00–17:30 and 22:30–24:00 (all Sunday outside off-peak)
//
// Peak energy costs ~1.7× normal and ~3.1× off-peak, so the real Vietnamese corporate play
// is LOAD SHIFTING, not demand-charge shaving: pre-cool the building's thermal mass overnight
// and through the day, then let the chillers coast through the evening cao điểm, buying cheap
// kWh instead of expensive ones. This is also a national priority after the 2023 Northern-grid
// shortfalls — EVN runs a formal demand-response (điều chỉnh phụ tải, DR) programme that pays
// large customers to do exactly this. These numbers quantify that shift.
//
// Rates are the EVN "Kinh doanh" (business) ≥22 kV TOU tier from Decision 1279/QĐ-BCT
// (effective 10 May 2025), in đồng/kWh — the tier a large commercial campus connects on, and
// the widest peak↔off-peak spread (VND 3,416/kWh) in the country, which is what makes storage
// and pre-cooling pay. Override per site (voltage tier, the "Sản xuất" manufacturing tariff,
// or a newer EVN price decision) via the VITE_TARIFF_* env vars. All values are plain numbers.
const num = (v, d) => (v != null && !Number.isNaN(Number(v)) ? Number(v) : d);

export const TARIFF = {
  currency: '₫',
  normalPerKwh: num(import.meta.env.VITE_TARIFF_NORMAL_KWH, 2887),   // giờ bình thường
  offPeakPerKwh: num(import.meta.env.VITE_TARIFF_OFFPEAK_KWH, 1609), // giờ thấp điểm
  peakPerKwh: num(import.meta.env.VITE_TARIFF_PEAK_KWH, 5025),       // giờ cao điểm
  // 17:30–22:30 = 5 continuous peak hours on a working day.
  peakHoursPerDay: num(import.meta.env.VITE_TARIFF_PEAK_HOURS, 5),
  // Mon–Sat carry cao điểm; Sunday has none → ~26 charged-peak days per month.
  workingDaysPerMonth: num(import.meta.env.VITE_TARIFF_WORK_DAYS, 26),
};

// touPeriod classifies the local wall-clock into an EVN TOU band (Decision 963/QĐ-BCT, 2026).
export function touPeriod(date = new Date()) {
  const day = date.getDay();                      // 0 = Sunday
  const mins = date.getHours() * 60 + date.getMinutes();
  if (mins < 6 * 60) return 'offpeak';                                  // 00:00–06:00 every day
  if (day !== 0 && mins >= 17 * 60 + 30 && mins < 22 * 60 + 30)         // 17:30–22:30 Mon–Sat
    return 'peak';
  return 'normal';                                                       // 06:00–17:30 & 22:30–24:00
}

export function touPeriodLabel(period) {
  return period === 'peak' ? 'peak hours'
    : period === 'offpeak' ? 'off-peak hours'
    : 'normal hours';
}

// Minutes until the next cao điểm window opens (17:30 on a Mon–Sat), or null when peak
// is already running or the next charged peak is more than a day away (Saturday night →
// Monday). Lets the AI layer warn "peak begins in 40 min" off the same clock that
// prices it, instead of a UI demo toggle.
export function minutesToPeak(date = new Date()) {
  if (touPeriod(date) === 'peak') return 0;
  const day = date.getDay();
  const mins = date.getHours() * 60 + date.getMinutes();
  const peakStart = 17 * 60 + 30;
  if (day !== 0 && mins < peakStart) return peakStart - mins; // later today
  return null; // tonight's window is past (or it's Sunday): next peak is not today
}

// Current TOU energy rate (đồng/kWh).
export function rateNow(date = new Date()) {
  const p = touPeriod(date);
  return p === 'peak' ? TARIFF.peakPerKwh : p === 'offpeak' ? TARIFF.offPeakPerKwh : TARIFF.normalPerKwh;
}

// A period's rate, formatted (e.g. "5,025 VND").
export function rateStr(period) {
  const r = period === 'peak' ? TARIFF.peakPerKwh : period === 'offpeak' ? TARIFF.offPeakPerKwh : TARIFF.normalPerKwh;
  return r.toLocaleString('en-US') + ' VND';
}

// Run-rate energy cost of sustaining `kw` for a day at the CURRENT TOU rate — a projection
// ("at this rate, ~X đ/day"), used to price waste on an unoccupied zone.
export function energyCostPerDay(kw, date = new Date()) {
  return Math.max(0, kw) * 24 * rateNow(date);
}

// The core Vietnamese saving: shifting `kw` of cooling load OUT of the daily cao điểm window
// into normal-rate hours (via pre-cooling / thermal-mass charging). Saving = shifted energy ×
// the peak-vs-normal rate gap. This is what EVN's DR programme and every facility manager
// optimises for, and it is a genuine number, not a fabricated demand charge.
export function peakShiftSavingPerDay(kw) {
  return Math.max(0, kw) * TARIFF.peakHoursPerDay * (TARIFF.peakPerKwh - TARIFF.normalPerKwh);
}

export function peakShiftSavingPerMonth(kw) {
  return peakShiftSavingPerDay(kw) * TARIFF.workingDaysPerMonth;
}

// Money formatter
export function money(vnd) {
  const n = Math.round(Number(vnd) || 0);
  const abs = Math.abs(n);
  if (abs >= 1e6) return (n / 1e6).toFixed(1) + 'M VND';
  if (abs >= 1e3) return Math.round(n / 1e3).toLocaleString('en-US') + 'k VND';
  return n.toLocaleString('en-US') + ' VND';
}
