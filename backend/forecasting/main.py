import os

import numpy as np
import torch
import joblib
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, field_validator

from config import (INPUT_SIZE, HIDDEN_SIZE, NUM_LAYERS, OUTPUT_SIZE,
                    SENSOR_FEATURES, WEIGHTS_PATH, SCALER_PATH)
from data_loader import fetch_weather_features
from model import PeakLoadLSTM

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
