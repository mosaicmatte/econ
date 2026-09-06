package simulation

import (
	"math"
	"testing"
	"time"
)

// ============================================================================
// CHALLENGE SCOPE 1: Adversarially Stress-Test Backend Occupancy Baseline
// ============================================================================

// 1.1: What happens if occupancy variance is 0?
func TestAdversarial_OccupancyVarianceZero(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	irActive := "COOL_22"

	// Sub-case A: Non-zero constant occupancy (e.g. room always had exactly 5 people)
	t.Run("ConstantNonZeroOccupancy_VarZero", func(t *testing.T) {
		b := NewBaselines()
		for i := 0; i < 30; i++ {
			b.Observe("zone-const-5", "occupancy", 5.0, at)
		}

		b.mu.Lock()
		st, ok := b.establishedStat("zone-const-5", "occupancy", at)
		b.mu.Unlock()

		if !ok || st == nil {
			t.Fatalf("expected established baseline")
		}
		if st.Var != 0.0 {
			t.Errorf("expected variance == 0, got %v", st.Var)
		}
		if st.std() != 0.0 {
			t.Errorf("expected std == 0, got %v", st.std())
		}

		// When variance is 0, score() uses math.Max(st.std(), spec.minSigma) = 0.5.
		// Test A.1: Current reading is 5 (equal to mean). z = 0.0 -> NO recommendation.
		repEqual := b.Recommend([]ZoneReading{{
			Zone: "zone-const-5", Label: "const-5", Occupancy: 5, IrState: &irActive,
		}}, 1.5, at, 10)
		for _, rec := range repEqual.Recommendations {
			if rec.Zone == "zone-const-5" && rec.Action == "turn_off_ac" {
				t.Errorf("unexpected turn_off_ac when occupancy equals constant mean: %+v", rec)
			}
		}

		// Test A.2: Current reading drops by 1 (occupancy = 4).
		// z = (4 - 5) / 0.5 = -2.0 <= -1.5 -> MUST trigger turn_off_ac with Basis: learned.
		repDrop1 := b.Recommend([]ZoneReading{{
			Zone: "zone-const-5", Label: "const-5", Occupancy: 4, IrState: &irActive,
		}}, 1.5, at, 10)
		var foundDrop1 bool
		for _, rec := range repDrop1.Recommendations {
			if rec.Zone == "zone-const-5" && rec.Action == "turn_off_ac" {
				foundDrop1 = true
				if rec.Basis != "learned" {
					t.Errorf("expected Basis == 'learned', got %s", rec.Basis)
				}
				if rec.Deviation != -2.0 {
					t.Errorf("expected Deviation == -2.0, got %f", rec.Deviation)
				}
				if rec.Severity != "info" { // -2.0 > -2.5 -> info
					t.Errorf("expected Severity == 'info', got %s", rec.Severity)
				}
			}
		}
		if !foundDrop1 {
			t.Errorf("expected turn_off_ac recommendation when occupancy dropped from 5 to 4 with zero variance")
		}

		// Test A.3: Current reading drops to 0 (occupancy = 0).
		// z = (0 - 5) / 0.5 = -10.0 <= -2.5 -> MUST trigger turn_off_ac with Severity: warning.
		repDrop0 := b.Recommend([]ZoneReading{{
			Zone: "zone-const-5", Label: "const-5", Occupancy: 0, IrState: &irActive,
		}}, 1.5, at, 10)
		var foundDrop0 bool
		for _, rec := range repDrop0.Recommendations {
			if rec.Zone == "zone-const-5" && rec.Action == "turn_off_ac" {
				foundDrop0 = true
				if rec.Deviation != -10.0 {
					t.Errorf("expected Deviation == -10.0, got %f", rec.Deviation)
				}
				if rec.Severity != "warning" {
					t.Errorf("expected Severity == 'warning', got %s", rec.Severity)
				}
			}
		}
		if !foundDrop0 {
			t.Errorf("expected turn_off_ac recommendation when occupancy dropped to 0")
		}
	})

	// Sub-case B: Zero constant occupancy (room was ALWAYS empty: 0 people for 30 samples)
	// ATTACK / STRESS SCENARIO: A room is normally vacant at this hour.
	// AC is left running (IrState: COOL_22). Room is currently vacant (Occupancy: 0).
	// Does the system turn off the AC?
	t.Run("ZeroConstantOccupancy_AC_Left_Running", func(t *testing.T) {
		b := NewBaselines()
		for i := 0; i < 30; i++ {
			b.Observe("zone-always-empty", "occupancy", 0.0, at)
		}

		b.mu.Lock()
		st, ok := b.establishedStat("zone-always-empty", "occupancy", at)
		b.mu.Unlock()

		if !ok || st == nil {
			t.Fatalf("expected established baseline")
		}
		if st.Mean != 0.0 || st.Var != 0.0 {
			t.Errorf("expected mean=0, var=0, got mean=%f, var=%f", st.Mean, st.Var)
		}

		// Room is empty (0 people) and AC is active!
		repEmpty := b.Recommend([]ZoneReading{{
			Zone: "zone-always-empty", Label: "empty-room", Occupancy: 0, IrState: &irActive,
		}}, 1.5, at, 10)

		var issuedTurnOffAc bool
		for _, rec := range repEmpty.Recommendations {
			if rec.Zone == "zone-always-empty" && rec.Action == "turn_off_ac" {
				issuedTurnOffAc = true
			}
		}

		// Document empirical behavior:
		// Because mean=0, z = (0 - 0) / 0.5 = 0.0.
		// Since z = 0.0 > -1.5, the learned model does NOT consider occupancy=0 an anomaly!
		// And because sc.mature is true (N=30 >= 20), the cold-start fallback (occupancy == 0) is bypassed!
		// RESULT: In a mature baseline where normal occupancy is 0, AC left ON in an empty room is NOT turned off!
		t.Logf("Empirical result for normally-empty room with AC running: issuedTurnOffAc = %v", issuedTurnOffAc)
		if !issuedTurnOffAc {
			t.Errorf("FAIL: When a zone matures with mean occupancy = 0, AC left ON in an empty room was NOT turned off!")
		}
	})
}

