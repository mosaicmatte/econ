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

// forecastRequest mirrors the Python service's POST /predict body.
type forecastRequest struct {
	SensorSequence [][]float64 `json:"sensor_sequence"`
}

// forecastHandler proxies a live-telemetry window to the Python LSTM forecaster and returns its
// JSON ({predicted_peak_load, outdoor_temp_used, outdoor_humidity_used, weather_source}). This is
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

		body, _ := json.Marshal(forecastRequest{SensorSequence: engine.ForecastWindow(12)})
		resp, err := client.Post(base+"/predict", "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("[forecast] service unreachable at %s: %v", base, err)
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "forecasting service unreachable: " + err.Error()})
			return
		}
		defer resp.Body.Close()

		w.WriteHeader(resp.StatusCode) // pass through 200 / 503 (model not trained) etc.
		io.Copy(w, resp.Body)
	}
}
