package main

import (
	"econ/simulation"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// weatherPoll is how often the poller asks Open-Meteo for current conditions. The API
// refreshes its "current" block sub-hourly, so 10 minutes tracks it without hammering.
const weatherPoll = 10 * time.Minute

// weatherCoord reads one coordinate from the environment, falling back to the demo
// building's site (Ho Chi Minh City) — the same coordinates the dashboard's sky and the
// LSTM's forecast already use, so every consumer of weather agrees on where it is.
func weatherCoord(env string, fallback float64) float64 {
	if s := os.Getenv(env); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil {
			return v
		}
	}
	return fallback
}

// weatherLoop feeds live outdoor temperature into the 2R1C envelope. Until it succeeds
// once — and again if the feed goes stale — the engine integrates against its
// climatological fallback, so a dead network degrades to exactly the behaviour the
// engine shipped with, never to a frozen or invented reading.
func weatherLoop(engine *simulation.Engine) {
	lat := weatherCoord("WEATHER_LAT", 10.8231)
	lon := weatherCoord("WEATHER_LON", 106.6297)
	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m",
		lat, lon)
	client := &http.Client{Timeout: 8 * time.Second}
	log.Printf("[weather] poller up: lat=%.4f lon=%.4f every %s", lat, lon, weatherPoll)

	fetch := func() {
		resp, err := client.Get(url)
		if err != nil {
			log.Printf("[weather] fetch failed (%v); envelope stays on last/fallback value", err)
			return
		}
		defer resp.Body.Close()
		var payload struct {
			Current struct {
				Temperature float64 `json:"temperature_2m"`
			} `json:"current"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			log.Printf("[weather] bad response (%v); ignored", err)
			return
		}
		t := payload.Current.Temperature
		// Plausibility gate, same spirit as the edge firmware's range checks: a zero
		// from a broken decode must not become "0 °C in Saigon".
		if t < -40 || t > 55 {
			log.Printf("[weather] implausible %.1f C; ignored", t)
			return
		}
		engine.SetOutdoorTemp(t)
		log.Printf("[weather] outdoor %.1f C -> envelope", t)
	}

	fetch() // once at boot, then on the ticker
	ticker := time.NewTicker(weatherPoll)
	defer ticker.Stop()
	for range ticker.C {
		fetch()
	}
}

// weatherHandler reports what the physics is integrating against right now.
func weatherHandler(engine *simulation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		t, live, age := engine.OutdoorStatus()
		json.NewEncoder(w).Encode(map[string]interface{}{
			"outdoorC": t,
			"live":     live,   // false = climatological fallback in use
			"ageSec":   age,    // -1 = never fetched
			"source":   "open-meteo",
		})
	}
}
