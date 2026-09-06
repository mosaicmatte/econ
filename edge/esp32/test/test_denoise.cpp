// =============================================================================
// test_denoise.cpp — Automated C++ Verification Suite for CurrentDenoiser
//
// Verifies:
//   1. Zero-signal noise invariance: 100+ consecutive windows with Gaussian
//      noise (sigma 8..20 counts) and transient spikes produce strictly 0.000A.
//   2. Loaded AC waveform accuracy & stability: 0.5A, 2.0A, 5.0A, 10.0A with
//      typical ESP32 ADC noise reconstruct within 5% accuracy and cycle-to-cycle
//      output standard deviation <= 0.015A.
//   3. Step response & zero-snapping: 0A -> 5A quick response (<= 2 windows)
//      and 5A -> 0A instant clean snap to 0.000A without EMA lag tail.
//   4. Starvation guards (< 100 samples return -1.0f) and deadband hold.
// =============================================================================

#define _USE_MATH_DEFINES
#include <cmath>
#ifndef M_PI
#define M_PI 3.14159265358979323846
#endif

#include <iostream>
#include <iomanip>
#include <vector>
#include <random>
#include <string>
#include <cmath>
#include <algorithm>
#include <numeric>
#include <sstream>

#include "current_denoiser.h"

static int gFailures = 0;
static int gPasses = 0;

static void check(bool condition, const std::string& description, const std::string& detail = "") {
  if (condition) {
    std::cout << "  [PASS] " << description;
    if (!detail.empty()) {
      std::cout << " (" << detail << ")";
    }
    std::cout << "\n";
    gPasses++;
  } else {
    std::cerr << "  [FAIL] " << description;
    if (!detail.empty()) {
      std::cerr << " (" << detail << ")";
    }
    std::cerr << "\n";
    gFailures++;
  }
}

// Generate synthetic ADC samples for a 100ms window at 10 kHz sample rate
static std::vector<int> generateMockWindow(
    double rmsAmps,
    double noiseSigma,
    std::mt19937& rng,
    int numSpikes = 0,
    double sampleRateHz = 10000.0,
    double durationSec = 0.100,
    double freqHz = 50.0,
    double dcVolts = 1.65,
    float calAPerV = 15.0f,
    float dividerRatio = 0.5f)
{
  int nSamples = static_cast<int>(durationSec * sampleRateHz);
  std::vector<int> samples(nSamples);

  // Peak voltage of AC signal at the ADC pin after divider
  double vPeakAdc = (rmsAmps * std::sqrt(2.0) / calAPerV) * dividerRatio;
  double dt = 1.0 / sampleRateHz;

  std::normal_distribution<double> noiseDist(0.0, noiseSigma);

  for (int i = 0; i < nSamples; ++i) {
    double t = i * dt;
    double vAc = vPeakAdc * std::sin(2.0 * M_PI * freqHz * t);
    double vTotal = dcVolts + vAc;
    double rawCount = vTotal * (4095.0 / 3.3) + noiseDist(rng);
    int count = static_cast<int>(std::round(rawCount));
    if (count < 0) count = 0;
    if (count > 4095) count = 4095;
    samples[i] = count;
  }

  // Inject transient isolated spikes
  if (numSpikes > 0 && nSamples > 10) {
    std::uniform_int_distribution<int> idxDist(2, nSamples - 3);
    std::uniform_real_distribution<double> spikeDist(35.0, 90.0);
    std::uniform_int_distribution<int> signDist(0, 1);
    for (int k = 0; k < numSpikes; ++k) {
      int idx = idxDist(rng);
      double sign = (signDist(rng) == 0) ? -1.0 : 1.0;
      int spiked = samples[idx] + static_cast<int>(std::round(sign * spikeDist(rng)));
      if (spiked < 0) spiked = 0;
      if (spiked > 4095) spiked = 4095;
      samples[idx] = spiked;
    }
  }

  return samples;
}

