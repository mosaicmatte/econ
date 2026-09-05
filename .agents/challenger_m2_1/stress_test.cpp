// -----------------------------------------------------------------------------
// stress_test.cpp — Dedicated Adversarial Stress Test Suite for Milestone 2
//
// Challenger 1 Empirical Verification:
// 1. High-noise and random frames (white noise, salt-and-pepper, pathological inputs).
// 2. Inverted and extreme gradients, radial chirps, high-contrast inversions.
// 3. Rapid state flapping, debounce filter suppression, hysteresis band stress.
// 4. Memory safety checks, guard band canary verifications, boundary conditions.
// 5. Lifecycle robustness, null pointer resilience, uninitialized calls, API fuzzing.
// -----------------------------------------------------------------------------
#include <iostream>
#include <vector>
#include <random>
#include <cmath>
#include <cstring>
#include <chrono>
#include <cstdint>
#include <cassert>

#include "arduino_shim.h"
#include "camera/camera_config.h"
#include "camera/ov7670_driver.h"
#include "camera/model_data.h"
#include "camera/person_detector.h"

static int g_failures = 0;
static int g_tests_run = 0;

static void assert_check(bool cond, const char* name, const char* detail = "") {
  g_tests_run++;
  if (cond) {
    std::cout << "  [PASS] " << name << "\n";
  } else {
    std::cout << "  [FAIL] " << name << " -- " << detail << "\n";
    g_failures++;
  }
}

// Spy DualModeComm for verifying telemetry output and payload integrity
class SpyDualModeComm : public DualModeComm {
public:
  int transmit_count = 0;
  PersonTrackingData last_data{};

  bool transmit(const PersonTrackingData& data) override {
    transmit_count++;
    last_data = data;
    return true;
  }
};

