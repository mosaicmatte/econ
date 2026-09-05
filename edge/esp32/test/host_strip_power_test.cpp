// Host-side adversarial verification for the ACS712 True-RMS power calculation algorithm
// and stripCalAPerV calibration multiplier (edge/esp32).
//
// Build and run:
//   g++ -std=c++17 -Wall -I .pio/libdeps/esp32dev/ArduinoJson/src -I src -I test test/host_strip_power_test.cpp -o test/host_strip_power_test.exe && test\host_strip_power_test.exe

#define _USE_MATH_DEFINES
#include <cmath>
#ifndef M_PI
#define M_PI 3.14159265358979323846
#endif
#include "arduino_shim.h"
#include <ArduinoJson.h>

#define ZONE_LABEL_OVERRIDE "Level 4"
#include "node_config.h"

#include <cstdio>
#include <vector>
#include <string>
#include <algorithm>
#include <random>

static int failures = 0;

static void check(bool cond, const char* what) {
  if (cond) {
    printf("  ok   %s\n", what);
  } else {
    printf("  FAIL %s\n", what);
    failures++;
  }
}

// Exact implementation of the RMS calculation logic in readStripAmps() in main.cpp
static float calculateStripAmps(const std::vector<int>& samples, float stripCalAPerV) {
  if (samples.size() < 100) return -1.0f;
  double sum = 0, sumSq = 0;
  int n = 0;
  for (int v : samples) {
    sum += v;
    sumSq += (double)v * v;
    n++;
  }
  if (n < 100) return -1.0f;
  double mean = sum / n;
  double rmsCounts = sqrt(fmax(0.0, sumSq / n - mean * mean));
  const double STRIP_NOISE_FLOOR_COUNTS = 12.0;
  if (rmsCounts < STRIP_NOISE_FLOOR_COUNTS) return 0.0f;  // below ADC noise floor = genuinely off
  return (float)(rmsCounts * (3.3 / 4095.0) * stripCalAPerV);
}

// Generate synthetic ADC samples
static std::vector<int> generateSamples(
    int nSamples,
    double sampleRateHz,
    double dcVolts,
    double peakAmps,
    double stripCalAPerV,
    double freqHz = 50.0,
    double clipMinV = 0.0,
    double clipMaxV = 3.3)
{
  std::vector<int> samples;
  samples.reserve(nSamples);
  double vPeak = (stripCalAPerV > 0) ? (peakAmps / stripCalAPerV) : 0.0;
  double dt = 1.0 / sampleRateHz;

  for (int i = 0; i < nSamples; i++) {
    double t = i * dt;
    double vAc = vPeak * sin(2.0 * M_PI * freqHz * t);
    double vTotal = dcVolts + vAc;
    // Hardware clipping at ADC rails (0..3.3V)
    if (vTotal < clipMinV) vTotal = clipMinV;
    if (vTotal > clipMaxV) vTotal = clipMaxV;
    int count = (int)round(vTotal * (4095.0 / 3.3));
    if (count < 0) count = 0;
    if (count > 4095) count = 4095;
    samples.push_back(count);
  }
  return samples;
}

// Generate synthetic ADC samples with Gaussian noise (ESP32 ADC simulation)
static std::vector<int> generateSamplesWithNoise(
    int nSamples,
    double sampleRateHz,
    double dcVolts,
    double rmsAmps,
    double stripCalAPerV,
    double noiseSigmaCounts,
    double freqHz = 50.0)
{
  std::vector<int> samples;
  samples.reserve(nSamples);
  double vPeak = (stripCalAPerV > 0) ? (rmsAmps * sqrt(2.0) / stripCalAPerV) : 0.0;
  double dt = 1.0 / sampleRateHz;
  std::mt19937 gen(42); // deterministic seed for reproducibility
  std::normal_distribution<double> noiseDist(0.0, noiseSigmaCounts);

  for (int i = 0; i < nSamples; i++) {
    double t = i * dt;
    double vAc = vPeak * sin(2.0 * M_PI * freqHz * t);
    double vTotal = dcVolts + vAc;
    double rawCount = vTotal * (4095.0 / 3.3) + noiseDist(gen);
    int count = (int)round(rawCount);
    if (count < 0) count = 0;
    if (count > 4095) count = 4095;
    samples.push_back(count);
  }
  return samples;
}

