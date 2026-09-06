package simulation

import (
	"math"
)

// CurrentDenoiseConfig specifies the parameters for the two-stage current sensor denoising pipeline.
// Ported with 1:1 mathematical fidelity from edge/esp32/src/current_denoiser.h.
type CurrentDenoiseConfig struct {
	CalAPerV      float64 // A/V sensitivity (ACS712-30A: 66.6 mV/A -> 15.0 A/V)
	DividerRatio  float64 // ACS712 10k/10k divider ratio (0.5 halves voltage swing to protect 3.3V ADC)
	AdcVref       float64 // ESP32 ADC reference voltage (3.3V)
	AdcMaxCounts  float64 // 12-bit ADC resolution (4095 counts full scale)
	NoiseVariance float64 // ADC intrinsic noise variance (~300.0 counts^2, sigma ~17.3 counts)
	CutoffAmps    float64 // Sub-threshold noise floor cutoff (< 0.15A -> 0.0A)
	EmaAlpha      float64 // Inter-window EMA smoothing factor (0.35)
	DeadbandAmps  float64 // Jitter deadband (0.03A holds steady reading within band)
	MinSamples    int     // Minimum samples per window (starvation guard, defaults to 20)
}

// DefaultCurrentDenoiseConfig returns the production configuration matching edge/esp32 firmware.
func DefaultCurrentDenoiseConfig() CurrentDenoiseConfig {
	return CurrentDenoiseConfig{
		CalAPerV:      15.0,
		DividerRatio:  0.5,
		AdcVref:       3.3,
		AdcMaxCounts:  4095.0,
		NoiseVariance: 300.0,
		CutoffAmps:    0.15,
		EmaAlpha:      0.35,
		DeadbandAmps:  0.03,
		MinSamples:    20,
	}
}

// CurrentDenoiser implements the two-stage DSP pipeline for AC current sensors.
// Stage 1: 3-point spike suppression, dynamic linear period detrending (50Hz/60Hz),
//
//	noise variance subtraction, and autocorrelation periodicity gate.
//
// Stage 2: Inter-window Exponential Moving Average (EMA) with jitter deadband and instant zero-snapping.
type CurrentDenoiser struct {
	cfg          CurrentDenoiseConfig
	filteredAmps float64
	initialized  bool
}

// NewCurrentDenoiser creates a new denoiser instance with the given configuration.
func NewCurrentDenoiser(cfg CurrentDenoiseConfig) *CurrentDenoiser {
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 20
	}
	return &CurrentDenoiser{
		cfg:          cfg,
		filteredAmps: 0.0,
		initialized:  false,
	}
}

// Reset clears the internal EMA state.
func (d *CurrentDenoiser) Reset() {
	d.filteredAmps = 0.0
	d.initialized = false
}

// GetFilteredAmps returns the current EMA filtered amperage.
func (d *CurrentDenoiser) GetFilteredAmps() float64 {
	return d.filteredAmps
}

// IsInitialized returns true if the denoiser has established a baseline reading.
func (d *CurrentDenoiser) IsInitialized() bool {
	return d.initialized
}

// SetCal updates the A/V calibration factor.
func (d *CurrentDenoiser) SetCal(cal float64) {
	d.cfg.CalAPerV = cal
}

// SetDividerRatio updates the voltage divider ratio.
func (d *CurrentDenoiser) SetDividerRatio(ratio float64) {
	d.cfg.DividerRatio = ratio
}

// SetNoiseVariance updates the noise variance subtraction threshold.
func (d *CurrentDenoiser) SetNoiseVariance(nv float64) {
	d.cfg.NoiseVariance = nv
}

// SetCutoffAmps updates the noise floor cutoff.
func (d *CurrentDenoiser) SetCutoffAmps(cutoff float64) {
	d.cfg.CutoffAmps = cutoff
}

