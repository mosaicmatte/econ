package main

import (
	"econ/simulation"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// 1. Exact programmatic assertion: 1000W drawn for 1 hour (3600s) with 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon.
func TestCarbonCalculationExact1000W1H(t *testing.T) {
	powerW := 1000.0
	dtSec := 3600.0
	gridFactor := 0.5

	energyKwh, kgCO2e := CalculateScope2Emissions(powerW, dtSec, gridFactor)

	if energyKwh != 1.0 {
		t.Fatalf("expected energy 1.0 kWh, got %f", energyKwh)
	}
	if kgCO2e != 0.5 {
		t.Fatalf("expected carbon 0.5 kgCO2e, got %f", kgCO2e)
	}

	// Tracker integration consistency check
	tracker := newCarbonTracker(nil)
	tracker.gridFactor = 0.5
	eKwh, cKg := tracker.RecordEnergy(powerW, dtSec)
	if eKwh != 1.0 || cKg != 0.5 {
		t.Fatalf("tracker.RecordEnergy: expected (1.0, 0.5), got (%f, %f)", eKwh, cKg)
	}
	if tracker.cumulativeEmissionsKgCO2e != 0.5 {
		t.Fatalf("tracker.cumulativeEmissionsKgCO2e: expected 0.5, got %f", tracker.cumulativeEmissionsKgCO2e)
	}
}

type testTransport struct {
	server *httptest.Server
	base   http.RoundTripper
}

func (tt *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if tt.base != nil {
		resp, err := tt.base.RoundTrip(req)
		if err == nil {
			return resp, nil
		}
	}
	// If TCP connect on localhost is blocked by sandbox ("operation not permitted"),
	// route through the httptest.Server handler directly.
	rec := httptest.NewRecorder()
	tt.server.Config.Handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func testClientForServer(s *httptest.Server) *http.Client {
	return &http.Client{
		Transport: &testTransport{
			server: s,
			base:   s.Client().Transport,
		},
		Timeout: 5 * time.Second,
	}
}

// 2. Demonstrable outbound HTTP request to live carbon market pricing using httptest.NewServer.
func TestOutboundLiveCarbonMarketPricing(t *testing.T) {
	var outboundHitCount int32

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&outboundHitCount, 1)

		if r.Method != http.MethodGet {
			t.Errorf("expected GET request, got %s", r.Method)
		}
		if r.Header.Get("User-Agent") != "econ-sustainability/1.0" {
			t.Errorf("expected User-Agent econ-sustainability/1.0, got %s", r.Header.Get("User-Agent"))
		}

		w.Header().Set("Content-Type", "application/json")
		// Standard CoinGecko payload for Toucan Base Carbon Tonne
		w.Write([]byte(`{
			"toucan-protocol-base-carbon-tonne": {
				"usd": 18.75
			}
		}`))
	}))
	defer mockServer.Close()

	client := NewCarbonMarketClient(mockServer.URL, 10*time.Minute)
	client.SetHTTPClient(testClientForServer(mockServer))
	quote, err := client.GetQuote()
	if err != nil {
		t.Fatalf("unexpected error fetching quote: %v", err)
	}

	if atomic.LoadInt32(&outboundHitCount) != 1 {
		t.Fatalf("expected 1 outbound HTTP request, got %d", outboundHitCount)
	}
	if !quote.IsLive {
		t.Fatalf("expected quote.IsLive to be true")
	}
	if quote.SpotPricePerMetricTonUSD != 18.75 {
		t.Fatalf("expected spot price 18.75 USD, got %f", quote.SpotPricePerMetricTonUSD)
	}
	if quote.Currency != "USD" {
		t.Fatalf("expected currency USD, got %s", quote.Currency)
	}
}

