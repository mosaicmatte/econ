package main

import (
	"bytes"
	"econ/simulation"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	// precoolWindow is how long one pre-cool window runs (both auto and manual).
	precoolWindow = 20 * time.Minute
	// precoolCooldown keeps the auto-trigger from chattering when the forecast sits
	// persistently above the threshold: after a window closes, the poller waits this
	// long before it may open another one. Manual triggers are never throttled.
	precoolCooldown = 30 * time.Minute
)

// precoolTriggerMw is the LSTM-predicted peak load (MW) at which the poller opens a
// pre-cool window automatically. Tunable per deployment via PRECOOL_TRIGGER_MW.
func precoolTriggerMw() float64 {
	if s := os.Getenv("PRECOOL_TRIGGER_MW"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			return v
		}
	}
	return 2.0
}

// precoolLoop closes the forecast→actuation loop: every 5 minutes it feeds the live
// telemetry window to the Python LSTM (same contract as /api/forecast) and, when the
// predicted peak crosses the trigger, opens a pre-cool window — the optimizer then
// drives occupied zones below setpoint so the thermal mass absorbs the coming peak.
// The forecaster being down or untrained just means no pre-cooling, never an error.
func precoolLoop(engine *simulation.Engine) {
	client := &http.Client{Timeout: 8 * time.Second}
	base := os.Getenv("FORECAST_URL")
	if base == "" {
		base = "http://localhost:8000"
	}
	trigger := precoolTriggerMw()
	log.Printf("[precool] poller up: forecaster=%s trigger=%.2f MW window=%s", base, trigger, precoolWindow)

	var lastAuto time.Time
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if active, _ := engine.PreCoolStatus(); active {
			continue // a window is already open
		}
		if !lastAuto.IsZero() && time.Since(lastAuto) < precoolWindow+precoolCooldown {
			continue // hysteresis after the last auto-window
		}

		body, _ := json.Marshal(forecastRequest{SensorSequence: engine.ForecastWindow(12)})
		resp, err := client.Post(base+"/predict", "application/json", bytes.NewReader(body))
		if err != nil {
			continue // forecaster unreachable: run without pre-cooling
		}
		var out struct {
			PredictedPeakLoad float64 `json:"predicted_peak_load"`
		}
		decodeErr := json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if decodeErr != nil || resp.StatusCode != http.StatusOK {
			continue // 503 = model not trained yet
		}

		if out.PredictedPeakLoad >= trigger {
			until := engine.StartPreCool(precoolWindow)
			lastAuto = time.Now()
			log.Printf("[precool] LSTM predicts %.2f MW peak (trigger %.2f): pre-cooling until %s",
				out.PredictedPeakLoad, trigger, until.Format("15:04:05"))
		}
	}
}

// precoolHandler exposes the window over HTTP: GET reports {active, until}; POST opens
// a window (optional ?minutes=, capped at 4 h). The dashboard's "peak-shaving" action
// uses the websocket path instead, but this gives scripts/tests a direct hook.
func precoolHandler(engine *simulation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost {
			d := precoolWindow
			if m := r.URL.Query().Get("minutes"); m != "" {
				if v, err := strconv.Atoi(m); err == nil && v > 0 && v <= 240 {
					d = time.Duration(v) * time.Minute
				}
			}
			until := engine.StartPreCool(d)
			log.Printf("[precool] manual window opened until %s", until.Format("15:04:05"))
			json.NewEncoder(w).Encode(map[string]interface{}{"active": true, "until": until})
			return
		}

		active, until := engine.PreCoolStatus()
		json.NewEncoder(w).Encode(map[string]interface{}{"active": active, "until": until})
	}
}
