package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"econ/simulation"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Blueprint import, server side. Two steps, deliberately separate:
//
//	POST /api/digitize          — proxy the blueprint to the digitizer for REVIEW.
//	POST /api/building          — deploy a reviewed result (destructive; guarded).
//	GET  /api/building/backups  — list the automatic pre-deploy backups.
//	POST /api/building/rollback — restore a backup (destructive; guarded).
//
// Deploying replaces the running building, so it is treated like the operational
// change it is: the current building is backed up first, the change is written to an
// append-only audit log, and — when ECON_ADMIN_TOKEN is set — both destructive
// endpoints require the token. The demo runs with the token unset; a commercial
// deployment sets it in the compose environment and shares it with operators.

const backupDir = "./data/backups"
const backupKeep = 20
const auditPath = "./data/deploy-log.jsonl"

func corsPreflight(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Admin-Token")
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// requireAdmin gates the destructive endpoints. With ECON_ADMIN_TOKEN unset the gate is
// open (demo mode); set, the exact token must arrive in X-Admin-Token. Constant-time
// comparison — an admin token that leaks its length or prefix through timing is theatre.
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	want := os.Getenv("ECON_ADMIN_TOKEN")
	if want == "" {
		return true
	}
	got := r.Header.Get("X-Admin-Token")
	if subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1 {
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error": "admin token required: this replaces the running building. Send it in the X-Admin-Token header.",
	})
	return false
}

// backupCurrent snapshots the live building + ontology before anything overwrites them.
// Returns the backup name ("" when there was nothing to back up — first boot).
func backupCurrent() string {
	cur, err := os.ReadFile("./data/building-data.json")
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		log.Printf("[building] backup dir: %v", err)
		return ""
	}
	name := time.Now().UTC().Format("20060102-150405")
	if err := os.WriteFile(filepath.Join(backupDir, "building-"+name+".json"), cur, 0644); err != nil {
		log.Printf("[building] backup write: %v", err)
		return ""
	}
	if onto, err := os.ReadFile("./data/brick-ontology.json"); err == nil {
		os.WriteFile(filepath.Join(backupDir, "ontology-"+name+".json"), onto, 0644)
	}
	pruneBackups()
	return name
}

func pruneBackups() {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); len(n) > 9 && n[:9] == "building-" {
			names = append(names, n)
		}
	}
	sort.Strings(names) // timestamped names sort chronologically
	for len(names) > backupKeep {
		stamp := names[0][len("building-") : len(names[0])-len(".json")]
		os.Remove(filepath.Join(backupDir, names[0]))
		os.Remove(filepath.Join(backupDir, "ontology-"+stamp+".json"))
		names = names[1:]
	}
}

// audit appends one line per operational change: who cannot be known without auth
// infrastructure, but what/when/how-big always can. Never fails the operation.
func audit(action, source string, payload []byte, zones, floors int) {
	sum := sha256.Sum256(payload)
	rec, _ := json.Marshal(map[string]interface{}{
		"ts": time.Now().UTC().Format(time.RFC3339), "action": action, "source": source,
		"zones": zones, "floors": floors, "sha256": hex.EncodeToString(sum[:8]),
	})
	f, err := os.OpenFile(auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[audit] %v", err)
		return
	}
	defer f.Close()
	f.Write(append(rec, '\n'))
	log.Printf("[audit] %s", rec)
}

func countBuilding(data []byte) (zones, floors int) {
	var bd struct {
		Floors []struct {
			Zones []json.RawMessage `json:"zones"`
		} `json:"floors"`
	}
	if json.Unmarshal(data, &bd) == nil {
		floors = len(bd.Floors)
		for _, f := range bd.Floors {
			zones += len(f.Zones)
		}
	}
	return
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
		Source       string          `json:"source"` // original filename, for the audit trail
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST the digitizer result to deploy it", http.StatusMethodNotAllowed)
			return
		}
		if !requireAdmin(w, r) {
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

		backup := backupCurrent()
		if err := os.WriteFile("./data/building-data.json", p.BuildingData, 0644); err != nil {
			log.Printf("[building] deploy persisted nothing: %v (twin IS swapped in memory)", err)
		}
		if len(p.Ontology) > 0 {
			if err := os.WriteFile("./data/brick-ontology.json", p.Ontology, 0644); err != nil {
				log.Printf("[building] ontology write failed: %v", err)
			}
		}
		zones, floors := countBuilding(p.BuildingData)
		audit("deploy", p.Source, p.BuildingData, zones, floors)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "backup": backup})
	}
}

func backupsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		out := []map[string]interface{}{}
		entries, err := os.ReadDir(backupDir)
		if err == nil {
			var names []string
			for _, e := range entries {
				if n := e.Name(); len(n) > 9 && n[:9] == "building-" {
					names = append(names, n)
				}
			}
			sort.Sort(sort.Reverse(sort.StringSlice(names))) // newest first
			for _, n := range names {
				data, err := os.ReadFile(filepath.Join(backupDir, n))
				if err != nil {
					continue
				}
				zones, floors := countBuilding(data)
				out = append(out, map[string]interface{}{
					"name": n[len("building-") : len(n)-len(".json")], "zones": zones, "floors": floors,
				})
			}
		}
		json.NewEncoder(w).Encode(out)
	}
}

func rollbackHandler(engine *simulation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "POST {name} to restore a backup", http.StatusMethodNotAllowed)
			return
		}
		if !requireAdmin(w, r) {
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req)
		if req.Name == "" || filepath.Base(req.Name) != req.Name {
			http.Error(w, "name must be a backup timestamp from /api/building/backups", http.StatusBadRequest)
			return
		}
		data, err := os.ReadFile(filepath.Join(backupDir, "building-"+req.Name+".json"))
		if err != nil {
			http.Error(w, "no such backup", http.StatusNotFound)
			return
		}
		if err := engine.ReloadBuilding(data); err != nil {
			http.Error(w, "backup rejected: "+err.Error(), http.StatusUnprocessableEntity)
			return
		}
		// Rolling back is itself a change: back up what we're replacing, so you can
		// roll forward again.
		backupCurrent()
		os.WriteFile("./data/building-data.json", data, 0644)
		if onto, err := os.ReadFile(filepath.Join(backupDir, "ontology-"+req.Name+".json")); err == nil {
			os.WriteFile("./data/brick-ontology.json", onto, 0644)
		}
		zones, floors := countBuilding(data)
		audit("rollback", req.Name, data, zones, floors)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}
}
