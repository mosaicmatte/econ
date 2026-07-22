package main

import (
	"archive/zip"
	"bytes"
	"econ/simulation"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Downloadable local models.
//
// The twin learns two models from the building's own data: the online baseline model
// (simulation/baselines.go), which is the recommendation engine, and the LSTM peak-load
// forecaster (backend/forecasting). This endpoint packages them — plus a dependency-free
// recommender that reproduces the engine's σ-scoring — into a single zip the operator can
// download and run offline. It is the "take the intelligence with you" surface: the same
// recommendations and alerts the dashboard shows, computed on the user's own machine from
// the processed model state, with no server required.
//
//	GET /api/model         — metadata: model maturity, forecaster readiness, bundle contents
//	GET /api/model/export  — the zip bundle (attachment)

//go:embed modelbundle/recommender.py modelbundle/econ_local.py modelbundle/README.md modelbundle/sample_readings.json modelbundle/sample_rooms.json
var modelBundleFS embed.FS

// modelInfo is the /api/model metadata the dashboard's download card reads.
type modelInfo struct {
	Baseline struct {
		Established int      `json:"established"`
		Learning    int      `json:"learning"`
		MatureAfter int      `json:"matureAfter"`
		Metrics     []string `json:"metrics"`
	} `json:"baseline"`
	// Rooms reports the second model: how many rooms the twin has actually identified a
	// physical model for. It matures on different evidence than the baselines (a room has
	// to move to reveal its dynamics), so it is reported separately rather than folded in.
	Rooms struct {
		Identified  int `json:"identified"`
		Learning    int `json:"learning"`
		MatureAfter int `json:"matureAfter"`
	} `json:"rooms"`
	Forecaster struct {
		Reachable bool   `json:"reachable"`
		Ready     bool   `json:"ready"`
		URL       string `json:"url"`
	} `json:"forecaster"`
	Bundle     []string    `json:"bundle"`
	ExportPath string      `json:"exportPath"`
	Tiers      []ModelTier `json:"tiers"`
}

func forecastBaseURL() string {
	base := os.Getenv("FORECAST_URL")
	if base == "" {
		base = "http://localhost:8000"
	}
	return base
}

// forecasterHealth asks the Python service whether it is up and has a trained model. Best
// effort: a down forecaster just means the bundle ships without the LSTM artifacts.
func forecasterHealth() (reachable, ready bool) {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(forecastBaseURL() + "/health")
	if err != nil {
		return false, false
	}
	defer resp.Body.Close()
	var h struct {
		ModelReady bool `json:"model_ready"`
	}
	if json.NewDecoder(resp.Body).Decode(&h) != nil {
		return true, false
	}
	return true, h.ModelReady
}

func modelInfoHandler(engine *simulation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")

		est, learning := engine.BaselineCoverage()
		reachable, ready := forecasterHealth()

		var info modelInfo
		info.Baseline.Established = est
		info.Baseline.Learning = learning
		info.Baseline.MatureAfter = simulation.BaselineModelSpec().MatureAfter
		for k := range simulation.BaselineModelSpec().Metrics {
			info.Baseline.Metrics = append(info.Baseline.Metrics, k)
		}
		ident, roomsLearning := engine.DynamicsCoverage()
		info.Rooms.Identified = ident
		info.Rooms.Learning = roomsLearning
		info.Rooms.MatureAfter = simulation.DynamicsMatureAfter()

		info.Forecaster.Reachable = reachable
		info.Forecaster.Ready = ready
		info.Forecaster.URL = forecastBaseURL()
		info.Bundle = bundleContents(defaultTier(), ready)
		info.ExportPath = "/api/model/export"
		info.Tiers = modelTiers
		json.NewEncoder(w).Encode(info)
	}
}

// defaultTier is what the bundle contains when the caller does not name a tier — the
// room-analyst set, since the room models are the part worth having and they still need
// nothing but Python 3.
func defaultTier() string { return "room-analyst" }

// bundleContents lists what a given tier's zip actually holds, so the download card can
// show it before the user commits to the download.
func bundleContents(tier string, forecasterReady bool) []string {
	files := []string{
		"baselines.json", "model-spec.json", "recommender.py",
		"sample_readings.json", "README.md", "MANIFEST.json",
	}
	if tierRank(tier) >= tierRank("room-analyst") {
		files = append(files, "dynamics.json", "econ_local.py", "sample_rooms.json")
	}
	if forecasterReady && tierRank(tier) >= tierRank("forecast-cpu") {
		files = append(files,
			"forecaster/model_weights.pth", "forecaster/scaler.pkl", "forecaster/config.json")
	}
	return files
}

// forecasterArtifacts fetches the trained LSTM weights/scaler/config from the Python
// service (base64) so they can be embedded in the zip. Returns nil on any failure — the
// bundle degrades to the baseline model + recommender, which is the part that generates
// recommendations anyway.
func forecasterArtifacts() map[string]interface{} {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(forecastBaseURL() + "/model/artifacts")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var out map[string]interface{}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(&out) != nil {
		return nil
	}
	return out
}

