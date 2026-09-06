package simulation

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestOccupancyBaseline_VaryingTelemetryTrainsBaseline verifies that varying occupancy
// telemetry over time trains the baseline model's mean and variance online.
func TestOccupancyBaseline_VaryingTelemetryTrainsBaseline(t *testing.T) {
	b := NewBaselines()
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)

	// Series of varying daytime office occupancy readings (mean = 8.0, with natural variance)
	telemetry := []float64{8, 9, 7, 8, 10, 6, 8, 9, 7, 8, 9, 7, 8, 6, 10, 8, 7, 9, 8, 7, 9, 8, 7, 8, 9}
	for _, val := range telemetry {
		b.Observe("zone-office-a", "occupancy", val, at)
	}

	b.mu.Lock()
	stat, ok := b.establishedStat("zone-office-a", "occupancy", at)
	b.mu.Unlock()

	if !ok || stat == nil {
		t.Fatalf("expected established occupancy baseline after %d observations", len(telemetry))
	}
	if stat.Count != len(telemetry) {
		t.Errorf("expected stat.Count == %d, got %d", len(telemetry), stat.Count)
	}
	if stat.Mean < 7.5 || stat.Mean > 8.5 {
		t.Errorf("expected learned mean near 8.0, got %.2f", stat.Mean)
	}
	if stat.std() <= 0 {
		t.Errorf("expected non-zero standard deviation from varying telemetry, got %.2f", stat.std())
	}

	est, _ := b.Coverage()
	if est < 1 {
		t.Errorf("expected at least 1 established bucket in Coverage, got %d", est)
	}
}

// TestOccupancyBaseline_ImmatureColdStartUsesStandardBasis verifies that an immature
// baseline (N < 20) falls back to Basis: "standard" for room vacancy with AC active.
func TestOccupancyBaseline_ImmatureColdStartUsesStandardBasis(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	irActive := "COOL_22"

	// Case 1: Brand new baseline (0 observations)
	cold := NewBaselines()
	readings := []ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Type: "office",
		Occupancy: 0, IrState: &irActive,
	}}

	rep := cold.Recommend(readings, 1.5, at, 10)
	var found bool
	for _, rec := range rep.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			found = true
			if rec.Basis != "standard" {
				t.Errorf("cold start with 0 samples: expected Basis == 'standard', got %q", rec.Basis)
			}
			if rec.Metric != "occupancy" {
				t.Errorf("expected Metric == 'occupancy', got %q", rec.Metric)
			}
			if rec.Severity != "info" {
				t.Errorf("expected Severity == 'info', got %q", rec.Severity)
			}
			if rec.Samples != 0 {
				t.Errorf("expected Samples == 0, got %d", rec.Samples)
			}
		}
	}
	if !found {
		t.Fatalf("expected turn_off_ac recommendation in cold start, got %+v", rep.Recommendations)
	}

	// Case 2: Partially trained but still immature (5 observations < 20)
	warming := NewBaselines()
	for i := 0; i < 5; i++ {
		warming.Observe("zone-office-a", "occupancy", 8.0, at)
	}

	repWarming := warming.Recommend(readings, 1.5, at, 10)
	found = false
	for _, rec := range repWarming.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			found = true
			if rec.Basis != "standard" {
				t.Errorf("cold start with 5 samples: expected Basis == 'standard', got %q", rec.Basis)
			}
			if rec.Samples != 5 {
				t.Errorf("expected Samples == 5, got %d", rec.Samples)
			}
		}
	}
	if !found {
		t.Fatalf("expected turn_off_ac recommendation in warming baseline, got %+v", repWarming.Recommendations)
	}

	// Case 3: Occupied room during cold start should NOT trigger recommendation
	occupiedReadings := []ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Type: "office",
		Occupancy: 3, IrState: &irActive,
	}}
	repOccupied := warming.Recommend(occupiedReadings, 1.5, at, 10)
	for _, rec := range repOccupied.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			t.Fatalf("unexpected turn_off_ac recommendation for occupied room during cold start: %+v", rec)
		}
	}
}

