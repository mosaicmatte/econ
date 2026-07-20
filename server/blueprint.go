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

// Blueprint import, server side. Two steps, deliberately separate:
//
//	POST /api/digitize  — proxy the uploaded blueprint (DXF/PDF/image) to the Python
//	                      digitizer service and return its result for REVIEW. Nothing
//	                      about the running twin changes.
//	POST /api/building  — deploy a reviewed result: persist building-data.json (+ brick
//	                      ontology) and hot-swap the running engine onto it.
//
// The dashboard only ever talks to this server, so the digitizer stays a private
// backend detail (DIGITIZER_URL; docker-compose wires it to the digitizer container).

func corsPreflight(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func digitizeHandler() http.HandlerFunc {
	base := os.Getenv("DIGITIZER_URL")
	if base == "" {
		base = "http://localhost:8090"
	}
	// CV segmentation on a 200 dpi A1 sheet legitimately takes tens of seconds.
	client := &http.Client{Timeout: 180 * time.Second}

	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST a multipart blueprint", http.StatusMethodNotAllowed)
			return
		}
		// Stream the multipart body through untouched — the digitizer parses it.
		body, err := io.ReadAll(io.LimitReader(r.Body, 45<<20))
		if err != nil {
			http.Error(w, "could not read upload", http.StatusBadRequest)
			return
		}
		req, _ := http.NewRequest(http.MethodPost, base+"/digitize", bytes.NewReader(body))
		req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "digitizer unreachable — is the digitizer container up? ("+err.Error()+")",
				http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}

func deployBuildingHandler(engine *simulation.Engine) http.HandlerFunc {
	type payload struct {
		BuildingData json.RawMessage `json:"buildingData"`
		Ontology     json.RawMessage `json:"ontology"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST the digitizer result to deploy it", http.StatusMethodNotAllowed)
			return
		}
		var p payload
		if err := json.NewDecoder(io.LimitReader(r.Body, 45<<20)).Decode(&p); err != nil || len(p.BuildingData) == 0 {
			http.Error(w, "body must be the /api/digitize result: {buildingData, ontology}", http.StatusBadRequest)
			return
		}

		// The engine validates on scratch state before swapping, so a bad payload is
		// rejected here with the twin untouched.
		if err := engine.ReloadBuilding(p.BuildingData); err != nil {
			http.Error(w, "blueprint rejected: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}

		// Persist AFTER the swap succeeded: the files back /api/building-data and
		// /api/ontology (read per request) and survive a server restart.
		if err := os.WriteFile("./data/building-data.json", p.BuildingData, 0644); err != nil {
			log.Printf("[building] deploy persisted nothing: %v (twin IS swapped in memory)", err)
		}
		if len(p.Ontology) > 0 {
			if err := os.WriteFile("./data/brick-ontology.json", p.Ontology, 0644); err != nil {
				log.Printf("[building] ontology write failed: %v", err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}
