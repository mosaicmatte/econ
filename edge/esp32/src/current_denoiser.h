#pragma once

#include <cmath>
#include <vector>
#include <cstdint>
#include <cstdlib>
#include <algorithm>

// Configuration parameters for two-stage current sensor denoising
struct CurrentDenoiseConfig {
  float  calAPerV       = 15.0f;   // A/V sensitivity (ACS712-30A: 66.6 mV/A -> 15 A/V)
  float  dividerRatio   = 0.5f;    // 10k/10k voltage divider (halves ACS712 voltage swing)
  float  adcVref        = 3.3f;    // ESP32 ADC reference voltage (3.3V)
  float  adcMaxCounts   = 4095.0f; // 12-bit ADC resolution (4095 counts full scale)
  double noiseVariance = 300.0;   // ESP32 ADC intrinsic noise variance (~17.3 counts sigma)
  float  cutoffAmps     = 0.15f;   // Sub-threshold noise floor cutoff (<0.15A -> 0.0A)
  float  emaAlpha       = 0.35f;   // Inter-window EMA smoothing factor
  float  deadbandAmps   = 0.03f;   // Jitter deadband (holds steady reading within band)
};

class CurrentDenoiser {
public:
  explicit CurrentDenoiser(const CurrentDenoiseConfig& cfg = CurrentDenoiseConfig())
      : cfg_(cfg), filteredAmps_(0.0f), initialized_(false) {}

  // Process a discrete window of raw ADC sample counts (Stage 1 + Stage 2)
  float processWindow(const int* samples, int n) {
    if (samples == nullptr || n < 100) {
      return -1.0f; // Starvation guard: window must contain at least 100 samples
    }

    // Pass 1: Transient spike suppression and statistics for linear detrending
    std::vector<double> cleanSamples(n);
    double sum = 0.0;

    for (int i = 0; i < n; ++i) {
      double val = static_cast<double>(samples[i]);
      if (i > 0 && i < n - 1) {
        double prev = static_cast<double>(samples[i - 1]);
        double next = static_cast<double>(samples[i + 1]);
        double d1 = val - prev;
        double d2 = val - next;
        // Suppress isolated single-sample transient spikes exceeding 50 counts
        if ((d1 > 50.0 && d2 > 50.0) || (d1 < -50.0 && d2 < -50.0)) {
          val = 0.5 * (prev + next);
        }
      }
      cleanSamples[i] = val;
      sum += val;
    }

    double mean = sum / n;

    // Linear detrending to eliminate intra-window dynamic DC offset drift:
    // Compute the slope via period-differencing to ensure zero attenuation on AC mains (50Hz / 60Hz) waveforms.
    int period50 = (n >= 20) ? (n / 5) : 0;
    int period60 = (n >= 24) ? static_cast<int>(std::round(n / 6.0)) : 0;
    double slope = 0.0;

    if (period50 > 0 && period50 < n) {
      double diffSum50 = 0.0;
      double diffSqSum50 = 0.0;
      int count50 = n - period50;
      for (int i = 0; i < count50; ++i) {
        double d = cleanSamples[i + period50] - cleanSamples[i];
        diffSum50 += d;
        diffSqSum50 += d * d;
      }
      double e50 = diffSqSum50 / count50;

      if (period60 > 0 && period60 < n) {
        double diffSum60 = 0.0;
        double diffSqSum60 = 0.0;
        int count60 = n - period60;
        for (int i = 0; i < count60; ++i) {
          double d = cleanSamples[i + period60] - cleanSamples[i];
          diffSum60 += d;
          diffSqSum60 += d * d;
        }
        double e60 = diffSqSum60 / count60;

        if (e60 < 0.7 * e50) {
          slope = (diffSum60 / count60) / period60;
        } else {
          slope = (diffSum50 / count50) / period50;
        }
      } else {
        slope = (diffSum50 / count50) / period50;
      }
    }

    double iMean = 0.5 * (n - 1);

    // Pass 2: Linear detrending (subtracting y_i = mean + slope*(i - iMean)) and variance computation
    double sumSqDiff = 0.0;
    for (int i = 0; i < n; ++i) {
      double trend = mean + slope * (i - iMean);
      cleanSamples[i] -= trend;
      sumSqDiff += cleanSamples[i] * cleanSamples[i];
    }
    double variance = sumSqDiff / n;

    // Stage 1: Intra-window noise variance subtraction
    double signalVariance = (variance > cfg_.noiseVariance) ? (variance - cfg_.noiseVariance) : 0.0;
    double rmsCounts = std::sqrt(signalVariance);

    float rawAmps = (float)(rmsCounts * (cfg_.adcVref / cfg_.adcMaxCounts) / cfg_.dividerRatio * cfg_.calAPerV);

    // Periodicity / Autocorrelation check:
    // Expand autocorrelation / normalized covariance check across small-to-moderate signals (< 1.50A).
    // Check both 50Hz (n / 10) and 60Hz (n / 12) half-period lags.
    // Real AC signals exhibit negative half-period normalized covariance (< -0.20),
    // whereas uncorrelated white noise or dynamic drift exhibits ~0.
    if (rawAmps < 1.50f && variance > 1e-6) {
      int halfPeriod50 = (n >= 10) ? (n / 10) : 0;
      int halfPeriod60 = (n >= 12) ? static_cast<int>(std::round(n / 12.0)) : 0;

      double minNormCov = 1.0;
      bool checked = false;

      if (halfPeriod50 > 0 && halfPeriod50 < n) {
        double covSum50 = 0.0;
        int covCount50 = n - halfPeriod50;
        for (int i = 0; i < covCount50; ++i) {
          covSum50 += cleanSamples[i] * cleanSamples[i + halfPeriod50];
        }
        double normCov50 = (covSum50 / covCount50) / variance;
        minNormCov = normCov50;
        checked = true;
      }

      if (halfPeriod60 > 0 && halfPeriod60 < n) {
        double covSum60 = 0.0;
        int covCount60 = n - halfPeriod60;
        for (int i = 0; i < covCount60; ++i) {
          covSum60 += cleanSamples[i] * cleanSamples[i + halfPeriod60];
        }
        double normCov60 = (covSum60 / covCount60) / variance;
        if (checked) {
          minNormCov = std::min(minNormCov, normCov60);
        } else {
          minNormCov = normCov60;
          checked = true;
        }
      }

      if (checked && minNormCov > -0.20) {
        rawAmps = 0.0f; // Pure uncorrelated noise floor -> clean zero
      }
    }

    // Noise floor cutoff (< 0.15A -> 0.0A)
    float gatedAmps = (rawAmps < cfg_.cutoffAmps) ? 0.0f : rawAmps;

    // Stage 2: Inter-window Exponential Moving Average with jitter deadband and zero-snapping
    if (gatedAmps == 0.0f) {
      filteredAmps_ = 0.0f;
      initialized_ = false;
      return 0.0f;
    }

    if (!initialized_) {
      filteredAmps_ = gatedAmps;
      initialized_ = true;
      return filteredAmps_;
    }

    // Apply deadband and EMA smoothing
    if (std::abs(gatedAmps - filteredAmps_) > cfg_.deadbandAmps) {
      filteredAmps_ = cfg_.emaAlpha * gatedAmps + (1.0f - cfg_.emaAlpha) * filteredAmps_;
    }
    return filteredAmps_;
  }

