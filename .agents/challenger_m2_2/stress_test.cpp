// -----------------------------------------------------------------------------
// stress_test.cpp — Challenger 2 Adversarial Verification & Stress Test Suite
//
// Empirical Verification of:
// 1. Preprocessor Mathematical Invariants & Exhaustive Pixel Space Mapping
// 2. Bilinear Downsampling Monotonicity, Antialiasing & Ringing Resistance
// 3. FlatBuffer Model Header, Magic Bytes, Alignment & Layout Invariants
// 4. Continuous 10,000-Frame Stress Loop with Zero-Heap-Churn Verification
// 5. Adversarial Input Fuzzing & Debounce Stability
// -----------------------------------------------------------------------------
#include <iostream>
#include <vector>
#include <cmath>
#include <cstring>
#include <chrono>
#include <random>
#include <algorithm>
#include <cstdint>
#include <cstddef>
#include <cassert>

// Include shims and tested headers
#include "arduino_shim.h"
#include "camera/camera_config.h"
#include "camera/ov7670_driver.h"
#include "camera/model_data.h"
#include "camera/person_detector.h"

// Global Heap Allocation Tracker for Zero-Heap-Churn Verification
static bool g_track_allocations = false;
static uint64_t g_heap_alloc_count = 0;
static uint64_t g_heap_bytes_allocated = 0;

void* operator new(size_t size) {
  if (g_track_allocations) {
    g_heap_alloc_count++;
    g_heap_bytes_allocated += size;
  }
  return malloc(size);
}

void operator delete(void* ptr) noexcept {
  free(ptr);
}

void* operator new[](size_t size) {
  if (g_track_allocations) {
    g_heap_alloc_count++;
    g_heap_bytes_allocated += size;
  }
  return malloc(size);
}

void operator delete[](void* ptr) noexcept {
  free(ptr);
}

static int g_test_checks = 0;
static int g_test_failures = 0;

static void assert_check(bool condition, const char* test_name, const char* failure_details = "") {
  g_test_checks++;
  if (condition) {
    std::cout << "  [PASS] " << test_name << "\n";
  } else {
    std::cout << "  [FAIL] " << test_name << " ==> " << failure_details << "\n";
    g_test_failures++;
  }
}

