package main

import (
	"bytes"
	"econ/simulation"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// forecastRequest mirrors the Python service's POST /predict body. The outdoor fields
// are the engine's own live Open-Meteo readings — handed over so the forecaster and the
// envelope physics never disagree about the weather; omitted (nil) when the feed is
// stale, in which case the Python service falls back to its own fetch and says so.
type forecastRequest struct {
	SensorSequence  [][]float64 `json:"sensor_sequence"`
	OutdoorTemp     *float64    `json:"outdoor_temp,omitempty"`
	OutdoorHumidity *float64    `json:"outdoor_humidity,omitempty"`
}

// forecastWindowLen must match the Python model's SEQ_LEN (12 steps of 5 minutes).
const forecastWindowLen = 12

// buildForecastRequest assembles the /predict body from live engine state. Shared by
// the HTTP proxy below and the pre-cool poller, so every forecast in the system is
// made from the same real inputs.
func buildForecastRequest(engine *simulation.Engine) (forecastRequest, int) {
	window, realSamples := engine.ForecastWindow(forecastWindowLen)
	req := forecastRequest{SensorSequence: window}
	if t, h, live := engine.OutdoorForForecast(); live {
		req.OutdoorTemp = &t
		req.OutdoorHumidity = &h
	}
	return req, realSamples
}

// loadForecastRequest mirrors the Python service's POST /forecast/load body — the zero-shot
// path. Where the LSTM needs a fixed 12-step [temp, airflow] window because that is the
// shape it was trained on, a foundation model takes the raw load series and needs no
// training, no scaler and no feature engineering at all.
type loadForecastRequest struct {
	History []float64 `json:"history"`
	Horizon int       `json:"horizon"`
}

// timesfmMinHistory matches the Python validator: below this there is not enough of a
// series to forecast from, and saying so is better than forecasting from noise.
const timesfmMinHistory = 8

// loadForecastHandler drives Google TimesFM over the building's own recorded load series.
//
//	GET /api/forecast/load[?horizon=N]
//
// This is the twin's cold-start answer to peak-load forecasting. The LSTM cannot say
// anything until train.py has been run against accumulated history; TimesFM is pretrained,
// so it forecasts a building it has never seen from whatever real history exists. The
// response carries how many real samples backed it and the engine/device that served it,
// so a forecast from 20 minutes of history is never mistaken for one from two days.
func loadForecastHandler(engine *simulation.Engine) http.HandlerFunc {
	// Generous timeout: the FIRST call may download a multi-gigabyte checkpoint.
	client := &http.Client{Timeout: 180 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		history := engine.LoadHistory()
		if len(history) < timesfmMinHistory {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": "not enough load history yet",
				"detail": "the zero-shot forecaster needs at least 8 recorded load samples; " +
					"the engine records one every 5 minutes",
				"samples": len(history), "need": timesfmMinHistory,
			})
			return
		}

		horizon := 12
		if q := r.URL.Query().Get("horizon"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n >= 1 && n <= 256 {
				horizon = n
			}
		}

		base := os.Getenv("FORECAST_URL")
		if base == "" {
			base = "http://localhost:8000"
		}
		body, _ := json.Marshal(loadForecastRequest{History: history, Horizon: horizon})
		resp, err := client.Post(base+"/forecast/load", "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("[forecast] TimesFM service unreachable at %s: %v", base, err)
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "forecasting service unreachable: " + err.Error()})
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var out map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&out) == nil {
				out["history_samples"] = len(history)
				out["step_minutes"] = int(histIntervalMinutes)
				out["horizon_minutes"] = horizon * int(histIntervalMinutes)
				json.NewEncoder(w).Encode(out)
				return
			}
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "forecaster returned malformed JSON"})
			return
		}
		w.WriteHeader(resp.StatusCode) // pass through 503 (TimesFM unavailable) etc.
		io.Copy(w, resp.Body)
	}
}

// histIntervalMinutes is the engine's history cadence in minutes — the unit every horizon
// in the forecast response is expressed in.
const histIntervalMinutes = 5