// TestOccupancyBaseline_MatureUsesLearnedStatisticalThresholdNotZeroCheck verifies that
// once trained, the engine uses the learned statistical threshold (not a hardcoded zero-check).
// Specifically, a room with non-zero occupancy that is statistically significantly below normal
// (z <= -1.5) triggers turn_off_ac with Basis: "learned", while normal occupancy does not.
func TestOccupancyBaseline_MatureUsesLearnedStatisticalThresholdNotZeroCheck(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	irActive := "COOL_22"

	// Train baseline with 25 samples of normal occupancy (mean ~ 8.0, std ~ 1.0)
	b := NewBaselines()
	samples := []float64{8, 9, 7, 8, 10, 7, 8, 9, 7, 8, 8, 9, 7, 8, 9, 8, 7, 9, 8, 7, 8, 9, 8, 7, 9}
	for _, s := range samples {
		b.Observe("zone-office-a", "occupancy", s, at)
	}

	// 1. NON-ZERO occupancy (e.g. 2 people) in an 8-person normal room is statistically vacant (z <= -1.5).
	// A hardcoded "occupancy == 0" check would FAIL to trigger here; the genuine statistical model MUST trigger.
	nonZeroLowOccupancy := []ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Type: "office",
		Occupancy: 2, IrState: &irActive,
	}}

	repLow := b.Recommend(nonZeroLowOccupancy, 1.5, at, 10)
	var foundLearned bool
	for _, rec := range repLow.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			foundLearned = true
			if rec.Basis != "learned" {
				t.Errorf("mature baseline must output Basis == 'learned', got %q", rec.Basis)
			}
			if rec.Samples < baselineMature {
				t.Errorf("expected Samples >= %d, got %d", baselineMature, rec.Samples)
			}
			if rec.Baseline < 7.0 || rec.Baseline > 9.0 {
				t.Errorf("expected Baseline to reflect learned mean (~8.0), got %.2f", rec.Baseline)
			}
			if rec.Sigma <= 0 {
				t.Errorf("expected Sigma > 0, got %.2f", rec.Sigma)
			}
			if rec.Deviation > -1.5 {
				t.Errorf("expected Deviation <= -1.5, got %.2f", rec.Deviation)
			}
			// Verify message cites learned mean and standard deviation
			if !strings.Contains(rec.Message, "below its learned") {
				t.Errorf("expected message to mention 'below its learned', got %q", rec.Message)
			}
			if !strings.Contains(rec.Message, fmt.Sprintf("%.1f±%.1f people", rec.Baseline, rec.Sigma)) {
				t.Errorf("expected message to cite learned mean and sigma (%.1f±%.1f people), got %q",
					rec.Baseline, rec.Sigma, rec.Message)
			}
			if !strings.Contains(rec.Message, fmt.Sprintf("learned from %d samples", rec.Samples)) {
				t.Errorf("expected message to cite sample count (%d samples), got %q", rec.Samples, rec.Message)
			}
		}
	}
	if !foundLearned {
		t.Fatalf("expected learned turn_off_ac recommendation for non-zero under-utilized room (occ=2), got %+v", repLow.Recommendations)
	}

	// 2. Normal occupancy (e.g. 8 people) must NOT trigger turn_off_ac
	normalOccupancy := []ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Type: "office",
		Occupancy: 8, IrState: &irActive,
	}}
	repNormal := b.Recommend(normalOccupancy, 1.5, at, 10)
	for _, rec := range repNormal.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			t.Fatalf("FALSE POSITIVE: turn_off_ac recommended for normal occupancy (8 people): %+v", rec)
		}
	}

	// 3. Complete vacancy (0 people) in mature baseline triggers with Basis: "learned"
	zeroOccupancy := []ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Type: "office",
		Occupancy: 0, IrState: &irActive,
	}}
	repZero := b.Recommend(zeroOccupancy, 1.5, at, 10)
	var foundZeroLearned bool
	for _, rec := range repZero.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			foundZeroLearned = true
			if rec.Basis != "learned" {
				t.Errorf("mature zero occupancy: expected Basis == 'learned', got %q", rec.Basis)
			}
			if rec.Deviation > -1.5 {
				t.Errorf("expected Deviation <= -1.5, got %.2f", rec.Deviation)
			}
		}
	}
	if !foundZeroLearned {
		t.Fatalf("expected learned turn_off_ac recommendation for zero occupancy under mature baseline, got %+v", repZero.Recommendations)
	}
}