  // Convenience overload for std::vector<int>
  float processWindow(const std::vector<int>& samples) {
    return processWindow(samples.data(), static_cast<int>(samples.size()));
  }

  // Statistical window processing when individual samples are not retained in RAM
  float processWindowStats(double sum, double sumSq, int n) {
    if (n < 100) {
      return -1.0f; // Starvation guard
    }

    double mean = sum / n;
    double variance = (sumSq / n) - (mean * mean);

    // Stage 1: Intra-window noise variance subtraction
    double signalVariance = (variance > cfg_.noiseVariance) ? (variance - cfg_.noiseVariance) : 0.0;
    double rmsCounts = std::sqrt(signalVariance);

    float rawAmps = (float)(rmsCounts * (cfg_.adcVref / cfg_.adcMaxCounts) / cfg_.dividerRatio * cfg_.calAPerV);
    float gatedAmps = (rawAmps < cfg_.cutoffAmps) ? 0.0f : rawAmps;

    // Stage 2: Inter-window Exponential Moving Average with jitter deadband and zero-snapping
    if (gatedAmps == 0.0f) {
      filteredAmps_ = 0.0f;
      initialized_ = false;
      return 0.0f;
    }

    if (!initialized_) {
      filteredAmps_ = gatedAmps;
      initialized_ = true;
      return filteredAmps_;
    }

    if (std::abs(gatedAmps - filteredAmps_) > cfg_.deadbandAmps) {
      filteredAmps_ = cfg_.emaAlpha * gatedAmps + (1.0f - cfg_.emaAlpha) * filteredAmps_;
    }
    return filteredAmps_;
  }

  void reset() {
    filteredAmps_ = 0.0f;
    initialized_ = false;
  }

  float getFilteredAmps() const { return filteredAmps_; }
  bool isInitialized() const { return initialized_; }

  void setCal(float cal) { cfg_.calAPerV = cal; }
  void setCalAPerV(float cal) { cfg_.calAPerV = cal; }
  void setNoiseVariance(double nv) { cfg_.noiseVariance = nv; }
  void setDividerRatio(float ratio) { cfg_.dividerRatio = ratio; }
  void setCutoffAmps(float cutoff) { cfg_.cutoffAmps = cutoff; }

  const CurrentDenoiseConfig& getConfig() const { return cfg_; }
  void setConfig(const CurrentDenoiseConfig& cfg) { cfg_ = cfg; }

private:
  CurrentDenoiseConfig cfg_;
  float filteredAmps_;
  bool initialized_;
};
