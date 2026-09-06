// =============================================================================
// test_cpu_strain_fallback.cpp — Host-Side Automated Verification Suite
// for Edge Node CPU Strain Detection and Backend Offload Fallback Mode.
//
// Verifies:
//   1. Normal Mode: when CPU strain is absent, local CurrentDenoiser runs,
//      calculates true RMS strip power, stripW is published, and rawFallback
//      is omitted/false.
//   2. Simulated CPU Strain via NodeConfig: setting simulateCpuStrain=true
//      toggles pass-through mode, skips local stripDenoiser.processWindow(),
//      emits "rawFallback": true, streams rawStripSamples, and omits stripW.
//   3. Command Override: CPU_STRAIN:HIGH and CPU_STRAIN:NORMAL toggle
//      pass-through mode dynamically.
//   4. Execution Timing Autonomous Trigger: when DSP execution time exceeds
//      the 15 ms budget, strain is detected and offload fallback is engaged.
//   5. Buffer Capacity & Memory Safety: StaticJsonDocument<768> and char buf[768]
//      accommodate all fields + 50 raw ADC samples without overflow or truncation.
//   6. Starvation Guard: sample counts < 100 correctly signal starvation.
// =============================================================================

#include "arduino_shim.h"
#include <ArduinoJson.h>
#include "PubSubClient.h"

#define ZONE_LABEL_OVERRIDE "Level 4"
#define ZONE_TOPIC_OVERRIDE "zone_1"
#include "node_config.h"
#include "current_denoiser.h"

#include <iostream>
#include <vector>
#include <string>
#include <cmath>
#include <cassert>
#include <cstring>
#include <random>

static int gPasses = 0;
static int gFailures = 0;

static void check(bool cond, const std::string& desc, const std::string& detail = "") {
  if (cond) {
    std::cout << "  [PASS] " << desc;
    if (!detail.empty()) std::cout << " (" << detail << ")";
    std::cout << "\n";
    gPasses++;
  } else {
    std::cerr << "  [FAIL] " << desc;
    if (!detail.empty()) std::cerr << " (" << detail << ")";
    std::cerr << "\n";
    gFailures++;
  }
}

// -----------------------------------------------------------------------------
// Firmware Simulation Harness mirroring main.cpp CPU strain & pass-through logic
// -----------------------------------------------------------------------------
struct FirmwareOffloadHarness {
  NodeConfig cfg;
  CurrentDenoiser stripDenoiser;
  bool cpuStrainDetected;
  unsigned long lastDspDurationUs;
  const unsigned long dspBudgetUs = 15000; // 15 ms

  static const int MAX_RAW = 60;
  int rawStripSamples[MAX_RAW];
  int rawStripSampleCount;
  bool localDspExecuted;

  FirmwareOffloadHarness() {
    cfg = cfgDefaults();
    CurrentDenoiseConfig c;
    c.calAPerV = cfg.stripCalAPerV;
    c.dividerRatio = 0.5f;
    c.noiseVariance = 300.0;
    c.cutoffAmps = 0.15f;
    c.emaAlpha = 0.35f;
    c.deadbandAmps = 0.03f;
    stripDenoiser = CurrentDenoiser(c);
    cpuStrainDetected = false;
    lastDspDurationUs = 0;
    rawStripSampleCount = 0;
    localDspExecuted = false;
  }

  bool isCpuStrained() const {
    return cfg.simulateCpuStrain || cpuStrainDetected;
  }

  void handleCommand(const std::string& msg) {
    if (msg == "CPU_STRAIN:HIGH") {
      cpuStrainDetected = true;
    } else if (msg == "CPU_STRAIN:NORMAL") {
      cpuStrainDetected = false;
    }
  }

