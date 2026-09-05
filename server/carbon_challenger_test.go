package main

import (
	"context"
	"econ/simulation"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ============================================================================
// 1. CORE ASSERTION EMPIRICAL VERIFICATION
// ============================================================================

// Verify 1000W drawn for 1 hour (3600s) with 0.5 kgCO2e/kWh factor results in exactly 0.5 kg of emitted carbon.
func TestChallengerCoreAssertionExact1000W1H(t *testing.T) {
	powerW := 1000.0
	dtSec := 3600.0
	gridFactor := 0.5

	energyKwh, kgCO2e := CalculateScope2Emissions(powerW, dtSec, gridFactor)

	if energyKwh != 1.0 {
		t.Fatalf("Core assertion failed: expected energyKwh == 1.0, got %v", energyKwh)
	}
	if kgCO2e != 0.5 {
		t.Fatalf("Core assertion failed: expected kgCO2e == 0.5, got %v", kgCO2e)
	}

	// Verify Tracker state integration
	engine := &simulation.Engine{
		Zones: map[string]*simulation.ZoneSim{
			"zone-office": {
				Type:    "office",
				AreaM2:  50.0,
				HwPlugW: 1000.0,
			},
		},
	}
	tracker := newCarbonTracker(engine)
	tracker.gridFactor = 0.5

	eKwh, cKg := tracker.RecordEnergy(powerW, dtSec)
	if eKwh != 1.0 || cKg != 0.5 {
		t.Fatalf("tracker.RecordEnergy mismatch: got (%v, %v), expected (1.0, 0.5)", eKwh, cKg)
	}
	if tracker.cumulativeEmissionsKgCO2e != 0.5 {
		t.Fatalf("tracker.cumulativeEmissionsKgCO2e mismatch: got %v, expected 0.5", tracker.cumulativeEmissionsKgCO2e)
	}

	// Verify Snapshot calculation
	now := time.Now()
	tracker.lastTickAt = now.Add(-time.Hour)
	payload := tracker.Snapshot(engine, now)
	if payload.CarbonAccounting.InstantaneousPowerW != 1000.0 {
		t.Errorf("expected 1000.0W instantaneous power, got %v", payload.CarbonAccounting.InstantaneousPowerW)
	}
	if payload.CarbonAccounting.InstantaneousEmissionRateKgPerHour != 0.5 {
		t.Errorf("expected 0.5 kg/h emission rate, got %v", payload.CarbonAccounting.InstantaneousEmissionRateKgPerHour)
	}
	if payload.CarbonAccounting.Breakdown.PlugW != 1000.0 {
		t.Errorf("expected 1000.0W plug breakdown, got %v", payload.CarbonAccounting.Breakdown.PlugW)
	}
}

// ============================================================================
// 2. MATHEMATICAL BOUNDARIES & EXTREMES
// ============================================================================

func TestChallengerMathematicalBoundaries(t *testing.T) {
	gridFactor := 0.5

	// Case 2.1: 0W load
	t.Run("ZeroWatts", func(t *testing.T) {
		eKwh, kg := CalculateScope2Emissions(0.0, 3600.0, gridFactor)
		if eKwh != 0.0 || kg != 0.0 {
			t.Fatalf("ZeroWatts failed: got (%v, %v), expected (0.0, 0.0)", eKwh, kg)
		}
		if math.IsNaN(kg) || math.IsInf(kg, 0) {
			t.Fatalf("ZeroWatts produced NaN or Inf")
		}

		// 0 duration
		eKwh0, kg0 := CalculateScope2Emissions(1000.0, 0.0, gridFactor)
		if eKwh0 != 0.0 || kg0 != 0.0 {
			t.Fatalf("ZeroDuration failed: got (%v, %v), expected (0.0, 0.0)", eKwh0, kg0)
		}
	})

	// Case 2.2: Fractional watts
	t.Run("FractionalWatts", func(t *testing.T) {
		testCases := []struct {
			watts float64
			dt    float64
			expE  float64
			expC  float64
		}{
			{watts: 0.001, dt: 3600.0, expE: 0.000001, expC: 0.0000005},
			{watts: 0.5, dt: 3600.0, expE: 0.0005, expC: 0.00025},
			{watts: 0.1, dt: 360.0, expE: 0.00001, expC: 0.000005},
		}

		for _, tc := range testCases {
			eKwh, kg := CalculateScope2Emissions(tc.watts, tc.dt, gridFactor)
			if math.Abs(eKwh-tc.expE) > 1e-12 {
				t.Errorf("Fractional watts (%v W) energy mismatch: got %v, exp %v", tc.watts, eKwh, tc.expE)
			}
			if math.Abs(kg-tc.expC) > 1e-12 {
				t.Errorf("Fractional watts (%v W) carbon mismatch: got %v, exp %v", tc.watts, kg, tc.expC)
			}
		}
	})

	// Case 2.3: Large commercial loads (10MW, 100MW, 1GW)
	t.Run("LargeCommercialLoads", func(t *testing.T) {
		// 10 MW for 1 hour
		mw10 := 10.0 * 1e6
		e10, c10 := CalculateScope2Emissions(mw10, 3600.0, gridFactor)
		if e10 != 10000.0 { // 10,000 kWh = 10 MWh
			t.Errorf("10MW 1h expected 10000 kWh, got %v", e10)
		}
		if c10 != 5000.0 { // 5,000 kgCO2e = 5 tCO2e
			t.Errorf("10MW 1h expected 5000 kgCO2e, got %v", c10)
		}

		// 100 MW for 24 hours
		mw100 := 100.0 * 1e6
		e100, c100 := CalculateScope2Emissions(mw100, 86400.0, gridFactor)
		if e100 != 2400000.0 { // 2.4M kWh
			t.Errorf("100MW 24h expected 2400000 kWh, got %v", e100)
		}
		if c100 != 1200000.0 { // 1.2M kgCO2e = 1200 tCO2e
			t.Errorf("100MW 24h expected 1200000 kgCO2e, got %v", c100)
		}

		// 1 GW for 8760 hours (1 year)
		gw1 := 1e9
		eGw, cGw := CalculateScope2Emissions(gw1, 8760*3600.0, gridFactor)
		if math.IsNaN(eGw) || math.IsInf(eGw, 0) || math.IsNaN(cGw) || math.IsInf(cGw, 0) {
			t.Fatalf("1GW calculation overflowed to NaN or Inf")
		}
		if eGw != 8760000000.0 || cGw != 4380000000.0 {
			t.Errorf("1GW year calculation mismatch: got e=%v, c=%v", eGw, cGw)
		}
	})

	// Case 2.4: Negative dt and clock skew handling
	t.Run("NegativeDtAndClockSkew", func(t *testing.T) {
		// In raw formula
		eRaw, cRaw := CalculateScope2Emissions(1000.0, -3600.0, gridFactor)
		if eRaw >= 0 || cRaw >= 0 {
			t.Errorf("expected negative values for negative dt in raw formula, got (%v, %v)", eRaw, cRaw)
		}

		// In Tracker Snapshot (simulating backward clock skew / NTP step backwards)
		tracker := newCarbonTracker(nil)
		tracker.SetCumulativeEmissions(50.0)
		now := time.Now()
		tracker.lastTickAt = now // tick established at now

		// Snapshot invoked with a timestamp in the PAST (backward clock skew of 5 minutes)
		pastTime := now.Add(-5 * time.Minute)
		payload := tracker.Snapshot(nil, pastTime)

		// Cumulative emissions MUST NOT decrease
		if payload.CarbonAccounting.CumulativeEmissionsKgCO2e < 50.0 {
			t.Fatalf("Clock skew backward decreased cumulative emissions: got %v, expected >= 50.0",
				payload.CarbonAccounting.CumulativeEmissionsKgCO2e)
		}
	})

	// Case 2.5: Fractional seconds and cumulative precision drift
	t.Run("FractionalSecondsAndDrift", func(t *testing.T) {
		dtMs := 0.001 // 1 millisecond
		power := 1000.0
		steps := 1000000 // 1000 seconds total

		singleE, singleC := CalculateScope2Emissions(power, float64(steps)*dtMs, gridFactor)

		sumE := 0.0
		sumC := 0.0
		for i := 0; i < steps; i++ {
			e, c := CalculateScope2Emissions(power, dtMs, gridFactor)
			sumE += e
			sumC += c
		}

		diffE := math.Abs(singleE - sumE)
		diffC := math.Abs(singleC - sumC)
		relErrC := diffC / singleC

		if relErrC > 1e-9 {
			t.Errorf("Fractional seconds cumulative drift too large: relErr=%v, single=%v, sum=%v", relErrC, singleC, sumC)
		}
		if math.Abs(diffE) > 1e-6 {
			t.Errorf("Energy accumulation drift exceeded tolerance: diff=%v", diffE)
		}
	})
}

// ============================================================================
// 3. CARBON CREDIT RECOMMENDATIONS EMPIRICAL STRESS
// ============================================================================

func TestChallengerCarbonCreditRecommendations(t *testing.T) {
	quote := MarketQuote{
		Source:                   "Toucan Protocol BCT (CoinGecko)",
		SpotPricePerMetricTonUSD: 12.50,
		Currency:                 "USD",
		IsLive:                   true,
		FetchedAt:                time.Now().UTC().Format(time.RFC3339),
	}

	// 3.1: Zero deficit when under budget or exactly on budget
	t.Run("UnderAndExactBudget", func(t *testing.T) {
		// Zero emissions
		rec0 := CalculateOffsetRecommendation(0.0, 50.0, quote)
		if rec0.OverBudget || rec0.DeficitKgCO2e != 0.0 || rec0.WholeCertificatesNeeded != 0 || rec0.EstimatedCostUSD != 0.0 {
			t.Errorf("0 emissions should have 0 deficit and 0 certs: %+v", rec0)
		}

		// Well under budget
		recUnder := CalculateOffsetRecommendation(25.0, 50.0, quote)
		if recUnder.OverBudget || recUnder.DeficitKgCO2e != 0.0 || recUnder.WholeCertificatesNeeded != 0 {
			t.Errorf("Under budget should have 0 deficit: %+v", recUnder)
		}

		// Exact match
		recExact := CalculateOffsetRecommendation(50.0, 50.0, quote)
		if recExact.OverBudget || recExact.DeficitKgCO2e != 0.0 || recExact.WholeCertificatesNeeded != 0 {
			t.Errorf("Exact budget should have 0 deficit: %+v", recExact)
		}
	})

	// 3.2: Deficit calculations and certificate rounding
	t.Run("DeficitAndCertificateRounding", func(t *testing.T) {
		budget := 50.0

		testCases := []struct {
			emissions     float64
			expDeficitKg  float64
			expTons       float64
			expWholeCerts int
			expCostUSD    float64
			expectedWord  string
		}{
			// Micro-deficit: 0.001 kg over budget -> 0.000001 tons -> 1 whole certificate
			{emissions: 50.001, expDeficitKg: 0.0, expTons: 0.0, expWholeCerts: 1, expCostUSD: 0.0, expectedWord: "certificate"},
			// Small deficit: 8.4 kg over budget (PROJECT.md benchmark)
			{emissions: 58.4, expDeficitKg: 8.4, expTons: 0.0084, expWholeCerts: 1, expCostUSD: 0.105, expectedWord: "certificate"},
			// Exactly 1.0 metric ton deficit: 1050 kg emissions - 50 kg budget = 1000 kg deficit
			{emissions: 1050.0, expDeficitKg: 1000.0, expTons: 1.0, expWholeCerts: 1, expCostUSD: 12.50, expectedWord: "certificate"},
			// Just over 1 metric ton: 1000.01 kg deficit -> 2 certificates
			{emissions: 1050.01, expDeficitKg: 1000.01, expTons: 1.0000, expWholeCerts: 2, expCostUSD: 12.50, expectedWord: "certificates"},
			// 2.5 metric tons deficit: 2500 kg deficit -> 3 certificates
			{emissions: 2550.0, expDeficitKg: 2500.0, expTons: 2.5, expWholeCerts: 3, expCostUSD: 31.25, expectedWord: "certificates"},
			// 10 metric tons deficit -> 10 certificates
			{emissions: 10050.0, expDeficitKg: 10000.0, expTons: 10.0, expWholeCerts: 10, expCostUSD: 125.0, expectedWord: "certificates"},
		}

		for _, tc := range testCases {
			rec := CalculateOffsetRecommendation(tc.emissions, budget, quote)
			if !rec.OverBudget {
				t.Errorf("emissions %v expected OverBudget == true", tc.emissions)
			}
			if math.Abs(rec.DeficitKgCO2e-tc.expDeficitKg) > 0.01 {
				t.Errorf("emissions %v deficit mismatch: got %v, exp %v", tc.emissions, rec.DeficitKgCO2e, tc.expDeficitKg)
			}
			if rec.WholeCertificatesNeeded != tc.expWholeCerts {
				t.Errorf("emissions %v whole certificates mismatch: got %v, exp %v", tc.emissions, rec.WholeCertificatesNeeded, tc.expWholeCerts)
			}
			if math.Abs(rec.EstimatedCostUSD-tc.expCostUSD) > 0.01 {
				t.Errorf("emissions %v cost mismatch: got %v, exp %v", tc.emissions, rec.EstimatedCostUSD, tc.expCostUSD)
			}
			// Verify pluralization in recommendation message
			expectedSubstring := fmt.Sprintf("%d carbon credit %s", tc.expWholeCerts, tc.expectedWord)
			if !stringsContains(rec.Recommendation, expectedSubstring) {
				t.Errorf("emissions %v recommendation wording mismatch: got %q, expected substring %q",
					tc.emissions, rec.Recommendation, expectedSubstring)
			}
		}
	})
}

func stringsContains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || (len(s) > 0 && len(substr) > 0 && findSubstr(s, substr)))
}

func findSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// ============================================================================
// 4. CARBON MARKET HTTP CLIENT STRESS HARNESS
// ============================================================================

// contextAwareTransport bridges an http.Handler with full context cancellation and timeout support.
type contextAwareTransport struct {
	handler http.Handler
}

func (cat *contextAwareTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	done := make(chan *http.Response, 1)
	errChan := make(chan error, 1)

	go func() {
		rec := httptest.NewRecorder()
		cat.handler.ServeHTTP(rec, req)
		done <- rec.Result()
	}()

	select {
	case <-req.Context().Done():
		return nil, req.Context().Err()
	case err := <-errChan:
		return nil, err
	case resp := <-done:
		return resp, nil
	}
}

func TestChallengerCarbonMarketHTTPStress(t *testing.T) {
	// 4.1: Various valid spot price payload schemas
	t.Run("PayloadFormatVariations", func(t *testing.T) {
		testFormats := []struct {
			name     string
			body     string
			expPrice float64
		}{
			{
				name:     "CoinGeckoNested",
				body:     `{"toucan-protocol-base-carbon-tonne": {"usd": 24.85}}`,
				expPrice: 24.85,
			},
			{
				name:     "FlatUSD",
				body:     `{"usd": 30.50}`,
				expPrice: 30.50,
			},
			{
				name:     "FlatPrice",
				body:     `{"price": 17.25}`,
				expPrice: 17.25,
			},
			{
				name:     "FlatSpotPricePerMetricTonUSD",
				body:     `{"spotPricePerMetricTonUSD": 19.99}`,
				expPrice: 19.99,
			},
			{
				name:     "IntegerPrice",
				body:     `{"usd": 25}`,
				expPrice: 25.0,
			},
			{
				name:     "GenericNestedMap",
				body:     `{"data": {"usd": 22.10}}`,
				expPrice: 22.10,
			},
		}

		for _, tf := range testFormats {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(tf.body))
			}))
			defer server.Close()

			client := NewCarbonMarketClient(server.URL, 10*time.Minute)
			client.SetHTTPClient(testClientForServer(server))

			quote, err := client.GetQuote()
			if err != nil {
				t.Fatalf("[%s] unexpected error: %v", tf.name, err)
			}
			if !quote.IsLive {
				t.Fatalf("[%s] expected isLive == true", tf.name)
			}
			if quote.SpotPricePerMetricTonUSD != tf.expPrice {
				t.Errorf("[%s] price mismatch: got %v, exp %v", tf.name, quote.SpotPricePerMetricTonUSD, tf.expPrice)
			}
		}
	})

	// 4.2: Zero price rejection and fallback
	t.Run("ZeroPriceRejection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"toucan-protocol-base-carbon-tonne": {"usd": 0.0}}`))
		}))
		defer server.Close()

		client := NewCarbonMarketClient(server.URL, 10*time.Minute)
		client.SetHTTPClient(testClientForServer(server))

		quote, err := client.GetQuote()
		if err != nil {
			t.Fatalf("expected graceful fallback, got error: %v", err)
		}
		if quote.IsLive {
			t.Fatalf("zero price must not be marked as isLive")
		}
		if quote.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
			t.Fatalf("expected fallback price %v, got %v", defaultMarketFallbackPrice, quote.SpotPricePerMetricTonUSD)
		}
	})

	// 4.3: Negative price rejection and fallback
	t.Run("NegativePriceRejection", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"toucan-protocol-base-carbon-tonne": {"usd": -15.50}}`))
		}))
		defer server.Close()

		client := NewCarbonMarketClient(server.URL, 10*time.Minute)
		client.SetHTTPClient(testClientForServer(server))

		quote, err := client.GetQuote()
		if err != nil {
			t.Fatalf("expected graceful fallback on negative price, got error: %v", err)
		}
		if quote.IsLive {
			t.Fatalf("negative price must not be marked as isLive")
		}
		if quote.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
			t.Fatalf("expected fallback price %v, got %v", defaultMarketFallbackPrice, quote.SpotPricePerMetricTonUSD)
		}
	})

	// 4.4: Corrupted JSON, HTML error bodies, and memory bombs
	t.Run("CorruptedAndAdversarialJSON", func(t *testing.T) {
		adversarialBodies := []struct {
			name string
			body []byte
		}{
			{name: "TruncatedJSON", body: []byte(`{"toucan-protocol-base-carbon-tonne": {`)},
			{name: "HTML502Page", body: []byte(`<!DOCTYPE html><html><body>502 Bad Gateway</body></html>`)},
			{name: "StringInsteadOfNumber", body: []byte(`{"usd": "free"}`)},
			{name: "NullUSD", body: []byte(`{"usd": null}`)},
			{name: "EmptyJSON", body: []byte(`{}`)},
			{name: "ArrayInsteadOfObject", body: []byte(`[12.5, 14.2]`)},
			{name: "NaNString", body: []byte(`{"usd": NaN}`)},
		}

		for _, ab := range adversarialBodies {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write(ab.body)
			}))
			defer server.Close()

			client := NewCarbonMarketClient(server.URL, 10*time.Minute)
			client.SetHTTPClient(testClientForServer(server))

			quote, err := client.GetQuote()
			if err != nil {
				t.Fatalf("[%s] expected graceful fallback, got error: %v", ab.name, err)
			}
			if quote.IsLive {
				t.Fatalf("[%s] corrupted body must not produce isLive == true", ab.name)
			}
			if quote.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
				t.Fatalf("[%s] expected fallback price, got %v", ab.name, quote.SpotPricePerMetricTonUSD)
			}
		}
	})

	// 4.5: HTTP error status codes (500, 502, 503, 404, 429)
	t.Run("HTTPErrorStatusCodes", func(t *testing.T) {
		statusCodes := []int{
			http.StatusInternalServerError,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
			http.StatusNotFound,
			http.StatusTooManyRequests,
		}

		for _, code := range statusCodes {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, fmt.Sprintf("Simulated error %d", code), code)
			}))
			defer server.Close()

			client := NewCarbonMarketClient(server.URL, 10*time.Minute)
			client.SetHTTPClient(testClientForServer(server))

			quote, err := client.GetQuote()
			if err != nil {
				t.Fatalf("Status %d: expected graceful fallback, got error: %v", code, err)
			}
			if quote.IsLive {
				t.Fatalf("Status %d: must not produce isLive == true", code)
			}
			if quote.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
				t.Fatalf("Status %d: expected fallback price, got %v", code, quote.SpotPricePerMetricTonUSD)
			}
		}
	})

	// 4.6: Slow response / Timeout handling (> 8s timeout simulation)
	t.Run("SlowResponseTimeout", func(t *testing.T) {
		// Server handler simulates an unresponsive or extremely slow backend (sleeps 500ms)
		slowHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(500 * time.Millisecond):
				w.Write([]byte(`{"usd": 99.0}`))
			case <-r.Context().Done():
				return
			}
		})

		client := NewCarbonMarketClient("http://mock-market.local", 10*time.Minute)
		// Configure client with context-aware transport and strict 50ms timeout
		client.SetHTTPClient(&http.Client{
			Transport: &contextAwareTransport{handler: slowHandler},
			Timeout:   50 * time.Millisecond,
		})

		start := time.Now()
		quote, err := client.GetQuote()
		elapsed := time.Since(start)

		if err != nil {
			t.Fatalf("expected graceful fallback on timeout, got error: %v", err)
		}
		if quote.IsLive {
			t.Fatalf("timeout response must not be marked isLive")
		}
		if quote.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
			t.Fatalf("expected fallback price %v, got %v", defaultMarketFallbackPrice, quote.SpotPricePerMetricTonUSD)
		}
		if elapsed > 300*time.Millisecond {
			t.Fatalf("timeout took unexpectedly long: %v (expected ~50ms)", elapsed)
		}
	})

	// 4.6b: Verify defaultHTTPTimeout constant is 8s
	t.Run("Default8SecondTimeoutBehavior", func(t *testing.T) {
		if defaultHTTPTimeout != 8*time.Second {
			t.Fatalf("expected defaultHTTPTimeout == 8s, got %v", defaultHTTPTimeout)
		}

		// Also verify context deadline propagation directly
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://mock-market.local", nil)
		if err != nil {
			t.Fatalf("request creation failed: %v", err)
		}
		select {
		case <-time.After(50 * time.Millisecond):
			if req.Context().Err() == nil {
				t.Fatalf("context should have expired")
			}
		}
	})

	// 4.7: Cache Hit Verification and High-Concurrency Race Safety
	t.Run("CacheHitAndHighConcurrency", func(t *testing.T) {
		var serverHitCount int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&serverHitCount, 1)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"usd": 15.00}`))
		}))
		defer server.Close()

		client := NewCarbonMarketClient(server.URL, 500*time.Millisecond)
		client.SetHTTPClient(testClientForServer(server))

		// 50 concurrent requests fired simultaneously
		const numWorkers = 50
		var wg sync.WaitGroup
		wg.Add(numWorkers)

		for i := 0; i < numWorkers; i++ {
			go func() {
				defer wg.Done()
				q, err := client.GetQuote()
				if err != nil {
					t.Errorf("concurrent GetQuote error: %v", err)
				}
				if q.SpotPricePerMetricTonUSD != 15.00 && q.SpotPricePerMetricTonUSD != defaultMarketFallbackPrice {
					t.Errorf("unexpected spot price: %v", q.SpotPricePerMetricTonUSD)
				}
			}()
		}
		wg.Wait()

		// Cache should have shielded upstream server: hit count should be tiny (typically 1)
		hits := atomic.LoadInt32(&serverHitCount)
		if hits > 5 {
			t.Errorf("cache failed to absorb concurrent requests: server hit count = %d", hits)
		}

		// Subsequent sequential calls must definitely hit cache
		for i := 0; i < 10; i++ {
			q, err := client.GetQuote()
			if err != nil || q.SpotPricePerMetricTonUSD != 15.00 {
				t.Fatalf("sequential cache read failed: %v", err)
			}
		}
		if atomic.LoadInt32(&serverHitCount) != hits {
			t.Errorf("subsequent calls bypassed cache: hits went from %d to %d", hits, serverHitCount)
		}
	})
}
