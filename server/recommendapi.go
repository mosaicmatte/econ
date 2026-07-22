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

const (
	baselineStatePath = "./data/baseline-model.json"
	dynamicsStatePath = "./data/room-dynamics.json"
	loadHistoryPath   = "./data/load-history.json"
)

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

// loadDynamicsState restores the identified room models at boot. Identification is the
// slower of the two models to earn — a room has to actually move before its response is
// visible — so throwing it away on every redeploy would be the most expensive thing the
// twin could forget.
func loadDynamicsState(engine *simulation.Engine) {
	data, err := os.ReadFile(dynamicsStatePath)
	if err != nil {
		return
	}
	if err := engine.LoadDynamics(data); err != nil {
		log.Printf("[recommend] room-dynamics state unreadable, starting fresh: %v", err)
		return
	}
	ident, learning := engine.DynamicsCoverage()
	log.Printf("[recommend] restored room models: %d identified, %d still learning", ident, learning)
}

func saveDynamicsState(engine *simulation.Engine) {
	data, err := engine.MarshalDynamics()
	if err != nil {
		log.Printf("[recommend] room-dynamics marshal: %v", err)
		return
	}
	if err := os.WriteFile(dynamicsStatePath, data, 0644); err != nil {
		log.Printf("[recommend] room-dynamics state save: %v", err)
	}
}

// loadHistoryState persists the building-load series the zero-shot forecaster reads. It
// accrues one sample per five minutes, so a restart that dropped it would permanently
// shorten the context TimesFM gets to forecast from.
func loadLoadHistoryState(engine *simulation.Engine) {
	data, err := os.ReadFile(loadHistoryPath)
	if err != nil {
		return
	}
	if err := engine.LoadLoadHistory(data); err != nil {
		log.Printf("[forecast] load history unreadable, starting fresh: %v", err)
		return
	}
	log.Printf("[forecast] restored %d load samples for the zero-shot forecaster",
		len(engine.LoadHistory()))
}

func saveLoadHistoryState(engine *simulation.Engine) {
	data, err := engine.MarshalLoadHistory()
	if err != nil {
		return
	}
	if err := os.WriteFile(loadHistoryPath, data, 0644); err != nil {
		log.Printf("[forecast] load history save: %v", err)
	}
}

// baselinePersistLoop checkpoints both learned models once a minute — cheap (each snapshot
// drops immature state) and bounds what a crash can lose to a minute of learning.
func baselinePersistLoop(engine *simulation.Engine) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		saveBaselineState(engine)
		saveDynamicsState(engine)
		saveLoadHistoryState(engine)
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

// roomModelsHandler exposes what the twin has actually identified about each room — the
// physical constants behind every prediction, so the dashboard can show the reasoning
// rather than just the conclusion.
//
//	GET /api/rooms/models
func roomModelsHandler(engine *simulation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")

		ident, learning := engine.DynamicsCoverage()
		models := engine.RoomModels()
		if models == nil {
			models = []simulation.RoomModel{}
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"identified":  ident,
			"learning":    learning,
			"matureAfter": simulation.DynamicsMatureAfter(),
			"rooms":       models,
		})
	}
}