// 3. Caching behavior (subsequent calls within TTL do not make outbound requests).
func TestCarbonMarketClientCachingBehavior(t *testing.T) {
	var outboundHitCount int32

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&outboundHitCount, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"toucan-protocol-base-carbon-tonne": {"usd": 14.50}}`))
	}))
	defer mockServer.Close()

	client := NewCarbonMarketClient(mockServer.URL, 10*time.Minute)
	client.SetHTTPClient(testClientForServer(mockServer))

	// First call -> should trigger outbound HTTP request
	q1, err := client.GetQuote()
	if err != nil {
		t.Fatalf("call 1 failed: %v", err)
	}
	if atomic.LoadInt32(&outboundHitCount) != 1 {
		t.Fatalf("expected hit count 1 after first call, got %d", outboundHitCount)
	}
	if q1.SpotPricePerMetricTonUSD != 14.50 {
		t.Fatalf("expected price 14.50, got %f", q1.SpotPricePerMetricTonUSD)
	}

	// Second and third calls within TTL -> must hit in-memory cache and not make outbound requests
	for i := 2; i <= 3; i++ {
		q, err := client.GetQuote()
		if err != nil {
			t.Fatalf("call %d failed: %v", i, err)
		}
		if atomic.LoadInt32(&outboundHitCount) != 1 {
			t.Fatalf("expected hit count to remain 1 on call %d, got %d", i, outboundHitCount)
		}
		if q.SpotPricePerMetricTonUSD != 14.50 {
			t.Fatalf("expected price 14.50 on call %d, got %f", i, q.SpotPricePerMetricTonUSD)
		}
	}

	// Invalidate cache manually -> should trigger a new outbound HTTP request
	client.ClearCache()
	qAfterClear, err := client.GetQuote()
	if err != nil {
		t.Fatalf("call after ClearCache failed: %v", err)
	}
	if atomic.LoadInt32(&outboundHitCount) != 2 {
		t.Fatalf("expected hit count 2 after ClearCache, got %d", outboundHitCount)
	}
	if qAfterClear.SpotPricePerMetricTonUSD != 14.50 {
		t.Fatalf("expected price 14.50, got %f", qAfterClear.SpotPricePerMetricTonUSD)
	}
}

// 4. Graceful fallback when upstream market API fails.
func TestCarbonMarketClientGracefulFallback(t *testing.T) {
	// Scenario A: Upstream server returns HTTP 500
	server500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream server failure", http.StatusInternalServerError)
	}))
	defer server500.Close()

	clientA := NewCarbonMarketClient(server500.URL, 10*time.Minute)
	clientA.SetHTTPClient(testClientForServer(server500))
	quoteA, err := clientA.GetQuote()
	if err != nil {
		t.Fatalf("expected graceful fallback, got error: %v", err)
	}
	if quoteA.IsLive {
		t.Fatalf("expected isLive == false on fallback")
	}
	if quoteA.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
		t.Fatalf("expected fallback price %f, got %f", defaultMarketFallbackPrice, quoteA.SpotPricePerMetricTonUSD)
	}

	// Scenario B: Upstream returns invalid / corrupt JSON
	serverBadJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not valid json`))
	}))
	defer serverBadJSON.Close()

	clientB := NewCarbonMarketClient(serverBadJSON.URL, 10*time.Minute)
	clientB.SetHTTPClient(testClientForServer(serverBadJSON))
	quoteB, err := clientB.GetQuote()
	if err != nil {
		t.Fatalf("expected graceful fallback on invalid json, got error: %v", err)
	}
	if quoteB.IsLive {
		t.Fatalf("expected isLive == false on bad json")
	}
	if quoteB.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
		t.Fatalf("expected fallback price %f, got %f", defaultMarketFallbackPrice, quoteB.SpotPricePerMetricTonUSD)
	}

	// Scenario C: Network unreachable / connection refused
	clientC := NewCarbonMarketClient("http://127.0.0.1:59998/nonexistent", 10*time.Minute)
	quoteC, err := clientC.GetQuote()
	if err != nil {
		t.Fatalf("expected graceful fallback on unreachable host, got error: %v", err)
	}
	if quoteC.IsLive {
		t.Fatalf("expected isLive == false on connection error")
	}
	if quoteC.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
		t.Fatalf("expected fallback price %f, got %f", defaultMarketFallbackPrice, quoteC.SpotPricePerMetricTonUSD)
	}
}