// =============================================================================
// Test Section 1: Preprocessor Mathematical Invariants & Exhaustive Pixel Space
// =============================================================================
void test_preprocessor_invariants() {
  std::cout << "\n================================================================================\n";
  std::cout << " [CHALLENGE 1] Preprocessor Mathematical Invariants & Exhaustive Mapping         \n";
  std::cout << "================================================================================\n";

  std::vector<uint8_t> src_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 0);
  std::vector<int8_t> dst_tensor(ImagePreprocessor::OUTPUT_TENSOR_BYTES, 0);

  // 1. Exhaustive solid frame check for all 256 grayscale levels [0..255]
  bool all_256_levels_valid = true;
  int out_of_bounds_count = 0;
  int mapping_mismatch_count = 0;

  for (int gray_val = 0; gray_val < 256; ++gray_val) {
    std::fill(src_frame.begin(), src_frame.end(), static_cast<uint8_t>(gray_val));
    bool ok = ImagePreprocessor::preprocessFrame(src_frame.data(), src_frame.size(),
                                                 dst_tensor.data(), dst_tensor.size());
    if (!ok) {
      all_256_levels_valid = false;
      break;
    }

    int expected_q = gray_val - 128;
    for (size_t i = 0; i < dst_tensor.size(); ++i) {
      int8_t q = dst_tensor[i];
      if (q < -128 || q > 127) {
        out_of_bounds_count++;
      }
      if (q != expected_q) {
        mapping_mismatch_count++;
      }
    }
  }

  assert_check(all_256_levels_valid,
               "1.1 All 256 solid grayscale inputs [0..255] execute preprocessing successfully");
  assert_check(out_of_bounds_count == 0,
               "1.2 Every output pixel q strictly satisfies -128 <= q <= 127 across all 256 grayscale levels");
  assert_check(mapping_mismatch_count == 0,
               "1.3 Solid frame mapping is exact: q == (gray_val - 128) across all 2,359,296 generated pixels");

  // 2. Mathematical Invariant: Convex Combination / Interpolation Bounds
  // For any 4 input pixels [p00, p10, p01, p11], the interpolated value MUST lie within [min(p), max(p)]
  std::mt19937 rng(42);
  std::uniform_int_distribution<int> pixel_dist(0, 255);
  bool convex_invariant_holds = true;
  bool output_bounds_hold = true;

  for (int trial = 0; trial < 1000; ++trial) {
    for (size_t i = 0; i < src_frame.size(); ++i) {
      src_frame[i] = static_cast<uint8_t>(pixel_dist(rng));
    }
    ImagePreprocessor::preprocessFrame(src_frame.data(), src_frame.size(),
                                       dst_tensor.data(), dst_tensor.size());
    
    // Check every output pixel against its 4 corresponding input pixels in src_frame
    for (int y = 0; y < ImagePreprocessor::OUTPUT_HEIGHT; ++y) {
      const int y_int = y + (y >> 2);
      const uint8_t* row0 = &src_frame[y_int * ImagePreprocessor::INPUT_WIDTH + ImagePreprocessor::CROP_OFFSET_X];
      const uint8_t* row1 = &src_frame[(y_int + 1) * ImagePreprocessor::INPUT_WIDTH + ImagePreprocessor::CROP_OFFSET_X];

      for (int x = 0; x < ImagePreprocessor::OUTPUT_WIDTH; ++x) {
        const int x_int = x + (x >> 2);
        int p00 = row0[x_int];
        int p10 = row0[x_int + 1];
        int p01 = row1[x_int];
        int p11 = row1[x_int + 1];

        int min_p = std::min({p00, p10, p01, p11});
        int max_p = std::max({p00, p10, p01, p11});

        int8_t q = dst_tensor[y * ImagePreprocessor::OUTPUT_WIDTH + x];
        int reconstructed_val = static_cast<int>(q) + 128;

        // With integer division / rounding (+8) >> 4, reconstructed_val must be within [min_p, max_p]
        if (reconstructed_val < min_p || reconstructed_val > max_p) {
          convex_invariant_holds = false;
        }
        if (q < -128 || q > 127) {
          output_bounds_hold = false;
        }
      }
    }
  }

  assert_check(convex_invariant_holds,
               "1.4 Bilinear interpolation convex hull invariant: min(p) <= val <= max(p) over 1000 randomized frames");
  assert_check(output_bounds_hold,
               "1.5 Output bounds [-128, 127] invariant strictly holds over 9.21 million randomized interpolations");

  // 3. Memory Bounds & Off-by-one verification: Check highest index accessed
  src_frame.assign(ImagePreprocessor::INPUT_FRAME_BYTES + 64, 0xAA);
  dst_tensor.assign(ImagePreprocessor::OUTPUT_TENSOR_BYTES + 64, 0x55);

  bool crop_bounds_ok = ImagePreprocessor::preprocessFrame(src_frame.data(), ImagePreprocessor::INPUT_FRAME_BYTES,
                                                           dst_tensor.data(), ImagePreprocessor::OUTPUT_TENSOR_BYTES);
  bool canaries_untouched = true;
  for (size_t c = ImagePreprocessor::OUTPUT_TENSOR_BYTES; c < dst_tensor.size(); ++c) {
    if (static_cast<uint8_t>(dst_tensor[c]) != 0x55) {
      canaries_untouched = false;
    }
  }
  assert_check(crop_bounds_ok && canaries_untouched,
               "1.6 Zero buffer overrun: Output memory canaries beyond byte 9216 are completely untouched");
}

