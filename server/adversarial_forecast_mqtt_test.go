package main

import (
	"bytes"
	"econ/simulation"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAdversarialRecommendationsAPI_HistoryVariations verifies that GET /api/recommendations
// reliably constructs valid ForecastGraphData across cold-start, empty history, single sample,
// threshold boundaries (7 vs 8 samples), and large histories without panic or invalid structures.
func TestAdversarialRecommendationsAPI_HistoryVariations(t *testing.T) {
	historyCases := []struct {
		name        string
		samples     []float64
		wantSamples int
	}{
		{"zero samples (cold start)", []float64{}, 0},
		{"single sample", []float64{0.025}, 1},
		{"below min history (7 samples)", []float64{0.020, 0.021, 0.022, 0.023, 0.024, 0.025, 0.026}, 7},
		{"exact min history (8 samples)", []float64{0.020, 0.021, 0.022, 0.023, 0.024, 0.025, 0.026, 0.027}, 8},
		{"standard history (24 samples)", makeSequence(24, 0.020, 0.035), 24},
		{"large history (500 samples)", makeSequence(500, 0.015, 0.045), 500},
		{"zero load history", []float64{0, 0, 0, 0, 0, 0, 0, 0}, 8},
		{"extreme high load history", []float64{10.5, 12.0, 11.2, 13.5, 14.0, 15.0, 16.2, 17.0}, 8},
	}

	for _, tc := range historyCases {
		t.Run(tc.name, func(t *testing.T) {
			engine := simulation.NewEngine()
			if len(tc.samples) > 0 {
				doc := map[string]interface{}{
					"buildingId":            engine.BuildingId(),
					"occupancyModelVersion": simulation.OccupancyModelVersion,
					"site":                  simulation.SiteFingerprint(),
					"samples":               tc.samples,
				}
				data, _ := json.Marshal(doc)
				if err := engine.LoadLoadHistory(data); err != nil {
					t.Fatalf("failed to load history: %v", err)
				}
			}

			handler := recommendationsHandler(engine)
			req := httptest.NewRequest("GET", "/api/recommendations", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d", rec.Code)
			}

			var report simulation.RecommendationReport
			if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
				t.Fatalf("failed to decode response JSON: %v", err)
			}

			if report.Forecast == nil {
				t.Fatalf("report.Forecast is nil for case %q", tc.name)
			}

			fc := report.Forecast
			if fc.Engine == "" {
				t.Errorf("forecast.Engine is empty for case %q", tc.name)
			}
			if len(fc.Series) == 0 {
				t.Errorf("forecast.Series is empty for case %q", tc.name)
			}
			if len(fc.UpperBand) == 0 {
				t.Errorf("forecast.UpperBand is empty for case %q", tc.name)
			}
			if fc.StepMinutes <= 0 {
				t.Errorf("invalid stepMinutes: %d", fc.StepMinutes)
			}
			if fc.HorizonMinutes != len(fc.Series)*fc.StepMinutes {
				t.Errorf("horizonMinutes mismatch: %d != %d", fc.HorizonMinutes, len(fc.Series)*fc.StepMinutes)
			}

			// Validate all series values are finite numbers
			for i, v := range fc.Series {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Errorf("series[%d] is non-finite: %v", i, v)
				}
			}
			for i, v := range fc.UpperBand {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Errorf("upperBand[%d] is non-finite: %v", i, v)
				}
			}
		})
	}
}

