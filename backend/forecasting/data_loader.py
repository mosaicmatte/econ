import os
import time
from collections import OrderedDict
from datetime import timedelta

import requests
from dotenv import load_dotenv

from config import SEQ_LEN

load_dotenv()

API_KEY = os.getenv("OPENWEATHER_API_KEY")
LAT = os.getenv("WEATHER_LAT", "10.7724")
LON = os.getenv("WEATHER_LON", "106.6581")

# Outdoor conditions barely change minute-to-minute, so cache them and avoid hammering the
# OpenWeather free tier (~60 req/min) on every /predict.
_CACHE_TTL_S = 600  # 10 minutes
_FALLBACK = (30.0, 70.0)  # sane tropical default if the API is unreachable
_cache = {"ts": 0.0, "temp": None, "hum": None, "source": None}


def fetch_weather_features():
    """Return (outdoor_temp_c, outdoor_humidity_pct, source) where source is
    'live' | 'cache' | 'fallback' so callers can tell a real reading from a guess."""
    now = time.time()
    if _cache["temp"] is not None and now - _cache["ts"] < _CACHE_TTL_S:
        return _cache["temp"], _cache["hum"], "cache"

    if not API_KEY:
        # No key configured: don't crash the service, but be explicit it's a fallback.
        return _FALLBACK[0], _FALLBACK[1], "fallback"

    url = (f"https://api.openweathermap.org/data/2.5/weather?"
           f"lat={LAT}&lon={LON}&appid={API_KEY}&units=metric")
    try:
        resp = requests.get(url, timeout=5)
        resp.raise_for_status()
        data = resp.json()
        temp, hum = float(data["main"]["temp"]), float(data["main"]["humidity"])
        _cache.update(ts=now, temp=temp, hum=hum, source="live")
        return temp, hum, "live"
    except Exception as e:
        print(f"[weather] fetch failed ({e}); using fallback {_FALLBACK}")
        return _FALLBACK[0], _FALLBACK[1], "fallback"


# ---------------------------------------------------------------------------
# Real-data training path.
#
# The Go engine persists, once a second to TimescaleDB, the exact building-average
# [avgTemp, avgAirflow] the live forecaster consumes, the outdoor conditions the envelope
# integrates against, and the target (buildingLoadMw). That means the LSTM can be retrained
# on the building's OWN accumulated history instead of only synthetic data — same serving
# contract, real inputs. This loader assembles those series into (X, y) sequences and
# returns None (so train.py falls back to synthetic) whenever the DB is unreachable or
# there isn't enough contiguous history yet.
# ---------------------------------------------------------------------------

def _db_url():
    # Same DSN the Go server uses; override with DB_URL for a remote warehouse.
    return os.getenv("DB_URL", "postgres://econ:econ@localhost:5432/econ?sslmode=disable")


# Feature order MUST match config.FEATURES = [room_temp, airflow, outdoor_temp, outdoor_humidity].
_TRAIN_METRICS = ["avgTemp", "avgAirflow", "outdoorTemp", "outdoorHum", "buildingLoadMw"]


def load_training_sequences(days=14, seq_len=SEQ_LEN, min_windows=200):
    """Build real (X, y) sequences from the twin's persisted history in TimescaleDB.

    X[t] = [room_temp, airflow, outdoor_temp, outdoor_humidity] per 5-minute bucket;
    y     = the PEAK building load over each seq_len window (a genuine conditions->peak
            regression — no load feature ever enters X, so there is no leakage).

    Returns (X, y) as float32 arrays, or None when psycopg2 is missing, the DB is
    unreachable, or fewer than `min_windows` contiguous windows exist — the signal for
    train.py to fall back to physics-grounded synthetic data.
    """
    try:
        import psycopg2
    except Exception:
        print("[data] psycopg2 not installed; skipping real-data path")
        return None
    try:
        conn = psycopg2.connect(_db_url())
    except Exception as e:
        print(f"[data] TimescaleDB unreachable ({e}); skipping real-data path")
        return None

    try:
        with conn.cursor() as cur:
            cur.execute(
                """
                SELECT bucket, sensor_type, avg_value
                FROM sensor_readings_5m
                WHERE zone_id = 'GLOBAL' AND sensor_type = ANY(%s)
                  AND bucket > now() - make_interval(days => %s)
                ORDER BY bucket ASC
                """,
                (_TRAIN_METRICS, days),
            )
            rows = cur.fetchall()
    except Exception as e:
        print(f"[data] query failed ({e}); skipping real-data path")
        return None
    finally:
        conn.close()

    # Pivot into bucket -> {metric: value}, preserving ascending time order.
    table = OrderedDict()
    for bucket, stype, val in rows:
        table.setdefault(bucket, {})[stype] = val
    if len(table) < seq_len + 1:
        print(f"[data] only {len(table)} 5-min buckets of history; need >= {seq_len + 1}")
        return None

    # Build per-bucket feature rows. Outdoor is persisted only while the weather feed was
    # live, so forward-fill it (and seed with the tropical fallback the serving path uses)
    # rather than dropping otherwise-complete buckets.
    import numpy as np

    feats = []  # (bucket, [temp, airflow, out_t, out_h], load)
    lt, lh = _FALLBACK
    for b, row in table.items():
        if "outdoorTemp" in row and row["outdoorTemp"] is not None:
            lt = row["outdoorTemp"]
        if "outdoorHum" in row and row["outdoorHum"] is not None:
            lh = row["outdoorHum"]
        if any(row.get(k) is None for k in ("avgTemp", "avgAirflow", "buildingLoadMw")):
            continue
        feats.append((b, [row["avgTemp"], row["avgAirflow"], lt, lh], row["buildingLoadMw"]))

    # Slide a seq_len window, but only over runs of buckets exactly 5 minutes apart: a
    # window straddling a gap (server was down) is not a real contiguous hour.
    step = timedelta(minutes=5)
    X, y = [], []
    i, n = 0, len(feats)
    while i + seq_len <= n:
        contiguous = all(feats[i + k + 1][0] - feats[i + k][0] == step for k in range(seq_len - 1))
        if not contiguous:
            i += 1
            continue
        X.append([feats[i + k][1] for k in range(seq_len)])
        y.append(max(feats[i + k][2] for k in range(seq_len)))
        i += 1

    if len(X) < min_windows:
        print(f"[data] only {len(X)} contiguous windows; need >= {min_windows}. Falling back to synthetic.")
        return None

    print(f"[data] built {len(X)} real training windows from TimescaleDB ({days}d history)")
    return np.array(X, dtype=np.float32), np.array(y, dtype=np.float32)