int main() {
  printf("=== ACS712 True-RMS Adversarial Host Test Suite ===\n\n");
  gCfg = cfgDefaults();

  // -------------------------------------------------------------
  // Test Group 1: DC Offset Subtraction Invariance (Zero AC Signal)
  // -------------------------------------------------------------
  printf("strip_power: DC offset subtraction with zero current\n");
  {
    double testOffsets[] = { 0.5, 1.0, 1.65, 2.0, 2.5, 2.8, 3.2 };
    for (double dc : testOffsets) {
      auto samples = generateSamples(1000, 10000.0, dc, 0.0, gCfg.stripCalAPerV);
      float amps = calculateStripAmps(samples, gCfg.stripCalAPerV);
      char msg[128];
      snprintf(msg, sizeof(msg), "DC offset %.2fV results in exactly 0.0A", dc);
      check(amps == 0.0f, msg);
    }
  }

  // -------------------------------------------------------------
  // Test Group 2: DC Offset Invariance with Known AC Signal
  // -------------------------------------------------------------
  printf("strip_power: DC offset invariance with 5A peak AC signal (3.535A RMS)\n");
  {
    double testOffsets[] = { 1.0, 1.65, 2.0, 2.5, 2.8 };
    double expRms = 5.0 / sqrt(2.0); // 3.535534 A
    for (double dc : testOffsets) {
      auto samples = generateSamples(1000, 10000.0, dc, 5.0, gCfg.stripCalAPerV);
      float amps = calculateStripAmps(samples, gCfg.stripCalAPerV);
      float watts = static_cast<float>(round(static_cast<double>(amps) * static_cast<double>(gCfg.plugMainsV) * 10.0) / 10.0);
      char msg[128];
      snprintf(msg, sizeof(msg), "DC %.2fV: amps=%.3fA, watts=%.1fW (expected ~813.2W)", dc, amps, watts);
      // Allow minor discretization error of <= 0.3W out of 813.2W (< 0.04%)
      check(std::abs(amps - (float)expRms) < 0.01f && std::abs(watts - 813.2f) <= 0.3f, msg);
    }
  }

  // -------------------------------------------------------------
  // Test Group 3: Pure Noise vs. Noise Floor Gating (< 12.0 counts RMS -> 0.0W)
  // -------------------------------------------------------------
  printf("strip_power: noise floor gating (< 12.0 counts RMS -> 0.0W)\n");
  {
    // Sub-threshold AC current: 0.07A RMS (0.099A peak, ~5.8 counts < 12.0 counts)
    auto samplesSub = generateSamples(1000, 10000.0, 2.5, 0.07 * sqrt(2.0), gCfg.stripCalAPerV);
    float ampsSub = calculateStripAmps(samplesSub, gCfg.stripCalAPerV);
    check(ampsSub == 0.0f, "0.07A RMS (~5.8 counts < 12.0 counts threshold) is clamped to 0.0A");

    // Pure ADC quantization noise around 2.5V (simulated by alternating +- 3 counts)
    std::vector<int> noiseSamples(1000);
    int center = (int)round(2.5 * 4095.0 / 3.3);
    for (int i = 0; i < 1000; i++) {
      noiseSamples[i] = center + ((i % 4 == 0) ? 3 : ((i % 4 == 2) ? -3 : 0));
    }
    float ampsNoise = calculateStripAmps(noiseSamples, gCfg.stripCalAPerV);
    check(ampsNoise == 0.0f, "ADC noise of +-3 counts is clamped to 0.0A (below 12.0 counts threshold)");

    // Above-threshold AC current: 0.15A RMS (0.212A peak, ~12.4 counts > 12.0 counts)
    auto samplesAbove = generateSamples(1000, 10000.0, 2.5, 0.15 * sqrt(2.0), gCfg.stripCalAPerV);
    float ampsAbove = calculateStripAmps(samplesAbove, gCfg.stripCalAPerV);
    check(ampsAbove >= 0.14f && ampsAbove <= 0.16f, "0.15A RMS (~12.4 counts > 12.0 counts threshold) is correctly reported");
  }

  // -------------------------------------------------------------
  // Test Group 4: Known AC Currents Benchmark at 230V Mains
  // -------------------------------------------------------------
  printf("strip_power: known currents benchmark at 230V mains\n");
  {
    struct TestCase { double rmsAmps; double expWatts; };
    TestCase cases[] = {
      { 1.0, 230.0 },
      { 2.0, 460.0 },
      { 3.5355339, 813.2 },
      { 5.0, 1150.0 },
      { 10.0, 2300.0 }
    };
    for (auto& tc : cases) {
      auto samples = generateSamples(1000, 10000.0, 1.65, tc.rmsAmps * sqrt(2.0), gCfg.stripCalAPerV);
      float amps = calculateStripAmps(samples, gCfg.stripCalAPerV);
      float watts = static_cast<float>(round(static_cast<double>(amps) * static_cast<double>(gCfg.plugMainsV) * 10.0) / 10.0);
      char msg[128];
      snprintf(msg, sizeof(msg), "Target %.3fA: got %.3fA, %.1fW (exp %.1fW)", tc.rmsAmps, amps, watts, tc.expWatts);
      check(std::abs(watts - (float)tc.expWatts) <= 0.5f, msg);
    }
  }

  // -------------------------------------------------------------
  // Test Group 5: Starved Sampling Window Guard (< 100 samples)
  // -------------------------------------------------------------
  printf("strip_power: starved sampling guard and telemetry omission\n");
  {
    int starveCounts[] = { 0, 1, 10, 50, 99 };
    for (int n : starveCounts) {
      std::vector<int> starvedSamples(n, 2048);
      float amps = calculateStripAmps(starvedSamples, gCfg.stripCalAPerV);
      char msg[128];
      snprintf(msg, sizeof(msg), "Sample count N=%d returns -1", n);
      check(amps == -1.0f, msg);

      // Verify MQTT JSON document omission contract
      StaticJsonDocument<384> doc;
      if (amps >= 0) {
        doc["stripW"] = round(amps * gCfg.plugMainsV * 10.0f) / 10.0f;
      }
      snprintf(msg, sizeof(msg), "Sample count N=%d omits stripW from JSON document", n);
      check(!doc.containsKey("stripW"), msg);
    }

    // N = 100 is the boundary where calculation starts
    auto samples100 = generateSamples(100, 1000.0, 1.65, 5.0, gCfg.stripCalAPerV);
    float amps100 = calculateStripAmps(samples100, gCfg.stripCalAPerV);
    check(amps100 >= 0.0f, "Sample count N=100 succeeds and does not return -1");

    StaticJsonDocument<384> docValid;
    if (amps100 >= 0) {
      docValid["stripW"] = round(amps100 * gCfg.plugMainsV * 10.0f) / 10.0f;
    }
    check(docValid.containsKey("stripW"), "Sample count N=100 populates stripW in JSON document");
  }

  // -------------------------------------------------------------
  // Test Group 6: Adversarial - Harmonics (Non-Linear SMPS Load)
  // -------------------------------------------------------------
  printf("strip_power: non-linear load with 3rd and 5th harmonics\n");
  {
    // Fundamental: 2.0A peak (1.414A RMS)
    // 3rd harmonic: 1.2A peak (0.849A RMS)
    // 5th harmonic: 0.6A peak (0.424A RMS)
    // True RMS = sqrt(2.0^2 / 2 + 1.2^2 / 2 + 0.6^2 / 2) = sqrt(2.0 + 0.72 + 0.18) = sqrt(2.9) = 1.7029 A
    // Expected Watts = 1.7029 * 230 = 391.7 W
    std::vector<int> harmSamples;
    int nPts = 1000;
    double dt = 0.100 / nPts;
    for (int i = 0; i < nPts; i++) {
      double t = i * dt;
      double iFund = 2.0 * sin(2.0 * M_PI * 50.0 * t);
      double i3rd  = 1.2 * sin(2.0 * M_PI * 150.0 * t);
      double i5th  = 0.6 * sin(2.0 * M_PI * 250.0 * t);
      double iTotal = iFund + i3rd + i5th;
      double vTotal = 1.65 + (iTotal / gCfg.stripCalAPerV);
      int count = (int)round(vTotal * 4095.0 / 3.3);
      harmSamples.push_back(count);
    }
    float ampsHarm = calculateStripAmps(harmSamples, gCfg.stripCalAPerV);
    float wattsHarm = static_cast<float>(round(static_cast<double>(ampsHarm) * static_cast<double>(gCfg.plugMainsV) * 10.0) / 10.0);
    check(std::abs(ampsHarm - 1.7029f) < 0.01f, "SMPS harmonics: RMS current within 0.01A of theoretical 1.703A");
    check(std::abs(wattsHarm - 391.7f) <= 0.5f, "SMPS harmonics: Watts within 0.5W of theoretical 391.7W");
  }

  // -------------------------------------------------------------
  // Test Group 7: Adversarial - Frequency Deviations (49Hz, 50Hz, 60Hz)
  // -------------------------------------------------------------
  printf("strip_power: grid frequency deviation over 100ms window\n");
  {
    double testFreqs[] = { 49.0, 50.0, 51.0, 60.0 };
    for (double f : testFreqs) {
      auto samples = generateSamples(1000, 10000.0, 1.65, 5.0, gCfg.stripCalAPerV, f);
      float amps = calculateStripAmps(samples, gCfg.stripCalAPerV);
      float watts = static_cast<float>(round(static_cast<double>(amps) * static_cast<double>(gCfg.plugMainsV) * 10.0) / 10.0);
      char msg[128];
      snprintf(msg, sizeof(msg), "Grid freq %.1f Hz: got %.1fW (leakage error < 1.0%%)", f, watts);
      check(std::abs(watts - 813.2f) < 10.0f, msg);
    }
  }

  // -------------------------------------------------------------
  // Test Group 8: stripCalAPerV Dynamic Range (1.0 .. 500.0 A/V)
  // -------------------------------------------------------------
  printf("strip_power: stripCalAPerV calibration limits\n");
  {
    float calLimits[] = { 1.0f, 15.0f, 50.0f, 100.0f, 500.0f };
    for (float cal : calLimits) {
      // 1V RMS signal at ADC pin
      std::vector<int> samples;
      for (int i = 0; i < 1000; i++) {
        double v = 1.65 + sqrt(2.0) * sin(2.0 * M_PI * 50.0 * (i / 10000.0));
        samples.push_back((int)round(v * 4095.0 / 3.3));
      }
      float amps = calculateStripAmps(samples, cal);
      char msg[128];
      snprintf(msg, sizeof(msg), "Cal %.1f A/V with 1V RMS input -> %.2f A (expected %.2f A)", cal, amps, cal);
      check(std::abs(amps - cal) / cal < 0.005f, msg);
    }
  }

  // -------------------------------------------------------------
  // Test Group 9: R3 Waveform Reconstruction with ESP32 ADC Noise (0A, 0.5A, 2A, 10A)
  // -------------------------------------------------------------
  printf("strip_power: R3 waveform reconstruction with ESP32 ADC noise (0A, 0.5A, 2A, 10A)\n");
  {
    // Test 0A with typical ESP32 ADC noise (sigma = 8.0 counts): must output 0.0A (prevent ghost readings)
    auto samples0A = generateSamplesWithNoise(1000, 10000.0, 1.65, 0.0, gCfg.stripCalAPerV, 8.0);
    float amps0A = calculateStripAmps(samples0A, gCfg.stripCalAPerV);
    check(amps0A == 0.0f, "0A with typical ESP32 ADC noise (sigma=8) outputs exactly 0.0A (no ghost reading)");

    // Test 0A with elevated ESP32 ADC noise (sigma = 10.0 counts): must output 0.0A
    auto samples0AHigh = generateSamplesWithNoise(1000, 10000.0, 1.65, 0.0, gCfg.stripCalAPerV, 10.0);
    float amps0AHigh = calculateStripAmps(samples0AHigh, gCfg.stripCalAPerV);
    check(amps0AHigh == 0.0f, "0A with elevated ESP32 ADC noise (sigma=10) outputs exactly 0.0A (no ghost reading)");

    // Test known AC loads with typical ESP32 ADC noise (sigma = 8.0 counts): must reconstruct within 5% accuracy
    struct NoiseTestCase { double targetAmps; const char* desc; };
    NoiseTestCase noiseCases[] = {
      { 0.5, "0.5A RMS with typical noise reconstructed within 5% accuracy" },
      { 2.0, "2.0A RMS with typical noise reconstructed within 5% accuracy" },
      { 10.0, "10.0A RMS with typical noise reconstructed within 5% accuracy" }
    };

    for (auto& tc : noiseCases) {
      auto samples = generateSamplesWithNoise(1000, 10000.0, 1.65, tc.targetAmps, gCfg.stripCalAPerV, 8.0);
      float amps = calculateStripAmps(samples, gCfg.stripCalAPerV);
      double errPct = std::abs((double)amps - tc.targetAmps) / tc.targetAmps * 100.0;
      char msg[128];
      snprintf(msg, sizeof(msg), "%s (got %.4fA, target %.4fA, err %.2f%%)", tc.desc, amps, tc.targetAmps, errPct);
      check(errPct <= 5.0, msg);
    }
  }

  // -------------------------------------------------------------
  // Test Group 10: Multi-Model ACS712 Sensitivity Scaling (5A, 20A, 30A)
  // -------------------------------------------------------------
  printf("strip_power: multi-model ACS712 sensitivity scaling (5A, 20A, 30A)\n");
  {
    // ACS712-05B (5.4 A/V): 12.0 counts = 0.052A RMS (~12.0W at 230V)
    // A light load of 0.08A RMS (~18.4W) was previously suppressed by the fixed 0.10A threshold.
    // With count-based threshold (12.0 counts), 0.08A = 18.4 counts > 12.0 counts, so it is reported!
    float cal05B = 5.4f;
    auto samples05B_0A = generateSamplesWithNoise(1000, 10000.0, 1.65, 0.0, cal05B, 8.0);
    check(calculateStripAmps(samples05B_0A, cal05B) == 0.0f, "ACS712-05B: 0A with noise cleanly suppressed to 0.0A");

    auto samples05B_light = generateSamplesWithNoise(1000, 10000.0, 1.65, 0.08, cal05B, 8.0);
    float amps05B = calculateStripAmps(samples05B_light, cal05B);
    check(amps05B > 0.0f && std::abs(amps05B - 0.08f) / 0.08f <= 0.10f,
          "ACS712-05B: light load 0.08A (~18.4W) detected and within 10% (fixed 0.10A would zero it)");

    auto samples05B_15 = generateSamplesWithNoise(1000, 10000.0, 1.65, 0.15, cal05B, 8.0);
    float amps05B_15 = calculateStripAmps(samples05B_15, cal05B);
    check(std::abs(amps05B_15 - 0.15f) / 0.15f <= 0.05f,
          "ACS712-05B: 0.15A load with noise reconstructed within 5% accuracy");

    // ACS712-20A (10.0 A/V): 12.0 counts = 0.097A RMS (~22.2W at 230V)
    float cal20A = 10.0f;
    auto samples20A_0A = generateSamplesWithNoise(1000, 10000.0, 1.65, 0.0, cal20A, 8.0);
    check(calculateStripAmps(samples20A_0A, cal20A) == 0.0f, "ACS712-20A: 0A with noise cleanly suppressed to 0.0A");
    auto samples20A_load = generateSamplesWithNoise(1000, 10000.0, 1.65, 1.0, cal20A, 8.0);
    float amps20A = calculateStripAmps(samples20A_load, cal20A);
    check(std::abs(amps20A - 1.0f) / 1.0f <= 0.05f, "ACS712-20A: 1.0A load reconstructed within 5%");

    // ACS712-30A (15.0 A/V): 12.0 counts = 0.145A RMS (~33.4W at 230V)
    float cal30A = 15.0f;
    auto samples30A_0A = generateSamplesWithNoise(1000, 10000.0, 1.65, 0.0, cal30A, 10.0);
    check(calculateStripAmps(samples30A_0A, cal30A) == 0.0f, "ACS712-30A: 0A with elevated noise (sigma=10) cleanly suppressed to 0.0A");
  }

  // -------------------------------------------------------------
  // Test Group 11: Extreme Edge Cases: Wide Frequency Drift & Motor Inrush
  // -------------------------------------------------------------
  printf("strip_power: extreme generator frequency drift & motor inrush clipping\n");
  {
    // Extreme frequency wander on cheap portable generators: 45 Hz and 65 Hz over 100ms window
    double extremeFreqs[] = { 45.0, 65.0 };
    for (double f : extremeFreqs) {
      auto samples = generateSamples(1000, 10000.0, 1.65, 5.0, gCfg.stripCalAPerV, f);
      float amps = calculateStripAmps(samples, gCfg.stripCalAPerV);
      float watts = static_cast<float>(round(static_cast<double>(amps) * static_cast<double>(gCfg.plugMainsV) * 10.0) / 10.0);
      double errPct = std::abs(watts - 813.2f) / 813.2f * 100.0;
      char msg[128];
      snprintf(msg, sizeof(msg), "Generator freq %.1f Hz: got %.1fW (err %.2f%% <= 2.0%%)", f, watts, errPct);
      check(errPct <= 2.0, msg);
    }

    // Heavy motor inrush current: 30A RMS (42.4A peak) hard-clipping at ADC rails (0V / 3.3V)
    // Sensor calculation must not crash, produce NaN, negative values, or freeze.
    auto samplesInrush = generateSamples(1000, 10000.0, 1.65, 30.0 * sqrt(2.0), gCfg.stripCalAPerV);
    float ampsInrush = calculateStripAmps(samplesInrush, gCfg.stripCalAPerV);
    check(ampsInrush > 0.0f && !std::isnan(ampsInrush) && !std::isinf(ampsInrush),
          "30A motor inrush hard clipping produces valid finite positive RMS without crashing");
  }

  printf("\n---------------------------------------------------\n");
  if (failures == 0) {
    printf("PASSED (0 failures)\n");
    return 0;
  } else {
    printf("FAILED (%d failures)\n", failures);
    return 1;
  }
}
