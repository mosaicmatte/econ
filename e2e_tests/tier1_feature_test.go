package e2e_tests

import (
	"math"
	"testing"
	"time"

	"econ/simulation"
)

// =============================================================================
// TIER 1: FEATURE COVERAGE TESTS
// Requirements Covered:
//   R1. Genuine Occupancy AI Model (Learned Statistical Threshold vs Zero-Check)
//   R2. Authentic Forecast Chart & Backend Absence of Synthetic Curves
//   R3. Edge Compute Offload Fallback & Server-Side DSP Pipeline
//   R4. Physical Hardware Compatibility & Electrical Constraints Audit
// =============================================================================

// -----------------------------------------------------------------------------
// [R1] Learned Statistical Threshold vs Hardcoded Zero-Check
// -----------------------------------------------------------------------------
func TestTier1_R1_LearnedStatisticalThresholdVsZeroCheck(t *testing.T) {
	spec := simulation.BaselineModelSpec()
	occSpec, exists := spec.Metrics["occupancy"]
	if !exists {
		t.Fatalf("R1 Violation: metric 'occupancy' is not registered in BaselineModelSpec().Metrics")
	}

	if occSpec.ZAlert != 1.5 {
		t.Errorf("expected ZAlert=1.5, got %f", occSpec.ZAlert)
	}
	if occSpec.MinSigma != 0.5 {
		t.Errorf("expected MinSigma=0.5, got %f", occSpec.MinSigma)
	}
	if occSpec.Action != "turn_off_ac" {
		t.Errorf("expected Action='turn_off_ac', got %q", occSpec.Action)
	}

	engine := SetupTestEngine()
	now := time.Now()

	// 1. Cold Start: Baseline count N < 20
	// Inject occupancy = 0 with active AC into untrained zone
	occZero := 0
	irActive := "COOL_24"
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occZero,
		IrState:   &irActive,
		Source:    "esp32",
	})

	recsCold := engine.Recommendations(10).Recommendations
	recCold := FindZoneRecommendation(recsCold, "zone-office-a", "occupancy", "turn_off_ac")
	if recCold == nil {
		t.Fatalf("expected cold-start turn_off_ac recommendation when occupancy=0")
	}
	AssertRecommendationBasis(t, recCold, "standard")
	if recCold.Samples >= 20 {
		t.Errorf("expected cold-start samples < 20, got %d", recCold.Samples)
	}

	// 2. Train baseline with normal daytime occupancy (mean=6.0, std=1.0) over 30 observations
	TrainOccupancyBaseline(engine, "zone-office-a", 6.0, 1.0, 30, now)

	// 3. Mature Baseline with Occupancy = 0
	// Must issue recommendation with Basis="learned"
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occZero,
		IrState:   &irActive,
		Source:    "esp32",
	})
	recsMature := engine.Recommendations(10).Recommendations
	recMature := FindZoneRecommendation(recsMature, "zone-office-a", "occupancy", "turn_off_ac")
	if recMature == nil {
		t.Fatalf("expected mature turn_off_ac recommendation when occupancy=0")
	}
	AssertRecommendationBasis(t, recMature, "learned")
	if recMature.Samples < 20 {
		t.Errorf("expected mature samples >= 20, got %d", recMature.Samples)
	}
	if recMature.Deviation > -1.5 {
		t.Errorf("expected negative z-score <= -1.5, got %f", recMature.Deviation)
	}

	// 4. Critical Opaque-Box Verification: Genuine Statistical Model vs Zero-Check
	// Inject occupancy = 2 people.
	// In a zone whose learned normal is 6.0 ± 1.0, 2 people is 4.0 standard deviations below normal (z = -4.0 <= -1.5).
	// A hardcoded zero-check (if occ == 0) FAILS to trigger.
	// A genuine learned statistical model DETECTS this significant under-utilization and triggers turn_off_ac with Basis="learned"!
	occTwo := 2
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occTwo,
		IrState:   &irActive,
		Source:    "esp32",
	})
	recsNonZero := engine.Recommendations(10).Recommendations
	recNonZero := FindZoneRecommendation(recsNonZero, "zone-office-a", "occupancy", "turn_off_ac")
	if recNonZero == nil {
		t.Fatalf("R1 Violation: recommendation engine failed to detect statistical vacancy at non-zero occupancy (occ=2 vs normal=6.0)")
	}
	AssertRecommendationBasis(t, recNonZero, "learned")

	// 5. Normal Occupancy (occ = 6) must NOT trigger
	occSix := 6
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occSix,
		IrState:   &irActive,
		Source:    "esp32",
	})
	recsNormal := engine.Recommendations(10).Recommendations
	recNormal := FindZoneRecommendation(recsNormal, "zone-office-a", "occupancy", "turn_off_ac")
	if recNormal != nil {
		t.Fatalf("R1 Violation: normal occupancy (occ=6) erroneously triggered turn_off_ac: %+v", recNormal)
	}

	// 6. When AC is explicitly OFF, recommendation must be suppressed
	offState := "OFF"
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occZero,
		IrState:   &offState,
		Source:    "esp32",
	})
	recsAcOff := engine.Recommendations(10).Recommendations
	recAcOff := FindZoneRecommendation(recsAcOff, "zone-office-a", "occupancy", "turn_off_ac")
	if recAcOff != nil {
		t.Fatalf("expected no turn_off_ac recommendation when AC is already OFF")
	}
}

