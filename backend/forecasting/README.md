# Backend: Forecasting

Predictive peak-load service for ECON. A PyTorch LSTM, served over FastAPI, forecasts the
building's upcoming peak **cooling load (MW)** from a short window of zone sensor readings plus
live outdoor weather.

## What it does
- **Input** (`POST /predict`): `{"sensor_sequence": [[room_temp, airflow], ...]}` — a time-ordered
  window the Go backend sends (each step is `[room_temp °C, airflow 0..1]`).
- The service appends live outdoor **temperature + humidity** (OpenWeather, cached 10 min, with a
  flagged fallback if the API is unreachable), scales the 4 features with the training-time scaler,
  and runs the LSTM.
- **Output**: `{predicted_peak_load, outdoor_temp_used, outdoor_humidity_used, weather_source}`
  where `weather_source ∈ {live, cache, fallback}`.
- `GET /health` → `{status, model_ready}`.

## Why it actually forecasts (not random)
The model is **trained** (`train.py`) and the weights + scaler are loaded at startup; if they're
missing, `/predict` returns **503** instead of serving random-init output. Training data is
synthesized to mirror the Go engine's real load physics
(`buildingLoad = coolingOutput/plantCop + base`, driven by outdoor heat, latent/humidity load,
airflow demand, setpoint overshoot, occupancy), so predictions are monotone in the physically
correct directions. Verified: cool/low-flow rooms → ~1.2 MW, hot/high-flow → ~2.3 MW, and load
range (~0.8–2.75 MW) matches the ECON testbed peak (~2.75 MW).

> Swap `train.py:synthesize()` for a real DB/feature pipeline once telemetry is persisted to
> TimescaleDB — the serving path (`main.py`) does not change.

## Run
```bash
# 1. (once) train -> writes model_weights.pth + scaler.pkl
python train.py

# 2. serve
uvicorn main:app --host 0.0.0.0 --port 8000

# 3. smoke + sanity test (server must be running)
python test_predict.py
```
Or in the stack: `docker compose up -d --build forecasting` from `econ/server/`
(the image trains at build time, so it ships ready). Provide weather via a `.env` next to the
compose file (`OPENWEATHER_API_KEY=...`); copy `.env.example` here for local dev.

## Files
| file | role |
|---|---|
| `config.py` | shared feature order, hyperparameters, artifact paths (train ⇄ serve contract) |
| `model.py` | `PeakLoadLSTM` (seq → 1 regressor) |
| `train.py` | synthesize physics-grounded data → fit scaler → train → save weights+scaler |
| `data_loader.py` | OpenWeather fetch with 10-min cache + fallback + source flag |
| `main.py` | FastAPI app: load artifacts, validate input, scale, predict |
| `test_predict.py` | health + monotonicity + input-validation checks |

## Go consumption (wired)
The Go engine exposes **`GET /api/forecast`** (`server/forecast.go`): it snapshots a live
telemetry window (building-average room temp + per-VAV airflow normalized to a 0..1 fraction of
nominal flow, via `Engine.ForecastWindow`), POSTs it to this service, and passes the JSON through
(503 if the service is down). `FORECAST_URL` (compose: `http://forecasting:8000`) points at it.
Next: have the dashboard/optimizer call `/api/forecast` and act on `predicted_peak_load`.

## Security
`.env` (real API key) is git-ignored and must never be committed; the Dockerfile uses runtime env
vars, not a baked-in key. If a key was ever pushed, rotate it.
