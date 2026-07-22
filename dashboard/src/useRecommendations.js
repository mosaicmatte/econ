// Learned recommendations: the ranked anomaly report from the engine's online baseline
// model (server/simulation/baselines.go), polled from GET /api/recommendations.
//
// This is what replaced the hardcoded threshold cards. Each recommendation is scored in σ
// against what a zone actually does at this hour, and the payload also reports the model's
// own maturity (established vs still-learning buckets) so the panel can say "nothing
// abnormal" and "still warming up" without ambiguity. One hook, both dashboards — desktop
// and mobile must agree on what the model is flagging.

import { useCallback, useEffect, useState } from 'react';
import { API_BASE } from './api';

export function useRecommendations(pollMs = 10000) {
  const [report, setReport] = useState(null); // {recommendations:[], model:{...}} | null

  const load = useCallback(() => {
    fetch(`${API_BASE}/api/recommendations`)
      .then((r) => (r.ok ? r.json() : null))
      .then((s) => { if (s) setReport(s); })
      .catch(() => {});
  }, []);

  useEffect(() => {
    load();
    const id = setInterval(load, pollMs);
    return () => clearInterval(id);
  }, [load, pollMs]);

  const recommendations = report?.recommendations || [];
  const model = report?.model || null;
  return { recommendations, model, report, reload: load };
}