// 1.2: What happens right at sample count N = 19 vs N = 20?
func TestAdversarial_BoundaryN19vsN20(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	irActive := "COOL_22"

	t.Run("VacancyAtN19vsN20_Mean8", func(t *testing.T) {
		b := NewBaselines()
		// Feed 19 samples of occupancy = 8.0
		for i := 0; i < 19; i++ {
			b.Observe("zone-step", "occupancy", 8.0, at)
		}

		// At N = 19:
		// Case 1: Occupancy is 0
		rep19Zero := b.Recommend([]ZoneReading{{
			Zone: "zone-step", Label: "step", Occupancy: 0, IrState: &irActive,
		}}, 1.5, at, 10)

		var rec19Zero *Recommendation
		for i := range rep19Zero.Recommendations {
			if rep19Zero.Recommendations[i].Zone == "zone-step" && rep19Zero.Recommendations[i].Action == "turn_off_ac" {
				rec19Zero = &rep19Zero.Recommendations[i]
				break
			}
		}
		if rec19Zero == nil {
			t.Fatalf("at N=19 with occ=0, expected turn_off_ac recommendation")
		}
		if rec19Zero.Basis != "standard" {
			t.Errorf("at N=19 with occ=0, expected Basis == 'standard', got %s", rec19Zero.Basis)
		}
		if rec19Zero.Samples != 19 {
			t.Errorf("at N=19, expected Samples == 19, got %d", rec19Zero.Samples)
		}

		// Case 2: Occupancy is 2 (under-utilized, non-zero).
		// At N = 19, immature baseline relies on cold-start (occ == 0). So occ = 2 must NOT trigger!
		rep19Two := b.Recommend([]ZoneReading{{
			Zone: "zone-step", Label: "step", Occupancy: 2, IrState: &irActive,
		}}, 1.5, at, 10)
		for _, rec := range rep19Two.Recommendations {
			if rec.Zone == "zone-step" && rec.Action == "turn_off_ac" {
				t.Errorf("at N=19 with occ=2, unexpected turn_off_ac recommendation: %+v", rec)
			}
		}

		// Now add exactly the 20th sample!
		b.Observe("zone-step", "occupancy", 8.0, at)

		// At N = 20:
		// Case 1: Occupancy is 0
		rep20Zero := b.Recommend([]ZoneReading{{
			Zone: "zone-step", Label: "step", Occupancy: 0, IrState: &irActive,
		}}, 1.5, at, 10)
		var rec20Zero *Recommendation
		for i := range rep20Zero.Recommendations {
			if rep20Zero.Recommendations[i].Zone == "zone-step" && rep20Zero.Recommendations[i].Action == "turn_off_ac" {
				rec20Zero = &rep20Zero.Recommendations[i]
				break
			}
		}
		if rec20Zero == nil {
			t.Fatalf("at N=20 with occ=0, expected turn_off_ac recommendation")
		}
		if rec20Zero.Basis != "learned" {
			t.Errorf("at N=20 with occ=0, expected Basis == 'learned', got %s", rec20Zero.Basis)
		}
		if rec20Zero.Samples != 20 {
			t.Errorf("at N=20, expected Samples == 20, got %d", rec20Zero.Samples)
		}

		// Case 2: Occupancy is 2.
		// At N = 20, mature baseline triggers learned anomaly: z = (2 - 8) / 0.5 = -12.0 <= -1.5!
		rep20Two := b.Recommend([]ZoneReading{{
			Zone: "zone-step", Label: "step", Occupancy: 2, IrState: &irActive,
		}}, 1.5, at, 10)
		var rec20Two *Recommendation
		for i := range rep20Two.Recommendations {
			if rep20Two.Recommendations[i].Zone == "zone-step" && rep20Two.Recommendations[i].Action == "turn_off_ac" {
				rec20Two = &rep20Two.Recommendations[i]
				break
			}
		}
		if rec20Two == nil {
			t.Fatalf("at N=20 with occ=2, expected learned turn_off_ac recommendation")
		}
		if rec20Two.Basis != "learned" {
			t.Errorf("at N=20 with occ=2, expected Basis == 'learned', got %s", rec20Two.Basis)
		}
		if rec20Two.Samples != 20 {
			t.Errorf("at N=20, expected Samples == 20, got %d", rec20Two.Samples)
		}
	})
}