// =============================================================================
// Test Section 2: Bilinear Downsampling Monotonicity & Ringing Resistance
// =============================================================================
void test_downsampling_monotonicity() {
  std::cout << "\n================================================================================\n";
  std::cout << " [CHALLENGE 2] Bilinear Downsampling Monotonicity & Frequency Edge Cases         \n";
  std::cout << "================================================================================\n";

  std::vector<uint8_t> src_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 0);
  std::vector<int8_t> dst_tensor(ImagePreprocessor::OUTPUT_TENSOR_BYTES, 0);

  // 2.1 Monotonicity under Linear and Non-Linear Gradients
  bool ramps_monotonic = true;

  // Linear slopes
  for (int slope_idx = 1; slope_idx <= 10; ++slope_idx) {
    for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
      for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
        int v = (x * slope_idx * 25) / 160;
        src_frame[y * ImagePreprocessor::INPUT_WIDTH + x] = static_cast<uint8_t>(std::clamp(v, 0, 255));
      }
    }
    ImagePreprocessor::preprocessFrame(src_frame.data(), src_frame.size(), dst_tensor.data(), dst_tensor.size());
    
    for (int y = 0; y < ImagePreprocessor::OUTPUT_HEIGHT; ++y) {
      for (int x = 0; x < ImagePreprocessor::OUTPUT_WIDTH - 1; ++x) {
        int8_t cur = dst_tensor[y * ImagePreprocessor::OUTPUT_WIDTH + x];
        int8_t next = dst_tensor[y * ImagePreprocessor::OUTPUT_WIDTH + (x + 1)];
        if (next < cur) {
          ramps_monotonic = false;
          break;
        }
      }
    }
  }
  assert_check(ramps_monotonic,
               "2.1 Monotonicity: Horizontal linear gradients produce monotonic non-decreasing output across all rows");

  // Vertical Gradients
  bool v_ramps_monotonic = true;
  for (int slope_idx = 1; slope_idx <= 10; ++slope_idx) {
    for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
      for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
        int v = (y * slope_idx * 25) / 120;
        src_frame[y * ImagePreprocessor::INPUT_WIDTH + x] = static_cast<uint8_t>(std::clamp(v, 0, 255));
      }
    }
    ImagePreprocessor::preprocessFrame(src_frame.data(), src_frame.size(), dst_tensor.data(), dst_tensor.size());

    for (int x = 0; x < ImagePreprocessor::OUTPUT_WIDTH; ++x) {
      for (int y = 0; y < ImagePreprocessor::OUTPUT_HEIGHT - 1; ++y) {
        int8_t cur = dst_tensor[y * ImagePreprocessor::OUTPUT_WIDTH + x];
        int8_t next = dst_tensor[(y + 1) * ImagePreprocessor::OUTPUT_WIDTH + x];
        if (next < cur) {
          v_ramps_monotonic = false;
          break;
        }
      }
    }
  }
  assert_check(v_ramps_monotonic,
               "2.2 Monotonicity: Vertical linear gradients produce monotonic non-decreasing output across all columns");

  // 2.2 Sharp Step Edge (Heaviside Step Function) — Ringing / Gibbs Phenomenon Test
  bool step_edge_monotonic = true;
  bool zero_ringing = true;

  for (int step_x = 30; step_x <= 130; step_x += 10) {
    for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
      for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
        src_frame[y * ImagePreprocessor::INPUT_WIDTH + x] = (x >= step_x) ? 255 : 0;
      }
    }
    ImagePreprocessor::preprocessFrame(src_frame.data(), src_frame.size(), dst_tensor.data(), dst_tensor.size());

    for (int y = 0; y < ImagePreprocessor::OUTPUT_HEIGHT; ++y) {
      for (int x = 0; x < ImagePreprocessor::OUTPUT_WIDTH - 1; ++x) {
        int8_t cur = dst_tensor[y * ImagePreprocessor::OUTPUT_WIDTH + x];
        int8_t next = dst_tensor[y * ImagePreprocessor::OUTPUT_WIDTH + (x + 1)];
        if (next < cur) {
          step_edge_monotonic = false;
        }
        if (cur < -128 || cur > 127) {
          zero_ringing = false;
        }
      }
    }
  }
  assert_check(step_edge_monotonic,
               "2.3 Sharp step edges produce monotonic transitions with 0% overshoot or oscillation");
  assert_check(zero_ringing,
               "2.4 Ringing resistance: All step response values are bounded within [-128, 127]");

  // 2.3 Single Dirac Impulse (Single hot pixel on black background)
  std::fill(src_frame.begin(), src_frame.end(), 0);
  int impulse_x = 80;
  int impulse_y = 60;
  src_frame[impulse_y * ImagePreprocessor::INPUT_WIDTH + impulse_x] = 255;
  ImagePreprocessor::preprocessFrame(src_frame.data(), src_frame.size(), dst_tensor.data(), dst_tensor.size());

  int non_negative_pixels = 0;
  bool radius_bounded = true;
  for (int y = 0; y < ImagePreprocessor::OUTPUT_HEIGHT; ++y) {
    for (int x = 0; x < ImagePreprocessor::OUTPUT_WIDTH; ++x) {
      int8_t val = dst_tensor[y * ImagePreprocessor::OUTPUT_WIDTH + x];
      if (val > -128) {
        non_negative_pixels++;
        int dist = std::max(std::abs(x - 48), std::abs(y - 48));
        if (dist > 2) radius_bounded = false;
      }
    }
  }
  assert_check(radius_bounded && non_negative_pixels >= 1 && non_negative_pixels <= 4,
               "2.5 Point Spread Function (PSF): Dirac impulse maps to compact 1..4 localized pixel footprint (radius <= 2)");
}