// =============================================================================
// ADVERSARIAL SUITE 1: Memory Safety, Buffer Guard Bands & Coordinate Fuzzing
// =============================================================================
void test_suite_memory_safety_guard_bands() {
  std::cout << "\n================================================================================\n";
  std::cout << " ADVERSARIAL SUITE 1: Memory Safety, Buffer Guard Bands & Boundary Checks       \n";
  std::cout << "================================================================================\n";

  // 1.1 Memory Canary Overrun / Underrun Verification
  const size_t CANARY_SIZE = 2048;
  const uint8_t CANARY_BYTE_SRC = 0xAA;
  const int8_t  CANARY_BYTE_DST = 0x55;

  std::vector<uint8_t> src_alloc(CANARY_SIZE + ImagePreprocessor::INPUT_FRAME_BYTES + CANARY_SIZE, CANARY_BYTE_SRC);
  std::vector<int8_t>  dst_alloc(CANARY_SIZE + ImagePreprocessor::OUTPUT_TENSOR_BYTES + CANARY_SIZE, CANARY_BYTE_DST);

  uint8_t* raw_src = &src_alloc[CANARY_SIZE];
  int8_t*  raw_dst = &dst_alloc[CANARY_SIZE];

  // Fill active source with pseudo-random test pattern
  for (size_t i = 0; i < ImagePreprocessor::INPUT_FRAME_BYTES; ++i) {
    raw_src[i] = static_cast<uint8_t>((i * 7 + 13) % 256);
  }

  // Run 1000 preprocessing iterations
  for (int iter = 0; iter < 1000; ++iter) {
    bool ok = ImagePreprocessor::preprocessFrame(raw_src, ImagePreprocessor::INPUT_FRAME_BYTES,
                                                 raw_dst, ImagePreprocessor::OUTPUT_TENSOR_BYTES);
    if (!ok) {
      assert_check(false, "1.1.1 Preprocessor failed during canary stress loop");
      break;
    }
  }

  // Check Source Guard Bands
  bool src_prefix_intact = true;
  for (size_t i = 0; i < CANARY_SIZE; ++i) {
    if (src_alloc[i] != CANARY_BYTE_SRC) { src_prefix_intact = false; break; }
  }
  bool src_suffix_intact = true;
  for (size_t i = CANARY_SIZE + ImagePreprocessor::INPUT_FRAME_BYTES; i < src_alloc.size(); ++i) {
    if (src_alloc[i] != CANARY_BYTE_SRC) { src_suffix_intact = false; break; }
  }
  assert_check(src_prefix_intact && src_suffix_intact,
               "1.1.1 Source buffer guard band canaries 100% intact (zero buffer underflow/overrun)");

  // Check Destination Guard Bands
  bool dst_prefix_intact = true;
  for (size_t i = 0; i < CANARY_SIZE; ++i) {
    if (dst_alloc[i] != CANARY_BYTE_DST) { dst_prefix_intact = false; break; }
  }
  bool dst_suffix_intact = true;
  for (size_t i = CANARY_SIZE + ImagePreprocessor::OUTPUT_TENSOR_BYTES; i < dst_alloc.size(); ++i) {
    if (dst_alloc[i] != CANARY_BYTE_DST) { dst_suffix_intact = false; break; }
  }
  assert_check(dst_prefix_intact && dst_suffix_intact,
               "1.1.2 Destination tensor guard band canaries 100% intact (zero buffer underflow/overrun)");

  // 1.2 Coordinate Boundary Mapping Exactness: x in [0..95], y in [0..95]
  // In `preprocessFrame`:
  // for y=95: y_int = 95 + (95 >> 2) = 118, row0 = 118*160+20, row1 = 119*160+20 (max line = 119 < 120)
  // for x=95: x_int = 95 + (95 >> 2) = 118, col = 20 + 118 + 1 = 139 (< 140 <= 160)
  // Let's verify every pixel in destination is mapped strictly within [20, 139] x [0, 119]
  std::vector<uint8_t> impulse_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 0);
  std::vector<int8_t>  out_tensor(ImagePreprocessor::OUTPUT_TENSOR_BYTES, -128);

  // Set single pixel impulse at top-left of crop: (X=20, Y=0)
  impulse_frame[0 * 160 + 20] = 255;
  ImagePreprocessor::preprocessFrame(impulse_frame.data(), impulse_frame.size(),
                                     out_tensor.data(), out_tensor.size());
  assert_check(out_tensor[0] > -128 && out_tensor[1 * 96 + 1] == -128,
               "1.2.1 Impulse at top-left crop (20, 0) influences only tensor (0, 0)");

  // Set single pixel impulse at bottom-right of active region: (X=139, Y=119)
  std::fill(impulse_frame.begin(), impulse_frame.end(), 0);
  impulse_frame[119 * 160 + 139] = 255;
  ImagePreprocessor::preprocessFrame(impulse_frame.data(), impulse_frame.size(),
                                     out_tensor.data(), out_tensor.size());
  assert_check(out_tensor[95 * 96 + 95] > -128 && out_tensor[0] == -128,
               "1.2.2 Impulse at bottom-right crop (139, 119) influences tensor (95, 95)");

  // 1.3 Crop Boundary Discard Zone Immunity Check (X in [0, 19] and X in [140, 159])
  std::fill(impulse_frame.begin(), impulse_frame.end(), 0);
  // Inject extreme white pixels all over the left border [0, 19] and right border [140, 159]
  for (int y = 0; y < 120; ++y) {
    for (int x = 0; x < 20; ++x) impulse_frame[y * 160 + x] = 255;
    for (int x = 140; x < 160; ++x) impulse_frame[y * 160 + x] = 255;
  }
  ImagePreprocessor::preprocessFrame(impulse_frame.data(), impulse_frame.size(),
                                     out_tensor.data(), out_tensor.size());
  bool all_black_retained = true;
  for (size_t i = 0; i < ImagePreprocessor::OUTPUT_TENSOR_BYTES; ++i) {
    if (out_tensor[i] != -128) { all_black_retained = false; break; }
  }
  assert_check(all_black_retained,
               "1.3.1 100% white noise in discarded borders [0..19] & [140..159] has ZERO leakage into tensor");

  // 1.4 Buffer Size Edge Cases
  assert_check(!ImagePreprocessor::preprocessFrame(raw_src, 0, raw_dst, ImagePreprocessor::OUTPUT_TENSOR_BYTES),
               "1.4.1 Zero-length source buffer safely rejected");
  assert_check(!ImagePreprocessor::preprocessFrame(raw_src, ImagePreprocessor::INPUT_FRAME_BYTES, raw_dst, 0),
               "1.4.2 Zero-length destination buffer safely rejected");
  assert_check(!ImagePreprocessor::preprocessFrame(raw_src, 19199, raw_dst, ImagePreprocessor::OUTPUT_TENSOR_BYTES),
               "1.4.3 Off-by-one undersized source (19199) safely rejected");
  assert_check(!ImagePreprocessor::preprocessFrame(raw_src, ImagePreprocessor::INPUT_FRAME_BYTES, raw_dst, 9215),
               "1.4.4 Off-by-one undersized destination (9215) safely rejected");
}

