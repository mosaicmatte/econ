import os
import time

import requests
from dotenv import load_dotenv

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
