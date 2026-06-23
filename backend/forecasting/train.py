"""Train PeakLoadLSTM on synthetic data grounded in the ECON load physics.

There is no historical telemetry yet (TimescaleDB is provisioned but unused), so we synthesize
training data whose target mirrors how the Go engine actually forms building load:

    buildingLoad = coolingOutput / plantCop + base
    coolingOutput rises with outdoor heat ingress, latent (humidity) load, airflow demand,
    and how far rooms sit above setpoint; plantCop degrades as the plant works harder.

The point is a model that is (a) trained, not random, and (b) monotonic in the physically correct
directions, so it returns sensible peak-load forecasts. Swap `synthesize()` for a real
DB/feature pipeline once telemetry is being persisted — the serving path won't change.

Run:  python train.py            # writes model_weights.pth + scaler.pkl
"""
import numpy as np
import torch
import torch.nn as nn
import joblib
from sklearn.preprocessing import StandardScaler

from config import (FEATURES, INPUT_SIZE, HIDDEN_SIZE, NUM_LAYERS, OUTPUT_SIZE,
                    SEQ_LEN, WEIGHTS_PATH, SCALER_PATH)
from model import PeakLoadLSTM

RNG = np.random.default_rng(42)


def _peak_load_mw(room_temp, airflow, outdoor_temp, outdoor_hum, occ):
    """Physically-sensible target. Mirrors the Go engine's drivers (see module docstring).
    Monotone increasing in outdoor temp, humidity, airflow demand, setpoint overshoot, occupancy."""
    setpoint = 22.0
    cooling_thermal = (
        0.085 * max(0.0, outdoor_temp - 18.0)      # envelope/solar ingress
        + 0.018 * max(0.0, outdoor_hum - 45.0)     # latent (dehumidification) load
        + 1.7 * airflow                            # delivered airflow demand (airflow ~0..1)
        + 0.25 * max(0.0, room_temp - setpoint)    # pulling an overshooting room back down
        + 1.4 * occ                                # occupant + equipment heat (occ ~0..1)
    )
    # plant COP degrades as it strains (matches engine's clamp(3.6-0.35*strain, 2.2, 3.8))
    strain = min(1.5, 0.04 * max(0.0, outdoor_temp - 24.0) + 0.6 * airflow)
    cop = float(np.clip(3.6 - 0.35 * strain, 2.2, 3.8))
    base_electrical = 0.6                           # lighting/plug loads floor
    return cooling_thermal / cop + base_electrical


def synthesize(n=6000, seq_len=SEQ_LEN):
    """Generate (X, y): X is (n, seq_len, INPUT_SIZE), y is (n,) peak load in MW."""
    X = np.zeros((n, seq_len, INPUT_SIZE), dtype=np.float32)
    y = np.zeros((n,), dtype=np.float32)
    for i in range(n):
        outdoor_temp = RNG.uniform(20, 41)         # tropical climate (HCMC-ish)
        outdoor_hum = RNG.uniform(45, 95)
        occ = RNG.uniform(0.0, 1.0)                # normalized occupancy/equipment factor
        base_room = RNG.uniform(21.5, 26.5)
        base_flow = np.clip(0.35 + 0.5 * occ + 0.02 * (outdoor_temp - 25), 0.1, 1.0)
        for t in range(seq_len):
            room_temp = base_room + RNG.normal(0, 0.25)
            airflow = float(np.clip(base_flow + RNG.normal(0, 0.04), 0.05, 1.0))
            X[i, t] = [room_temp, airflow, outdoor_temp + RNG.normal(0, 0.2),
                       outdoor_hum + RNG.normal(0, 1.0)]
        # target uses the sequence-mean conditions + small observation noise
        m = X[i].mean(axis=0)
        y[i] = _peak_load_mw(m[0], m[1], m[2], m[3], occ) + RNG.normal(0, 0.05)
    return X, np.clip(y, 0.1, None)


def main():
    print("[train] synthesizing data ...")
    X, y = synthesize()

    # Fit scaler on flattened timesteps so each feature is standardized consistently.
    scaler = StandardScaler().fit(X.reshape(-1, INPUT_SIZE))
    Xs = scaler.transform(X.reshape(-1, INPUT_SIZE)).reshape(X.shape).astype(np.float32)

    n_val = int(0.15 * len(X))
    Xtr, ytr = Xs[n_val:], y[n_val:]
    Xva, yva = Xs[:n_val], y[:n_val]

    device = "mps" if torch.backends.mps.is_available() else ("cuda" if torch.cuda.is_available() else "cpu")
    model = PeakLoadLSTM(INPUT_SIZE, HIDDEN_SIZE, NUM_LAYERS, OUTPUT_SIZE).to(device)
    opt = torch.optim.Adam(model.parameters(), lr=1e-3)
    loss_fn = nn.MSELoss()

    Xtr_t = torch.tensor(Xtr, device=device)
    ytr_t = torch.tensor(ytr, device=device).unsqueeze(1)
    Xva_t = torch.tensor(Xva, device=device)
    yva_t = torch.tensor(yva, device=device).unsqueeze(1)

    print(f"[train] device={device}  train={len(Xtr)}  val={len(Xva)}")
    batch = 256
    for epoch in range(60):
        model.train()
        perm = torch.randperm(len(Xtr_t), device=device)
        for b in range(0, len(perm), batch):
            idx = perm[b:b + batch]
            opt.zero_grad()
            loss = loss_fn(model(Xtr_t[idx]), ytr_t[idx])
            loss.backward()
            opt.step()
        if (epoch + 1) % 10 == 0 or epoch == 0:
            model.eval()
            with torch.no_grad():
                vpred = model(Xva_t)
                vloss = loss_fn(vpred, yva_t).item()
                mae = (vpred - yva_t).abs().mean().item()
            print(f"  epoch {epoch+1:2d}  val_mse={vloss:.4f}  val_mae={mae:.4f} MW")

    torch.save(model.state_dict(), WEIGHTS_PATH)
    joblib.dump(scaler, SCALER_PATH)
    print(f"[train] saved weights -> {WEIGHTS_PATH}")
    print(f"[train] saved scaler  -> {SCALER_PATH}")
    print(f"[train] target load range: {y.min():.2f}..{y.max():.2f} MW  (mean {y.mean():.2f})")


if __name__ == "__main__":
    main()
