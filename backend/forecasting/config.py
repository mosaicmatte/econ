"""Shared contract between training (train.py) and serving (main.py).

Keeping the feature order, model hyperparameters, and artifact paths in one place guarantees the
LSTM is built identically at train time and inference time, and that the same scaler is reused.
"""
import os

# One timestep = [room_temp (°C), airflow (0..1), outdoor_temp (°C), outdoor_humidity (%)].
# The Go backend sends the first two per timestep; data_loader appends the two weather features.
FEATURES = ["room_temp", "airflow", "outdoor_temp", "outdoor_humidity"]
INPUT_SIZE = len(FEATURES)
SENSOR_FEATURES = 2  # how many features per timestep the client (Go) is expected to send

HIDDEN_SIZE = 64
NUM_LAYERS = 2
OUTPUT_SIZE = 1  # predicted peak load (MW)

SEQ_LEN = 12  # timesteps the model expects to reason over (e.g. last hour @ 5-min cadence)

_HERE = os.path.dirname(os.path.abspath(__file__))
WEIGHTS_PATH = os.path.join(_HERE, "model_weights.pth")
SCALER_PATH = os.path.join(_HERE, "scaler.pkl")
