package main

import (
	"bytes"
	"econ/simulation"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

type roundTripperFunc func(req *http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// TestRecommendationsApiReturnsForecastGraph verifies that GET /api/recommendations returns
// the full RecommendationReport with non-empty embedded ForecastGraphData.
func TestRecommendationsApiReturnsForecastGraph(t *testing.T) {
	engine := simulation.NewEngine()
	handler := recommendationsHandler(engine)

	req := httptest.NewRequest("GET", "/api/recommendations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	var report simulation.RecommendationReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode RecommendationReport: %v", err)
	}

	if report.Forecast == nil {
		t.Fatal("expected report.Forecast to be non-nil, got nil")
	}

	fc := report.Forecast
	if fc.Engine == "" {
		t.Error("expected non-empty fc.Engine")
	}
	if len(fc.Series) == 0 {
		t.Error("expected non-empty fc.Series")
	}
	if len(fc.UpperBand) == 0 {
		t.Error("expected non-empty fc.UpperBand")
	}
	if fc.StepMinutes != 5 {
		t.Errorf("expected StepMinutes = 5, got %d", fc.StepMinutes)
	}
	if fc.HorizonMinutes != len(fc.Series)*fc.StepMinutes {
		t.Errorf("expected HorizonMinutes = %d, got %d", len(fc.Series)*fc.StepMinutes, fc.HorizonMinutes)
	}
	if fc.PeakUpperMw == nil || *fc.PeakUpperMw <= 0 {
		t.Errorf("expected positive PeakUpperMw, got %v", fc.PeakUpperMw)
	}
	if fc.UpperQuantile == "" {
		t.Error("expected non-empty UpperQuantile")
	}
	if fc.Quantiles == nil || len(fc.Quantiles) == 0 {
		t.Error("expected non-empty Quantiles map")
	}
}

// TestRecommendationsApiWithTimesFMMock verifies that when TimesFM backend answers,
// GET /api/recommendations attaches the TimesFM zero-shot forecast, quantile spread, and LSTM peak.
func TestRecommendationsApiWithTimesFMMock(t *testing.T) {
	origClient := forecastHttpClient
	forecastHttpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/forecast/load" {
				resp := map[string]interface{}{
					"forecast": []float64{0.021, 0.023, 0.025, 0.027, 0.028, 0.030},
					"quantiles": map[string][]float64{
						"q1": {0.018, 0.019, 0.020, 0.022, 0.023, 0.024},
						"q5": {0.021, 0.023, 0.025, 0.027, 0.028, 0.030},
						"q9": {0.025, 0.028, 0.031, 0.033, 0.035, 0.037},
					},
					"engine": "timesfm",
				}
				body, _ := json.Marshal(resp)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}
			if req.URL.Path == "/predict" {
				resp := map[string]interface{}{
					"predicted_peak_load": 0.0295,
					"weather_source":      "live",
				}
				body, _ := json.Marshal(resp)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
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
	// Pre-load sufficient history (>= 8 samples) for TimesFM query
	histDoc := map[string]interface{}{
		"buildingId":            engine.BuildingId(),
		"occupancyModelVersion": simulation.OccupancyModelVersion,
		"site":                  simulation.SiteFingerprint(),
		"samples":               []float64{0.020, 0.021, 0.022, 0.023, 0.022, 0.024, 0.025, 0.026},
	}
	data, _ := json.Marshal(histDoc)
	if err := engine.LoadLoadHistory(data); err != nil {
		t.Fatalf("LoadLoadHistory failed: %v", err)
	}

	handler := recommendationsHandler(engine)
	req := httptest.NewRequest("GET", "/api/recommendations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	var report simulation.RecommendationReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if report.Forecast == nil {
		t.Fatal("expected report.Forecast to be non-nil")
	}

	fc := report.Forecast
	if fc.Engine != "timesfm" {
		t.Errorf("expected Engine 'timesfm', got %s", fc.Engine)
	}
	if len(fc.Series) != 6 {
		t.Errorf("expected 6 points in Series, got %d", len(fc.Series))
	}
	if fc.UpperQuantile != "q9" {
		t.Errorf("expected UpperQuantile 'q9', got %s", fc.UpperQuantile)
	}
	if fc.PeakUpperMw == nil || *fc.PeakUpperMw != 0.037 {
		t.Errorf("expected PeakUpperMw 0.037, got %v", fc.PeakUpperMw)
	}
	if fc.LstmPeakMw == nil || *fc.LstmPeakMw != 0.0295 {
		t.Errorf("expected LstmPeakMw 0.0295, got %v", fc.LstmPeakMw)
	}
	if len(fc.UpperBand) != 6 {
		t.Errorf("expected 6 points in UpperBand, got %d", len(fc.UpperBand))
	}
}

// TestRecommendationsApiWithLSTMMock verifies that when TimesFM is unavailable but LSTM answers,
// the forecast graph is constructed from the LSTM peak prediction.
func TestRecommendationsApiWithLSTMMock(t *testing.T) {
	origClient := forecastHttpClient
	forecastHttpClient = &http.Client{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			if req.URL.Path == "/forecast/load" {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       io.NopCloser(bytes.NewReader([]byte(`{"error":"TimesFM not ready"}`))),
					Header:     make(http.Header),
				}, nil
			}
			if req.URL.Path == "/predict" {
				resp := map[string]interface{}{
					"predicted_peak_load": 0.032,
					"weather_source":      "engine",
				}
				body, _ := json.Marshal(resp)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(bytes.NewReader(body)),
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
	handler := recommendationsHandler(engine)

	req := httptest.NewRequest("GET", "/api/recommendations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	var report simulation.RecommendationReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if report.Forecast == nil {
		t.Fatal("expected report.Forecast to be non-nil")
	}

	fc := report.Forecast
	if fc.Engine != "lstm" {
		t.Errorf("expected Engine 'lstm', got %s", fc.Engine)
	}
	if fc.LstmPeakMw == nil || *fc.LstmPeakMw != 0.032 {
		t.Errorf("expected LstmPeakMw 0.032, got %v", fc.LstmPeakMw)
	}
	if len(fc.Series) != 12 {
		t.Errorf("expected 12 points in Series, got %d", len(fc.Series))
	}
	if len(fc.UpperBand) != 12 {
		t.Errorf("expected 12 points in UpperBand, got %d", len(fc.UpperBand))
	}
}

// TestRecommendationsApiFallbackWhenOffline verifies that when the forecasting backend is offline,
// GET /api/recommendations returns 200 OK with a valid fallback forecast graph.
func TestRecommendationsApiFallbackWhenOffline(t *testing.T) {
	origURL := os.Getenv("FORECAST_URL")
	os.Setenv("FORECAST_URL", "http://127.0.0.1:59999") // unreachable port
	defer os.Setenv("FORECAST_URL", origURL)

	origClient := forecastHttpClient
	forecastHttpClient = nil
	defer func() { forecastHttpClient = origClient }()

	engine := simulation.NewEngine()
	handler := recommendationsHandler(engine)

	req := httptest.NewRequest("GET", "/api/recommendations", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	var report simulation.RecommendationReport
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if report.Forecast == nil {
		t.Fatal("expected report.Forecast to be non-nil")
	}

	fc := report.Forecast
	if fc.Engine != "fallback" {
		t.Errorf("expected Engine 'fallback', got %s", fc.Engine)
	}
	if len(fc.Series) != 12 {
		t.Errorf("expected 12 points in Series, got %d", len(fc.Series))
	}
	if len(fc.UpperBand) != 12 {
		t.Errorf("expected 12 points in UpperBand, got %d", len(fc.UpperBand))
	}
	if fc.StepMinutes != 5 {
		t.Errorf("expected StepMinutes 5, got %d", fc.StepMinutes)
	}
	if fc.HorizonMinutes != 60 {
		t.Errorf("expected HorizonMinutes 60, got %d", fc.HorizonMinutes)
	}
}
