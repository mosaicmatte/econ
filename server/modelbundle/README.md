# ECON local models

This bundle is the twin's learned intelligence, exported so you can run recommendations and
alerts **locally** — no server, no cloud, no network. Everything here was produced by the
running ECON digital twin from your building's own telemetry.

## What's inside

| File | What it is |
|------|-----------|
| `baselines.json` | The **learned baseline model**: per-`(zone, metric, hour-of-day)` distributions (mean, variance, sample count) the engine built online from the live telemetry stream. This is the model that decides what's *normal* for each room at each hour. |
| `model-spec.json` | The model's parameters and per-metric thresholds (σ floor, alert level, action, the ASHRAE CO₂ floor). Keeps offline scoring identical to the live engine. |
| `recommender.py` | A dependency-free (**Python 3 standard library only**) recommender that reproduces the engine's σ-scoring. Feed it your current readings; it prints the same recommendations and alerts the dashboard shows. |
| `sample_readings.json` | An example input so you can try it immediately. |
| `forecaster/` | The trained **LSTM peak-load forecaster** (`model_weights.pth`, `scaler.pkl`, `config.json`) — present when the forecasting service was reachable at export time. Running it needs PyTorch; the recommender does not. |
| `MANIFEST.json` | Provenance: when it was exported, the model's maturity, and which pieces are included. |

## Run it

```bash
python3 recommender.py sample_readings.json
```

- **stdout** is machine-readable JSON (pipe it into your monitoring).
- **stderr** is human-readable alert lines (`[CRITICAL] …`, `[WARNING] …`, or `[OK] …`).
- **exit code** is the alert signal: `0` nothing abnormal, `1` a warning, `2` a critical — so
  a cron job can page on a non-zero exit.

### Feeding your own readings

`recommender.py` takes a JSON array of your live sensor values:

```json
[
  {"zone": "zone-office-a", "metric": "co2",  "value": 1180, "hour": 14},
  {"zone": "zone-office-a", "metric": "temp", "value": 27.5, "hour": 14, "setpoint": 24}
]
```

- `metric` is one of the keys in `model-spec.json` (`co2`, `temp`, `buildingLoadMw`, `plugKw`).
- `hour` is optional (defaults to the current local hour); scoring falls back to the model's
  pooled all-hours distribution when a specific hour hasn't matured.
- `setpoint` is optional and only used for `temp` — a hot room at/below setpoint is being
  cooled correctly and is not flagged.

## How scoring works (the short version)

For each reading, the recommender looks up the learned distribution for that zone, metric and
hour, and measures how many standard deviations (σ) the value sits from that learned mean. A
reading past the metric's alert threshold (default ~3σ) is flagged — an anomaly **relative to
what this room actually does**, not a fixed number. Until a zone's baseline has seen enough
samples to be trusted, a recognized fixed standard (the ASHRAE ≤ 1000 ppm CO₂ guideline) is
the labelled fallback. That's exactly what the live twin does; this bundle just lets you do it
on your own machine.