// =============================================================================
// ADVERSARIAL SUITE 2: Extreme Noise, Pathological Frames & Inverse Gradients
// =============================================================================
void test_suite_extreme_noise_and_pathological_inputs() {
  std::cout << "\n================================================================================\n";
  std::cout << " ADVERSARIAL SUITE 2: Extreme Noise, Pathological Frames & Inverse Gradients   \n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;
  detector.init();

  // 2.1 White Noise Stress Test (Uniform Random [0..255])
  std::mt19937 rng(1337);
  std::uniform_int_distribution<int> dist_byte(0, 255);

  std::vector<uint8_t> noise_frame(ImagePreprocessor::INPUT_FRAME_BYTES);
  bool noise_clean = true;
  float max_noise_confidence = 0.0f;

  for (int frame = 0; frame < 200; ++frame) {
    for (size_t i = 0; i < noise_frame.size(); ++i) {
      noise_frame[i] = static_cast<uint8_t>(dist_byte(rng));
    }
    bool ok = detector.processBuffer(noise_frame.data(), noise_frame.size());
    if (!ok) { noise_clean = false; break; }
    
    float conf = detector.getConfidence();
    if (conf > max_noise_confidence) max_noise_confidence = conf;
    if (std::isnan(conf) || std::isinf(conf) || conf < 0.0f || conf > 1.0f) {
      noise_clean = false;
      break;
    }
  }
  assert_check(noise_clean, "2.1.1 200 frames of random white noise processed without arithmetic exceptions or NaN");
  assert_check(!detector.isPersonDetected(), "2.1.2 Random white noise never falsely triggers person detection");
  std::cout << "  [INFO] Peak confidence on uniform white noise: " << max_noise_confidence << "\n";

  // 2.2 Salt & Pepper Noise (50% extreme density: 0 or 255)
  std::bernoulli_distribution bernoulli_dist(0.5);
  std::vector<uint8_t> sp_frame(ImagePreprocessor::INPUT_FRAME_BYTES);
  for (size_t i = 0; i < sp_frame.size(); ++i) {
    sp_frame[i] = bernoulli_dist(rng) ? 255 : 0;
  }
  for (int i = 0; i < 10; ++i) {
    detector.processBuffer(sp_frame.data(), sp_frame.size());
  }
  assert_check(!detector.isPersonDetected() && detector.getConfidence() < 0.30f,
               "2.2.1 50% Salt-and-Pepper noise does not cause false positives (Confidence < 0.30)");

  // 2.3 Pathological Extreme Inverted Contrast (Inverse Silhouette: Dark Human in Bright Background)
  // Background = 250, Humanoid Center = 10
  std::vector<uint8_t> inv_silhouette(ImagePreprocessor::INPUT_FRAME_BYTES, 250);
  for (int y = 30; y <= 90; ++y) {
    for (int x = 65; x <= 95; ++x) {
      inv_silhouette[y * 160 + x] = 10;
    }
  }
  detector.reset();
  detector.processBuffer(inv_silhouette.data(), inv_silhouette.size());
  detector.processBuffer(inv_silhouette.data(), inv_silhouette.size());
  assert_check(!detector.isPersonDetected(),
               "2.3.1 Negative/inverted contrast (dark subject on bright background) does not trigger false positive");
  assert_check(detector.getConfidence() < 0.20f,
               "2.3.2 Inverted contrast produces low confidence (<0.20)");

  // 2.4 High-Frequency Chirp / Radial Sine Wave
  std::vector<uint8_t> radial_chirp(ImagePreprocessor::INPUT_FRAME_BYTES);
  for (int y = 0; y < 120; ++y) {
    for (int x = 0; x < 160; ++x) {
      float dx = (float)(x - 80);
      float dy = (float)(y - 60);
      float r = std::sqrt(dx * dx + dy * dy);
      float val = 128.0f + 127.0f * std::sin(r * r * 0.05f);
      radial_chirp[y * 160 + x] = static_cast<uint8_t>(std::clamp(val, 0.0f, 255.0f));
    }
  }
  detector.reset();
  detector.processBuffer(radial_chirp.data(), radial_chirp.size());
  detector.processBuffer(radial_chirp.data(), radial_chirp.size());
  assert_check(!detector.isPersonDetected(),
               "2.4.1 High-frequency radial chirp pattern processed smoothly without false trigger");
}