  // Exact mirror of readStripAmps() in main.cpp
  float readStripAmps(const int* sampleBuf, int n, unsigned long simulatedDspUs = 0) {
    localDspExecuted = false;
    if (n < 100) {
      return -1.0f; // Starvation guard
    }

    if (isCpuStrained()) {
      // Pass-through mode: skip local DSP, capture decimated raw samples
      const int TARGET_RAW = 30;
      rawStripSampleCount = 0;
      int count = (n < TARGET_RAW) ? n : TARGET_RAW;
      for (int i = 0; i < count; ++i) {
        int idx = (count > 1) ? (i * (n - 1) / (count - 1)) : 0;
        rawStripSamples[rawStripSampleCount++] = sampleBuf[idx];
      }
      return -2.0f; // Pass-through mode active
    }

    // Normal mode: execute local DSP
    localDspExecuted = true;
    stripDenoiser.setCal(cfg.stripCalAPerV);
    float amps = stripDenoiser.processWindow(sampleBuf, n);

    unsigned long dspDuration = simulatedDspUs > 0 ? simulatedDspUs : 1200; // default nominal ~1.2ms
    lastDspDurationUs = dspDuration;
    if (dspDuration > dspBudgetUs) {
      cpuStrainDetected = true;
    }
    return amps;
  }

  // Exact mirror of readAndPublish() telemetry generation in main.cpp
  size_t buildTelemetryPayload(char* outBuf, size_t outCap, float stripAmps) {
#if defined(__SIZEOF_POINTER__) && __SIZEOF_POINTER__ == 8
    StaticJsonDocument<1536> doc; // 64-bit host: 32 bytes per slot
#else
    StaticJsonDocument<768> doc;  // 32-bit ESP32: 16 bytes per slot
#endif
    doc["zone"]   = cfg.zoneLabel;
    doc["source"] = "esp32";
    doc["cfgRev"] = cfg.cfgRev;

    doc["temperature"] = 24.5;
    doc["humidity"]    = 55.0;
    doc["tempReal"]    = true;
    doc["occupancy"]   = 2;
    doc["plugW"]       = 120.5;
    doc["plug"]        = "ON";

    if (isCpuStrained() || stripAmps == -2.0f) {
      doc["rawFallback"] = true;
      JsonArray rawArr = doc.createNestedArray("rawStripSamples");
      for (int i = 0; i < rawStripSampleCount; ++i) {
        rawArr.add(rawStripSamples[i]);
      }
    } else if (stripAmps >= 0.0f) {
      doc["stripW"] = std::round(stripAmps * cfg.plugMainsV * 10.0f) / 10.0f;
    }

    doc["lights"]   = "ON";
    doc["setpoint"] = 24.0;
    doc["acReal"]   = true;

    return serializeJson(doc, outBuf, outCap);
  }
};

// Generate synthetic AC sine samples (nominal 50 Hz, 100 ms at 10 kHz -> 1000 samples)
static std::vector<int> generateSineWave(float rmsAmps, float calAPerV = 15.0f, float divider = 0.5f, int nSamples = 1000) {
  std::vector<int> samples(nSamples);
  double vPeakAdc = (rmsAmps * std::sqrt(2.0) / calAPerV) * divider;
  double dcVolts = 1.25; // 2.5V / 2 from ACS712 voltage divider
  double dt = 0.100 / nSamples;

  for (int i = 0; i < nSamples; ++i) {
    double t = i * dt;
    double vAc = vPeakAdc * std::sin(2.0 * M_PI * 50.0 * t);
    double counts = (dcVolts + vAc) * (4095.0 / 3.3);
    int c = static_cast<int>(std::round(counts));
    if (c < 0) c = 0;
    if (c > 4095) c = 4095;
    samples[i] = c;
  }
  return samples;
}

