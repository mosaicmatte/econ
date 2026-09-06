package simulation

import (
	"math"
	"math/rand"
	"testing"
)

// generateMockWaveform synthesizes ADC counts for an AC sinusoidal current.
// Matches the signal generation model from edge/esp32/test/test_denoise.cpp.
func generateMockWaveform(
	rmsAmps float64,
	noiseSigma float64,
	numSpikes int,
	sampleRateHz float64,
	durationSec float64,
	freqHz float64,
	dcVolts float64,
	calAPerV float64,
	dividerRatio float64,
	seed int64,
) []int {
	nSamples := int(durationSec * sampleRateHz)
	samples := make([]int, nSamples)

	// Peak voltage at the ADC pin after divider
	vPeakAdc := (rmsAmps * math.Sqrt(2.0) / calAPerV) * dividerRatio
	dt := 1.0 / sampleRateHz

	rng := rand.New(rand.NewSource(seed))

	for i := 0; i < nSamples; i++ {
		t := float64(i) * dt
		vAc := vPeakAdc * math.Sin(2.0*math.Pi*freqHz*t)
		vTotal := dcVolts + vAc
		noise := rng.NormFloat64() * noiseSigma
		rawCount := vTotal*(4095.0/3.3) + noise
		count := int(math.Round(rawCount))
		if count < 0 {
			count = 0
		}
		if count > 4095 {
			count = 4095
		}
		samples[i] = count
	}

	// Inject isolated transient spikes
	if numSpikes > 0 && nSamples > 10 {
		for k := 0; k < numSpikes; k++ {
			idx := 2 + rng.Intn(nSamples-4)
			sign := 1.0
			if rng.Intn(2) == 0 {
				sign = -1.0
			}
			spike := 35.0 + rng.Float64()*55.0
			spiked := samples[idx] + int(math.Round(sign*spike))
			if spiked < 0 {
				spiked = 0
			}
			if spiked > 4095 {
				spiked = 4095
			}
			samples[idx] = spiked
		}
	}

	return samples
}

// C-reference computation reproducing C++ CurrentDenoiser::processWindow directly
func cRefProcessWindow(samples []int, cfg CurrentDenoiseConfig) float64 {
	n := len(samples)
	if n < cfg.MinSamples {
		return -1.0
	}

	cleanSamples := make([]float64, n)
	sum := 0.0

	for i := 0; i < n; i++ {
		val := float64(samples[i])
		if i > 0 && i < n-1 {
			prev := float64(samples[i-1])
			next := float64(samples[i+1])
			d1 := val - prev
			d2 := val - next
			if (d1 > 50.0 && d2 > 50.0) || (d1 < -50.0 && d2 < -50.0) {
				val = 0.5 * (prev + next)
			}
		}
		cleanSamples[i] = val
		sum += val
	}

	mean := sum / float64(n)

	period50 := 0
	if n >= 20 {
		period50 = n / 5
	}
	period60 := 0
	if n >= 24 {
		period60 = int(math.Round(float64(n) / 6.0))
	}
	slope := 0.0

	if period50 > 0 && period50 < n {
		diffSum50 := 0.0
		diffSqSum50 := 0.0
		count50 := n - period50
		for i := 0; i < count50; i++ {
			d := cleanSamples[i+period50] - cleanSamples[i]
			diffSum50 += d
			diffSqSum50 += d * d
		}
		e50 := diffSqSum50 / float64(count50)

		if period60 > 0 && period60 < n {
			diffSum60 := 0.0
			diffSqSum60 := 0.0
			count60 := n - period60
			for i := 0; i < count60; i++ {
				d := cleanSamples[i+period60] - cleanSamples[i]
				diffSum60 += d
				diffSqSum60 += d * d
			}
			e60 := diffSqSum60 / float64(count60)

			if e60 < 0.7*e50 {
				slope = (diffSum60 / float64(count60)) / float64(period60)
			} else {
				slope = (diffSum50 / float64(count50)) / float64(period50)
			}
		} else {
			slope = (diffSum50 / float64(count50)) / float64(period50)
		}
	}

	iMean := 0.5 * float64(n-1)
	sumSqDiff := 0.0
	for i := 0; i < n; i++ {
		trend := mean + slope*(float64(i)-iMean)
		cleanSamples[i] -= trend
		sumSqDiff += cleanSamples[i] * cleanSamples[i]
	}
	variance := sumSqDiff / float64(n)

	signalVariance := 0.0
	if variance > cfg.NoiseVariance {
		signalVariance = variance - cfg.NoiseVariance
	}
	rmsCounts := math.Sqrt(signalVariance)
	rawAmps := rmsCounts * (cfg.AdcVref / cfg.AdcMaxCounts) / cfg.DividerRatio * cfg.CalAPerV

	if rawAmps < 1.50 && variance > 1e-6 {
		halfPeriod50 := 0
		if n >= 10 {
			halfPeriod50 = n / 10
		}
		halfPeriod60 := 0
		if n >= 12 {
			halfPeriod60 = int(math.Round(float64(n) / 12.0))
		}

		minNormCov := 1.0
		checked := false

		if halfPeriod50 > 0 && halfPeriod50 < n {
			covSum50 := 0.0
			covCount50 := n - halfPeriod50
			for i := 0; i < covCount50; i++ {
				covSum50 += cleanSamples[i] * cleanSamples[i+halfPeriod50]
			}
			normCov50 := (covSum50 / float64(covCount50)) / variance
			minNormCov = normCov50
			checked = true
		}

		if halfPeriod60 > 0 && halfPeriod60 < n {
			covSum60 := 0.0
			covCount60 := n - halfPeriod60
			for i := 0; i < covCount60; i++ {
				covSum60 += cleanSamples[i] * cleanSamples[i+halfPeriod60]
			}
			normCov60 := (covSum60 / float64(covCount60)) / variance
			if checked {
				if normCov60 < minNormCov {
					minNormCov = normCov60
				}
			} else {
				minNormCov = normCov60
				checked = true
			}
		}

		if checked && minNormCov > -0.20 {
			rawAmps = 0.0
		}
	}

	if rawAmps < cfg.CutoffAmps {
		return 0.0
	}
	return rawAmps
}