// 1.3: What happens with extreme outlier occupancy values?
func TestAdversarial_ExtremeOutlierOccupancy(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)
	irActive := "COOL_22"

	t.Run("ObservationPoisoning_ExtremeHighValue", func(t *testing.T) {
		b := NewBaselines()
		for i := 0; i < 20; i++ {
			b.Observe("zone-poison", "occupancy", 5.0, at)
		}

		// Inject extreme outlier observation: 1,000,000 occupants!
		b.Observe("zone-poison", "occupancy", 1_000_000.0, at)

		b.mu.Lock()
		st := b.stats[baselineKey("zone-poison", "occupancy")][at.Hour()]
		b.mu.Unlock()

		t.Logf("After 1,000,000 outlier: Mean = %f, Var = %e, Std = %f", st.Mean, st.Var, st.std())

		// Now test what happens when occupancy is 0 (room is completely vacant).
		rep := b.Recommend([]ZoneReading{{
			Zone: "zone-poison", Label: "poison", Occupancy: 0, IrState: &irActive,
		}}, 1.5, at, 10)

		var triggered bool
		for _, rec := range rep.Recommendations {
			if rec.Zone == "zone-poison" && rec.Action == "turn_off_ac" {
				triggered = true
				t.Logf("Deviation for occ=0 after poison: %f", rec.Deviation)
			}
		}
		// Notice: d = 1e6 - 5 = ~1e6. a = 0.04. Mean jumps to ~40,000.
		// Var = 0.96 * (0 + 0.04 * 1e12) = 3.84e10. Std = 195,959.
		// For occ = 0: z = (0 - 40000) / 195959 = -0.204.
		// Since -0.204 > -1.5, it FAILS to trigger turn_off_ac!
		t.Logf("Empirical result for occ=0 after 1M poison: triggered = %v", triggered)
		if !triggered {
			t.Errorf("FAIL: Extreme high observation suppressed vacancy alert!")
		}
		if st.Var > 20000.0 {
			t.Errorf("FAIL: Observation was not properly clamped; variance exploded to %e", st.Var)
		}
	})

	t.Run("ReadingOutlier_NegativeOccupancy", func(t *testing.T) {
		b := NewBaselines()
		for i := 0; i < 25; i++ {
			b.Observe("zone-neg", "occupancy", 5.0, at)
		}

		// Current reading has corrupted negative occupancy (e.g. -100)
		rep := b.Recommend([]ZoneReading{{
			Zone: "zone-neg", Label: "neg", Occupancy: -100, IrState: &irActive,
		}}, 1.5, at, 10)

		for _, rec := range rep.Recommendations {
			if rec.Zone == "zone-neg" && rec.Action == "turn_off_ac" {
				t.Logf("Negative occupancy (-100) produced: Deviation = %f, Severity = %s", rec.Deviation, rec.Severity)
				if rec.Deviation > -1.5 {
					t.Errorf("expected highly negative deviation, got %f", rec.Deviation)
				}
			}
		}
	})

	t.Run("ReadingOutlier_ExtremePositiveOccupancy", func(t *testing.T) {
		b := NewBaselines()
		for i := 0; i < 25; i++ {
			b.Observe("zone-huge", "occupancy", 5.0, at)
		}

		// Current reading has huge positive occupancy: 99,999
		rep := b.Recommend([]ZoneReading{{
			Zone: "zone-huge", Label: "huge", Occupancy: 99999, IrState: &irActive,
		}}, 1.5, at, 10)

		for _, rec := range rep.Recommendations {
			if rec.Zone == "zone-huge" && rec.Action == "turn_off_ac" {
				t.Errorf("unexpected turn_off_ac for massive occupancy: %+v", rec)
			}
		}
	})

	t.Run("ObservationOutlier_NaN_Inf_HandledSafely", func(t *testing.T) {
		b := NewBaselines()
		b.Observe("zone-nan", "occupancy", math.NaN(), at)
		b.Observe("zone-nan", "occupancy", math.Inf(1), at)
		b.Observe("zone-nan", "occupancy", math.Inf(-1), at)

		b.mu.Lock()
		st := b.stats[baselineKey("zone-nan", "occupancy")]
		b.mu.Unlock()

		if st != nil {
			t.Errorf("expected NaN/Inf to be completely rejected by Observe, but found bucket: %+v", st)
		}
	})
}

