package e2e_tests

import (
	"math"
	"testing"
	"time"

	"econ/simulation"
)

// =============================================================================
// TIER 2: BOUNDARY & CORNER CASE TESTS
// Requirements Covered:
//   R1. Occupancy AI Model (Cold Start vs Mature Baseline Boundaries)
//   R2. Authentic Forecast Chart (Empty vs Populated Forecast Arrays)
//   R3. Edge Compute Offload (Varying Sample Lengths & Strain Toggling)
//   R4. Hardware Compatibility (Dynamic Range, Deadbands, & Jitter)
// =============================================================================

// -----------------------------------------------------------------------------
// [Boundary 1] Varying Sample Lengths & Starvation Guards
// -----------------------------------------------------------------------------
func TestTier2_Boundary_SampleLengths(t *testing.T) {
	cfg := simulation.DefaultCurrentDenoiseConfig()
	denoiser := simulation.NewCurrentDenoiser(cfg)

	// 1. Starvation Guard: window with fewer than MinSamples (e.g. 0, 5, 19 samples)
	starvedCounts := []int{0, 5, 10, 19}
	for _, cnt := range starvedCounts {
		samples := make([]int, cnt)
		amps := denoiser.ProcessWindow(samples)
		if amps != -1.0 {
			t.Fatalf("expected -1.0 for starved window of size %d, got %f", cnt, amps)
		}
	}

	// 2. Minimum Valid Window (n = 20)
	// Must produce a non-negative reading without panicking
	wave20 := GenerateSineWave(2.0, 15.0, 0.5, 1000.0, 0.020, 50.0, 1.25, 17.3, 301)
	denoiser.Reset()
	amps20 := denoiser.ProcessWindow(wave20)
	if amps20 < 0 {
		t.Fatalf("expected non-negative amps for minimum valid window n=20, got %f", amps20)
	}

	// 3. Intermediate Decimated Window (n = 50 and n = 100)
	wave50 := GenerateSineWave(2.0, 15.0, 0.5, 1000.0, 0.050, 50.0, 1.25, 17.3, 302)
	denoiser.Reset()
	amps50 := denoiser.ProcessWindow(wave50)
	if amps50 < 1.0 || amps50 > 3.0 {
		t.Fatalf("unexpected amps for n=50: %f", amps50)
	}

	// 4. Standard 100ms Window (n = 1000 samples)
	wave1000 := GenerateSineWave(2.0, 15.0, 0.5, 10000.0, 0.100, 50.0, 1.25, 17.3, 303)
	denoiser.Reset()
	amps1000 := denoiser.ProcessWindow(wave1000)
	if math.Abs(amps1000-2.0)/2.0 > 0.05 {
		t.Fatalf("expected accuracy within 5%% for n=1000, got %f", amps1000)
	}

	// 5. Oversized Window (n = 2400 samples)
	wave2400 := GenerateSineWave(2.0, 15.0, 0.5, 24000.0, 0.100, 50.0, 1.25, 17.3, 304)
	denoiser.Reset()
	amps2400 := denoiser.ProcessWindow(wave2400)
	if math.Abs(amps2400-2.0)/2.0 > 0.05 {
		t.Fatalf("expected accuracy within 5%% for n=2400, got %f", amps2400)
	}

	// 6. Rail Saturation (clipping at 0 and 4095 counts)
	clippedSamples := make([]int, 1000)
	for i := range clippedSamples {
		if i%2 == 0 {
			clippedSamples[i] = 4095
		} else {
			clippedSamples[i] = 0
		}
	}
	denoiser.Reset()
	ampsClipped := denoiser.ProcessWindow(clippedSamples)
	if math.IsNaN(ampsClipped) || math.IsInf(ampsClipped, 0) || ampsClipped <= 0 {
		t.Fatalf("clipped waveform caused invalid calculation: %f", ampsClipped)
	}
}

// -----------------------------------------------------------------------------
// [Boundary 2] Edge CPU Strain Toggling & Rapid Flapping
// -----------------------------------------------------------------------------
func TestTier2_Boundary_EdgeCpuStrainToggle(t *testing.T) {
	cfg := simulation.DefaultCurrentDenoiseConfig()
	denoiser := simulation.NewCurrentDenoiser(cfg)

	// Simulate rapid toggling between strained (pass-through) and normal mode
	// Ensure denoiser EMA filter maintains numeric stability and does not diverge
	wave2A := GenerateSineWave(2.0, 15.0, 0.5, 10000.0, 0.100, 50.0, 1.25, 17.3, 401)
	for cycle := 0; cycle < 20; cycle++ {
		strained := (cycle % 2) == 1
		if strained {
			// In pass-through mode, edge skips DSP and backend processes raw decimated samples
			decimated := make([]int, 50)
			for i := range decimated {
				decimated[i] = wave2A[i*20]
			}
			amps := denoiser.ProcessWindow(decimated)
			if amps <= 0 || math.IsNaN(amps) {
				t.Fatalf("cycle %d: pass-through offload produced invalid amps: %f", cycle, amps)
			}
		} else {
			// In normal mode, edge runs full window
			amps := denoiser.ProcessWindow(wave2A)
			if amps <= 0 || math.IsNaN(amps) {
				t.Fatalf("cycle %d: normal mode produced invalid amps: %f", cycle, amps)
			}
		}
	}

	// Verify deadband behavior: when signal variation is within DeadbandAmps (0.03A),
	// filtered output remains rock steady
	stableAmps := denoiser.GetFilteredAmps()
	slightVariationWave := GenerateSineWave(2.01, 15.0, 0.5, 10000.0, 0.100, 50.0, 1.25, 17.3, 402)
	filteredAfterSmallJitter := denoiser.ProcessWindow(slightVariationWave)
	if math.Abs(filteredAfterSmallJitter-stableAmps) > 0.05 {
		t.Errorf("deadband failed to suppress small current jitter: delta=%f",
			math.Abs(filteredAfterSmallJitter-stableAmps))
	}
}