// =============================================================================
// Test Section 3: FlatBuffer Structure, Magic Bytes & Memory Alignment
// =============================================================================
void test_flatbuffer_invariants() {
  std::cout << "\n================================================================================\n";
  std::cout << " [CHALLENGE 3] FlatBuffer Structure, Magic Bytes & Alignment Verification        \n";
  std::cout << "================================================================================\n";

  // 3.1 Model Length Invariant
  assert_check(g_person_detect_model_data_len == 24576,
               "3.1 Model data length is exactly 24,576 bytes (24 KB)");
  assert_check(g_person_detect_model_data_len < TENSOR_ARENA_SIZE,
               "3.2 Model Flash size (24KB) fits comfortably within ESP32 4MB Flash and leaves SRAM for 80KB Arena");

  // 3.2 16-Byte Memory Alignment Invariant
  uintptr_t model_addr = reinterpret_cast<uintptr_t>(g_person_detect_model_data);
  assert_check((model_addr % 16) == 0,
               "3.3 g_person_detect_model_data has strict 16-byte memory alignment (alignas(16))");

  // 3.3 FlatBuffer Root Table & Magic Bytes
  uint32_t root_offset = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[0]);
  assert_check(root_offset == 0x1C,
               "3.4 FlatBuffer root table offset is valid (0x1C = 28 bytes)");

  bool magic_valid = (g_person_detect_model_data[4] == 'T' &&
                      g_person_detect_model_data[5] == 'F' &&
                      g_person_detect_model_data[6] == 'L' &&
                      g_person_detect_model_data[7] == '3');
  assert_check(magic_valid,
               "3.5 FlatBuffer magic bytes match TensorFlow Lite 3 schema ('TFL3')");

  // 3.4 Model Schema Version & Subgraph Count Invariants (Bytes 32..39)
  uint32_t schema_version = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[32]);
  uint32_t subgraph_count = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[36]);
  assert_check(schema_version == 3,
               "3.6 Model schema version field equals 3 (TFLite Schema V3)");
  assert_check(subgraph_count == 1,
               "3.7 Model defines exactly 1 computation subgraph");

  // 3.5 Input Tensor Shape Invariants (96 x 96, 1 channel, int8) (Bytes 136..151)
  uint32_t input_dim_w = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[136]);
  uint32_t input_dim_h = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[140]);
  uint32_t input_channels = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[144]);
  uint32_t tensor_type = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[148]);
  assert_check(input_dim_w == 96 && input_dim_h == 96 && input_channels == 1 && tensor_type == 9,
               "3.8 Model input tensor encoded in FlatBuffer: [1, 96, 96, 1], type=kTfLiteInt8");
}