// 1.4: Does it ever issue turn_off_ac if AC is already off?
func TestAdversarial_AcAlreadyOffScenarios(t *testing.T) {
	at := time.Date(2026, 7, 21, 14, 0, 0, 0, time.Local)

	b := NewBaselines()
	for i := 0; i < 25; i++ {
		b.Observe("zone-ac-test", "occupancy", 8.0, at)
	}

	testCases := []struct {
		name       string
		irState    *string
		shouldSkip bool
	}{
		{
			name:       "IrState_OFF_Exact",
			irState:    ptr("OFF"),
			shouldSkip: true, // MUST NOT issue turn_off_ac
		},
		{
			name:       "IrState_Nil_Unknown",
			irState:    nil,
			shouldSkip: false, // State unknown -> will issue turn_off_ac
		},
		{
			name:       "IrState_off_Lowercase",
			irState:    ptr("off"),
			shouldSkip: true, // Case-insensitive: MUST suppress redundant turn_off_ac
		},
		{
			name:       "IrState_Off_MixedCase",
			irState:    ptr("Off"),
			shouldSkip: true, // Case-insensitive: MUST suppress redundant turn_off_ac
		},
		{
			name:       "IrState_STANDBY",
			irState:    ptr("STANDBY"),
			shouldSkip: false,
		},
		{
			name:       "IrState_COOL_22",
			irState:    ptr("COOL_22"),
			shouldSkip: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rep := b.Recommend([]ZoneReading{{
				Zone: "zone-ac-test", Label: "ac-test", Occupancy: 0, IrState: tc.irState,
			}}, 1.5, at, 10)

			var issuedTurnOffAc bool
			for _, rec := range rep.Recommendations {
				if rec.Zone == "zone-ac-test" && rec.Action == "turn_off_ac" {
					issuedTurnOffAc = true
					break
				}
			}

			if tc.shouldSkip && issuedTurnOffAc {
				t.Errorf("[%s] FAILED: turn_off_ac was issued even though IrState was %v", tc.name, *tc.irState)
			}
			if !tc.shouldSkip && !issuedTurnOffAc {
				t.Errorf("[%s] FAILED: turn_off_ac was NOT issued even though IrState was %v", tc.name, formatPtr(tc.irState))
			}
			t.Logf("[%s] IrState=%v -> issuedTurnOffAc=%v (expected skip: %v)",
				tc.name, formatPtr(tc.irState), issuedTurnOffAc, tc.shouldSkip)
		})
	}
}

