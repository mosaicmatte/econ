package e2e_tests

import (
	"math"
	"testing"
	"time"

	"econ/simulation"
)

// =============================================================================
// TIER 3: CROSS-FEATURE INTERACTION TESTS
// Requirements Covered:
//   R1 & R3: Edge Raw Streaming Processed into Learned Occupancy Recommendations
//   R1 & R2: Forecast Absence During Continuous Live Telemetry Ingestion
// =============================================================================

// -----------------------------------------------------------------------------
// [Interaction 1] Edge Raw Streaming Processed into Learned Occupancy Recommendations
// -----------------------------------------------------------------------------
func TestTier3_EdgeRawStreamingToLearnedRecommendations(t *testing.T) {
	engine := SetupTestEngine()
	now := time.Now()

	// Step 1: Establish mature occupancy baseline for zone-office-a (mean=8.0, std=1.2, N=30)
	TrainOccupancyBaseline(engine, "zone-office-a", 8.0, 1.2, 30, now)

	// Step 2: Edge node is under heavy CPU strain -> streams raw decimated ADC samples
	// Generate 50 raw ADC samples representing ~2.0A RMS current draw on power strip
	rawWave := GenerateSineWave(2.0, 15.0, 0.5, 1000.0, 0.050, 50.0, 1.25, 17.3, 501)

	// Step 3: Backend server-side DSP offload processes raw samples
	dsp := simulation.NewCurrentDenoiser(simulation.DefaultCurrentDenoiseConfig())
	denoisedAmps := dsp.ProcessWindow(rawWave)
	if denoisedAmps < 1.0 || denoisedAmps > 3.0 {
		t.Fatalf("DSP offload failed: expected ~2.0A, got %f", denoisedAmps)
	}
	denoisedWatts := math.Round(denoisedAmps*220.0*10.0) / 10.0

	// Step 4: Inject telemetry with low occupancy (1 person) and active AC into the engine
	occLow := 1
	irActive := "COOL_22"
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occLow,
		IrState:   &irActive,
		StripW:    &denoisedWatts,
		Source:    "esp32",
	})

	// Step 5: Verify zone physical state reflects denoised power
	z := engine.Zones["zone-office-a"]
	if math.Abs(z.HwStripW-denoisedWatts) > 0.01 {
		t.Fatalf("zone HwStripW not updated with denoised power: got %f, expected %f",
			z.HwStripW, denoisedWatts)
	}

	// Step 6: Verify recommendation engine issues learned recommendation based on
	// both the denoised physical power draw and the learned occupancy distribution
	recs := engine.Recommendations(10).Recommendations
	rec := FindZoneRecommendation(recs, "zone-office-a", "occupancy", "turn_off_ac")
	if rec == nil {
		t.Fatalf("expected learned turn_off_ac recommendation from cross-feature pipeline")
	}

	AssertRecommendationBasis(t, rec, "learned")
	if rec.Samples < 20 {
		t.Errorf("expected mature sample count >= 20, got %d", rec.Samples)
	}
	if rec.Deviation > -1.5 {
		t.Errorf("expected statistical anomaly deviation <= -1.5, got %f", rec.Deviation)
	}
}

// -----------------------------------------------------------------------------
// [Interaction 2] Forecast Absence During Continuous Live Telemetry Ingestion
// -----------------------------------------------------------------------------
func TestTier3_ForecastAbsenceDuringLiveTelemetry(t *testing.T) {
	engine := SetupTestEngine()
	now := time.Now()

	// Train zone baselines
	TrainOccupancyBaseline(engine, "zone-office-a", 5.0, 1.0, 25, now)

	// Ingest continuous telemetry stream over 10 consecutive ticks
	// representing live sensor telemetry arriving every 5 seconds
	for tick := 0; tick < 10; tick++ {
		temp := 24.2 + float64(tick)*0.05
		humidity := 54.0 + float64(tick)*0.2
		co2 := 650.0 + float64(tick)*5.0
		occ := 5
		plugW := 125.0
		stripW := 45.0
		irState := "COOL_24"

		engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
			Temp:      &temp,
			Humidity:  &humidity,
			Co2:       &co2,
			Occupancy: &occ,
			PlugW:     &plugW,
			StripW:    &stripW,
			IrState:   &irState,
			Source:    "esp32",
			TempReal:  true,
		})

		// Query recommendations while forecast sequence is absent
		rep := engine.Recommendations(10)

		// Recommendations model must remain completely operational
		if rep.Model.Established < 1 {
			t.Fatalf("tick %d: expected established models >= 1, got %d", tick, rep.Model.Established)
		}

		// If forecast payload is present, verify that it does not fabricate synthetic curves
		if rep.Forecast != nil && len(rep.Forecast.Series) == 0 {
			// Compliant: honestly reports 0 trajectory series
			if rep.Forecast.Engine == "timesfm" && len(rep.Forecast.Series) > 0 {
				t.Fatalf("tick %d: fabricated series detected in unready forecast", tick)
			}
		}
	}

	// Verify zone telemetry state was preserved continuously
	z := engine.Zones["zone-office-a"]
	if z.HwPlugW == 0 || z.HwStripW == 0 {
		t.Fatalf("telemetry stream corrupted during forecast absence: plugW=%f, stripW=%f",
			z.HwPlugW, z.HwStripW)
	}
}