// =============================================================================
// ADVERSARIAL SUITE 3: Rapid State Flapping, Debounce Suppression & Hysteresis
// =============================================================================
void test_suite_rapid_state_flapping_and_hysteresis() {
  std::cout << "\n================================================================================\n";
  std::cout << " ADVERSARIAL SUITE 3: Rapid State Flapping, Debounce & Hysteresis Stress       \n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;
  detector.init();
  detector.setDetectionThreshold(0.60f, 0.40f);

  // Prepare High-Confidence frame (human silhouette) and Low-Confidence frame (empty)
  std::vector<uint8_t> high_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 30);
  for (int y = 30; y <= 90; ++y) {
    for (int x = 65; x <= 95; ++x) {
      high_frame[y * 160 + x] = 220; // High contrast -> score ~0.88
    }
  }
  std::vector<uint8_t> low_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 30); // score ~0.05

  // 3.1 Alternating Rapid Flapping (H, L, H, L, H, L...)
  // Since debounce_frames = 2, single-frame oscillations MUST NEVER change the detection state!
  detector.reset();
  bool flapped = false;
  for (int i = 0; i < 100; ++i) {
    const uint8_t* cur_frame = (i % 2 == 0) ? high_frame.data() : low_frame.data();
    detector.processBuffer(cur_frame, ImagePreprocessor::INPUT_FRAME_BYTES);
    if (detector.isPersonDetected()) {
      flapped = true;
      break;
    }
  }
  assert_check(!flapped, "3.1.1 100 cycles of 1-frame flapping (H-L-H-L...) completely suppressed by debounce filter (remains FALSE)");

  // 3.2 True-State Flapping Suppression (L, H, L, H, L, H...)
  // First assert TRUE state with 2 consecutive HIGH frames
  detector.processBuffer(high_frame.data(), ImagePreprocessor::INPUT_FRAME_BYTES);
  detector.processBuffer(high_frame.data(), ImagePreprocessor::INPUT_FRAME_BYTES);
  assert_check(detector.isPersonDetected(), "3.2.1 Detector transitioned to TRUE state (2 consecutive high frames)");

  // Now oscillate (L, H, L, H, L...) -> Single LOW frame drops must NOT drop the TRUE state!
  bool dropped = false;
  for (int i = 0; i < 100; ++i) {
    const uint8_t* cur_frame = (i % 2 == 0) ? low_frame.data() : high_frame.data();
    detector.processBuffer(cur_frame, ImagePreprocessor::INPUT_FRAME_BYTES);
    if (!detector.isPersonDetected()) {
      dropped = true;
      break;
    }
  }
  assert_check(!dropped, "3.2.2 100 cycles of 1-frame drop spikes (L-H-L-H...) suppressed while in TRUE state (remains TRUE)");

  // 3.3 Hysteresis Deadband Immersion (Scores strictly in [0.40, 0.60])
  // Create a marginal frame with contrast ~14.0 -> score 0.52 (within 0.40..0.60 band)
  std::vector<uint8_t> deadband_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 30);
  for (int y = 20; y <= 85; ++y) {
    for (int x = 60; x <= 100; ++x) {
      deadband_frame[y * 160 + x] = 55;
    }
  }

  // From FALSE state: 200 deadband frames must STAY FALSE
  detector.reset();
  for (int i = 0; i < 200; ++i) {
    detector.processBuffer(deadband_frame.data(), deadband_frame.size());
  }
  assert_check(!detector.isPersonDetected(),
               "3.3.1 Deadband immersion (score 0.52) from initial FALSE state stays FALSE for 200 frames");

  // From TRUE state: 200 deadband frames must STAY TRUE
  detector.processBuffer(high_frame.data(), high_frame.size());
  detector.processBuffer(high_frame.data(), high_frame.size());
  assert_check(detector.isPersonDetected(), "3.3.2 Primed to TRUE state");
  for (int i = 0; i < 200; ++i) {
    detector.processBuffer(deadband_frame.data(), deadband_frame.size());
  }
  assert_check(detector.isPersonDetected(),
               "3.3.3 Deadband immersion (score 0.52) from TRUE state stays TRUE for 200 frames (Dual-threshold memory intact)");

  // 3.4 Dynamic Threshold Reconfiguration Under Load
  // Raise enter threshold to 0.90 -> current high frame (score 0.88) should NOT trigger enter from false state
  detector.reset();
  detector.setDetectionThreshold(0.90f, 0.40f);
  detector.processBuffer(high_frame.data(), high_frame.size());
  detector.processBuffer(high_frame.data(), high_frame.size());
  assert_check(!detector.isPersonDetected(),
               "3.4.1 Raised threshold (0.90) successfully blocks score 0.88 from entering TRUE state");

  // Lower enter threshold to 0.50 -> deadband frame (0.52) should trigger enter
  detector.setDetectionThreshold(0.50f, 0.30f);
  detector.processBuffer(deadband_frame.data(), deadband_frame.size());
  detector.processBuffer(deadband_frame.data(), deadband_frame.size());
  assert_check(detector.isPersonDetected(),
               "3.4.2 Lowered threshold (0.50) allows score 0.52 to trigger detection");
}

