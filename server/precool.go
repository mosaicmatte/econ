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

// precoolTriggerMw is the FALLBACK LSTM-predicted peak load (MW) at which the poller opens
// a pre-cool window. It is used only until the engine's learned load baseline has matured;
// after that the trigger is data-driven (see precoolLoop). Tunable via PRECOOL_TRIGGER_MW.
func precoolTriggerMw() float64 {
	if s := os.Getenv("PRECOOL_TRIGGER_MW"); s != "" {
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			return v
		}
	}
	return 2.0
}

// precoolSigmaK is how far above the learned mean load (in σ, for the coming hour) a
// forecast peak must sit to be worth pre-cooling. 1.5σ ≈ the top ~7% of the building's own
// load distribution — "unusually high FOR THIS BUILDING", which is exactly the judgment a
// single hardcoded MW figure can't make across different buildings.
const precoolSigmaK = 1.5

// precoolLead looks slightly ahead of now when reading the learned baseline, so the
// trigger anticipates the hour the forecast peak lands in rather than the current one.
const precoolLead = 30 * time.Minute

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

		// Same request builder as /api/forecast: real sampled window + the engine's
		// live outdoor conditions, so auto-pre-cool decisions and the dashboard's
		// forecast card are always looking at the same prediction.
		req, _ := buildForecastRequest(engine)
		body, _ := json.Marshal(req)
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

		// Data-driven trigger: prefer the learned load baseline — pre-cool when the
		// forecast peak runs above what THIS building normally draws in the coming hour
		// (mean + kσ). That line adapts per building and per time of day, unlike the fixed
		// MW fallback used only until the model has matured.
		threshold, basis := trigger, "fixed"
		if learned, ok := engine.LoadForecastThreshold(precoolSigmaK, precoolLead); ok && learned > 0 {
			threshold, basis = learned, "learned"
		}
		if out.PredictedPeakLoad >= threshold {
			until := engine.StartPreCool(precoolWindow)
			lastAuto = time.Now()
			log.Printf("[precool] LSTM predicts %.2f MW peak (%s trigger %.2f): pre-cooling until %s",
				out.PredictedPeakLoad, basis, threshold, until.Format("15:04:05"))
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
			// Opening a pre-cool window drives the whole building's setpoints down for
			// hours and costs real energy. It is a control action, so it is guarded like
			// one — the same admin token the plug policy and the building deploy use.
			// It was the only unauthenticated write on the API.
			if !requireAdmin(w, r) {
				return
			}
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