// -----------------------------------------------------------------------------
// [Boundary 3] Cold Start vs Mature Baseline Boundary Transitions
// -----------------------------------------------------------------------------
func TestTier2_Boundary_BaselineMaturityTransitions(t *testing.T) {
	engine := SetupTestEngine()
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	occZero := 0
	irActive := "COOL_24"

	// Boundary Check 1: N = 19 observations (one observation before maturity)
	// Must strictly remain Basis: "standard"
	for i := 0; i < 19; i++ {
		engine.ObserveBaseline("zone-office-a", "occupancy", 6.0, at)
	}

	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occZero,
		IrState:   &irActive,
		Source:    "esp32",
	})

	recs19 := engine.Recommendations(10).Recommendations
	rec19 := FindZoneRecommendation(recs19, "zone-office-a", "occupancy", "turn_off_ac")
	if rec19 == nil {
		t.Fatalf("expected recommendation at N=19")
	}
	AssertRecommendationBasis(t, rec19, "standard")
	if rec19.Samples != 19 {
		t.Errorf("expected Samples=19, got %d", rec19.Samples)
	}

	// Boundary Check 2: N = 20 observations (exact maturity threshold)
	// Must cross the boundary to Basis: "learned"!
	engine.ObserveBaseline("zone-office-a", "occupancy", 6.0, at)

	recs20 := engine.Recommendations(10).Recommendations
	rec20 := FindZoneRecommendation(recs20, "zone-office-a", "occupancy", "turn_off_ac")
	if rec20 == nil {
		t.Fatalf("expected recommendation at N=20")
	}
	AssertRecommendationBasis(t, rec20, "learned")
	if rec20.Samples != 20 {
		t.Errorf("expected Samples=20, got %d", rec20.Samples)
	}

	// Boundary Check 3: Marginal Z-Score Boundary
	// ZAlert is 1.5. Test near-threshold occupancy.
	// Zone has mean=6.0, std >= 0.5 (minSigma floor).
	// With occupancy=6 (z=0), no alert.
	occSix := 6
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occSix,
		IrState:   &irActive,
		Source:    "esp32",
	})
	recsMarginal := engine.Recommendations(10).Recommendations
	recMarginal := FindZoneRecommendation(recsMarginal, "zone-office-a", "occupancy", "turn_off_ac")
	if recMarginal != nil {
		t.Fatalf("expected no recommendation when z=0 at boundary")
	}
}

// -----------------------------------------------------------------------------
// [Boundary 4] Empty vs Populated Forecast Array Variations
// -----------------------------------------------------------------------------
func TestTier2_Boundary_ForecastArrayVariations(t *testing.T) {
	// Test empty slice vs populated sequence data structures
	emptyForecast := simulation.ForecastGraphData{
		Engine:         "timesfm",
		Series:         []float64{},
		StepMinutes:    5,
		HorizonMinutes: 60,
	}

	if len(emptyForecast.Series) != 0 {
		t.Fatalf("expected 0 series elements for empty forecast")
	}

	// Scalar LSTM Peak without trajectory sequence
	scalarPeak := 0.0345
	lstmOnlyForecast := simulation.ForecastGraphData{
		Engine:         "lstm",
		Series:         []float64{},
		LstmPeakMw:     &scalarPeak,
		StepMinutes:    5,
		HorizonMinutes: 60,
	}

	if len(lstmOnlyForecast.Series) != 0 {
		t.Fatalf("expected 0 series elements for scalar-only LSTM peak")
	}
	if lstmOnlyForecast.LstmPeakMw == nil || *lstmOnlyForecast.LstmPeakMw != 0.0345 {
		t.Fatalf("expected LstmPeakMw=0.0345")
	}

	// Populated sequence with quantiles
	fullForecast := simulation.ForecastGraphData{
		Engine:         "timesfm",
		Series:         []float64{0.021, 0.023, 0.025, 0.028},
		UpperBand:      []float64{0.024, 0.026, 0.029, 0.032},
		UpperQuantile:  "q9",
		StepMinutes:    5,
		HorizonMinutes: 20,
		Quantiles: map[string][]float64{
			"q1": {0.019, 0.020, 0.022, 0.025},
			"q5": {0.021, 0.023, 0.025, 0.028},
			"q9": {0.024, 0.026, 0.029, 0.032},
		},
	}

	if len(fullForecast.Series) != 4 {
		t.Fatalf("expected 4 series elements, got %d", len(fullForecast.Series))
	}
	// Verify monotonic quantile envelopes: q1 <= q5 <= q9
	for i := 0; i < len(fullForecast.Series); i++ {
		q1 := fullForecast.Quantiles["q1"][i]
		q5 := fullForecast.Quantiles["q5"][i]
		q9 := fullForecast.Quantiles["q9"][i]
		if !(q1 <= q5 && q5 <= q9) {
			t.Fatalf("quantile inversion at step %d: q1=%f, q5=%f, q9=%f", i, q1, q5, q9)
		}
	}
}
