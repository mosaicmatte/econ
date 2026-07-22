package main

import (
	"econ/simulation"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// Learned recommendations over HTTP.
//
//	GET /api/recommendations — the ranked, learned-anomaly report the dashboard renders.
//
// This is the read surface for the intelligence layer that replaced the hardcoded
// threshold cards: each recommendation is scored against the online baseline model
// (simulation/baselines.go), so it reflects what a zone actually does at this hour, not a
// number typed into the UI. The model itself is persisted like the plug savings counter —
// a twin that forgot its learned normal on every redeploy would be re-learning forever.

const baselineStatePath = "./data/baseline-model.json"

// loadBaselineState restores the learned model at boot; an absent file just means the
// model starts empty and warms up (which the report reports honestly as "learning").
func loadBaselineState(engine *simulation.Engine) {
	data, err := os.ReadFile(baselineStatePath)
	if err != nil {
		return
	}
	if err := engine.LoadBaselines(data); err != nil {
		log.Printf("[recommend] baseline state unreadable, starting fresh: %v", err)
		return
	}
	est, learning := engine.BaselineCoverage()
	log.Printf("[recommend] restored learned baselines: %d established, %d still learning", est, learning)
}

func saveBaselineState(engine *simulation.Engine) {
	data, err := engine.MarshalBaselines()
	if err != nil {
		log.Printf("[recommend] baseline marshal: %v", err)
		return
	}
	if err := os.WriteFile(baselineStatePath, data, 0644); err != nil {
		log.Printf("[recommend] baseline state save: %v", err)
	}
}

// baselinePersistLoop checkpoints the learned model once a minute — cheap (the snapshot
// drops immature buckets) and bounds what a crash can lose to a minute of learning.
func baselinePersistLoop(engine *simulation.Engine) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		saveBaselineState(engine)
	}
}

func recommendationsHandler(engine *simulation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(engine.Recommendations(8))
	}
}
