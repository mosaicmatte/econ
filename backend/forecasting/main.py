import base64
import os

import numpy as np
import torch
import joblib
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, field_validator

from config import (INPUT_SIZE, HIDDEN_SIZE, NUM_LAYERS, OUTPUT_SIZE, SEQ_LEN,
                    SENSOR_FEATURES, WEIGHTS_PATH, SCALER_PATH)
from data_loader import fetch_weather_features
from model import PeakLoadLSTM
from timesfm_forecaster import TIMESFM

app = FastAPI(title="EcoSync Forecasting API", version="1.0.0")

# ---------------------------------------------------------------------------
# Load the trained model + scaler once at startup. If artifacts are missing, we
# refuse to serve random predictions — /predict returns 503 until `train.py` has run.
# ---------------------------------------------------------------------------
model = PeakLoadLSTM(INPUT_SIZE, HIDDEN_SIZE, NUM_LAYERS, OUTPUT_SIZE)
scaler = None
MODEL_READY = False

if os.path.exists(WEIGHTS_PATH) and os.path.exists(SCALER_PATH):
    model.load_state_dict(torch.load(WEIGHTS_PATH, map_location="cpu"))
    model.eval()
    scaler = joblib.load(SCALER_PATH)
    MODEL_READY = True
    print(f"[startup] loaded trained model + scaler")
else:
    print(f"[startup] WARNING: {WEIGHTS_PATH} / {SCALER_PATH} not found — run train.py. "
          f"/predict will return 503.")


class ForecastRequest(BaseModel):
    # Go backend sends a sequence of sensor readings: [[room_temp, airflow], ...]
    sensor_sequence: list[list[float]]
    # Optional weather handover: the Go engine's own live Open-Meteo readings — the same
    # numbers its 2R1C envelope integrates against. When provided (and plausible), they
    # are used directly so the forecaster and the physics never disagree about the
    # weather; when absent, this service falls back to its own fetch and labels it.
    outdoor_temp: float | None = None
    outdoor_humidity: float | None = None

    @field_validator("sensor_sequence")
    @classmethod
    def _check(cls, v):
        if not v:
            raise ValueError("sensor_sequence must be non-empty")
        for i, row in enumerate(v):
            if len(row) != SENSOR_FEATURES:
                raise ValueError(
                    f"each timestep must have exactly {SENSOR_FEATURES} features "
                    f"[room_temp, airflow]; row {i} has {len(row)}")
        return v


class ForecastResponse(BaseModel):
    predicted_peak_load: float
    outdoor_temp_used: float
    outdoor_humidity_used: float
    weather_source: str  # 'engine' | 'live' | 'cache' | 'fallback'


@app.get("/health")
def health():
    return {"status": "ok", "model_ready": MODEL_READY}


@app.get("/model/info")
def model_info():
    """Which forecasting engines this service can actually serve, and why not when it
    cannot. The two are complementary rather than redundant: the LSTM is supervised and
    specialises on THIS building once train.py has real history to learn from; TimesFM is a
    pretrained foundation model that forecasts a series it has never seen, so it covers the
    cold start where the LSTM has nothing to offer."""
    return {
        "lstm": {
            "available": MODEL_READY,
            "trained_on": "this building's own history (supervised)",
            "reason": None if MODEL_READY else "not trained yet — run train.py",
        },
        "timesfm": TIMESFM.info(),
    }


class LoadForecastRequest(BaseModel):
    # The building's own recent load in MW, oldest first. TimesFM is univariate and
    # zero-shot: no scaler, no training, no feature engineering — just the series.
    history: list[float]
    horizon: int = 12
    context_len: int | None = None

    @field_validator("history")
    @classmethod
    def _check_history(cls, v):
        if len(v) < 8:
            raise ValueError("history needs at least 8 points to forecast from")
        for x in v:
            if x != x or x in (float("inf"), float("-inf")):
                raise ValueError("history contains a non-finite value")
        return v

    @field_validator("horizon")
    @classmethod
    def _check_horizon(cls, v):
        if not 1 <= v <= 256:
            raise ValueError("horizon must be between 1 and 256 steps")
        return v


@app.post("/forecast/load")
def forecast_load(request: LoadForecastRequest):
    """Zero-shot building-load forecast via Google TimesFM.

    Unlike /predict this needs no trained artifacts at all, which is the entire point: it
    works on a twin's first day, from nothing but the load history the engine has already
    persisted. 503 (not a fabricated number) when TimesFM is unavailable."""
    try:
        return TIMESFM.forecast(request.history, request.horizon, request.context_len)
    except RuntimeError as e:
        raise HTTPException(status_code=503, detail=str(e))
    except ValueError as e:
        raise HTTPException(status_code=422, detail=str(e))
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"TimesFM inference error: {e}")


@app.get("/model/artifacts")
def model_artifacts():
    """Export the trained LSTM artifacts (base64) so the Go server can bundle them into the
    downloadable local-model pack. Serving-side only — the weights and the fitted scaler,
    no secrets. Returns null fields (not an error) when the model hasn't been trained yet,
    so the bundle degrades to the baseline model + recommender."""
    out = {
        "model_ready": MODEL_READY,
        "config": {
            "input_size": INPUT_SIZE,
            "hidden_size": HIDDEN_SIZE,
            "num_layers": NUM_LAYERS,
            "output_size": OUTPUT_SIZE,
            "seq_len": SEQ_LEN,
            "sensor_features": SENSOR_FEATURES,
            "feature_order": ["room_temp", "airflow", "outdoor_temp", "outdoor_humidity"],
        },
    }
    for field, path in (("weights_b64", WEIGHTS_PATH), ("scaler_b64", SCALER_PATH)):
        try:
            with open(path, "rb") as f:
                out[field] = base64.b64encode(f.read()).decode("ascii")
        except Exception:
            out[field] = None
    return out


@app.post("/predict", response_model=ForecastResponse)
def predict_peak_load(request: ForecastRequest):
    if not MODEL_READY:
        raise HTTPException(status_code=503,
                            detail="Model not trained: run train.py to produce weights + scaler.")
    try:
        # Prefer the engine's weather handover (one weather truth across services);
        # plausibility-gated so a corrupt value degrades to the local fetch, never
        # into the model.
        if (request.outdoor_temp is not None and request.outdoor_humidity is not None
                and -40 <= request.outdoor_temp <= 55
                and 0 < request.outdoor_humidity <= 100):
            outdoor_temp = request.outdoor_temp
            outdoor_humidity = request.outdoor_humidity
            source = "engine"
        else:
            outdoor_temp, outdoor_humidity, source = fetch_weather_features()

        # Combine per-timestep sensor data with the (shared) weather features.
        combined = [row + [outdoor_temp, outdoor_humidity] for row in request.sensor_sequence]
        data = np.array(combined, dtype=np.float32)

        # Apply the SAME scaler used at training time, then add the batch dimension.
        data = scaler.transform(data).astype(np.float32)
        tensor_input = torch.tensor(data).unsqueeze(0)  # (1, seq_len, INPUT_SIZE)

        with torch.no_grad():
            predicted_load = float(model(tensor_input).item())

        return ForecastResponse(
            predicted_peak_load=predicted_load,
            outdoor_temp_used=outdoor_temp,
            outdoor_humidity_used=outdoor_humidity,
            weather_source=source,
        )
    except Exception as e:
        raise HTTPException(status_code=500, detail=f"Internal prediction error: {str(e)}")