// TestZeroSignalInvariance verifies that pure noise at 0A produces strictly 0.000A.
func TestZeroSignalInvariance(t *testing.T) {
	cfg := DefaultCurrentDenoiseConfig()
	denoiser := NewCurrentDenoiser(cfg)

	for win := 0; win < 50; win++ {
		noiseSigma := 10.0 + float64(win%10)
		samples := generateMockWaveform(0.0, noiseSigma, 3, 10000.0, 0.100, 50.0, 1.25, 15.0, 0.5, int64(win*997+1))
		result := denoiser.ProcessWindow(samples)
		if result != 0.0 {
			t.Fatalf("Window %d: expected strictly 0.000A on zero signal, got %.4f A", win, result)
		}
	}
}

// TestWaveformAccuracyAndCppEquivalence verifies that Go DSP output matches expected RMS
// and is identical to C++ reference within 1% across multiple test amplitudes.
func TestWaveformAccuracyAndCppEquivalence(t *testing.T) {
	cfg := DefaultCurrentDenoiseConfig()
	testAmps := []float64{0.5, 1.0, 2.0, 5.0, 10.0}
	noiseSigma := math.Sqrt(cfg.NoiseVariance) // ~17.32 counts, matching intrinsic noise variance

	for _, targetAmps := range testAmps {
		denoiser := NewCurrentDenoiser(cfg)

		// Run 30 consecutive windows to settle EMA
		var lastResult float64
		for win := 0; win < 30; win++ {
			samples := generateMockWaveform(targetAmps, noiseSigma, 2, 10000.0, 0.100, 50.0, 1.25, 15.0, 0.5, int64(win*313+7))
			lastResult = denoiser.ProcessWindow(samples)

			// Verify single-window output against C++ reference matches within 1%
			cRef := cRefProcessWindow(samples, cfg)
			singleGo := NewCurrentDenoiser(cfg).ProcessWindow(samples)
			diff := math.Abs(singleGo - cRef)
			relDiff := diff / math.Max(cRef, 1e-3)
			if relDiff > 0.01 {
				t.Fatalf("Target %.1fA window %d: Go output %.6f differs from C++ reference %.6f by %.4f%% (>1%% limit)",
					targetAmps, win, singleGo, cRef, relDiff*100)
			}
		}

		// Reconstructed value should be within 5% of true ground truth RMS
		errorPct := math.Abs(lastResult-targetAmps) / targetAmps * 100.0
		if errorPct > 5.0 {
			t.Fatalf("Target %.1fA: settled output %.4f differs from ground truth by %.2f%% (>5%%)",
				targetAmps, lastResult, errorPct)
		}
	}
}