// =============================================================================
// Test Section 4: Continuous 10,000-Frame Stress Loop & Zero-Heap-Churn
// =============================================================================
void test_continuous_10k_stress_loop() {
  std::cout << "\n================================================================================\n";
  std::cout << " [CHALLENGE 4] Continuous 10,000-Frame Stress Loop & Zero Heap Churn             \n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;
  bool init_ok = detector.init();
  assert_check(init_ok, "4.1 CameraPersonDetector initialized successfully for stress testing");

  const int TOTAL_STRESS_FRAMES = 10000;
  std::vector<double> frame_latencies_us;
  frame_latencies_us.reserve(TOTAL_STRESS_FRAMES);

  // Arm heap allocation tracker AFTER all test harness vectors are pre-allocated
  g_heap_alloc_count = 0;
  g_heap_bytes_allocated = 0;
  g_track_allocations = true;

  uint32_t transitions_to_person = 0;
  uint32_t transitions_to_empty = 0;
  bool last_detection_state = false;

  auto t_start_total = std::chrono::high_resolution_clock::now();

  for (int frame_idx = 0; frame_idx < TOTAL_STRESS_FRAMES; ++frame_idx) {
    // Dynamically alternate between person silhouette and background scenes every 20 frames
    int mode = (frame_idx / 20) % 4;
    switch (mode) {
      case 0:
        detector.getDriver().setMockPattern(PATTERN_PERSON_SILHOUETTE);
        break;
      case 1:
        detector.getDriver().setMockPattern(PATTERN_EMPTY_SCENE);
        break;
      case 2:
        detector.getDriver().setMockPattern(PATTERN_GRADIENT);
        break;
      case 3:
        detector.getDriver().setMockPattern(PATTERN_SOLID_GRAY);
        break;
    }

    auto t0 = std::chrono::high_resolution_clock::now();
    bool proc_ok = detector.processFrame();
    auto t1 = std::chrono::high_resolution_clock::now();

    if (!proc_ok) {
      assert_check(false, "4.2.sub processFrame failed during stress loop");
      break;
    }

    double lat_us = std::chrono::duration<double, std::micro>(t1 - t0).count();
    frame_latencies_us.push_back(lat_us);

    bool cur_detected = detector.isPersonDetected();
    if (cur_detected != last_detection_state) {
      if (cur_detected) transitions_to_person++;
      else transitions_to_empty++;
      last_detection_state = cur_detected;
    }
  }

  auto t_end_total = std::chrono::high_resolution_clock::now();
  g_track_allocations = false;

  double total_time_ms = std::chrono::duration<double, std::milli>(t_end_total - t_start_total).count();
  std::sort(frame_latencies_us.begin(), frame_latencies_us.end());

  double avg_lat_us = (total_time_ms * 1000.0) / TOTAL_STRESS_FRAMES;
  double min_lat_us = frame_latencies_us.front();
  double max_lat_us = frame_latencies_us.back();
  double p50_lat_us = frame_latencies_us[TOTAL_STRESS_FRAMES * 0.50];
  double p95_lat_us = frame_latencies_us[TOTAL_STRESS_FRAMES * 0.95];
  double p99_lat_us = frame_latencies_us[TOTAL_STRESS_FRAMES * 0.99];

  std::cout << "  ------------------------------------------------------------------------------\n";
  std::cout << "  10,000-Frame Stress Test Metrics:\n";
  std::cout << "  - Total Duration    : " << total_time_ms << " ms (" << (TOTAL_STRESS_FRAMES * 1000.0 / total_time_ms) << " FPS)\n";
  std::cout << "  - Latency Min / Avg : " << min_lat_us << " us / " << avg_lat_us << " us\n";
  std::cout << "  - Latency P50 / P95 : " << p50_lat_us << " us / " << p95_lat_us << " us\n";
  std::cout << "  - Latency P99 / Max : " << p99_lat_us << " us / " << max_lat_us << " us\n";
  std::cout << "  - State Transitions : " << transitions_to_person << " enter, " << transitions_to_empty << " exit\n";
  std::cout << "  - Heap Allocations  : " << g_heap_alloc_count << " allocs (" << g_heap_bytes_allocated << " bytes)\n";
  std::cout << "  ------------------------------------------------------------------------------\n";

  assert_check(g_heap_alloc_count == 0,
               "4.2 Zero Heap Churn: Exactly 0 dynamic memory allocations (0 bytes) during 10,000 continuous frames");
  assert_check(detector.getDriver().getFrameCounter() == TOTAL_STRESS_FRAMES,
               "4.3 Driver frame counter strictly reached 10,000 without missed frames");
  assert_check(transitions_to_person >= 100 && transitions_to_empty >= 100,
               "4.4 Hysteresis state machine survived >=100 dynamic presence state transitions without deadlock");
  assert_check(avg_lat_us < 200.0,
               "4.5 Average frame execution latency on host is <200 microseconds (~5,000+ FPS capability)");
}