// engineResult is one forecaster's answer, or the reason it has none. Both engines are
// always reported: "the LSTM is not trained" and "TimesFM could not download its
// checkpoint" are findings about the twin, not errors to swallow.
type engineResult struct {
	Available bool      `json:"available"`
	PeakMw    *float64  `json:"peakMw"`           // the comparable number: predicted peak load
	Series    []float64 `json:"series,omitempty"` // TimesFM only — the full horizon
	Error     string    `json:"error,omitempty"`
	// Predictive spread, TimesFM only. The foundation model returns per-decile heads
	// alongside its central path, and the module that produces them argues — correctly —
	// that for a pre-cool decision the spread matters more than the mean, because the
	// decision is about the RISK of the peak. They were decoded in Python and then dropped
	// at this boundary, so no consumer had ever seen them.
	Quantiles     map[string][]float64 `json:"quantiles,omitempty"`
	PeakUpperMw   *float64             `json:"peakUpperMw,omitempty"`   // peak of the highest decile
	UpperQuantile string               `json:"upperQuantile,omitempty"` // which head that was
	// Provenance: how much real history backed this answer. The two engines consume
	// different windows, so neither figure is meaningful without saying which it is.
	RealSamples int    `json:"realSamples"`
	WindowLen   int    `json:"windowLen,omitempty"`
	Basis       string `json:"basis"`
	// Plausibility against the building's OWN observed load. A supervised model carries
	// the building it was trained on inside its weights: point the LSTM trained on a
	// 39,776 m2 tower at a 72 m2 house and it answers 2.4 MW for a building that has never
	// drawn more than 0.03 MW. The number is not wrong arithmetic, it is a model being
	// asked about a building it has never seen — and a dashboard that renders it as "the
	// predicted peak" is making a claim the twin cannot support.
	Implausible  bool   `json:"implausible"`
	Plausibility string `json:"plausibility,omitempty"`
}

// outOfDistributionFactor is how far outside the observed load range a forecast may sit
// before it is reported as out of distribution. Loads legitimately exceed anything yet
// recorded — that is what a peak forecast is FOR — so this is deliberately loose: it fires
// on an order-of-magnitude mismatch, not on a model predicting a new high.
const outOfDistributionFactor = 4.0

// minRangeSamples is how much observed history is needed before the range means anything.
// Below it no judgement is offered, because "this building has never drawn 2 MW" is not a
// finding when the building has only been watched for four minutes.
const minRangeSamples = 24

// checkPlausible annotates a result against the building's own observed load range.
func checkPlausible(res *engineResult, lo, hi float64, n int) {
	if res.PeakMw == nil || n < minRangeSamples || hi <= 0 {
		return
	}
	p := *res.PeakMw
	switch {
	case p > hi*outOfDistributionFactor:
		res.Implausible = true
		res.Plausibility = fmt.Sprintf(
			"%.2f MW is %.0fx the highest load this building has been observed at (%.3f MW over %d recorded samples). "+
				"A supervised model carries its training building in its weights; this reads as a model asked about a "+
				"building it was not trained on, not as a forecast of this one.", p, p/hi, hi, n)
	case lo > 0 && p < lo/outOfDistributionFactor:
		res.Implausible = true
		res.Plausibility = fmt.Sprintf(
			"%.3f MW is far below the lowest load this building has been observed at (%.3f MW over %d recorded samples).",
			p, lo, n)
	default:
		res.Plausibility = fmt.Sprintf("within the observed range for this building (%.3f-%.3f MW over %d samples).", lo, hi, n)
	}
}

// compareForecastHandler runs BOTH forecasters over the same instant and returns both
// answers side by side.
//
//	GET /api/forecast/compare[?horizon=N]
//
// The two are not redundant and not interchangeable: the LSTM is supervised and only knows
// this building once train.py has had real history to learn from, while TimesFM is
// pretrained and forecasts a series it has never seen. Until now they were reachable only
// through separate endpoints, so nothing in the system ever put their answers next to each
// other — which is the only way to find out which one to trust for THIS building.
//
// Both are reduced to one comparable scalar: the predicted peak load in MW. For the LSTM
// that is what it emits directly; for TimesFM it is the maximum of the forecast horizon,
// which is the same quantity the pre-cool decision actually turns on.
//
// Neither engine being reachable is a 200 with two unavailable results, not a 503: the
// endpoint's job is to report the state of the forecasting layer, and "both are down" is a
// perfectly good report.
func compareForecastHandler(engine *simulation.Engine) http.HandlerFunc {
	// The LSTM answers in milliseconds; TimesFM may be downloading a multi-gigabyte
	// checkpoint on its first call, so it gets the generous timeout.
	lstmClient := &http.Client{Timeout: 8 * time.Second}
	tfmClient := &http.Client{Timeout: 180 * time.Second}

	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		base := os.Getenv("FORECAST_URL")
		if base == "" {
			base = "http://localhost:8000"
		}
		horizon := 12
		if q := r.URL.Query().Get("horizon"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n >= 1 && n <= 256 {
				horizon = n
			}
		}

		// Both calls go out concurrently: they hit the same service but are independent,
		// and serializing them would make the response wait out TimesFM's cold start
		// before it could report the LSTM's answer.
		type pair struct {
			name string
			res  engineResult
		}
		ch := make(chan pair, 2)

		go func() { ch <- pair{"lstm", queryLSTM(engine, lstmClient, base)} }()
		go func() { ch <- pair{"timesfm", queryTimesFM(engine, tfmClient, base, horizon)} }()

		out := map[string]interface{}{}
		for i := 0; i < 2; i++ {
			p := <-ch
			out[p.name] = p.res
		}

		lstm := out["lstm"].(engineResult)
		tfm := out["timesfm"].(engineResult)
		// Both answers are checked against the same measured range, so "the LSTM is out of
		// distribution and TimesFM is not" is a conclusion the payload can actually carry.
		lo, hi, n := engine.ObservedLoadRange()
		checkPlausible(&lstm, lo, hi, n)
		checkPlausible(&tfm, lo, hi, n)
		out["lstm"] = lstm
		out["timesfm"] = tfm
		out["observedLoad"] = map[string]interface{}{"minMw": lo, "maxMw": hi, "samples": n}
		out["stepMinutes"] = histIntervalMinutes
		out["horizonMinutes"] = horizon * histIntervalMinutes
		out["agreement"] = forecastAgreement(lstm, tfm)
		json.NewEncoder(w).Encode(out)
	}
}