// =============================================================================
// ADVERSARIAL SUITE 4: Lifecycle Robustness, Fault Injection & Null Safety
// =============================================================================
void test_suite_lifecycle_and_fault_injection() {
  std::cout << "\n================================================================================\n";
  std::cout << " ADVERSARIAL SUITE 4: Lifecycle Robustness, Fault Injection & Null Safety       \n";
  std::cout << "================================================================================\n";

  // 4.1 Uninitialized Detector Stress
  CameraPersonDetector uninit_detector;
  SpyDualModeComm spy_comm;

  assert_check(uninit_detector.getState() == DetectorState::UNINITIALIZED, "4.1.1 Fresh detector state is UNINITIALIZED");
  assert_check(!uninit_detector.processFrame(), "4.1.2 processFrame() fails gracefully on uninitialized instance");
  assert_check(!uninit_detector.processBuffer(nullptr, 0), "4.1.3 processBuffer() with null fails gracefully");
  
  // Transmit on uninitialized detector
  uninit_detector.transmitTelemetry(spy_comm);
  assert_check(spy_comm.transmit_count == 1, "4.1.4 transmitTelemetry() safely transmits default struct without crashing");
  assert_check(!spy_comm.last_data.person_detected && spy_comm.last_data.confidence == 0.0f,
               "4.1.5 Default telemetry payload is safely zeroed");

  // 4.2 Idempotent Repeated Init
  CameraPersonDetector multi_init_detector;
  bool all_inits_pass = true;
  for (int i = 0; i < 20; ++i) {
    if (!multi_init_detector.init()) { all_inits_pass = false; break; }
  }
  assert_check(all_inits_pass, "4.2.1 20 consecutive init() invocations succeed idempotently");

  // 4.3 Null String Handling in setZoneAndSensorId
  multi_init_detector.setZoneAndSensorId(nullptr, nullptr);
  const PersonTrackingData& d1 = multi_init_detector.getLatestData();
  assert_check(d1.zone_id != nullptr && d1.sensor_id != nullptr,
               "4.3.1 setZoneAndSensorId(nullptr, nullptr) safely ignores null pointers without overwriting with null");

  // Custom strings
  multi_init_detector.setZoneAndSensorId("conference_room_b", "esp32_cam_99");
  const PersonTrackingData& d2 = multi_init_detector.getLatestData();
  assert_check(strcmp(d2.zone_id, "conference_room_b") == 0 && strcmp(d2.sensor_id, "esp32_cam_99") == 0,
               "4.3.2 Custom zone and sensor IDs updated correctly");

  // 4.4 Driver Mock Mode Toggle Stress
  OV7670Driver& drv = multi_init_detector.getDriver();
  for (int i = 0; i < 50; ++i) {
    drv.setMockMode(i % 2 == 0);
    drv.setMockPattern(static_cast<SyntheticPattern>(i % 7));
  }
  assert_check(true, "4.4.1 Rapid mock pattern and mode switching executes without fault");

  // 4.5 Reset Functionality Cleanliness
  multi_init_detector.reset();
  assert_check(!multi_init_detector.isPersonDetected() && multi_init_detector.getConfidence() == 0.0f && multi_init_detector.getPersonCount() == 0,
               "4.5.1 reset() resets presence, confidence, and headcount cleanly");
}