func ptr[T any](v T) *T {
	return &v
}

func formatPtr(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}

// ============================================================================
// CHALLENGE SCOPE 2: Adversarially Stress-Test Edge Compute Offload & Server DSP
// ============================================================================

// 2.1: What happens if the raw sample stream contains all zeroes or all 4095 (ADC rail)?
func TestAdversarial_DSP_AdcRailAllZeroesAndAll4095(t *testing.T) {
	denoiser := NewCurrentDenoiser(DefaultCurrentDenoiseConfig())

	// Test A: 100 samples of all 0 (GND rail)
	samplesZero := make([]int, 100)
	for i := range samplesZero {
		samplesZero[i] = 0
	}

	ampsZero := denoiser.ProcessWindow(samplesZero)
	t.Logf("ProcessWindow(all 0s) = %f A", ampsZero)
	if ampsZero != 0.0 {
		t.Errorf("expected exactly 0.0 A for solid zero ADC ground rail, got %f", ampsZero)
	}

	// Test B: 100 samples of all 4095 (VCC rail / saturation)
	samples4095 := make([]int, 100)
	for i := range samples4095 {
		samples4095[i] = 4095
	}

	amps4095 := denoiser.ProcessWindow(samples4095)
	t.Logf("ProcessWindow(all 4095s) = %f A", amps4095)
	if amps4095 != 0.0 {
		t.Errorf("expected exactly 0.0 A for solid 4095 ADC Vcc rail (zero AC variance), got %f", amps4095)
	}

	// Test C: ProcessWindowStats with all zero (sum=0, sumSq=0)
	statsZero := denoiser.ProcessWindowStats(0.0, 0.0, 100)
	if statsZero != 0.0 {
		t.Errorf("expected 0.0 A for ProcessWindowStats(0, 0), got %f", statsZero)
	}

	// Test D: ProcessWindowStats with all 4095 (sum=409500, sumSq=4095^2 * 100)
	sum4095 := 4095.0 * 100.0
	sumSq4095 := 4095.0 * 4095.0 * 100.0
	stats4095 := denoiser.ProcessWindowStats(sum4095, sumSq4095, 100)
	if stats4095 != 0.0 {
		t.Errorf("expected 0.0 A for ProcessWindowStats(all 4095), got %f", stats4095)
	}
}

// 2.2: What happens if sample starvation occurs?
func TestAdversarial_DSP_SampleStarvation(t *testing.T) {
	denoiser := NewCurrentDenoiser(DefaultCurrentDenoiseConfig())

	starvationCounts := []int{0, 1, 2, 5, 10, 19}
	for _, n := range starvationCounts {
		samples := make([]int, n)
		for i := range samples {
			samples[i] = 2048
		}
		amps := denoiser.ProcessWindow(samples)
		if amps != -1.0 {
			t.Errorf("for n=%d samples (< MinSamples 20), expected -1.0 (starvation error), got %f", n, amps)
		}

		statsAmps := denoiser.ProcessWindowStats(float64(n*2048), float64(n*2048*2048), n)
		if statsAmps != -1.0 {
			t.Errorf("for stats n=%d (< MinSamples 20), expected -1.0, got %f", n, statsAmps)
		}
	}

	// Nil slice
	if amps := denoiser.ProcessWindow(nil); amps != -1.0 {
		t.Errorf("for nil samples, expected -1.0, got %f", amps)
	}

	// Exactly MinSamples = 20 (threshold boundary)
	samples20 := make([]int, 20)
	for i := range samples20 {
		samples20[i] = 2048
	}
	amps20 := denoiser.ProcessWindow(samples20)
	if amps20 < 0.0 {
		t.Errorf("for n=20 (MinSamples), expected non-negative reading (>= 0.0), got %f", amps20)
	}
}

