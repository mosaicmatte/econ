#!/usr/bin/env python3
"""ECON local recommender — run the twin's learned model offline.

This is a faithful, dependency-free port of the engine's recommendation scoring
(server/simulation/recommend.go). It loads the learned baseline model you exported from
the dashboard and, given your own current sensor readings, prints the same σ-scored
recommendations and alerts the live twin would — no server, no network, just Python 3.

    python3 recommender.py sample_readings.json

Files in this bundle it uses:
  baselines.json   the learned per-(zone, metric, hour-of-day) distributions (mean/var/n)
  model-spec.json  the model parameters + per-metric thresholds, so scoring matches exactly

Input (a JSON array of your live readings):
  [{"zone": "zone-office-a", "metric": "co2", "value": 1180, "hour": 14, "setpoint": 24}, ...]
  - "hour" is optional (defaults to the current local hour).
  - "setpoint" is only used for the temperature gate (a hot room at/below setpoint is
    being cooled correctly and is not flagged).

Exit code doubles as an alert signal for cron/monitoring:
  0 = nothing abnormal, 1 = at least one warning, 2 = at least one critical.
"""
import datetime
import json
import math
import sys


def _load(path):
    with open(path, "r", encoding="utf-8") as f:
        return json.load(f)


def score(stats, spec, zone, metric, value, hour, mature_after, pooled_hour):
    """Best learned bucket for (zone, metric) at `hour`: the clock-hour bucket if
    established, else the pooled all-hours bucket, else None. Mirrors baselines.go:score."""
    key = zone + "\x1f" + metric  # matches the engine's baselineKey (unit-separator joined)
    buckets = stats.get(key)
    if not buckets:
        return None
    st = buckets.get(str(hour))
    chosen = hour
    if st is None or st["n"] < mature_after:
        pooled = buckets.get(str(pooled_hour))
        if pooled is not None:
            st, chosen = pooled, pooled_hour
    if st is None:
        return None
    std = math.sqrt(max(0.0, st["v"]))
    sd = max(std, spec["minSigma"])
    z = (value - st["m"]) / sd if sd > 0 else 0.0
    return {"z": z, "mean": st["m"], "std": std, "n": st["n"], "hour": chosen,
            "mature": st["n"] >= mature_after}


def _hour_label(h, pooled_hour):
    return "all-hours" if h == pooled_hour else "%02d:00" % h


def recommend(spec_doc, stats, readings):
    metrics = spec_doc["metrics"]
    mature_after = spec_doc["matureAfter"]
    pooled_hour = spec_doc["pooledHour"]
    now_hour = datetime.datetime.now().hour
    recs = []

    for r in readings:
        metric = r.get("metric")
        spec = metrics.get(metric)
        if spec is None:
            continue
        zone = r.get("zone", "?")
        value = float(r["value"])
        hour = int(r.get("hour", now_hour))
        sc = score(stats, spec, zone, metric, value, hour, mature_after, pooled_hour)

        # Learned anomaly: an established baseline says this is far from normal.
        if sc and sc["mature"] and sc["z"] >= spec["zAlert"] and value > 0:
            # Temperature only counts as a problem when the room is also above setpoint.
            if metric == "temp" and r.get("setpoint") is not None and value <= float(r["setpoint"]):
                continue
            severity = "critical" if sc["z"] >= 5.0 else "warning"
            recs.append({
                "zone": zone, "metric": metric, "severity": severity, "basis": "learned",
                "value": value, "unit": spec["unit"], "baseline": round(sc["mean"], 2),
                "sigma": round(sc["std"], 2), "deviation": round(sc["z"], 2),
                "samples": sc["n"], "action": spec["action"],
                "message": "%s %s is %.1f %s — %.1fσ above its learned %s normal of %.1f±%.1f (%d samples)." % (
                    zone, spec["label"], value, spec["unit"], sc["z"],
                    _hour_label(sc["hour"], pooled_hour), sc["mean"], sc["std"], sc["n"]),
                "_score": sc["z"],
            })
        # Cold-start floor: a recognized fixed standard while the baseline is still learning.
        elif spec.get("standardHi") and value > spec["standardHi"]:
            recs.append({
                "zone": zone, "metric": metric, "severity": "warning", "basis": "standard",
                "value": value, "unit": spec["unit"], "action": spec["action"],
                "message": "%s %s is %.1f %s (above the %.0f %s recognized guideline; baseline still learning)." % (
                    zone, spec["label"], value, spec["unit"], spec["standardHi"], spec["unit"]),
                "_score": 2.0 + (value / spec["standardHi"] - 1.0),
            })

    recs.sort(key=lambda x: -x["_score"])
    for r in recs:
        r.pop("_score", None)
    return recs


def main(argv):
    if len(argv) < 2:
        print("usage: python3 recommender.py <readings.json> "
              "[baselines.json] [model-spec.json]", file=sys.stderr)
        return 64
    readings_path = argv[1]
    baselines_path = argv[2] if len(argv) > 2 else "baselines.json"
    spec_path = argv[3] if len(argv) > 3 else "model-spec.json"

    stats = _load(baselines_path)
    spec_doc = _load(spec_path)
    readings = _load(readings_path)
    recs = recommend(spec_doc, stats, readings)

    # Machine-readable on stdout; human-readable alert lines on stderr so a pipeline can
    # consume the JSON while an operator still sees the alerts.
    print(json.dumps(recs, ensure_ascii=False, indent=2))
    worst = 0
    for r in recs:
        tag = r["severity"].upper()
        worst = max(worst, 2 if r["severity"] == "critical" else 1)
        print("[%s] %s" % (tag, r["message"]), file=sys.stderr)
    if not recs:
        print("[OK] no anomalies against the learned baselines", file=sys.stderr)
    return worst


if __name__ == "__main__":
    sys.exit(main(sys.argv))
