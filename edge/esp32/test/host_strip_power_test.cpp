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
  float amps = (float)(rmsCounts * (3.3 / 4095.0) * stripCalAPerV);
  return amps < 0.10 ? 0.0f : amps;  // below noise floor = genuinely off
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
      float watts = round(amps * gCfg.plugMainsV * 10.0f) / 10.0f;
      char msg[128];
      snprintf(msg, sizeof(msg), "DC %.2fV: amps=%.3fA, watts=%.1fW (expected ~813.2W)", dc, amps, watts);
      // Allow minor discretization error of <= 0.3W out of 813.2W (< 0.04%)
      check(std::abs(amps - (float)expRms) < 0.01f && std::abs(watts - 813.2f) <= 0.3f, msg);
    }
  }

  // -------------------------------------------------------------
  // Test Group 3: Pure Noise vs. Noise Floor Gating (< 0.10A -> 0.0W)
  // -------------------------------------------------------------
  printf("strip_power: noise floor gating (< 0.10A -> 0.0W)\n");
  {
    // Sub-threshold AC current: 0.07A RMS (0.099A peak)
    auto samplesSub = generateSamples(1000, 10000.0, 2.5, 0.07 * sqrt(2.0), gCfg.stripCalAPerV);
    float ampsSub = calculateStripAmps(samplesSub, gCfg.stripCalAPerV);
    check(ampsSub == 0.0f, "0.07A RMS (< 0.10A) is clamped to 0.0A");

    // Pure ADC quantization noise around 2.5V (simulated by alternating +- 3 counts)
    std::vector<int> noiseSamples(1000);
    int center = (int)round(2.5 * 4095.0 / 3.3);
    for (int i = 0; i < 1000; i++) {
      noiseSamples[i] = center + ((i % 4 == 0) ? 3 : ((i % 4 == 2) ? -3 : 0));
    }
    float ampsNoise = calculateStripAmps(noiseSamples, gCfg.stripCalAPerV);
    check(ampsNoise == 0.0f, "ADC noise of +-3 counts is clamped to 0.0A (below 0.10A threshold)");

    // Above-threshold AC current: 0.15A RMS (0.212A peak)
    auto samplesAbove = generateSamples(1000, 10000.0, 2.5, 0.15 * sqrt(2.0), gCfg.stripCalAPerV);
    float ampsAbove = calculateStripAmps(samplesAbove, gCfg.stripCalAPerV);
    check(ampsAbove >= 0.14f && ampsAbove <= 0.16f, "0.15A RMS (> 0.10A) is correctly reported");
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
      float watts = round(amps * gCfg.plugMainsV * 10.0f) / 10.0f;
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
    float wattsHarm = round(ampsHarm * gCfg.plugMainsV * 10.0f) / 10.0f;
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
      float watts = round(amps * gCfg.plugMainsV * 10.0f) / 10.0f;
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

  printf("\n---------------------------------------------------\n");
  if (failures == 0) {
    printf("PASSED (0 failures)\n");
    return 0;
  } else {
    printf("FAILED (%d failures)\n", failures);
    return 1;
  }
}