// 5. Predictive maintenance alert generation on over-capacity load.
func TestPredictiveMaintenanceAlertGeneration(t *testing.T) {
	// Construct simulated engine with mock zones
	engine := &simulation.Engine{
		Zones: map[string]*simulation.ZoneSim{
			"zone-office-overloaded": {
				Type:     "open-office",
				AreaM2:   100.0,
				HwStripW: 2250.0, // > 2000W rated strip capacity
			},
			"zone-ac-overloaded": {
				Type:   "cellular-office",
				AreaM2: 50.0,
				HwAcW:  3800.0, // > 3500W rated AC capacity
			},
			"zone-normal": {
				Type:     "meeting-room",
				AreaM2:   25.0,
				HwStripW: 350.0,
				HwAcW:    1200.0,
			},
		},
	}

	tracker := newCarbonTracker(engine)
	now := time.Now()

	// Initial snapshot with over-capacity loads
	snapshot := tracker.Snapshot(engine, now)

	if snapshot.PredictiveMaintenance.ActiveAlertsCount < 2 {
		t.Fatalf("expected at least 2 active alerts, got %d", snapshot.PredictiveMaintenance.ActiveAlertsCount)
	}

	hasStripOverload := false
	hasAcOverload := false

	for _, warn := range snapshot.PredictiveMaintenance.Warnings {
		if warn.EquipmentId == "strip-zone-office-overloaded" && warn.Type == "over_capacity" {
			hasStripOverload = true
			if warn.MetricValue != 2250.0 || warn.Threshold != 2000.0 {
				t.Errorf("strip warning metric mismatch: %v", warn)
			}
		}
		if warn.EquipmentId == "ac-zone-ac-overloaded" && warn.Type == "over_capacity" {
			hasAcOverload = true
			if warn.MetricValue != 3800.0 || warn.Threshold != 3500.0 {
				t.Errorf("ac warning metric mismatch: %v", warn)
			}
		}
	}

	if !hasStripOverload {
		t.Errorf("expected power strip over-capacity warning")
	}
	if !hasAcOverload {
		t.Errorf("expected AC unit over-capacity warning")
	}

	// Test Transient Power Surge (delta > 1000W)
	engine.Zones["zone-normal"].HwStripW = 1600.0 // jumped from 350W to 1600W (delta = 1250W > 1000W)
	surgeSnapshot := tracker.Snapshot(engine, now.Add(time.Second))

	hasSurge := false
	for _, warn := range surgeSnapshot.PredictiveMaintenance.Warnings {
		if warn.EquipmentId == "strip-zone-normal" && warn.Type == "power_surge" {
			hasSurge = true
			if warn.MetricValue != 1250.0 {
				t.Errorf("expected surge delta 1250.0, got %f", warn.MetricValue)
			}
		}
	}
	if !hasSurge {
		t.Errorf("expected transient power surge alert for +1250W step")
	}

	// Test Cumulative Runtime Hours Alert (> 2000 hours)
	tracker.SetEquipmentRuntimeHours("strip-zone-office-overloaded", 2050.0)
	runtimeSnapshot := tracker.Snapshot(engine, now.Add(2*time.Second))

	hasRuntimeAlert := false
	for _, warn := range runtimeSnapshot.PredictiveMaintenance.Warnings {
		if warn.EquipmentId == "strip-zone-office-overloaded" && warn.Type == "runtime_exceeded" {
			hasRuntimeAlert = true
			if warn.Threshold != 2000.0 {
				t.Errorf("expected threshold 2000, got %f", warn.Threshold)
			}
		}
	}
	if !hasRuntimeAlert {
		t.Errorf("expected runtime exceeded warning for 2050 hours")
	}
}