// TestDecimatedWaveformProcessing verifies that the server can denoise a 30-sample decimated array
// streamed during edge node offload fallback.
func TestDecimatedWaveformProcessing(t *testing.T) {
	cfg := DefaultCurrentDenoiseConfig()
	denoiser := NewCurrentDenoiser(cfg)

	// Synthesize 30 decimated samples (100 ms at 300 Hz)
	targetAmps := 2.5
	samples := generateMockWaveform(targetAmps, 5.0, 0, 300.0, 0.100, 50.0, 1.25, 15.0, 0.5, 42)
	if len(samples) != 30 {
		t.Fatalf("Expected 30 samples, got %d", len(samples))
	}

	result := denoiser.ProcessWindow(samples)
	if result <= 0.0 {
		t.Fatalf("Expected positive amperage for 2.5A decimated waveform, got %.4f", result)
	}

	relError := math.Abs(result-targetAmps) / targetAmps
	if relError > 0.10 {
		t.Fatalf("Decimated waveform reconstructed amperage %.4f deviated by %.1f%% from 2.5A",
			result, relError*100)
	}
}

// TestStepResponseAndZeroSnapping verifies fast rise time and instant zero snap without lingering decay.
func TestStepResponseAndZeroSnapping(t *testing.T) {
	cfg := DefaultCurrentDenoiseConfig()
	denoiser := NewCurrentDenoiser(cfg)

	// 1. Initial 0A baseline
	zeroSamples := generateMockWaveform(0.0, 10.0, 1, 10000.0, 0.100, 50.0, 1.25, 15.0, 0.5, 1)
	if r := denoiser.ProcessWindow(zeroSamples); r != 0.0 {
		t.Fatalf("Expected 0.0, got %.4f", r)
	}

	// 2. Step up: 0A -> 4.0A
	stepSamples := generateMockWaveform(4.0, 12.0, 2, 10000.0, 0.100, 50.0, 1.25, 15.0, 0.5, 2)
	w1 := denoiser.ProcessWindow(stepSamples)
	if w1 < 3.5 || w1 > 4.5 {
		t.Fatalf("Window 1 after step: expected ~4.0A, got %.4f", w1)
	}

	// 3. Step down: 4.0A -> 0A (must snap to 0.0 immediately)
	snapZero := denoiser.ProcessWindow(zeroSamples)
	if snapZero != 0.0 {
		t.Fatalf("Expected instant zero-snap to 0.000A, got %.4f (lingering EMA tail)", snapZero)
	}
	if denoiser.IsInitialized() {
		t.Fatalf("Denoiser should reset initialized state upon zero-snapping")
	}
}

// TestStarvationGuard verifies that windows with insufficient samples return -1.0.
func TestStarvationGuard(t *testing.T) {
	cfg := DefaultCurrentDenoiseConfig()
	denoiser := NewCurrentDenoiser(cfg)

	starvedSamples := make([]int, 10)
	for i := range starvedSamples {
		starvedSamples[i] = 1550
	}

	result := denoiser.ProcessWindow(starvedSamples)
	if result != -1.0 {
		t.Fatalf("Expected -1.0 for starved sample count (<20), got %.4f", result)
	}
}

// TestStatisticalWindowProcessing verifies ProcessWindowStats against manual calculation.
func TestStatisticalWindowProcessing(t *testing.T) {
	cfg := DefaultCurrentDenoiseConfig()
	denoiser := NewCurrentDenoiser(cfg)

	samples := generateMockWaveform(3.0, 10.0, 0, 10000.0, 0.100, 50.0, 1.25, 15.0, 0.5, 123)
	sum := 0.0
	sumSq := 0.0
	for _, s := range samples {
		sum += float64(s)
		sumSq += float64(s) * float64(s)
	}

	statResult := denoiser.ProcessWindowStats(sum, sumSq, len(samples))
	if statResult < 2.5 || statResult > 3.5 {
		t.Fatalf("Expected ~3.0A from statistical processing, got %.4f", statResult)
	}
}
