// Both load forecasters over the same instant (GET /api/forecast/compare).
//
// The panel needs this for one specific reason: honesty about what can be drawn.
//
// The supervised LSTM returns a single scalar — the predicted peak. It does not return a
// trajectory, and it never has. The forecast card nonetheless drew a 13-point curve from
// the live load to that peak using a smoothstep ease, labelled "PROJECTED RAMP". The two
// endpoints were real; every point between them was invented by the browser, and nothing
// on the chart said so. An operator reading the slope was reading a cosmetic easing
// function.
//
// TimesFM, by contrast, is a sequence model: it returns the whole horizon. So the rule the
// card now follows is simply "plot the series that exists". When the zero-shot forecaster
// has answered, its real horizon is drawn. When only the LSTM has answered, no curve is
// drawn at all — the two numbers are shown as two numbers.

import { useCallback, useEffect, useState } from 'react';
import { API_BASE } from './api';

export function useForecastCompare(pollMs = 60000) {
  const [data, setData] = useState(null);

  const load = useCallback(() => {
    fetch(`${API_BASE}/api/forecast/compare`)
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => { if (d) setData(d); })
      .catch(() => {});
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, pollMs);
    return () => clearInterval(id);
  }, [load, pollMs]);

  // The zero-shot engine's horizon, when it produced one. Anything else is null so a
  // caller cannot accidentally treat an absent forecast as a flat one.
  const timesfm = data?.timesfm || null;
  const lstm = data?.lstm || null;
  const series = Array.isArray(timesfm?.series) && timesfm.series.length > 0 ? timesfm.series : null;

  // The zero-shot model's predictive spread. TimesFM returns per-decile heads alongside
  // its central path, and until now they were decoded in Python and dropped at the Go
  // boundary — so no consumer had ever seen how wide the forecast actually was. The upper
  // band is what says whether a peak is firm or a coin toss.
  const upperBand = Array.isArray(timesfm?.quantiles?.[timesfm?.upperQuantile])
    ? timesfm.quantiles[timesfm.upperQuantile]
    : null;

  return {
    data, lstm, timesfm, series,
    upperBand,
    upperQuantile: timesfm?.upperQuantile ?? null,
    peakUpperMw: timesfm?.peakUpperMw ?? null,
    agreement: data?.agreement ?? null,
    stepMinutes: data?.stepMinutes ?? 5,
    reload: load,
  };
}

export default useForecastCompare;