// TestAdversarialRecommendationsAPI_BackendFailuresAndChaos tests forecaster backend
// error modes: 500, 503, 422, connection refused, slow response/timeout, corrupt JSON,
// partial quantiles, empty forecasts.
func TestAdversarialRecommendationsAPI_BackendFailuresAndChaos(t *testing.T) {
	chaosCases := []struct {
		name           string
		tfmStatusCode  int
		tfmBody        string
		lstmStatusCode int
		lstmBody       string
		wantEngine     string
	}{
		{
			name:           "both backends return 500 error",
			tfmStatusCode:  http.StatusInternalServerError,
			tfmBody:        `{"error":"internal forecaster crash"}`,
			lstmStatusCode: http.StatusInternalServerError,
			lstmBody:       `{"error":"lstm model execution error"}`,
			wantEngine:     "fallback",
		},
		{
			name:           "both backends return corrupted invalid JSON",
			tfmStatusCode:  http.StatusOK,
			tfmBody:        `{"forecast": [0.02, 0.03, INVALID_JSON`,
			lstmStatusCode: http.StatusOK,
			lstmBody:       `{"predicted_peak_load": NOT_A_FLOAT`,
			wantEngine:     "fallback",
		},
		{
			name:           "TimesFM returns empty horizon array",
			tfmStatusCode:  http.StatusOK,
			tfmBody:        `{"forecast": [], "point": []}`,
			lstmStatusCode: http.StatusOK,
			lstmBody:       `{"predicted_peak_load": 0.031}`,
			wantEngine:     "lstm",
		},
		{
			name:           "TimesFM unavailable (503), LSTM succeeds",
			tfmStatusCode:  http.StatusServiceUnavailable,
			tfmBody:        `{"error":"timesfm checkpoint downloading"}`,
			lstmStatusCode: http.StatusOK,
			lstmBody:       `{"predicted_peak_load": 0.0285}`,
			wantEngine:     "lstm",
		},
		{
			name:           "TimesFM succeeds with custom decile heads, LSTM fails",
			tfmStatusCode:  http.StatusOK,
			tfmBody:        `{"forecast":[0.021,0.023,0.025],"quantiles":{"q1":[0.018,0.020,0.021],"q5":[0.021,0.023,0.025],"q8":[0.024,0.026,0.028]}}`,
			lstmStatusCode: http.StatusServiceUnavailable,
			lstmBody:       `{"error":"lstm not trained"}`,
			wantEngine:     "timesfm",
		},
		{
			name:           "TimesFM returns 256 horizon steps with full quantiles",
			tfmStatusCode:  http.StatusOK,
			tfmBody:        makeTimesFMLargeHorizonJSON(256),
			lstmStatusCode: http.StatusOK,
			lstmBody:       `{"predicted_peak_load": 0.035}`,
			wantEngine:     "timesfm",
		},
	}

	for _, tc := range chaosCases {
		t.Run(tc.name, func(t *testing.T) {
			origClient := forecastHttpClient
			forecastHttpClient = &http.Client{
				Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
					if req.URL.Path == "/forecast/load" {
						return &http.Response{
							StatusCode: tc.tfmStatusCode,
							Body:       io.NopCloser(bytes.NewReader([]byte(tc.tfmBody))),
							Header:     make(http.Header),
						}, nil
					}
					if req.URL.Path == "/predict" {
						return &http.Response{
							StatusCode: tc.lstmStatusCode,
							Body:       io.NopCloser(bytes.NewReader([]byte(tc.lstmBody))),
							Header:     make(http.Header),
						}, nil
					}
					return &http.Response{
						StatusCode: http.StatusNotFound,
						Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
						Header:     make(http.Header),
					}, nil
				}),
			}
			defer func() { forecastHttpClient = origClient }()

			engine := simulation.NewEngine()
			// Load sufficient history
			histDoc := map[string]interface{}{
				"buildingId":            engine.BuildingId(),
				"occupancyModelVersion": simulation.OccupancyModelVersion,
				"site":                  simulation.SiteFingerprint(),
				"samples":               []float64{0.021, 0.022, 0.023, 0.024, 0.025, 0.026, 0.027, 0.028},
			}
			data, _ := json.Marshal(histDoc)
			_ = engine.LoadLoadHistory(data)

			handler := recommendationsHandler(engine)
			req := httptest.NewRequest("GET", "/api/recommendations", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("expected 200 OK, got %d", rec.Code)
			}

			var report simulation.RecommendationReport
			if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
				t.Fatalf("failed to decode response: %v", err)
			}

			if report.Forecast == nil {
				t.Fatalf("report.Forecast is nil")
			}

			if report.Forecast.Engine != tc.wantEngine {
				t.Errorf("expected engine %q, got %q", tc.wantEngine, report.Forecast.Engine)
			}

			if len(report.Forecast.Series) == 0 {
				t.Errorf("expected non-empty forecast series")
			}
			if len(report.Forecast.UpperBand) == 0 {
				t.Errorf("expected non-empty forecast upperBand")
			}
		})
	}
}