int main() {
  std::cout << "================================================================================\n";
  std::cout << "        CURRENT SENSOR DENOISER HOST VERIFICATION SUITE (test_denoise)         \n";
  std::cout << "================================================================================\n\n";

  std::mt19937 rng(1337); // Deterministic seed for reproducible host test execution

  // ---------------------------------------------------------------------------
  // SUITE 1: Zero-Signal Noise Invariance & Ghost Suppression (120 Windows)
  // ---------------------------------------------------------------------------
  std::cout << ">>> [SUITE 1] Zero-Signal Noise Invariance & Ghost Suppression (120 Windows)\n";
  {
    CurrentDenoiser denoiser;
    bool allStrictlyZero = true;
    int nonZeroCount = 0;
    const int totalWindows = 120;

    for (int w = 0; w < totalWindows; ++w) {
      // Sweep noise sigma across realistic ESP32 ADC noise bounds (8.0 to 20.0 counts)
      double sigma = 8.0 + (12.0 * w) / (totalWindows - 1);
      int spikes = (w % 3 == 0) ? (1 + (w % 4)) : 0; // Inject transient spikes every 3rd window
      auto window = generateMockWindow(0.0, sigma, rng, spikes);

      float amps = denoiser.processWindow(window);
      if (amps != 0.0f) {
        allStrictlyZero = false;
        nonZeroCount++;
      }
    }

    check(allStrictlyZero, "100% of 120 zero-signal noisy windows produce strictly 0.000A (0.0W)",
          "non-zero count = " + std::to_string(nonZeroCount));
    check(denoiser.getFilteredAmps() == 0.0f, "Denoiser internal filteredAmps remains exactly 0.000A");
  }

  // ---------------------------------------------------------------------------
  // SUITE 2: Loaded AC Waveform Reconstruction Accuracy & Stability
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [SUITE 2] Loaded AC Waveform Accuracy (within 5%) & Stability (std <= 0.015A)\n";
  {
    struct LoadTestCase {
      double targetAmps;
      const char* label;
    };

    LoadTestCase cases[] = {
      { 0.5,  "0.5A AC load" },
      { 2.0,  "2.0A AC load" },
      { 5.0,  "5.0A AC load" },
      { 10.0, "10.0A AC load" }
    };

    const double typicalEsp32NoiseSigma = std::sqrt(300.0); // ~17.32 counts intrinsic noise
    const int numWindows = 50;

    for (const auto& tc : cases) {
      CurrentDenoiser denoiser;
      std::vector<float> steadyOutputs;
      steadyOutputs.reserve(numWindows);

      for (int w = 0; w < numWindows; ++w) {
        auto window = generateMockWindow(tc.targetAmps, typicalEsp32NoiseSigma, rng);
        float amps = denoiser.processWindow(window);
        // Exclude the first 2 windows to evaluate steady-state convergence
        if (w >= 2) {
          steadyOutputs.push_back(amps);
        }
      }

      double sum = std::accumulate(steadyOutputs.begin(), steadyOutputs.end(), 0.0);
      double mean = sum / steadyOutputs.size();

      double sqDiffSum = 0.0;
      for (float v : steadyOutputs) {
        double d = v - mean;
        sqDiffSum += d * d;
      }
      double stdDev = std::sqrt(sqDiffSum / steadyOutputs.size());
      double errorPct = std::abs(mean - tc.targetAmps) / tc.targetAmps * 100.0;

      std::ostringstream detail;
      detail << std::fixed << std::setprecision(4)
             << "mean=" << mean << "A, target=" << tc.targetAmps
             << "A, err=" << std::setprecision(2) << errorPct
             << "%, stdDev=" << std::setprecision(4) << stdDev << "A";

      check(errorPct <= 5.0, std::string(tc.label) + " reconstructed within 5% accuracy", detail.str());
      check(stdDev <= 0.015, std::string(tc.label) + " cycle-to-cycle stability stdDev <= 0.015A", detail.str());
    }
  }

  // ---------------------------------------------------------------------------
  // SUITE 3: Dynamic Step Response (0A -> 5A) & Instant Zero-Snapping (5A -> 0A)
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [SUITE 3] Dynamic Step Response (0A -> 5A) & Instant Zero-Snapping (5A -> 0A)\n";
  {
    CurrentDenoiser denoiser;
    const double noiseSigma = std::sqrt(300.0);

    // Initial steady state at 0A
    for (int i = 0; i < 5; ++i) {
      auto zeroWin = generateMockWindow(0.0, noiseSigma, rng);
      float a0 = denoiser.processWindow(zeroWin);
      check(a0 == 0.0f, "Zero load window produces 0.000A before step");
    }

    // Step to 5A load
    auto stepUpWin1 = generateMockWindow(5.0, noiseSigma, rng);
    float step1Amps = denoiser.processWindow(stepUpWin1);
    check(step1Amps >= 4.5f && step1Amps <= 5.5f,
          "Step 0A -> 5A responds immediately on Window 1 without startup delay",
          "got " + std::to_string(step1Amps) + "A");

    auto stepUpWin2 = generateMockWindow(5.0, noiseSigma, rng);
    float step2Amps = denoiser.processWindow(stepUpWin2);
    check(std::abs(step2Amps - 5.0f) / 5.0f <= 0.05f,
          "Step 0A -> 5A reaches steady state within 5% on Window 2",
          "got " + std::to_string(step2Amps) + "A");

    // Maintain 5A for 3 windows
    for (int i = 0; i < 3; ++i) {
      auto loadWin = generateMockWindow(5.0, noiseSigma, rng);
      denoiser.processWindow(loadWin);
    }

    // Step down to 0A: must instantaneously snap to 0.0A on Window 1
    auto stepDownWin = generateMockWindow(0.0, noiseSigma, rng);
    float snapZeroAmps = denoiser.processWindow(stepDownWin);
    check(snapZeroAmps == 0.0f,
          "Step 5A -> 0A instantaneously snaps to 0.000A on first sub-threshold window (zero EMA lag)",
          "got " + std::to_string(snapZeroAmps) + "A");

    // Next window remains clean 0.0A
    auto nextZeroWin = generateMockWindow(0.0, noiseSigma, rng);
    float nextZeroAmps = denoiser.processWindow(nextZeroWin);
    check(nextZeroAmps == 0.0f, "Consecutive zero window after step-down remains strictly 0.000A");
  }

  // ---------------------------------------------------------------------------
  // SUITE 4: Starvation Guards, Deadband Stability & State Reset
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [SUITE 4] Starvation Guards, Deadband Stability & State Reset\n";
  {
    CurrentDenoiser denoiser;

    // Starvation tests (< 100 samples)
    int starveCounts[] = { 0, 1, 10, 50, 99 };
    for (int sc : starveCounts) {
      std::vector<int> starved(sc, 2048);
      float res = denoiser.processWindow(starved);
      check(res == -1.0f, "Sample count N=" + std::to_string(sc) + " returns -1.0f (starvation guard)");
    }

    // N = 100 boundary condition succeeds
    std::vector<int> win100(100, 2048);
    float res100 = denoiser.processWindow(win100);
    check(res100 == 0.0f, "Sample count N=100 succeeds (not starved) and returns 0.0A for flat baseline");

    // Jitter Deadband Test: small jitter <= 0.02A around 2.0A holds steady
    CurrentDenoiser deadbandDenoiser;
    // Prime with 2.0A
    auto winPrime = generateMockWindow(2.0, 0.0, rng);
    float primeAmps = deadbandDenoiser.processWindow(winPrime);

    // Apply minor deviation within 0.03A deadband (+0.015A)
    auto winJitter = generateMockWindow(2.015, 0.0, rng);
    float jitterAmps = deadbandDenoiser.processWindow(winJitter);
    check(jitterAmps == primeAmps, "Jitter deadband suppresses <= 0.03A delta and holds previous reading",
          "prime=" + std::to_string(primeAmps) + ", jitter=" + std::to_string(jitterAmps));

    // Reset Test
    deadbandDenoiser.reset();
    check(!deadbandDenoiser.isInitialized(), "reset() clears initialized flag");
    check(deadbandDenoiser.getFilteredAmps() == 0.0f, "reset() clears filteredAmps to 0.0f");
  }

  std::cout << "\n================================================================================\n";
  std::cout << "                         TEST EXECUTION SUMMARY                                 \n";
  std::cout << "================================================================================\n";
  std::cout << "  Passed:  " << gPasses << "\n";
  std::cout << "  Failed:  " << gFailures << "\n";

  if (gFailures == 0) {
    std::cout << "  Verdict: ALL TESTS PASSED (100% SUCCESS)\n";
    std::cout << "================================================================================\n";
    return 0;
  } else {
    std::cerr << "  Verdict: FAILURES DETECTED\n";
    std::cout << "================================================================================\n";
    return 1;
  }
}