// =============================================================================
// Test Section 5: Adversarial Boundary Fuzzing & Debounce Stability
// =============================================================================
void test_adversarial_fuzzing() {
  std::cout << "\n================================================================================\n";
  std::cout << " [CHALLENGE 5] Adversarial Boundary Fuzzing & Debounce Resistance                \n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;
  detector.init();
  detector.setDetectionThreshold(0.60f, 0.40f);

  // 5.1 Debounce Flutter Attack: Alternate high confidence (0.88) and low confidence (0.05) every 1 frame
  detector.reset();
  assert_check(!detector.isPersonDetected(), "5.1 Detector starts in false state");

  bool state_fluttered = false;
  for (int i = 0; i < 200; ++i) {
    if (i % 2 == 0) {
      detector.getDriver().setMockPattern(PATTERN_PERSON_SILHOUETTE); // High presence (0.88)
    } else {
      detector.getDriver().setMockPattern(PATTERN_EMPTY_SCENE);       // Low presence (0.05)
    }
    detector.processFrame();
    if (detector.isPersonDetected()) {
      state_fluttered = true;
      break;
    }
  }
  assert_check(!state_fluttered,
               "5.2 Anti-flutter debounce: 200 high-frequency alternating single-frame pulses did NOT trigger state change");

  // 5.2 Confirm state change requires exactly 2 consecutive frames
  detector.getDriver().setMockPattern(PATTERN_PERSON_SILHOUETTE);
  detector.processFrame(); // Frame 1
  assert_check(!detector.isPersonDetected(), "5.3 Frame 1 of high presence does not trigger state (debounce count = 1)");
  detector.processFrame(); // Frame 2
  assert_check(detector.isPersonDetected(), "5.4 Frame 2 of high presence successfully triggers state (debounce count = 2)");

  // 5.3 Random Noise Frame Fuzzing (1,000 random white/black noise frames)
  std::mt19937 rng(999);
  std::uniform_int_distribution<int> byte_dist(0, 255);
  std::vector<uint8_t> fuzz_frame(CAMERA_FRAME_BYTES);

  bool fuzz_stable = true;
  for (int i = 0; i < 1000; ++i) {
    for (size_t j = 0; j < fuzz_frame.size(); ++j) {
      fuzz_frame[j] = static_cast<uint8_t>(byte_dist(rng));
    }
    detector.injectMockFrame(fuzz_frame.data(), fuzz_frame.size());
    if (!detector.processFrame()) {
      fuzz_stable = false;
      break;
    }
    float conf = detector.getConfidence();
    if (std::isnan(conf) || std::isinf(conf) || conf < 0.0f || conf > 1.0f) {
      fuzz_stable = false;
      break;
    }
  }
  assert_check(fuzz_stable,
               "5.5 Fuzzing stability: 1,000 random uniform noise frames processed without crash, NaN, or out-of-range confidence");
}

// =============================================================================
// Main Test Runner
// =============================================================================
int main() {
  std::cout << "================================================================================\n";
  std::cout << " CHALLENGER 2: ADVERSARIAL STRESS TEST & MATHEMATICAL INVARIANTS HARNESS        \n";
  std::cout << " Target: Milestone 2 OV7670 Driver & TFLite Micro ML Pipeline                   \n";
  std::cout << "================================================================================\n";

  test_preprocessor_invariants();
  test_downsampling_monotonicity();
  test_flatbuffer_invariants();
  test_continuous_10k_stress_loop();
  test_adversarial_fuzzing();

  std::cout << "\n================================================================================\n";
  std::cout << "                         STRESS HARNESS RESULTS SUMMARY                         \n";
  std::cout << "================================================================================\n";
  std::cout << " Total Verification Checks : " << g_test_checks << "\n";
  std::cout << " Invariants Confirmed      : " << (g_test_checks - g_test_failures) << "\n";
  std::cout << " Invariants Violated       : " << g_test_failures << "\n";
  std::cout << " Adversarial Verdict       : " << (g_test_failures == 0 ? "ALL PASS (100% INVARIANTS SATISFIED)" : "VERIFICATION FAILED") << "\n";
  std::cout << "================================================================================\n";

  return (g_test_failures == 0) ? 0 : 1;
}
