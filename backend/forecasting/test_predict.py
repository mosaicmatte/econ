"""Smoke + sanity test against a running forecasting service (uvicorn main:app).

Checks: (1) /health, (2) /predict returns a load, (3) the trained model is sensible —
hotter rooms (more cooling needed) should not predict LESS load than cool rooms.
"""
import requests

BASE = "http://127.0.0.1:8000"


def main():
    h = requests.get(f"{BASE}/health", timeout=5).json()
    print("health:", h)

    cool = {"sensor_sequence": [[22.0, 0.4]] * 12}
    hot = {"sensor_sequence": [[28.0, 0.9]] * 12}

    r_cool = requests.post(f"{BASE}/predict", json=cool, timeout=10).json()
    r_hot = requests.post(f"{BASE}/predict", json=hot, timeout=10).json()
    print("cool rooms ->", r_cool)
    print("hot  rooms ->", r_hot)

    assert r_hot["predicted_peak_load"] >= r_cool["predicted_peak_load"], \
        "model not monotone: hotter/higher-airflow should not forecast lower load"
    print("OK: prediction rises with cooling demand")

    # validation: wrong feature count should be rejected (422)
    bad = requests.post(f"{BASE}/predict", json={"sensor_sequence": [[1, 2, 3]]}, timeout=5)
    print("bad-input status:", bad.status_code, "(expect 422)")


if __name__ == "__main__":
    main()