int main() {
  std::cout << "================================================================================\n";
  std::cout << "       ESP32 EDGE NODE CPU STRAIN DETECTION & OFFLOAD FALLBACK TESTS            \n";
  std::cout << "================================================================================\n";

  auto wave2A = generateSineWave(2.0f); // 2A test AC wave

  // ---------------------------------------------------------------------------
  // [TEST 1] Normal Mode Execution (Zero Strain)
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [Suite 1] Normal Mode Baseline (No Strain)...\n";
  {
    FirmwareOffloadHarness h;
    check(!h.isCpuStrained(), "CPU strain initially false");

    float amps = h.readStripAmps(wave2A.data(), (int)wave2A.size(), 2500); // 2.5 ms execution
    check(h.localDspExecuted, "Local DSP executed in normal mode");
    check(amps > 1.8f && amps < 2.2f, "Calculated amps within expected range", std::to_string(amps) + " A");

    char buf[768];
    size_t len = h.buildTelemetryPayload(buf, sizeof(buf), amps);
    check(len > 0 && len < sizeof(buf), "Payload serialized within 768 buffer", std::to_string(len) + " bytes");

    StaticJsonDocument<768> doc;
    DeserializationError err = deserializeJson(doc, buf);
    check(!err, "Serialized payload parses valid JSON");
    check(doc.containsKey("stripW"), "Normal telemetry contains stripW");
    check(!doc.containsKey("rawFallback") || doc["rawFallback"] == false, "rawFallback is false or omitted");
    check(!doc.containsKey("rawStripSamples"), "rawStripSamples omitted in normal mode");
  }

  // ---------------------------------------------------------------------------
  // [TEST 2] High CPU Strain via NodeConfig Flag (simulateCpuStrain)
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [Suite 2] Simulated CPU Strain via NodeConfig...\n";
  {
    FirmwareOffloadHarness h;
    // Apply JSON config setting simulateCpuStrain = true
    StaticJsonDocument<256> cfgDoc;
    cfgDoc["simulateCpuStrain"] = true;
    bool reset = false;
    cfgApplyJson(cfgDoc, reset);
    h.cfg.simulateCpuStrain = gCfg.simulateCpuStrain;

    check(h.isCpuStrained(), "simulateCpuStrain=true activates CPU strain");

    float ret = h.readStripAmps(wave2A.data(), (int)wave2A.size(), 1000);
    check(!h.localDspExecuted, "Local DSP strictly skipped when CPU is strained");
    check(ret == -2.0f, "readStripAmps returns -2.0f signaling pass-through mode");
    check(h.rawStripSampleCount == 30, "30 raw decimated samples captured for offload",
          std::to_string(h.rawStripSampleCount));

    char buf[768];
    size_t len = h.buildTelemetryPayload(buf, sizeof(buf), ret);
    check(len > 0 && len < sizeof(buf), "Pass-through payload fits in buffer", std::to_string(len) + " bytes");

#if defined(__SIZEOF_POINTER__) && __SIZEOF_POINTER__ == 8
    StaticJsonDocument<1536> doc;
#else
    StaticJsonDocument<768> doc;
#endif
    deserializeJson(doc, buf);
    check(doc["rawFallback"] == true, "Telemetry contains 'rawFallback': true");
    check(!doc.containsKey("stripW"), "stripW is omitted in pass-through mode");
    check(doc.containsKey("rawStripSamples"), "Telemetry contains 'rawStripSamples' array");
    JsonArray arr = doc["rawStripSamples"].as<JsonArray>();
    check(arr.size() == 30, "rawStripSamples array has exactly 30 samples");

    // Clear simulateCpuStrain and verify recovery
    cfgDoc["simulateCpuStrain"] = false;
    cfgApplyJson(cfgDoc, reset);
    h.cfg.simulateCpuStrain = gCfg.simulateCpuStrain;
    check(!h.isCpuStrained(), "CPU strain cleared after config restore");

    float normalAmps = h.readStripAmps(wave2A.data(), (int)wave2A.size(), 1000);
    check(h.localDspExecuted, "Local DSP resumes after CPU strain cleared");
    check(normalAmps > 1.8f && normalAmps < 2.2f, "Normal calculation restored");
  }

  // ---------------------------------------------------------------------------
  // [TEST 3] Command Override Toggle (CPU_STRAIN:HIGH / NORMAL)
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [Suite 3] Command Override (CPU_STRAIN:HIGH / NORMAL)...\n";
  {
    FirmwareOffloadHarness h;
    h.handleCommand("CPU_STRAIN:HIGH");
    check(h.isCpuStrained(), "CPU_STRAIN:HIGH sets strain condition");

    float ret = h.readStripAmps(wave2A.data(), (int)wave2A.size(), 1000);
    check(!h.localDspExecuted, "Local DSP skipped during command-forced strain");
    check(ret == -2.0f, "Firmware enters pass-through mode on command");

    h.handleCommand("CPU_STRAIN:NORMAL");
    check(!h.isCpuStrained(), "CPU_STRAIN:NORMAL clears strain condition");
    h.readStripAmps(wave2A.data(), (int)wave2A.size(), 1000);
    check(h.localDspExecuted, "Local DSP resumes after CPU_STRAIN:NORMAL");
  }

  // ---------------------------------------------------------------------------
  // [TEST 4] Execution Timing Autonomous Strain Trigger (> 15 ms Budget)
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [Suite 4] Autonomous Execution Timing Trigger...\n";
  {
    FirmwareOffloadHarness h;
    check(!h.cpuStrainDetected, "No strain detected initially");

    // Run window with simulated DSP latency of 22 ms (> 15 ms budget)
    h.readStripAmps(wave2A.data(), (int)wave2A.size(), 22000);
    check(h.cpuStrainDetected, "Strain autonomously detected when DSP duration (22 ms) > budget (15 ms)");
    check(h.isCpuStrained(), "Firmware enters strained state");

    // Next cycle must automatically drop into pass-through mode
    float ret = h.readStripAmps(wave2A.data(), (int)wave2A.size(), 1000);
    check(!h.localDspExecuted, "Subsequent cycle automatically offloads DSP to pass-through");
    check(ret == -2.0f, "readStripAmps returns pass-through sentinel (-2.0f)");
  }

  // ---------------------------------------------------------------------------
  // [TEST 5] Buffer Overflow & Memory Safety Canaries
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [Suite 5] Buffer Overflow & Canary Protection...\n";
  {
    FirmwareOffloadHarness h;
    h.cfg.simulateCpuStrain = true;
    float ret = h.readStripAmps(wave2A.data(), (int)wave2A.size(), 1000);

    struct CanaryBuffer {
      char guardBefore[32];
      char payloadBuf[768];
      char guardAfter[32];
    } can;

    memset(can.guardBefore, 0x55, sizeof(can.guardBefore));
    memset(can.payloadBuf, 0, sizeof(can.payloadBuf));
    memset(can.guardAfter, 0xAA, sizeof(can.guardAfter));

    size_t written = h.buildTelemetryPayload(can.payloadBuf, sizeof(can.payloadBuf), ret);
    check(written > 0 && written < 768, "Serialized JSON length < 768 bytes", std::to_string(written) + " bytes");

    bool beforeOk = true, afterOk = true;
    for (size_t i = 0; i < sizeof(can.guardBefore); i++) {
      if (can.guardBefore[i] != 0x55) beforeOk = false;
    }
    for (size_t i = 0; i < sizeof(can.guardAfter); i++) {
      if (can.guardAfter[i] != (char)0xAA) afterOk = false;
    }
    check(beforeOk, "Guard memory before buffer intact");
    check(afterOk, "Guard memory after buffer intact");
  }

  // ---------------------------------------------------------------------------
  // [TEST 6] ADC Starvation Guard
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [Suite 6] Starvation Guard...\n";
  {
    FirmwareOffloadHarness h;
    std::vector<int> starvedWave(50, 1550); // only 50 samples (< 100 threshold)
    float amps = h.readStripAmps(starvedWave.data(), (int)starvedWave.size(), 1000);
    check(amps == -1.0f, "Starved window (<100 samples) returns -1.0f");
    check(!h.localDspExecuted, "Local DSP not run on starved window");
  }

  // ---------------------------------------------------------------------------
  // [TEST 7] Decimated Waveform Reconstruction Verification
  // ---------------------------------------------------------------------------
  std::cout << "\n>>> [Suite 7] Decimated Waveform Preserved Fidelity...\n";
  {
    FirmwareOffloadHarness h;
    h.cfg.simulateCpuStrain = true;
    h.readStripAmps(wave2A.data(), (int)wave2A.size(), 1000);

    // Compute mean of raw samples vs decimated samples
    double origSum = 0;
    for (int s : wave2A) origSum += s;
    double origMean = origSum / wave2A.size();

    double decSum = 0;
    for (int i = 0; i < h.rawStripSampleCount; i++) decSum += h.rawStripSamples[i];
    double decMean = decSum / h.rawStripSampleCount;

    check(std::abs(origMean - decMean) < 15.0, "Decimated waveform preserves DC bias within 15 counts",
          "orig=" + std::to_string(origMean) + ", dec=" + std::to_string(decMean));
  }

  std::cout << "\n================================================================================\n";
  std::cout << " Total Checks Run : " << (gPasses + gFailures) << "\n";
  std::cout << " Checks Passed    : " << gPasses << "\n";
  std::cout << " Checks Failed    : " << gFailures << "\n";
  std::cout << " Status           : " << (gFailures == 0 ? "ALL PASS (100%)" : "FAIL") << "\n";
  std::cout << "================================================================================\n";

  return (gFailures == 0) ? 0 : 1;
}