// queryLSTM asks the supervised forecaster for its predicted peak over the sampled window.
func queryLSTM(engine *simulation.Engine, client *http.Client, base string) engineResult {
	req, realSamples := buildForecastRequest(engine)
	res := engineResult{
		RealSamples: realSamples,
		WindowLen:   forecastWindowLen,
		Basis:       "supervised LSTM over a 12-step [avgTemp, airflow] window",
	}
	body, _ := json.Marshal(req)
	resp, err := client.Post(base+"/predict", "application/json", bytes.NewReader(body))
	if err != nil {
		res.Error = "forecasting service unreachable: " + err.Error()
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 503 here means train.py has not been run — the honest and common case.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		res.Error = fmt.Sprintf("forecaster returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		return res
	}
	var decoded struct {
		PredictedPeakLoad float64 `json:"predicted_peak_load"`
	}
	if json.NewDecoder(resp.Body).Decode(&decoded) != nil {
		res.Error = "forecaster returned malformed JSON"
		return res
	}
	res.Available = true
	peak := decoded.PredictedPeakLoad
	res.PeakMw = &peak
	return res
}

// queryTimesFM asks the zero-shot foundation model for the coming horizon, reduced to the
// peak it contains. The engine's own minimum-history rule is applied here rather than
// letting the service reject it, so the reason reads as a property of the twin.
func queryTimesFM(engine *simulation.Engine, client *http.Client, base string, horizon int) engineResult {
	history := engine.LoadHistory()
	res := engineResult{
		RealSamples: len(history),
		Basis:       "zero-shot TimesFM over the recorded building-load series",
	}
	if len(history) < timesfmMinHistory {
		res.Error = fmt.Sprintf("not enough load history yet: %d of %d samples "+
			"(the engine records one every %d minutes)", len(history), timesfmMinHistory, histIntervalMinutes)
		return res
	}

	body, _ := json.Marshal(loadForecastRequest{History: history, Horizon: horizon})
	resp, err := client.Post(base+"/forecast/load", "application/json", bytes.NewReader(body))
	if err != nil {
		res.Error = "forecasting service unreachable: " + err.Error()
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		res.Error = fmt.Sprintf("forecaster returned %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
		return res
	}
	var decoded struct {
		Forecast  []float64            `json:"forecast"`
		Point     []float64            `json:"point"`
		Quantiles map[string][]float64 `json:"quantiles"`
	}
	if json.NewDecoder(resp.Body).Decode(&decoded) != nil {
		res.Error = "forecaster returned malformed JSON"
		return res
	}
	series := decoded.Forecast
	if len(series) == 0 {
		series = decoded.Point
	}
	if len(series) == 0 {
		res.Error = "forecaster returned an empty horizon"
		return res
	}
	peak := series[0]
	for _, v := range series[1:] {
		if v > peak {
			peak = v
		}
	}
	res.Available = true
	res.PeakMw = &peak
	res.Series = series

	// Carry the spread through. The upper band is the highest-numbered quantile head the
	// checkpoint returned — read that way rather than hardcoded to "q9", because the head
	// count is a property of the checkpoint and 2.5-200m need not match 2.0-500m.
	if len(decoded.Quantiles) > 0 {
		res.Quantiles = decoded.Quantiles
		if key, band := highestQuantile(decoded.Quantiles); band != nil {
			res.UpperQuantile = key
			res.PeakUpperMw = band
		}
	}
	return res
}

// highestQuantile returns the name and peak value of the highest-numbered quantile head
// ("q1".."q9" — deciles, ascending). Heads that are not of that form are ignored rather
// than guessed at.
func highestQuantile(q map[string][]float64) (string, *float64) {
	bestName, bestIdx := "", -1
	for name := range q {
		if len(name) < 2 || name[0] != 'q' {
			continue
		}
		idx, err := strconv.Atoi(name[1:])
		if err != nil {
			continue
		}
		if idx > bestIdx {
			bestIdx, bestName = idx, name
		}
	}
	if bestName == "" || len(q[bestName]) == 0 {
		return "", nil
	}
	peak := q[bestName][0]
	for _, v := range q[bestName][1:] {
		if v > peak {
			peak = v
		}
	}
	return bestName, &peak
}

// forecastAgreement describes how far apart the two engines are, when both answered. It is
// deliberately a description rather than a verdict: with one supervised model possibly
// trained on synthetic data and one zero-shot model, a disagreement says "these need
// comparing against outturn", not "this one is wrong".
func forecastAgreement(lstm, tfm engineResult) map[string]interface{} {
	if !lstm.Available || !tfm.Available || lstm.PeakMw == nil || tfm.PeakMw == nil {
		return map[string]interface{}{
			"comparable": false,
			"note":       "both engines must answer before their forecasts can be compared",
		}
	}
	a, b := *lstm.PeakMw, *tfm.PeakMw
	diff := a - b
	rel := 0.0
	if denom := math.Max(math.Abs(a), math.Abs(b)); denom > 0 {
		rel = math.Abs(diff) / denom
	}
	return map[string]interface{}{
		"comparable":   true,
		"deltaMw":      diff,
		"relativeDiff": rel,
		"higher":       map[bool]string{true: "lstm", false: "timesfm"}[a >= b],
		"note": "peak MW from each engine over the same instant; neither is ground truth " +
			"until compared against measured outturn",
	}
}

// forecastEnginesHandler surfaces which forecasting engines the Python service can serve
// (GET /model/info there), so the dashboard can show whether the twin is running the
// supervised LSTM, the zero-shot foundation model, or neither — and why.
func forecastEnginesHandler() http.HandlerFunc {
	client := &http.Client{Timeout: 5 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		if corsPreflight(w, r) {
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		base := os.Getenv("FORECAST_URL")
		if base == "" {
			base = "http://localhost:8000"
		}
		resp, err := client.Get(base + "/model/info")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"reachable": false,
				"error":     err.Error(),
			})
			return
		}
		defer resp.Body.Close()
		var out map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&out) != nil {
			out = map[string]interface{}{}
		}
		out["reachable"] = true
		json.NewEncoder(w).Encode(out)
	}
}