// TestOccupancyBaseline_MaturityTransitionStepByStep verifies the transition from
// Basis: "standard" at N=19 to Basis: "learned" at N=20.
func TestOccupancyBaseline_MaturityTransitionStepByStep(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	irActive := "COOL_22"
	b := NewBaselines()

	reading := []ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Type: "office",
		Occupancy: 0, IrState: &irActive,
	}}

	// Observe exactly 19 samples (N = 19 < baselineMature)
	for i := 0; i < baselineMature-1; i++ {
		b.Observe("zone-office-a", "occupancy", 6.0, at)
	}

	rep19 := b.Recommend(reading, 1.5, at, 10)
	var found19 bool
	for _, rec := range rep19.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			found19 = true
			if rec.Basis != "standard" {
				t.Fatalf("at N=%d, expected Basis == 'standard', got %q", baselineMature-1, rec.Basis)
			}
			if rec.Samples != 19 {
				t.Errorf("at N=19, expected Samples == 19, got %d", rec.Samples)
			}
		}
	}
	if !found19 {
		t.Fatalf("expected turn_off_ac recommendation at N=19")
	}

	// Add 20th sample to reach baselineMature
	b.Observe("zone-office-a", "occupancy", 6.0, at)

	rep20 := b.Recommend(reading, 1.5, at, 10)
	var found20 bool
	for _, rec := range rep20.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			found20 = true
			if rec.Basis != "learned" {
				t.Fatalf("at N=%d, expected Basis == 'learned', got %q", baselineMature, rec.Basis)
			}
			if rec.Samples != 20 {
				t.Errorf("at N=20, expected Samples == 20, got %d", rec.Samples)
			}
			if rec.Deviation > -1.5 {
				t.Errorf("expected Deviation <= -1.5, got %.2f", rec.Deviation)
			}
		}
	}
	if !found20 {
		t.Fatalf("expected turn_off_ac recommendation at N=20")
	}
}

// TestOccupancyBaseline_AcAlreadyOffSuppressesRecommendation verifies that if the AC is
// already OFF, no redundant turn_off_ac recommendation is produced in either cold or mature mode.
func TestOccupancyBaseline_AcAlreadyOffSuppressesRecommendation(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	irOff := "OFF"

	b := NewBaselines()
	for i := 0; i < 25; i++ {
		b.Observe("zone-office-a", "occupancy", 8.0, at)
	}

	readings := []ZoneReading{{
		Zone: "zone-office-a", Label: "office-a", Type: "office",
		Occupancy: 0, IrState: &irOff,
	}}

	rep := b.Recommend(readings, 1.5, at, 10)
	for _, rec := range rep.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			t.Fatalf("unexpected turn_off_ac recommendation when AC is already OFF: %+v", rec)
		}
	}
}

// TestOccupancyBaseline_EngineIntegration verifies end-to-end recommendation generation
// through the Engine interface with live zone telemetry.
func TestOccupancyBaseline_EngineIntegration(t *testing.T) {
	engine := NewEngine()
	engine.Zones = make(map[string]*ZoneSim)
	irActive := "COOL_22"
	engine.Zones["zone-office-a"] = &ZoneSim{
		Type:      "office",
		Temp:      24.0,
		Setpoint:  24.0,
		Occupancy: 2,
		IrState:   &irActive,
	}

	now := time.Now()
	// Train engine with 25 observations of occupancy = 8.0
	for i := 0; i < 25; i++ {
		engine.ObserveBaseline("zone-office-a", "occupancy", 8.0, now)
	}

	rep := engine.Recommendations(10)
	var found bool
	for _, rec := range rep.Recommendations {
		if rec.Zone == "zone-office-a" && rec.Action == "turn_off_ac" {
			found = true
			if rec.Basis != "learned" {
				t.Errorf("expected Basis == 'learned', got %q", rec.Basis)
			}
			if rec.Metric != "occupancy" {
				t.Errorf("expected Metric == 'occupancy', got %q", rec.Metric)
			}
			if rec.Samples < 20 {
				t.Errorf("expected Samples >= 20, got %d", rec.Samples)
			}
		}
	}
	if !found {
		t.Fatalf("expected engine.Recommendations to produce learned turn_off_ac recommendation, got %+v", rep.Recommendations)
	}
}