func modelExportHandler(engine *simulation.Engine) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}

		// The tier decides what goes in the zip and how much work the local runtime is
		// configured to do. An unrecognized tier falls back to the default rather than
		// failing the download — and never silently upgrades past what was asked for.
		tier := defaultTier()
		if q := r.URL.Query().Get("tier"); q != "" {
			if _, ok := tierByID(q); ok {
				tier = q
			} else {
				log.Printf("[model] ignoring unknown tier %q, using %q", q, tier)
			}
		}
		// Worker count is the client's measured parallelism (modelcatalog.go). Clamped
		// here too: the query string is user input, not a trusted value.
		workers := 0
		if q := r.URL.Query().Get("workers"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				workers = n
				if workers > 64 {
					workers = 64
				}
			}
		}

		// Assemble the bundle in memory (it is small — the learned model is compact and
		// the LSTM weights are ~200 KB), then stream it as one attachment.
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		add := func(name string, data []byte) {
			f, err := zw.Create(name)
			if err != nil {
				return
			}
			f.Write(data)
		}
		addEmbedded := func(name, embeddedPath string) {
			data, err := modelBundleFS.ReadFile(embeddedPath)
			if err != nil {
				log.Printf("[model] bundle asset missing: %s (%v)", embeddedPath, err)
				return
			}
			add(name, data)
		}

		// The live learned model + its spec — the two files recommender.py needs.
		baselines, _ := engine.MarshalBaselines()
		add("baselines.json", baselines)
		specJSON, _ := json.MarshalIndent(simulation.BaselineModelSpec(), "", "  ")
		add("model-spec.json", specJSON)

		addEmbedded("recommender.py", "modelbundle/recommender.py")
		addEmbedded("README.md", "modelbundle/README.md")
		addEmbedded("sample_readings.json", "modelbundle/sample_readings.json")

		// From the room-analyst tier up, ship the identified room models and the runtime
		// that actually uses them — the prediction half of the intelligence, which is the
		// part that turns a downloaded copy from a scorer into an analyst.
		roomsIncluded := false
		if tierRank(tier) >= tierRank("room-analyst") {
			dyn, err := engine.MarshalDynamics()
			if err != nil || len(dyn) == 0 {
				dyn = []byte("{}")
			}
			add("dynamics.json", dyn)
			addEmbedded("econ_local.py", "modelbundle/econ_local.py")
			addEmbedded("sample_rooms.json", "modelbundle/sample_rooms.json")
			roomsIncluded = true
		}

		// Best-effort LSTM forecaster artifacts — only for the tiers that can run torch.
		est, learning := engine.BaselineCoverage()
		identified, roomsLearning := engine.DynamicsCoverage()
		forecasterIncluded := false
		if art := forecasterArtifacts(); art != nil && tierRank(tier) >= tierRank("forecast-cpu") {
			if cfg, ok := art["config"]; ok {
				if cfgJSON, err := json.MarshalIndent(cfg, "", "  "); err == nil {
					add("forecaster/config.json", cfgJSON)
				}
			}
			for field, name := range map[string]string{
				"weights_b64": "forecaster/model_weights.pth",
				"scaler_b64":  "forecaster/scaler.pkl",
			} {
				if s, ok := art[field].(string); ok && s != "" {
					if raw, err := base64.StdEncoding.DecodeString(s); err == nil {
						add(name, raw)
						forecasterIncluded = true
					}
				}
			}
		}

		usage := "python3 recommender.py sample_readings.json"
		if roomsIncluded {
			usage = "python3 econ_local.py analyze sample_rooms.json"
		}
		manifest := map[string]interface{}{
			"generatedAt": time.Now().UTC().Format(time.RFC3339),
			"source":      "ECON digital twin",
			"tier":        tier,
			// The parallelism the local runtime should use, measured from the machine that
			// asked for the bundle (modelcatalog.go). econ_local.py reads this.
			"workers": workers,
			"baseline": map[string]interface{}{
				"established": est,
				"learning":    learning,
				"matureAfter": simulation.BaselineModelSpec().MatureAfter,
			},
			"rooms": map[string]interface{}{
				"identified":  identified,
				"learning":    roomsLearning,
				"matureAfter": simulation.DynamicsMatureAfter(),
				"included":    roomsIncluded,
			},
			"forecasterIncluded": forecasterIncluded,
			"usage":              usage,
			"notes": "baselines.json holds the learned per-(zone,metric,hour) distributions; " +
				"dynamics.json holds each room's identified thermal and CO2 balance; " +
				"model-spec.json holds the scoring parameters. recommender.py reproduces the " +
				"engine's anomaly scoring and econ_local.py reproduces its room predictions, " +
				"both with only the Python 3 standard library.",
		}
		manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
		add("MANIFEST.json", manifestJSON)

		if err := zw.Close(); err != nil {
			http.Error(w, "failed to assemble bundle", http.StatusInternalServerError)
			return
		}

		fname := fmt.Sprintf("econ-local-models-%s-%s.zip", tier, time.Now().Format("2006-01-02"))
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+fname+"\"")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(buf.Bytes())
		log.Printf("[model] exported %q bundle (%d bytes, rooms=%v forecaster=%v workers=%d)", tier, buf.Len(), roomsIncluded, forecasterIncluded, workers)
	}
}