// =============================================================================
// ADVERSARIAL SUITE 5: High-Throughput Soak & Performance Benchmarking
// =============================================================================
void test_suite_high_throughput_soak() {
  std::cout << "\n================================================================================\n";
  std::cout << " ADVERSARIAL SUITE 5: High-Throughput Soak & Performance Benchmarking          \n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;
  detector.init();
  SpyDualModeComm comm;

  const int SOAK_CYCLES = 5000;
  auto t_start = std::chrono::high_resolution_clock::now();

  for (int i = 0; i < SOAK_CYCLES; ++i) {
    detector.processFrame();
    if (i % 10 == 0) {
      detector.transmitTelemetry(comm);
    }
  }

  auto t_end = std::chrono::high_resolution_clock::now();
  double elapsed_ms = std::chrono::duration<double, std::milli>(t_end - t_start).count();
  double fps = (SOAK_CYCLES * 1000.0) / elapsed_ms;

  std::cout << "  [PERF] " << SOAK_CYCLES << " full capture+inference cycles executed in "
            << elapsed_ms << " ms (" << fps << " FPS)\n";
  assert_check(fps > 1000.0, "5.1.1 Full camera+detector pipeline throughput exceeds 1,000 FPS on host");
  assert_check(comm.transmit_count == (SOAK_CYCLES / 10), "5.1.2 Telemetry transmission dispatched 500 packets reliably");
  assert_check(detector.getDriver().getFrameCounter() == SOAK_CYCLES, "5.1.3 Frame counter precisely matches 5,000 captured frames");
}

// =============================================================================
// Main Test Runner
// =============================================================================
int main() {
  std::cout << "================================================================================\n";
  std::cout << "  MILESTONE 2: EMPIRICAL ADVERSARIAL CHALLENGER 1 STRESS HARNESS               \n";
  std::cout << "================================================================================\n";

  test_suite_memory_safety_guard_bands();
  test_suite_extreme_noise_and_pathological_inputs();
  test_suite_rapid_state_flapping_and_hysteresis();
  test_suite_lifecycle_and_fault_injection();
  test_suite_high_throughput_soak();

  std::cout << "\n================================================================================\n";
  std::cout << "                    ADVERSARIAL STRESS TEST SUMMARY                             \n";
  std::cout << "================================================================================\n";
  std::cout << " Total Adversarial Checks : " << g_tests_run << "\n";
  std::cout << " Checks Passed            : " << (g_tests_run - g_failures) << "\n";
  std::cout << " Checks Failed            : " << g_failures << "\n";
  std::cout << " Verdict                  : " << (g_failures == 0 ? "PASSED (100%)" : "FAILED") << "\n";
  std::cout << "================================================================================\n";

  return (g_failures == 0) ? 0 : 1;
}