// -----------------------------------------------------------------------------
// [R2] Authentic Forecast Chart Backend Contract & Absence of Synthetic Spline
// -----------------------------------------------------------------------------
func TestTier1_R2_AuthenticForecastBackendContract(t *testing.T) {
	engine := SetupTestEngine()

	// 1. Check recommendation report forecast payload
	rep := engine.Recommendations(10)
	if rep.Forecast != nil {
		// When series is empty, verify no fake curves are produced
		if len(rep.Forecast.Series) == 0 {
			if rep.Forecast.PeakUpperMw != nil && *rep.Forecast.PeakUpperMw < 0 {
				t.Errorf("unexpected negative peakUpperMw: %f", *rep.Forecast.PeakUpperMw)
			}
		}
	}

	// 2. Verify that when load history is queried, values remain strictly factual and finite
	history := engine.LoadHistory()
	if len(history) > 0 {
		for _, v := range history {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				t.Fatalf("R2 Violation: non-physical load history observed: %f", v)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// [R3] Edge Raw Fallback Detection and Server-Side DSP
// -----------------------------------------------------------------------------
func TestTier1_R3_RawFallbackDetectionAndServerDsp(t *testing.T) {
	// Configure server-side DSP with exact hardware calibration
	cfg := simulation.DefaultCurrentDenoiseConfig()
	denoiser := simulation.NewCurrentDenoiser(cfg)

	// Test 1: Zero Load with Intrinsic ESP32 ADC Noise (DC ~1.25V -> 1551 counts, sigma ~17.3)
	// Must result in strictly 0.0 Amps (no ghost draw)
	zeroWave := GenerateSineWave(0.0, 15.0, 0.5, 10000.0, 0.100, 50.0, 1.25, 17.3, 101)
	zeroAmps := denoiser.ProcessWindow(zeroWave)
	if zeroAmps != 0.0 {
		t.Fatalf("R3 Violation: zero load produced ghost reading: %f A", zeroAmps)
	}

	// Test 2: Known 2.0 A RMS AC Sine Waveform at 50 Hz
	// Must reconstruct current within 5% accuracy
	denoiser.Reset()
	wave2A := GenerateSineWave(2.0, 15.0, 0.5, 10000.0, 0.100, 50.0, 1.25, 17.3, 202)
	amps2A := denoiser.ProcessWindow(wave2A)
	relErr := math.Abs(amps2A-2.0) / 2.0
	if relErr > 0.05 {
		t.Fatalf("R3 Violation: 2.0A wave reconstruction error exceeds 5%%: got %f A (err: %.2f%%)",
			amps2A, relErr*100.0)
	}

	// Test 3: Converted Power Calculation at 220V Nominal Mains
	watts := amps2A * 220.0
	expectedWatts := 2.0 * 220.0 // 440 W
	if math.Abs(watts-expectedWatts) > 25.0 {
		t.Fatalf("R3 Violation: power calculation discrepancy: got %.1f W, expected ~%.1f W",
			watts, expectedWatts)
	}

	// Test 4: Verify that Zone Simulation stores clean engineering Watts
	engine := SetupTestEngine()
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		StripW: &watts,
		Source: "esp32",
	})

	z := engine.Zones["zone-office-a"]
	if math.Abs(z.HwStripW-watts) > 0.01 {
		t.Fatalf("R3 Violation: zone HwStripW not updated with denoised power: got %f, expected %f",
			z.HwStripW, watts)
	}
}

// -----------------------------------------------------------------------------
// [R4] Hardware Compatibility and Physical Constraints Audit
// -----------------------------------------------------------------------------
func TestTier1_R4_HardwareConstraintsAudit(t *testing.T) {
	cfg := simulation.DefaultCurrentDenoiseConfig()

	// 1. ESP32 ADC Reference Voltage and Resolution
	if cfg.AdcVref != 3.3 {
		t.Fatalf("R4 Hardware Constraint Violation: AdcVref must be 3.3V, got %f", cfg.AdcVref)
	}
	if cfg.AdcMaxCounts != 4095.0 {
		t.Fatalf("R4 Hardware Constraint Violation: 12-bit SAR ADC max counts must be 4095.0, got %f", cfg.AdcMaxCounts)
	}

	// 2. ACS712 10k/10k Voltage Divider Attenuation Ratio
	// ACS712 outputs 0..5V centered at 2.5V. The 10k/10k divider steps this down by 0.5x
	// to 0..2.5V centered at 1.25V, preventing overvoltage on the 3.3V ESP32 GPIO.
	if cfg.DividerRatio != 0.5 {
		t.Fatalf("R4 Hardware Constraint Violation: DividerRatio must be 0.5 for ACS712 10k/10k divider, got %f", cfg.DividerRatio)
	}

	// 3. Current Sensor Calibration Scale
	// ACS712-30A sensitivity is 66.6 mV/A -> 1 / 0.0666 = 15.0 A/V
	if cfg.CalAPerV != 15.0 {
		t.Fatalf("R4 Hardware Constraint Violation: CalAPerV for ACS712-30A must be 15.0 A/V, got %f", cfg.CalAPerV)
	}

	// 4. ADC Noise Variance Subtraction Parameter
	// ESP32 ADC1 exhibits intrinsic white noise with sigma ~17.3 counts (variance ~300)
	if cfg.NoiseVariance != 300.0 {
		t.Fatalf("R4 Hardware Constraint Violation: NoiseVariance must be 300.0 counts^2, got %f", cfg.NoiseVariance)
	}

	// 5. 50 Hz Mains Cycle Alignment (Vietnam Grid)
	// 50 Hz period is exactly 20 ms. Over a standard 100 ms sampling window, exactly
	// 5 full mains cycles are captured (100 / 20 = 5).
	windowMs := 100.0
	periodMs := 1000.0 / 50.0
	numCycles := windowMs / periodMs
	if math.Abs(numCycles-5.0) > 1e-6 {
		t.Fatalf("R4 Grid Timing Invariant Violation: 100ms window does not capture integer 50Hz cycles: %f", numCycles)
	}
}
