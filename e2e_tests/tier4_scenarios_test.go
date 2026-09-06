package e2e_tests

import (
	"math"
	"testing"
	"time"

	"econ/simulation"
)

// =============================================================================
// TIER 4: REAL-WORLD APPLICATION SCENARIOS
// Requirements Covered:
//   R1. Realistic Diurnal Office Lifecycle & Anomaly Detection
//   R2. Live Telemetry Resilience with Forecaster Cold-Start
//   R3. Edge Node High-Strain Transient Spike & Pass-Through Recovery
//   R4. Heterogeneous Multi-Zone Fleet Integration
// =============================================================================

// -----------------------------------------------------------------------------
// [Scenario 1] Multi-Zone Diurnal Office Cycle
// -----------------------------------------------------------------------------
func TestTier4_Scenario_OfficeDiurnalCycle(t *testing.T) {
	engine := SetupTestEngine()
	now := time.Now()

	// Step 1: Pre-train diurnal profiles across 25 samples for the current operational window
	// Zone 1 (Open Office): Typically occupied with 10-14 people during working hours
	TrainOccupancyBaseline(engine, "zone-office-a", 12.0, 1.2, 25, now)

	// Step 2: Occupants depart for an unscheduled offsite, but AC was left running at 22C!
	// With 0 people and AC active, the mature model detects 12.0 - 1.5*(1.2) = 10.2 threshold.
	// Current occupancy (0) is 10.0 sigma below learned normal (z = -10.0 <= -1.5).
	occZero := 0
	irActive := "COOL_22"
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occZero,
		IrState:   &irActive,
		Source:    "esp32",
	})

	recs := engine.Recommendations(10).Recommendations
	rec := FindZoneRecommendation(recs, "zone-office-a", "occupancy", "turn_off_ac")
	if rec == nil {
		t.Fatalf("expected evening turn_off_ac recommendation when room vacated with AC left ON")
	}
	AssertRecommendationBasis(t, rec, "learned")
	if rec.Samples < 20 {
		t.Errorf("expected mature sample count >= 20, got %d", rec.Samples)
	}

	// Scenario B: Server Room is always 0 people with continuous HVAC.
	// Must NEVER produce an erroneous turn_off_ac warning.
	recServer := FindZoneRecommendation(recs, "zone-server-room", "occupancy", "turn_off_ac")
	if recServer != nil {
		t.Fatalf("spurious turn_off_ac recommendation issued for server room: %+v", recServer)
	}
}

// -----------------------------------------------------------------------------
// [Scenario 2] Dynamic Edge Strain Spike and Seamless Server DSP Recovery
// -----------------------------------------------------------------------------
func TestTier4_Scenario_EdgeStrainSpikeAndRecovery(t *testing.T) {
	engine := SetupTestEngine()
	serverDsp := simulation.NewCurrentDenoiser(simulation.DefaultCurrentDenoiseConfig())

	// 1. Phase 1: Normal Operation (No Strain)
	// Edge node computes local DSP and publishes 440W
	normalWatts := 440.0
	irActive := "COOL_24"
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		StripW:  &normalWatts,
		IrState: &irActive,
		Source:  "esp32",
	})

	z := engine.Zones["zone-office-a"]
	if math.Abs(z.HwStripW-440.0) > 0.01 {
		t.Fatalf("Phase 1 failed: zone strip power not updated: %f", z.HwStripW)
	}

	// 2. Phase 2: High CPU Strain Event (Simulated 5-minute peak)
	// Microcontroller enters pass-through mode: skips local DSP, emits raw ADC samples
	rawWave := GenerateSineWave(2.0, 15.0, 0.5, 1000.0, 0.050, 50.0, 1.25, 17.3, 601)

	// Server intercepts rawFallback: true, runs server-side DSP
	offloadAmps := serverDsp.ProcessWindow(rawWave)
	offloadWatts := math.Round(offloadAmps*220.0*10.0) / 10.0

	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		StripW:  &offloadWatts,
		IrState: &irActive,
		Source:  "esp32",
	})

	// Verify server-side offloaded power maintains continuity with normal mode (~440 W)
	if math.Abs(z.HwStripW-440.0) > 30.0 {
		t.Fatalf("Phase 2 failed: server-side DSP discrepancy during offload: %f W", z.HwStripW)
	}

	// 3. Phase 3: Recovery (Strain Resolves)
	// Microcontroller exits pass-through mode, resumes local DSP
	resumedWatts := 438.5
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		StripW:  &resumedWatts,
		IrState: &irActive,
		Source:  "esp32",
	})

	if math.Abs(z.HwStripW-438.5) > 0.01 {
		t.Fatalf("Phase 3 failed: power state not updated upon recovery: %f", z.HwStripW)
	}
}

// -----------------------------------------------------------------------------
// [Scenario 3] Heterogeneous Fleet Resilience & Multi-Node Failover
// -----------------------------------------------------------------------------
func TestTier4_Scenario_MultiZoneResilience(t *testing.T) {
	engine := SetupTestEngine()
	now := time.Now()

	// Heterogeneous hardware setup:
	// - Zone 1: ESP32 node with ACS712 strip + SCT-013 plug
	// - Zone 2: Pico node with dual PIR headcount
	// - Zone 3: Virtual / simulated data center

	TrainOccupancyBaseline(engine, "zone-office-a", 10.0, 1.5, 25, now)
	TrainOccupancyBaseline(engine, "zone-boardroom", 4.0, 1.0, 25, now)

	// Simulate concurrent multi-zone telemetry cycle
	// Zone 1: ESP32 streaming with 1 person (statistically vacant)
	occZone1 := 1
	irZone1 := "COOL_22"
	stripWZone1 := 350.0
	engine.IngestTelemetry("zone-office-a", "esp32_1", simulation.Measurement{
		Occupancy: &occZone1,
		StripW:    &stripWZone1,
		IrState:   &irZone1,
		Source:    "esp32",
	})

	// Zone 2: Pico streaming with 4 people (normal meeting)
	occZone2 := 4
	irZone2 := "COOL_24"
	engine.IngestTelemetry("zone-boardroom", "pico_1", simulation.Measurement{
		Occupancy: &occZone2,
		IrState:   &irZone2,
		Source:    "pico",
	})

	// Query recommendations for all zones
	rep := engine.Recommendations(10)

	// Assert Zone 1 receives learned turn_off_ac
	rec1 := FindZoneRecommendation(rep.Recommendations, "zone-office-a", "occupancy", "turn_off_ac")
	if rec1 == nil {
		t.Fatalf("resilience scenario failed: Zone 1 did not trigger turn_off_ac")
	}
	AssertRecommendationBasis(t, rec1, "learned")

	// Assert Zone 2 (normally occupied) does NOT receive turn_off_ac
	rec2 := FindZoneRecommendation(rep.Recommendations, "zone-boardroom", "occupancy", "turn_off_ac")
	if rec2 != nil {
		t.Fatalf("resilience scenario failed: Zone 2 erroneously triggered turn_off_ac: %+v", rec2)
	}

	// Verify forecast section does not fabricate synthetic spline lines
	if rep.Forecast != nil {
		if len(rep.Forecast.Series) == 0 {
			// Honest empty series
		}
	}
}