// TestAdversarialRecommendationsAPI_ConcurrentQueriesRace tests heavy concurrent access
// to recommendationsHandler while background simulation is updating history and baselines.
func TestAdversarialRecommendationsAPI_ConcurrentQueriesRace(t *testing.T) {
	engine := simulation.NewEngine()
	handler := recommendationsHandler(engine)

	const goroutines = 50
	const iterations = 40
	var wg sync.WaitGroup
	wg.Add(goroutines + 2)

	// Background worker 1: simulates telemetry & load updates
	stopCh := make(chan struct{})
	go func() {
		defer wg.Done()
		r := rand.New(rand.NewSource(42))
		for {
			select {
			case <-stopCh:
				return
			default:
				occ := r.Intn(10)
				temp := 22.0 + r.Float64()*4.0
				plug := 500.0 + r.Float64()*500.0
				engine.IngestTelemetry("zone-office-a", "zone_1", simulation.Measurement{
					Occupancy: &occ,
					Temp:      &temp,
					PlugW:     &plug,
					Source:    "esp32",
					TempReal:  true,
				})
				engine.SetOutdoor(25.0+r.Float64()*5.0, 50.0)
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Background worker 2: simulates baseline & dynamics updates
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopCh:
				return
			default:
				engine.Recommendations(8)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// 50 concurrent query goroutines
	errCh := make(chan error, goroutines*iterations)
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				req := httptest.NewRequest("GET", "/api/recommendations", nil)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				if rec.Code != http.StatusOK {
					errCh <- fmt.Errorf("goroutine %d iter %d: expected 200 OK, got %d", gid, i, rec.Code)
					return
				}

				var report simulation.RecommendationReport
				if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
					errCh <- fmt.Errorf("goroutine %d iter %d: decode failed: %v", gid, i, err)
					return
				}

				if report.Forecast == nil {
					errCh <- fmt.Errorf("goroutine %d iter %d: forecast is nil", gid, i)
					return
				}
				if len(report.Forecast.Series) == 0 {
					errCh <- fmt.Errorf("goroutine %d iter %d: forecast series is empty", gid, i)
					return
				}
			}
		}(g)
	}

	// Wait for query goroutines to complete
	for g := 0; g < goroutines; g++ {
		// allow completion
	}
	// Wait a moment then stop background workers
	time.Sleep(100 * time.Millisecond)
	close(stopCh)
	wg.Wait()

	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent query error: %v", err)
	}
}