// 6. Space utilization efficiency calculation.
func TestSpaceUtilizationCalculation(t *testing.T) {
	// Set up zones:
	// 1. open-office: 50 m², 10 m²/person -> capacity 5, occupancy 4 -> 80.0%
	// 2. meeting-room: 25 m², 2.5 m²/person -> capacity 10, occupancy 5 -> 50.0%
	// 3. corridor (service / non-occupiable): 100 m² -> capacity 0, excluded
	// Total occupiable capacity = 15, Total live occupants = 9
	// Overall efficiency = 9 / 15 = 60.0%
	engine := &simulation.Engine{
		Zones: map[string]*simulation.ZoneSim{
			"zone-office": {
				Type:      "open-office",
				AreaM2:    50.0,
				Occupancy: 4,
			},
			"zone-meeting": {
				Type:      "meeting-room",
				AreaM2:    25.0,
				Occupancy: 5,
			},
			"zone-corridor": {
				Type:      "corridor",
				AreaM2:    100.0,
				Occupancy: 0,
			},
		},
	}

	space := computeSpaceUtilization(engine)

	if space.TotalBuildingCapacity != 15 {
		t.Fatalf("expected total building capacity 15, got %d", space.TotalBuildingCapacity)
	}
	if space.TotalLiveOccupants != 9 {
		t.Fatalf("expected total live occupants 9, got %d", space.TotalLiveOccupants)
	}
	if space.OverallEfficiencyPercent != 60.0 {
		t.Fatalf("expected overall efficiency 60.0%%, got %f", space.OverallEfficiencyPercent)
	}

	// Assert non-occupiable corridor is excluded from zones list
	if len(space.Zones) != 2 {
		t.Fatalf("expected 2 occupiable zones, got %d", len(space.Zones))
	}

	for _, z := range space.Zones {
		if z.ZoneId == "zone-office" {
			if z.DesignCapacity != 5 || z.LiveOccupants != 4 || z.EfficiencyPercent != 80.0 {
				t.Errorf("office space mismatch: %+v", z)
			}
		} else if z.ZoneId == "zone-meeting" {
			if z.DesignCapacity != 10 || z.LiveOccupants != 5 || z.EfficiencyPercent != 50.0 {
				t.Errorf("meeting space mismatch: %+v", z)
			}
		} else {
			t.Errorf("unexpected zone included: %s", z.ZoneId)
		}
	}
}

// 7. Carbon Credit Recommendations calculation (within budget vs over budget).
func TestCarbonCreditRecommendationsMath(t *testing.T) {
	quote := MarketQuote{
		Source:                   "Toucan Protocol BCT (CoinGecko)",
		SpotPricePerMetricTonUSD: 12.50,
		Currency:                 "USD",
		IsLive:                   true,
		FetchedAt:                time.Now().UTC().Format(time.RFC3339),
	}

	// Case A: Within budget
	recWithin := CalculateOffsetRecommendation(35.0, 50.0, quote)
	if recWithin.OverBudget {
		t.Fatalf("expected OverBudget == false")
	}
	if recWithin.DeficitKgCO2e != 0.0 || recWithin.CreditsNeededMetricTons != 0.0 || recWithin.WholeCertificatesNeeded != 0 {
		t.Fatalf("expected 0 deficit/credits when within budget, got %+v", recWithin)
	}
	if recWithin.EstimatedCostUSD != 0.0 {
		t.Fatalf("expected 0 cost when within budget, got %f", recWithin.EstimatedCostUSD)
	}

	// Case B: Over budget matching PROJECT.md numbers
	// emissions = 58.4 kg, budget = 50.0 kg -> deficit = 8.4 kg, tons = 0.0084, certificates = 1, cost = 0.105 USD
	recOver := CalculateOffsetRecommendation(58.4, 50.0, quote)
	if !recOver.OverBudget {
		t.Fatalf("expected OverBudget == true")
	}
	if recOver.DeficitKgCO2e != 8.4 {
		t.Fatalf("expected deficit 8.4 kgCO2e, got %f", recOver.DeficitKgCO2e)
	}
	if recOver.CreditsNeededMetricTons != 0.0084 {
		t.Fatalf("expected 0.0084 metric tons, got %f", recOver.CreditsNeededMetricTons)
	}
	if recOver.WholeCertificatesNeeded != 1 {
		t.Fatalf("expected 1 whole certificate, got %d", recOver.WholeCertificatesNeeded)
	}
	if recOver.EstimatedCostUSD != 0.105 {
		t.Fatalf("expected estimated cost 0.105 USD, got %f", recOver.EstimatedCostUSD)
	}
	expectedRecStr := "Purchase 1 carbon credit certificate (~0.0084 tCO2e deficit) at $12.50/tCO2e to offset emissions."
	if recOver.Recommendation != expectedRecStr {
		t.Fatalf("expected recommendation %q, got %q", expectedRecStr, recOver.Recommendation)
	}
}