// ProcessWindow executes the complete two-stage denoising pipeline over an array of raw ADC counts.
// Returns the filtered current in Amperes, or -1.0 if sample count is starved (< MinSamples).
func (d *CurrentDenoiser) ProcessWindow(samples []int) float64 {
	n := len(samples)
	minSamples := d.cfg.MinSamples
	if minSamples <= 0 {
		minSamples = 20
	}
	if n < minSamples {
		return -1.0 // Starvation guard
	}

	// Pass 1: Transient spike suppression and sum accumulation
	cleanSamples := make([]float64, n)
	sum := 0.0

	for i := 0; i < n; i++ {
		val := float64(samples[i])
		if i > 0 && i < n-1 {
			prev := float64(samples[i-1])
			next := float64(samples[i+1])
			d1 := val - prev
			d2 := val - next
			// Suppress isolated single-sample transient spikes exceeding 50 counts
			if (d1 > 50.0 && d2 > 50.0) || (d1 < -50.0 && d2 < -50.0) {
				val = 0.5 * (prev + next)
			}
		}
		cleanSamples[i] = val
		sum += val
	}

	mean := sum / float64(n)

	// Linear detrending to eliminate intra-window dynamic DC offset drift.
	// Compute the slope via period-differencing to ensure zero attenuation on AC mains (50Hz / 60Hz) waveforms.
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
			diff := cleanSamples[i+period50] - cleanSamples[i]
			diffSum50 += diff
			diffSqSum50 += diff * diff
		}
		e50 := diffSqSum50 / float64(count50)

		if period60 > 0 && period60 < n {
			diffSum60 := 0.0
			diffSqSum60 := 0.0
			count60 := n - period60
			for i := 0; i < count60; i++ {
				diff := cleanSamples[i+period60] - cleanSamples[i]
				diffSum60 += diff
				diffSqSum60 += diff * diff
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

	// Pass 2: Linear detrending (subtracting trend = mean + slope*(i - iMean)) and variance computation
	sumSqDiff := 0.0
	for i := 0; i < n; i++ {
		trend := mean + slope*(float64(i)-iMean)
		cleanSamples[i] -= trend
		sumSqDiff += cleanSamples[i] * cleanSamples[i]
	}
	variance := sumSqDiff / float64(n)

	// Stage 1: Intra-window noise variance subtraction
	signalVariance := 0.0
	if variance > d.cfg.NoiseVariance {
		signalVariance = variance - d.cfg.NoiseVariance
	}
	rmsCounts := math.Sqrt(signalVariance)

	rawAmps := rmsCounts * (d.cfg.AdcVref / d.cfg.AdcMaxCounts) / d.cfg.DividerRatio * d.cfg.CalAPerV

	// Autocorrelation / Periodicity gate:
	// Evaluates half-period covariance at 50Hz (n/10) and 60Hz (n/12).
	// Real AC waveforms exhibit negative covariance (< -0.20), while noise exhibits ~0.
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
			rawAmps = 0.0 // Pure uncorrelated noise floor -> clean zero
		}
	}

	// Noise floor cutoff (< 0.15A -> 0.0A)
	gatedAmps := rawAmps
	if rawAmps < d.cfg.CutoffAmps {
		gatedAmps = 0.0
	}

	// Stage 2: Inter-window Exponential Moving Average with jitter deadband and zero-snapping
	if gatedAmps == 0.0 {
		d.filteredAmps = 0.0
		d.initialized = false
		return 0.0
	}

	if !d.initialized {
		d.filteredAmps = gatedAmps
		d.initialized = true
		return d.filteredAmps
	}

	// Apply deadband and EMA smoothing
	if math.Abs(gatedAmps-d.filteredAmps) > d.cfg.DeadbandAmps {
		d.filteredAmps = d.cfg.EmaAlpha*gatedAmps + (1.0-d.cfg.EmaAlpha)*d.filteredAmps
	}
	return d.filteredAmps
}

// ProcessWindowStats performs statistical window processing when individual samples are not stored.
func (d *CurrentDenoiser) ProcessWindowStats(sum, sumSq float64, n int) float64 {
	minSamples := d.cfg.MinSamples
	if minSamples <= 0 {
		minSamples = 20
	}
	if n < minSamples {
		return -1.0
	}

	mean := sum / float64(n)
	variance := (sumSq / float64(n)) - (mean * mean)

	signalVariance := 0.0
	if variance > d.cfg.NoiseVariance {
		signalVariance = variance - d.cfg.NoiseVariance
	}
	rmsCounts := math.Sqrt(signalVariance)

	rawAmps := rmsCounts * (d.cfg.AdcVref / d.cfg.AdcMaxCounts) / d.cfg.DividerRatio * d.cfg.CalAPerV
	gatedAmps := rawAmps
	if rawAmps < d.cfg.CutoffAmps {
		gatedAmps = 0.0
	}

	if gatedAmps == 0.0 {
		d.filteredAmps = 0.0
		d.initialized = false
		return 0.0
	}

	if !d.initialized {
		d.filteredAmps = gatedAmps
		d.initialized = true
		return d.filteredAmps
	}

	if math.Abs(gatedAmps-d.filteredAmps) > d.cfg.DeadbandAmps {
		d.filteredAmps = d.cfg.EmaAlpha*gatedAmps + (1.0-d.cfg.EmaAlpha)*d.filteredAmps
	}
	return d.filteredAmps
}