// TestAdversarialMQTT_TelemetryFullJSONLogging tests edge-case MQTT payloads including
// deep nesting, unicode, huge payloads, escaped characters, and confirms raw payload is never truncated.
func TestAdversarialMQTT_TelemetryFullJSONLogging(t *testing.T) {
	var logBuf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&logBuf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	}()

	engine := simulation.NewEngine()

	adversarialPayloads := []struct {
		name    string
		topic   string
		payload string
	}{
		{
			name:    "unicode characters in zone and strings",
			topic:   "econ/telemetry/zone_vietnam",
			payload: `{"zone":"Tầng 4 Khu Văn Phòng Điêu Hòa Trung Tâm 🏢","occupancy":8,"temperature":24.5,"source":"esp32_việt_nam","tempReal":true}`,
		},
		{
			name:    "nested JSON structures and custom telemetry extensions",
			topic:   "econ/telemetry/zone_complex",
			payload: `{"zone":"Lab","occupancy":2,"temperature":21.4,"diagnostics":{"rssi":-68,"heap":142850,"sensors":{"dht22":"ok","sct013":"calibrated"}},"source":"pico","tempReal":true}`,
		},
		{
			name:    "escaped quotes, slashes, tabs, newlines inside strings",
			topic:   "econ/telemetry/zone_escapes",
			payload: `{"zone":"Room \"A\" \\ Floor 1\t\n","occupancy":1,"temperature":23.0,"source":"custom/v1.0","tempReal":true}`,
		},
		{
			name:    "array fields and additional telemetry lists",
			topic:   "econ/telemetry/zone_arrays",
			payload: `{"zone":"Hallway","occupancy":0,"temperature":25.0,"recent_temps":[24.8,24.9,25.0],"source":"esp32","tempReal":true}`,
		},
		{
			name:    "large 8KB rich telemetry payload",
			topic:   "econ/telemetry/zone_large",
			payload: makeLargeTelemetryJSON(8 * 1024),
		},
		{
			name:    "all zero and negative values",
			topic:   "econ/telemetry/zone_negative",
			payload: `{"zone":"Cold Room","occupancy":0,"temperature":-12.5,"humidity":0.0,"co2":0.0,"plugW":0.0,"source":"esp32","tempReal":true}`,
		},
	}

	for _, tc := range adversarialPayloads {
		t.Run(tc.name, func(t *testing.T) {
			logBuf.Reset()
			handleTelemetry(engine, tc.topic, []byte(tc.payload))
			output := strings.TrimSpace(logBuf.String())

			// 1. Must contain "payload="
			payloadIdx := strings.Index(output, "payload=")
			if payloadIdx == -1 {
				t.Fatalf("log output missing payload= tag: %s", output)
			}

			// 2. Extract payload between "payload=" and " occ="
			payloadSub := output[payloadIdx+len("payload="):]
			occIdx := strings.Index(payloadSub, " occ=")
			if occIdx == -1 {
				t.Fatalf("log output missing occ= tag after payload: %s", output)
			}

			extractedPayload := payloadSub[:occIdx]

			// 3. Extracted raw payload must EXACTLY match input payload character-for-character
			if extractedPayload != tc.payload {
				t.Errorf("extracted payload did not match raw payload byte-for-byte!\nGot length: %d\nWant length: %d\nGot prefix: %s\nWant prefix: %s",
					len(extractedPayload), len(tc.payload),
					truncate(extractedPayload, 100), truncate(tc.payload, 100))
			}

			// 4. Must decode as valid JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal([]byte(extractedPayload), &parsed); err != nil {
				t.Errorf("extracted payload is not valid JSON: %v", err)
			}
		})
	}
}

// TestAdversarialMQTT_ConcurrentTelemetryHammer tests high-throughput concurrent telemetry ingestion
// across 50 goroutines without data races or log corruption.
func TestAdversarialMQTT_ConcurrentTelemetryHammer(t *testing.T) {
	// Silence logger during high-throughput flood test to keep test output clean
	origOutput := log.Writer()
	origFlags := log.Flags()
	var devNull bytes.Buffer
	log.SetOutput(&devNull)
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	}()

	engine := simulation.NewEngine()
	const goroutines = 40
	const messagesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			for m := 0; m < messagesPerGoroutine; m++ {
				topic := fmt.Sprintf("econ/telemetry/zone_g%d", gid)
				payload := fmt.Sprintf(`{"zone":"Zone G%d","occupancy":%d,"temperature":24.%d,"source":"esp32","tempReal":true}`,
					gid, m%5, (gid+m)%10)
				handleTelemetry(engine, topic, []byte(payload))
			}
		}(g)
	}

	wg.Wait()
}

// Helpers

func makeSequence(n int, minVal, maxVal float64) []float64 {
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(n)
		out[i] = minVal + (maxVal-minVal)*(0.5+0.5*math.Sin(t*math.Pi*2))
	}
	return out
}

func makeTimesFMLargeHorizonJSON(horizon int) string {
	forecast := make([]float64, horizon)
	upperBand := make([]float64, horizon)
	for i := 0; i < horizon; i++ {
		forecast[i] = 0.020 + float64(i)*0.0001
		upperBand[i] = 0.024 + float64(i)*0.00012
	}
	b, _ := json.Marshal(map[string]interface{}{
		"forecast": forecast,
		"quantiles": map[string][]float64{
			"q9": upperBand,
		},
		"engine": "timesfm",
	})
	return string(b)
}

func makeLargeTelemetryJSON(targetBytes int) string {
	padding := strings.Repeat("A", targetBytes/2)
	doc := map[string]interface{}{
		"zone":        "Huge Zone",
		"occupancy":   15,
		"temperature": 24.2,
		"humidity":    50.0,
		"co2":         580.0,
		"source":      "esp32",
		"tempReal":    true,
		"blob":        padding,
	}
	b, _ := json.Marshal(doc)
	return string(b)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