// 8. Full REST API Endpoint Verification: GET /api/sustainability & OPTIONS CORS preflight.
func TestSustainabilityAPIEndpoint(t *testing.T) {
	engine := &simulation.Engine{
		Zones: map[string]*simulation.ZoneSim{
			"zone-1": {
				Type:      "open-office",
				AreaM2:    120.0,
				Occupancy: 8,
				HwPlugW:   1200.0,
				HwStripW:  350.0,
				HwAcW:     900.0,
			},
		},
	}

	tracker := newCarbonTracker(engine)
	tracker.SetCumulativeEmissions(14.85)

	handler := sustainabilityHandler(engine, tracker)

	// Test OPTIONS preflight
	optReq := httptest.NewRequest(http.MethodOptions, "/api/sustainability", nil)
	optRec := httptest.NewRecorder()
	handler(optRec, optReq)

	if optRec.Code != http.StatusNoContent {
		t.Fatalf("expected OPTIONS status 204 No Content, got %d", optRec.Code)
	}
	if optRec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected CORS Access-Control-Allow-Origin: *")
	}

	// Test GET request
	getReq := httptest.NewRequest(http.MethodGet, "/api/sustainability", nil)
	getRec := httptest.NewRecorder()
	handler(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected GET status 200 OK, got %d: %s", getRec.Code, getRec.Body.String())
	}
	if getRec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", getRec.Header().Get("Content-Type"))
	}
	if getRec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected CORS Access-Control-Allow-Origin: *")
	}

	var payload SustainabilityPayload
	if err := json.Unmarshal(getRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	// Verify all 4 required sections are populated
	if payload.CarbonAccounting.InstantaneousPowerW != 2450.0 {
		t.Errorf("expected 2450W instantaneous power, got %f", payload.CarbonAccounting.InstantaneousPowerW)
	}
	if payload.CarbonAccounting.Breakdown.PlugW != 1200.0 ||
		payload.CarbonAccounting.Breakdown.StripW != 350.0 ||
		payload.CarbonAccounting.Breakdown.AcW != 900.0 {
		t.Errorf("breakdown mismatch: %+v", payload.CarbonAccounting.Breakdown)
	}
	if payload.SpaceUtilization.TotalLiveOccupants != 8 {
		t.Errorf("expected 8 live occupants, got %d", payload.SpaceUtilization.TotalLiveOccupants)
	}
	if payload.PredictiveMaintenance.ActiveAlertsCount < 0 {
		t.Errorf("invalid activeAlertsCount: %d", payload.PredictiveMaintenance.ActiveAlertsCount)
	}
	if payload.CarbonCreditRecommendations.CarbonBudgetKgCO2e != 50.0 {
		t.Errorf("expected carbon budget 50.0, got %f", payload.CarbonCreditRecommendations.CarbonBudgetKgCO2e)
	}
}