// forecastHandler proxies a live-telemetry window to the Python LSTM forecaster and returns its
// JSON ({predicted_peak_load, outdoor_temp_used, outdoor_humidity_used, weather_source}),
// annotated with the window's provenance (window_real_samples / window_len) so the AI layer
// can say "warming up: 3/12 real samples" instead of presenting padding as history. This is
// the human-in-the-loop hook for the dashboard/optimizer: the Go engine owns the building state,
// the Python service owns the model. FORECAST_URL points at the service (compose: forecasting:8000).
func forecastHandler(engine *simulation.Engine) http.HandlerFunc {
	client := &http.Client{Timeout: 8 * time.Second}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")

		base := os.Getenv("FORECAST_URL")
		if base == "" {
			base = "http://localhost:8000"
		}

		req, realSamples := buildForecastRequest(engine)
		body, _ := json.Marshal(req)
		resp, err := client.Post(base+"/predict", "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("[forecast] service unreachable at %s: %v", base, err)
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "forecasting service unreachable: " + err.Error()})
			return
		}
		defer resp.Body.Close()

		// Annotate a successful prediction with the input window's provenance. Anything
		// that doesn't decode as a JSON object (error bodies, 503s) passes through as-is.
		if resp.StatusCode == http.StatusOK {
			var out map[string]interface{}
			if json.NewDecoder(resp.Body).Decode(&out) == nil {
				out["window_real_samples"] = realSamples
				out["window_len"] = forecastWindowLen
				// Same plausibility check the compare endpoint applies, on the same
				// measured range — this is the endpoint the AI panel reads, so it is the
				// one that most needs to say when the model is answering about a
				// different building than the one it is pointed at.
				if peak, ok := out["predicted_peak_load"].(float64); ok {
					lo, hi, n := engine.ObservedLoadRange()
					res := engineResult{PeakMw: &peak}
					checkPlausible(&res, lo, hi, n)
					out["implausible"] = res.Implausible
					if res.Plausibility != "" {
						out["plausibility"] = res.Plausibility
					}
					out["observed_min_mw"], out["observed_max_mw"], out["observed_samples"] = lo, hi, n
				}
				json.NewEncoder(w).Encode(out)
				return
			}
			w.WriteHeader(http.StatusBadGateway)
			json.NewEncoder(w).Encode(map[string]string{"error": "forecaster returned malformed JSON"})
			return
		}
		w.WriteHeader(resp.StatusCode) // pass through 503 (model not trained) etc.
		io.Copy(w, resp.Body)
	}
}
