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

// precoolHorizonSteps is how far ahead the zero-shot forecaster is asked to look, in
// 5-minute engine steps — one hour, matching the window the pre-cool decision is about.
// Named separately from the LSTM's forecastWindowLen, which happens to be the same number
// but means the opposite thing: that one is an INPUT window length, this is an output
// horizon, and letting one stand in for the other is how they silently diverge.
const precoolHorizonSteps = 12

// precoolLoop closes the forecast→actuation loop: every 5 minutes it asks the forecasting
// layer for the coming peak and, when that peak crosses the trigger, opens a pre-cool
// window — the optimizer then drives occupied zones below setpoint so the thermal mass
// absorbs the peak. The forecaster being down or untrained just means no pre-cooling,
// never an error.
//
// WHICH forecaster is asked matters more than it looks. Until now this polled the
// supervised LSTM exclusively, which meant the only actuating consumer of a forecast in
// the entire system was wired to the one model that carries its training building inside
// its weights. Pointed at a building train.py has not seen, it answers with the old
// building's megawatts and the automation dutifully acts. The zero-shot foundation model
// has no such attachment — it reads THIS building's own recorded series and nothing else —
// so it is asked first whenever it has enough of that series to answer from.
func precoolLoop(engine *simulation.Engine) {
	client := &http.Client{Timeout: 8 * time.Second}
	// TimesFM may be downloading a checkpoint on its first call; the poller runs every
	// five minutes and nothing waits on it, so it can afford to be patient.
	tfmClient := &http.Client{Timeout: 180 * time.Second}
	base := os.Getenv("FORECAST_URL")
	if base == "" {
		base = "http://localhost:8000"
	}
	trigger := precoolTriggerMw()
	log.Printf("[precool] poller up: forecaster=%s trigger=%.2f MW window=%s", base, trigger, precoolWindow)

	var lastAuto time.Time
	// lastRefusal rate-limits the "refusing to act" log to once an hour: the condition
	// persists until someone retrains the model, and a line every poll would bury
	// everything else in the log rather than making the problem more visible.
	var lastRefusal time.Time
	// lastEngine tracks which forecaster last drove a decision, so a change of engine is
	// reported without logging the same line every five minutes.
	var lastEngine string
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if active, _ := engine.PreCoolStatus(); active {
			continue // a window is already open
		}
		if !lastAuto.IsZero() && time.Since(lastAuto) < precoolWindow+precoolCooldown {
			continue // hysteresis after the last auto-window
		}

		// Prefer the zero-shot model, fall back to the supervised one. Both go through
		// the SAME helpers the dashboard's /api/forecast/compare uses, so the poller and
		// the panels can never be looking at different numbers — which they could when
		// this loop issued its own hand-rolled POST.
		var (
			res        engineResult
			engineName string
		)
		// queryTimesFM applies the minimum-history rule itself and reports Available=false
		// with the reason when it has too little, so the condition is not restated here —
		// one threshold, one place.
		if r := queryTimesFM(engine, tfmClient, base, precoolHorizonSteps); r.Available && r.PeakMw != nil {
			res, engineName = r, "TimesFM (zero-shot)"
		}
		if engineName == "" {
			if r := queryLSTM(engine, client, base); r.Available && r.PeakMw != nil {
				res, engineName = r, "LSTM (supervised)"
			}
		}
		if engineName == "" {
			continue // neither engine answered: run without pre-cooling
		}
		predictedPeak := *res.PeakMw

		// Say WHICH engine is driving the decision, once, and again whenever it changes.
		// The loop is otherwise silent on success — it only logs when it opens a window —
		// so on a building whose forecast never crosses the trigger there was no way to
		// confirm from the outside which model the automation was actually consulting.
		// That is exactly the fact that turned out to matter most about this loop.
		if engineName != lastEngine {
			lastEngine = engineName
			log.Printf("[precool] consulting %s (%d real samples): %.3f MW predicted peak",
				engineName, res.RealSamples, predictedPeak)
		}

		// A forecast the twin cannot vouch for must not actuate the building.
		//
		// This is the sharpest edge in the whole forecasting path, and it applies to
		// WHICHEVER engine answered — preferring the zero-shot model makes the bad case
		// rarer, it does not make it impossible. The trigger below is data-driven: it
		// compares the predicted peak against what THIS building normally draws. But a
		// supervised model's weights encode whichever building train.py last saw, so
		// pointed at a different one it answers with the old building's numbers — 2.4 MW
		// for a house that has never drawn more than 0.03 MW. That clears a learned
		// threshold of ~0.01 MW on every single poll, so the building would sit in a
		// permanently re-opened pre-cool window, driving every setpoint down and burning
		// real energy, on the authority of a model that has never seen it. Reporting a bad
		// number on a dashboard is a display bug; actuating on one is not.
		lo, hi, n := engine.ObservedLoadRange()
		if n < minRangeSamples {
			// Not enough observation to judge the forecast yet. Pre-cooling is an
			// optimisation, not a safety function, so the safe default while the twin
			// cannot vouch for a number is to NOT drive the whole building's setpoints
			// down on it. Waiting a few minutes after boot costs nothing; acting on an
			// unvouchable forecast costs energy, and it is precisely the window in which
			// a model trained on another building gets its way unchallenged.
			continue
		}
		checkPlausible(&res, lo, hi, n)
		if res.Implausible {
			if time.Since(lastRefusal) > time.Hour {
				lastRefusal = time.Now()
				log.Printf("[precool] REFUSING to act on the %s forecast: %s "+
					"Pre-cooling stays closed until the model is retrained on this building "+
					"(backend/forecasting/train.py) or the forecast returns to a plausible range.",
					engineName, res.Plausibility)
			}
			continue
		}

		// Data-driven trigger: prefer the learned load baseline — pre-cool when the
		// forecast peak runs above what THIS building normally draws in the coming hour
		// (mean + kσ). That line adapts per building and per time of day, unlike the fixed
		// MW fallback used only until the model has matured.
		threshold, basis := trigger, "fixed"
		if learned, ok := engine.LoadForecastThreshold(precoolSigmaK, precoolLead); ok && learned > 0 {
			threshold, basis = learned, "learned"
		}
		// The trigger compares CENTRAL estimate against threshold, deliberately.
		//
		// The learned threshold is already mean + k·sigma of this building's own load, so it
		// encodes the risk appetite once. Comparing an upper-decile forecast against it
		// would count the spread twice and pre-cool on the tail of a tail. The upper band
		// is logged instead, because "0.012 MW central, 0.014 MW at the 90th" is what tells
		// an operator how firm the decision was — a number no consumer of this system has
		// ever been shown.
		if predictedPeak >= threshold {
			until := engine.StartPreCool(precoolWindow)
			lastAuto = time.Now()
			band := ""
			if res.PeakUpperMw != nil {
				band = fmt.Sprintf(" [%s %.3f MW]", res.UpperQuantile, *res.PeakUpperMw)
			}
			log.Printf("[precool] %s predicts %.3f MW peak%s (%s trigger %.3f): pre-cooling until %s",
				engineName, predictedPeak, band, basis, threshold, until.Format("15:04:05"))
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
