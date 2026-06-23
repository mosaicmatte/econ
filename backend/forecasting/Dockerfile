FROM python:3.10-slim

WORKDIR /app

COPY requirements.txt .
# CPU-only torch keeps the image lean (no multi-GB CUDA wheel); LSTM inference needs no GPU.
RUN pip install --no-cache-dir torch --index-url https://download.pytorch.org/whl/cpu \
 && pip install --no-cache-dir -r requirements.txt

COPY . .

# Train at build time so the image ships with weights + scaler (no random-init predictions,
# no model artifacts committed to git). Swap for a `model_weights.pth` COPY once a real
# trained model is produced from persisted telemetry.
RUN python train.py

EXPOSE 8000
CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
