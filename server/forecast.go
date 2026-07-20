package main

import (
	"bytes"
	"econ/simulation"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// forecastRequest mirrors the Python service's POST /predict body. The outdoor fields
// are the engine's own live Open-Meteo readings — handed over so the forecaster and the
// envelope physics never disagree about the weather; omitted (nil) when the feed is
// stale, in which case the Python service falls back to its own fetch and says so.
type forecastRequest struct {
	SensorSequence  [][]float64 `json:"sensor_sequence"`
	OutdoorTemp     *float64    `json:"outdoor_temp,omitempty"`
	OutdoorHumidity *float64    `json:"outdoor_humidity,omitempty"`
}

// forecastWindowLen must match the Python model's SEQ_LEN (12 steps of 5 minutes).
const forecastWindowLen = 12

// buildForecastRequest assembles the /predict body from live engine state. Shared by
// the HTTP proxy below and the pre-cool poller, so every forecast in the system is
// made from the same real inputs.
func buildForecastRequest(engine *simulation.Engine) (forecastRequest, int) {
	window, realSamples := engine.ForecastWindow(forecastWindowLen)
	req := forecastRequest{SensorSequence: window}
	if t, h, live := engine.OutdoorForForecast(); live {
		req.OutdoorTemp = &t
		req.OutdoorHumidity = &h
	}
	return req, realSamples
}

// forecastHandler proxies a live-telemetry window to the Python LSTM forecaster and returns its
// JSON ({predicted_peak_load, outdoor_temp_used, outdoor_humidity_used, weather_source}),
// annotated with the window's provenance (window_real_samples / window_len) so the AI layer
// can say "warming up: 3/12 real samples" instead of presenting padding as history. This is
// the human-in-the-loop hook for the dashboard/optimizer: the Go engine owns the building state,
// the Python service owns the model. FORECAST_URL points at the service (compose: forecasting:8000).
func forecastHandler(engine *simulation.Engine) http.HandlerFunc {
	client := &http.Client{Timeout: 8 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		base := os.Getenv("FORECAST_URL")
		if base == "" {
			base = "http://localhost:8000"
		}

		req, realSamples := buildForecastRequest(engine)
		body, _ := json.Marshal(req)
		resp, err := client.Post(base+"/predict", "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("[forecast] service unreachable at %s: %v", base, err)
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "forecasting service unreachable: " + err.Error()})
			return
		}
		defer resp.Body.Close()

		// Annotate a successful prediction with the input window's provenance. Anything
		// that doesn't decode as a JSON object (error bodies, 503s) passes through as-is.
		if resp.StatusCode == http.StatusOK {
			var out map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&out) == nil {
				out["window_real_samples"] = realSamples
				out["window_len"] = forecastWindowLen
				json.NewEncoder(w).Encode(out)
				return
			}
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "forecaster returned malformed JSON"})
			return
		}
		w.WriteHeader(resp.StatusCode) // pass through 503 (model not trained) etc.
		io.Copy(w, resp.Body)
	}
}
