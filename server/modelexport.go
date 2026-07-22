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

//go:embed modelbundle/recommender.py modelbundle/README.md modelbundle/sample_readings.json
var modelBundleFS embed.FS

// modelInfo is the /api/model metadata the dashboard's download card reads.
type modelInfo struct {
	Baseline struct {
		Established int      `json:"established"`
		Learning    int      `json:"learning"`
		MatureAfter int      `json:"matureAfter"`
		Metrics     []string `json:"metrics"`
	} `json:"baseline"`
	Forecaster struct {
		Reachable bool   `json:"reachable"`
		Ready     bool   `json:"ready"`
		URL       string `json:"url"`
	} `json:"forecaster"`
	Bundle     []string `json:"bundle"`
	ExportPath string   `json:"exportPath"`
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
		info.Forecaster.Reachable = reachable
		info.Forecaster.Ready = ready
		info.Forecaster.URL = forecastBaseURL()
		info.Bundle = []string{
			"baselines.json", "model-spec.json", "recommender.py",
			"sample_readings.json", "README.md", "MANIFEST.json",
		}
		if ready {
			info.Bundle = append(info.Bundle,
				"forecaster/model_weights.pth", "forecaster/scaler.pkl", "forecaster/config.json")
		}
		info.ExportPath = "/api/model/export"
		json.NewEncoder(w).Encode(info)
	}
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

		// Best-effort LSTM forecaster artifacts.
		est, learning := engine.BaselineCoverage()
		forecasterIncluded := false
		if art := forecasterArtifacts(); art != nil {
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

		manifest := map[string]interface{}{
			"generatedAt": time.Now().UTC().Format(time.RFC3339),
			"source":      "ECON digital twin",
			"baseline": map[string]interface{}{
				"established": est,
				"learning":    learning,
				"matureAfter": simulation.BaselineModelSpec().MatureAfter,
			},
			"forecasterIncluded": forecasterIncluded,
			"usage":              "python3 recommender.py sample_readings.json",
			"notes": "baselines.json holds the learned per-(zone,metric,hour) distributions; " +
				"model-spec.json holds the scoring parameters; recommender.py reproduces the " +
				"engine's scoring with only the Python 3 standard library.",
		}
		manifestJSON, _ := json.MarshalIndent(manifest, "", "  ")
		add("MANIFEST.json", manifestJSON)

		if err := zw.Close(); err != nil {
			http.Error(w, "failed to assemble bundle", http.StatusInternalServerError)
			return
		}

		fname := fmt.Sprintf("econ-local-models-%s.zip", time.Now().Format("2006-01-02"))
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+fname+"\"")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(buf.Bytes())
		log.Printf("[model] exported local-model bundle (%d bytes, forecaster=%v)", buf.Len(), forecasterIncluded)
	}
}
