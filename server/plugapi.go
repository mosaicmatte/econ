package main

import (
	"econ/simulation"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// Plug-load management over HTTP.
//
//	GET  /api/plugs — live snapshot: totals, sweep state, savings, phantom leaderboard.
//	POST /api/plugs — change the sweep policy (admin-guarded when ECON_ADMIN_TOKEN set,
//	                  audited, persisted so it survives a container rebuild).
//
// Changing when a building's sockets switch off is an operational change with the same
// blast radius as a BMS schedule edit, so it gets the same treatment as deploying a
// building: token, audit line, durable state on the data volume.

const plugStatePath = "./data/plug-state.json"

// plugState is what persists: the policy and the cumulative avoided energy. A savings
// counter that zeroes on every redeploy is not a number anyone can put in a report.
type plugState struct {
	Config   simulation.PlugConfig `json:"config"`
	SavedKwh float64               `json:"savedKwh"`
}

// loadPlugState restores policy + savings at boot; absent file means defaults.
func loadPlugState(engine *simulation.Engine) {
	data, err := os.ReadFile(plugStatePath)
	if err != nil {
		return
	}
	var s plugState
	if err := json.Unmarshal(data, &s); err != nil {
		log.Printf("[plugs] state file unreadable, using defaults: %v", err)
		return
	}
	engine.SetPlugConfig(s.Config)
	engine.RestorePlugSavedKwh(s.SavedKwh)
	log.Printf("[plugs] restored: enabled=%v work=%02d-%02d grace=%dm saved=%.2f kWh",
		s.Config.Enabled, s.Config.WorkStartHour, s.Config.WorkEndHour, s.Config.GraceMinutes, s.SavedKwh)
}

func savePlugState(engine *simulation.Engine) {
	s := plugState{Config: engine.PlugSnapshot(0).Config, SavedKwh: engine.PlugSavedKwh()}
	data, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(plugStatePath, data, 0644); err != nil {
		log.Printf("[plugs] state save: %v", err)
	}
}

// plugPersistLoop checkpoints the savings counter once a minute — cheap, and bounds
// what a crash can lose to sixty seconds of accumulation.
func plugPersistLoop(engine *simulation.Engine) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		savePlugState(engine)
	}
}

func plugsHandler(engine *simulation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")

		if r.Method == http.MethodPost {
			if !requireAdmin(w, r) {
				return
			}
			var c simulation.PlugConfig
			if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&c); err != nil {
				http.Error(w, "body must be a plug sweep config: {enabled, workStartHour, workEndHour, graceMinutes, criticalTypes}", http.StatusBadRequest)
				return
			}
			applied := engine.SetPlugConfig(c)
			savePlugState(engine)
			cfgJSON, _ := json.Marshal(applied)
			audit("plug-config", r.Header.Get("X-Forwarded-For"), cfgJSON, 0, 0)
			log.Printf("[plugs] sweep policy updated: enabled=%v work=%02d-%02d grace=%dm",
				applied.Enabled, applied.WorkStartHour, applied.WorkEndHour, applied.GraceMinutes)
		}

		json.NewEncoder(w).Encode(engine.PlugSnapshot(10))
	}
}