// 2.3: Verification that server-side Go DSP accurately filters spikes
func TestAdversarial_DSP_SpikeFilteringFidelity(t *testing.T) {
	cfg := DefaultCurrentDenoiseConfig()
	denoiserClean := NewCurrentDenoiser(cfg)
	denoiserSpiked := NewCurrentDenoiser(cfg)

	n := 100 // 5 mains cycles at 50Hz, 20 samples/cycle
	cleanWave := make([]int, n)
	spikedWave := make([]int, n)

	// Generate clean 50Hz sine wave (1.5A RMS: peak counts ~ 186 counts around center 2048)
	// Peak Amps = 1.5 * sqrt(2) = 2.1213 A
	// Peak Volts (sensor) = 2.1213 / 15.0 = 0.1414 V
	// Peak Volts (ADC pin after 0.5 divider) = 0.0707 V
	// Peak Counts = 0.0707 * 4095 / 3.3 = 87.7 counts
	peakCounts := 87.7
	for i := 0; i < n; i++ {
		theta := 2.0 * math.Pi * float64(i) / 20.0
		val := 2048.0 + peakCounts*math.Sin(theta)
		cleanWave[i] = int(math.Round(val))
		spikedWave[i] = cleanWave[i]
	}

	// Inject isolated 1-sample spikes exceeding 50 counts
	spikedWave[15] += 300 // Huge +300 spike
	spikedWave[45] -= 250 // Huge -250 spike
	spikedWave[75] += 400 // Huge +400 spike

	ampsClean := denoiserClean.ProcessWindow(cleanWave)
	ampsSpiked := denoiserSpiked.ProcessWindow(spikedWave)

	diff := math.Abs(ampsSpiked - ampsClean)
	t.Logf("ampsClean = %f A, ampsSpiked = %f A, diff = %f A", ampsClean, ampsSpiked, diff)

	// With 3-point spike suppression, the difference should be minimal (< 0.05 A)
	if diff > 0.05 {
		t.Errorf("spike filter failed to suppress transient spikes: clean=%f, spiked=%f, diff=%f (> 0.05 A)",
			ampsClean, ampsSpiked, diff)
	}
}

// 2.4: Direct cross-language equivalence with C++ CurrentDenoiser
func TestAdversarial_DSP_ExactCppParity(t *testing.T) {
	cfg := DefaultCurrentDenoiseConfig()
	denoiser := NewCurrentDenoiser(cfg)

	// Vector 1: SOLID_0
	s0 := make([]int, 100)
	res0 := denoiser.ProcessWindow(s0)
	if res0 != 0.0 {
		t.Errorf("SOLID_0: expected 0.0, got %f", res0)
	}

	// Vector 2: SOLID_4095
	denoiser.Reset()
	s4095 := make([]int, 100)
	for i := range s4095 {
		s4095[i] = 4095
	}
	res4095 := denoiser.ProcessWindow(s4095)
	if res4095 != 0.0 {
		t.Errorf("SOLID_4095: expected 0.0, got %f", res4095)
	}

	// Vector 3: SINE_2A (identical to C++ test)
	denoiser.Reset()
	s2A := make([]int, 100)
	for i := 0; i < 100; i++ {
		ti := float64(i) / 1000.0
		val := 1550.0 + 117.0*math.Sin(2.0*math.Pi*50.0*ti)
		s2A[i] = int(math.Round(val))
	}
	res2A := denoiser.ProcessWindow(s2A)
	// C++ output was: 1.956525
	cppRes2A := 1.956525
	t.Logf("SINE_2A: Go = %.6f, C++ = %.6f, diff = %.6f", res2A, cppRes2A, math.Abs(res2A-cppRes2A))
	if math.Abs(res2A-cppRes2A) > 1e-4 {
		t.Errorf("SINE_2A disparity between Go and C++: Go=%.6f, C++=%.6f", res2A, cppRes2A)
	}

	// Vector 4: SINE_2A_SPIKED (identical to C++ test)
	denoiser.Reset()
	s2ASpiked := make([]int, 100)
	copy(s2ASpiked, s2A)
	s2ASpiked[15] += 200
	s2ASpiked[45] -= 180
	s2ASpiked[75] += 250
	res2ASpiked := denoiser.ProcessWindow(s2ASpiked)
	// C++ output was: 1.949971
	cppRes2ASpiked := 1.949971
	t.Logf("SINE_2A_SPIKED: Go = %.6f, C++ = %.6f, diff = %.6f", res2ASpiked, cppRes2ASpiked, math.Abs(res2ASpiked-cppRes2ASpiked))
	if math.Abs(res2ASpiked-cppRes2ASpiked) > 1e-4 {
		t.Errorf("SINE_2A_SPIKED disparity between Go and C++: Go=%.6f, C++=%.6f", res2ASpiked, cppRes2ASpiked)
	}
}

