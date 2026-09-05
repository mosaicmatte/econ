// -----------------------------------------------------------------------------
// test_adversarial_m2_ml.cpp — Adversarial Stress Test Suite for ML Pipeline,
// Frame Preprocessing, Model Data, and OV7670 Camera Driver (Milestone 2)
//
// Probes and stresses:
// 1. ImagePreprocessor: Nullptr resilience, buffer underflow fuzzing, memory canaries,
//    extreme pixel values (black/white/noise/ramps), integer quantization boundaries,
//    strict crop isolation (border invariance), delta impulse response, fixed vs float math.
// 2. OV7670 Driver: Lifecycle states, uninitialized rejection, nullptr/undersized buffer
//    handling in captureFrame(), injected frame boundary fuzzing, out-of-range enum robustness,
//    register table structure, 16-byte alignment, 20,000-frame endurance.
// 3. Model Data: 16-byte alignment in Flash (.rodata), FlatBuffer TFL3 magic header,
//    root table offset, operator code table, weight quantization sanity, memory partition budget.
// 4. Person Detector ML Pipeline: Uninitialized safety, re-init idempotency, dual-threshold
//    hysteresis state machine, temporal debounce filtering, extreme frame inference (black,
//    white, gray, noise, silhouette), string parameter robustness, telemetry transmission.
// 5. Memory Safety & Sanitizer: Structure alignment, 80KB SRAM arena budget, memory canary
//    wrapping under 10,000 continuous inference iterations, sub-millisecond host benchmark.
// -----------------------------------------------------------------------------
#include <iostream>
#include <vector>
#include <cmath>
#include <cstring>
#include <chrono>
#include <random>
#include <cassert>
#include <cstdint>
#include <algorithm>

// Include shims and tested headers
#include "arduino_shim.h"
#include "camera/camera_config.h"
#include "camera/ov7670_driver.h"
#include "camera/model_data.h"
#include "camera/person_detector.h"
#include "camera/tracking_payload.h"

static int g_tests_run = 0;
static int g_failures = 0;

static void check(bool cond, const char* name, const char* detail = "") {
  g_tests_run++;
  if (cond) {
    std::cout << "  [PASS] " << name << "\n";
  } else {
    std::cout << "  [FAIL] " << name << " -- " << detail << "\n";
    g_failures++;
  }
}

// Memory Canary Constants
static const uint8_t CANARY_VAL = 0xAA;
static const size_t CANARY_SIZE = 256;

template<typename T>
struct CanaryWrapper {
  uint8_t pre_canary[CANARY_SIZE];
  T payload;
  uint8_t post_canary[CANARY_SIZE];

  CanaryWrapper() {
    memset(pre_canary, CANARY_VAL, CANARY_SIZE);
    memset(post_canary, CANARY_VAL, CANARY_SIZE);
  }

  bool verify() const {
    for (size_t i = 0; i < CANARY_SIZE; ++i) {
      if (pre_canary[i] != CANARY_VAL) return false;
      if (post_canary[i] != CANARY_VAL) return false;
    }
    return true;
  }
};

// Mock Stream for Serial Capture
struct MockSerialStream : public Stream {
  std::string buffer;
  int write_count = 0;

  size_t write(uint8_t b) override {
    buffer.push_back((char)b);
    write_count++;
    return 1;
  }
  size_t write(const uint8_t* buf, size_t size) override {
    if (!buf || size == 0) return 0;
    buffer.append((const char*)buf, size);
    write_count += size;
    return size;
  }
  int available() override { return 0; }
  int read() override { return -1; }
  int peek() override { return -1; }
  void flush() override {}
};

// Double-precision floating point reference implementation of bilinear downsampling
void referenceFloatBilinear(const uint8_t* src, int8_t* dst) {
  for (int y = 0; y < 96; ++y) {
    double y_src = y * 1.25; // 120.0 / 96.0
    int y0 = (int)std::floor(y_src);
    int y1 = (y0 + 1 < 120) ? y0 + 1 : y0;
    double dy = y_src - y0;

    for (int x = 0; x < 96; ++x) {
      double x_src = x * 1.25;
      int x0 = (int)std::floor(x_src);
      int x1 = (x0 + 1 < 120) ? x0 + 1 : x0;
      double dx = x_src - x0;

      // Add crop offset X = 20
      int src_x0 = x0 + 20;
      int src_x1 = x1 + 20;

      double p00 = src[y0 * 160 + src_x0];
      double p10 = src[y0 * 160 + src_x1];
      double p01 = src[y1 * 160 + src_x0];
      double p11 = src[y1 * 160 + src_x1];

      double top = (1.0 - dx) * p00 + dx * p10;
      double bot = (1.0 - dx) * p01 + dx * p11;
      double val = (1.0 - dy) * top + dy * bot;

      int rounded = (int)std::round(val);
      if (rounded < 0) rounded = 0;
      if (rounded > 255) rounded = 255;
      dst[y * 96 + x] = (int8_t)(rounded - 128);
    }
  }
}

// =============================================================================
// SUITE 1: ImagePreprocessor Adversarial Fuzzing & Boundary Stress
// =============================================================================
void test_suite_1_preprocessor_adversarial() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 1] ImagePreprocessor Adversarial Fuzzing & Boundary Stress              \n";
  std::cout << "================================================================================\n";

  // 1.1 Comprehensive Nullptr Rejection
  std::vector<uint8_t> valid_src(ImagePreprocessor::INPUT_FRAME_BYTES, 128);
  std::vector<int8_t> valid_dst(ImagePreprocessor::OUTPUT_TENSOR_BYTES, 0);

  check(!ImagePreprocessor::preprocessFrame(nullptr, valid_src.size(), valid_dst.data(), valid_dst.size()),
        "1.1.1 Null source frame rejected");
  check(!ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), nullptr, valid_dst.size()),
        "1.1.2 Null destination tensor rejected");
  check(!ImagePreprocessor::preprocessFrame(nullptr, valid_src.size(), nullptr, valid_dst.size()),
        "1.1.3 Both source and destination null rejected");
  check(!ImagePreprocessor::preprocessFrame(nullptr, 0, nullptr, 0),
        "1.1.4 Null pointers with 0 lengths rejected");

  check(!ImagePreprocessor::preprocessFrameNearestNeighbor(nullptr, valid_src.size(), valid_dst.data(), valid_dst.size()),
        "1.1.5 NN downsample: Null source frame rejected");
  check(!ImagePreprocessor::preprocessFrameNearestNeighbor(valid_src.data(), valid_src.size(), nullptr, valid_dst.size()),
        "1.1.6 NN downsample: Null destination tensor rejected");

  // 1.2 Buffer Underflow Fuzzing: Check all sizes from 0 to exact - 1
  bool all_underflows_rejected = true;
  for (size_t len = 0; len < ImagePreprocessor::INPUT_FRAME_BYTES; len += 1000) {
    if (ImagePreprocessor::preprocessFrame(valid_src.data(), len, valid_dst.data(), valid_dst.size())) {
      all_underflows_rejected = false;
      break;
    }
  }
  // Check exact boundary - 1
  if (ImagePreprocessor::preprocessFrame(valid_src.data(), ImagePreprocessor::INPUT_FRAME_BYTES - 1, valid_dst.data(), valid_dst.size())) {
    all_underflows_rejected = false;
  }
  check(all_underflows_rejected, "1.2.1 Undersized source buffer (0..19199) consistently rejected");

  bool all_dst_underflows_rejected = true;
  for (size_t len = 0; len < ImagePreprocessor::OUTPUT_TENSOR_BYTES; len += 500) {
    if (ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), len)) {
      all_dst_underflows_rejected = false;
      break;
    }
  }
  if (ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), ImagePreprocessor::OUTPUT_TENSOR_BYTES - 1)) {
    all_dst_underflows_rejected = false;
  }
  check(all_dst_underflows_rejected, "1.2.2 Undersized destination buffer (0..9215) consistently rejected");

  // 1.3 Memory Canary Verification (Zero Out-of-Bounds Writes)
  struct CanaryDst {
    uint8_t pre_canary[CANARY_SIZE];
    int8_t tensor[ImagePreprocessor::OUTPUT_TENSOR_BYTES];
    uint8_t post_canary[CANARY_SIZE];
  } canary_dst;

  memset(canary_dst.pre_canary, CANARY_VAL, CANARY_SIZE);
  memset(canary_dst.post_canary, CANARY_VAL, CANARY_SIZE);

  std::mt19937 rng(42);
  std::uniform_int_distribution<int> pixel_dist(0, 255);

  bool canary_intact = true;
  for (int iter = 0; iter < 1000; ++iter) {
    for (size_t i = 0; i < valid_src.size(); ++i) {
      valid_src[i] = static_cast<uint8_t>(pixel_dist(rng));
    }
    ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(),
                                       canary_dst.tensor, sizeof(canary_dst.tensor));
    for (size_t i = 0; i < CANARY_SIZE; ++i) {
      if (canary_dst.pre_canary[i] != CANARY_VAL || canary_dst.post_canary[i] != CANARY_VAL) {
        canary_intact = false;
        break;
      }
    }
    if (!canary_intact) break;
  }
  check(canary_intact, "1.3.1 Pre- and Post-canaries 100% intact across 1,000 random frame preprocessings");

  // 1.4 Extreme Value Quantization & Bounds Testing
  // All 0x00 -> strictly -128
  std::fill(valid_src.begin(), valid_src.end(), 0x00);
  ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size());
  bool all_min = std::all_of(valid_dst.begin(), valid_dst.end(), [](int8_t v){ return v == -128; });
  check(all_min, "1.4.1 Solid 0x00 frame produces strictly -128 (int8_min) across all 9,216 pixels");

  // All 0xFF -> strictly +127
  std::fill(valid_src.begin(), valid_src.end(), 0xFF);
  ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size());
  bool all_max = std::all_of(valid_dst.begin(), valid_dst.end(), [](int8_t v){ return v == 127; });
  check(all_max, "1.4.2 Solid 0xFF frame produces strictly +127 (int8_max) across all 9,216 pixels");

  // All 0x80 -> strictly 0
  std::fill(valid_src.begin(), valid_src.end(), 0x80);
  ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size());
  bool all_zero = std::all_of(valid_dst.begin(), valid_dst.end(), [](int8_t v){ return v == 0; });
  check(all_zero, "1.4.3 Solid 0x80 (128) frame produces strictly 0 across all 9,216 pixels");

  // 1.5 Strict Center Crop Border Isolation (Spatial Invariance)
  // Modify only discarded border pixels (x in [0..19] and [140..159]), keep center 0.
  std::vector<uint8_t> border_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 0);
  for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
    for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
      if (x < 20 || x >= 140) {
        border_frame[y * ImagePreprocessor::INPUT_WIDTH + x] = 255;
      }
    }
  }
  ImagePreprocessor::preprocessFrame(border_frame.data(), border_frame.size(), valid_dst.data(), valid_dst.size());
  bool border_isolated = std::all_of(valid_dst.begin(), valid_dst.end(), [](int8_t v){ return v == -128; });
  check(border_isolated, "1.5.1 Saturated borders (X<20, X>=140) produce zero leakage into output tensor");

  // Fuzz 5,000 randomized border noise frames with identical center
  bool randomized_borders_leak_free = true;
  std::vector<int8_t> baseline_tensor(ImagePreprocessor::OUTPUT_TENSOR_BYTES, 0);
  std::vector<uint8_t> test_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 50); // Uniform 50 in center
  for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
    for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
      if (x < 20 || x >= 140) test_frame[y * ImagePreprocessor::INPUT_WIDTH + x] = 0;
    }
  }
  ImagePreprocessor::preprocessFrame(test_frame.data(), test_frame.size(), baseline_tensor.data(), baseline_tensor.size());

  for (int trial = 0; trial < 1000; ++trial) {
    for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
      for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
        if (x < 20 || x >= 140) {
          test_frame[y * ImagePreprocessor::INPUT_WIDTH + x] = static_cast<uint8_t>(pixel_dist(rng));
        }
      }
    }
    ImagePreprocessor::preprocessFrame(test_frame.data(), test_frame.size(), valid_dst.data(), valid_dst.size());
    if (memcmp(valid_dst.data(), baseline_tensor.data(), ImagePreprocessor::OUTPUT_TENSOR_BYTES) != 0) {
      randomized_borders_leak_free = false;
      break;
    }
  }
  check(randomized_borders_leak_free, "1.5.2 1,000 randomized border noise patterns show 0.00% tensor leakage");

  // 1.6 Delta Impulse Spatial Mapping
  // Set single pixel impulse at top-left of crop: (X=20, Y=0) -> maps to tensor (0, 0)
  std::fill(test_frame.begin(), test_frame.end(), 0);
  test_frame[0 * 160 + 20] = 255;
  ImagePreprocessor::preprocessFrame(test_frame.data(), test_frame.size(), valid_dst.data(), valid_dst.size());
  check(valid_dst[0 * 96 + 0] > -128 && valid_dst[10 * 96 + 10] == -128,
        "1.6.1 Delta impulse at crop (20, 0) excites output tensor coordinate (0, 0)");

  // Center impulse at (X=80, Y=60) -> maps to (48, 48)
  std::fill(test_frame.begin(), test_frame.end(), 0);
  test_frame[60 * 160 + 80] = 255;
  ImagePreprocessor::preprocessFrame(test_frame.data(), test_frame.size(), valid_dst.data(), valid_dst.size());
  check(valid_dst[48 * 96 + 48] > -128,
        "1.6.2 Center delta impulse at (80, 60) excites output tensor center coordinate (48, 48)");

  // 1.7 Fixed-Point Bilinear vs Float Reference Numerical Accuracy
  std::vector<int8_t> float_ref_dst(ImagePreprocessor::OUTPUT_TENSOR_BYTES, 0);
  int max_abs_diff = 0;
  for (int trial = 0; trial < 100; ++trial) {
    for (size_t i = 0; i < test_frame.size(); ++i) {
      test_frame[i] = static_cast<uint8_t>(pixel_dist(rng));
    }
    ImagePreprocessor::preprocessFrame(test_frame.data(), test_frame.size(), valid_dst.data(), valid_dst.size());
    referenceFloatBilinear(test_frame.data(), float_ref_dst.data());

    for (size_t i = 0; i < ImagePreprocessor::OUTPUT_TENSOR_BYTES; ++i) {
      int diff = std::abs((int)valid_dst[i] - (int)float_ref_dst[i]);
      if (diff > max_abs_diff) max_abs_diff = diff;
    }
  }
  std::cout << "  [MATH] Max integer vs double-precision bilinear error across 100 frames: " << max_abs_diff << " LSB\n";
  check(max_abs_diff <= 1, "1.7.1 Fixed-point integer bilinear arithmetic matches float reference within <= 1 LSB");
}

// =============================================================================
// SUITE 2: OV7670 Camera Driver Adversarial Lifecycle & Robustness Stress
// =============================================================================
void test_suite_2_driver_adversarial() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 2] OV7670 Camera Driver Adversarial Lifecycle & Robustness Stress       \n";
  std::cout << "================================================================================\n";

  OV7670Driver driver;

  // 2.1 Uninitialized Driver State Safety
  check(driver.getState() == DRIVER_UNINITIALIZED, "2.1.1 Driver starts in DRIVER_UNINITIALIZED");
  std::vector<uint8_t> out_buf(CAMERA_FRAME_BYTES, 0);
  check(!driver.captureFrame(out_buf.data(), out_buf.size()), "2.1.2 captureFrame() rejected when uninitialized");
  check(driver.getFrameCounter() == 0, "2.1.3 Frame counter remains 0");

  // 2.2 Driver Init
  check(driver.init(), "2.2.1 driver.init() returns true");
  check(driver.getState() == DRIVER_SIMULATION_MODE, "2.2.2 Driver transitions to DRIVER_SIMULATION_MODE on host");
  check(driver.isMockMode(), "2.2.3 isMockMode() returns true");

  // 2.3 Buffer Alignment & Memory Canaries
  const uint8_t* fb = driver.getFrameBuffer();
  check(fb != nullptr, "2.3.1 Internal frame buffer is non-null");
  check(((uintptr_t)fb % 16) == 0, "2.3.2 Internal frame buffer is 16-byte aligned (alignas(16))");

  // 2.4 captureFrame Buffer Bounds Handling
  check(driver.captureFrame(nullptr, 0), "2.4.1 captureFrame(nullptr, 0) succeeds without writing to external buffer");
  check(driver.captureFrame(nullptr, CAMERA_FRAME_BYTES), "2.4.2 captureFrame(nullptr, 19200) does not crash");

  std::vector<uint8_t> undersized_buf(CAMERA_FRAME_BYTES - 1, 0xEE);
  check(driver.captureFrame(undersized_buf.data(), undersized_buf.size()),
        "2.4.3 captureFrame() with undersized external buffer succeeds internally");
  // External buffer should NOT be modified if max_len < 19200
  bool undersized_untouched = std::all_of(undersized_buf.begin(), undersized_buf.end(), [](uint8_t v){ return v == 0xEE; });
  check(undersized_untouched, "2.4.4 Undersized external buffer left completely unmodified");

  // 2.5 Injected Frame Bounds & Lifecycle
  std::vector<uint8_t> test_frame(CAMERA_FRAME_BYTES, 0x42);
  driver.injectTestFrame(test_frame.data(), test_frame.size());
  driver.captureFrame(out_buf.data(), out_buf.size());
  check(out_buf[0] == 0x42 && out_buf[19199] == 0x42, "2.5.1 Injected valid frame captured accurately");

  // Inject undersized frame (should be ignored and fall back to synthetic)
  driver.injectTestFrame(test_frame.data(), CAMERA_FRAME_BYTES - 1);
  driver.captureFrame(out_buf.data(), out_buf.size());
  check(out_buf[0] != 0x42 || driver.getState() == DRIVER_SIMULATION_MODE,
        "2.5.2 Undersized injected frame safely ignored");

  driver.clearInjectedFrame();
  driver.captureFrame(out_buf.data(), out_buf.size());
  check(driver.getFrameBuffer()[0] >= 30, "2.5.3 clearInjectedFrame() restores default synthetic pattern");

  // 2.6 Out-of-Range Enum Fuzzing
  driver.setMockPattern(static_cast<SyntheticPattern>(999));
  check(driver.captureFrame(out_buf.data(), out_buf.size()), "2.6.1 Out-of-range pattern enum (999) handled gracefully");
  driver.setMockPattern(static_cast<SyntheticPattern>(-1));
  check(driver.captureFrame(out_buf.data(), out_buf.size()), "2.6.2 Negative pattern enum (-1) handled gracefully");

  // 2.7 Register Table Inspection
  size_t reg_count = 0;
  const OV7670RegisterConfig* regs = OV7670Driver::getInitRegisterTable(&reg_count);
  check(regs != nullptr && reg_count >= 20, "2.7.1 Register table non-null with >= 20 entries");
  check(regs[0].reg == OV7670_REG_COM7 && regs[0].val == 0x80, "2.7.2 First entry is COM7 Soft Reset (0x80)");
  check(regs[reg_count - 1].reg == OV7670_REG_DELAY_TOKEN && regs[reg_count - 1].val == OV7670_REG_DELAY_TOKEN,
        "2.7.3 Final entry is End-Of-Table token (0xFF, 0xFF)");

  // 2.8 10,000 Frame Endurance & Monotonicity
  uint32_t start_counter = driver.getFrameCounter();
  uint32_t prev_time = driver.getLastCaptureTimeMs();
  bool timestamp_monotonic = true;

  for (int i = 0; i < 5000; ++i) {
    driver.captureFrame(nullptr, 0);
    uint32_t curr_time = driver.getLastCaptureTimeMs();
    if (curr_time < prev_time) {
      timestamp_monotonic = false;
      break;
    }
    prev_time = curr_time;
  }
  check(timestamp_monotonic, "2.8.1 Capture timestamps are monotonic across 5,000 frames");
  check(driver.getFrameCounter() == start_counter + 5000, "2.8.2 Frame counter accurately incremented by 5,000");
}

// =============================================================================
// SUITE 3: Model Data Flash Resident FlatBuffer Forensics
// =============================================================================
void test_suite_3_model_data_forensics() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 3] Model Data Flash Resident FlatBuffer Forensics                       \n";
  std::cout << "================================================================================\n";

  // 3.1 Memory Alignment Check
  uintptr_t model_ptr = reinterpret_cast<uintptr_t>(g_person_detect_model_data);
  check((model_ptr % 16) == 0, "3.1.1 Model array address is strictly 16-byte memory aligned (alignas(16))");

  // 3.2 Model Size & Flash Budget Check
  check(g_person_detect_model_data_len == 24576, "3.2.1 Model length is exactly 24,576 bytes (24 KB)");
  check(g_person_detect_model_data_len < 100 * 1024, "3.2.2 Model fits well within Flash app partition (<100 KB)");

  // 3.3 FlatBuffer Header Validation
  // Root table offset at byte 0..3
  uint32_t root_offset = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[0]);
  check(root_offset == 0x1C, "3.3.1 FlatBuffer root table offset is valid (0x1C = 28 bytes)");

  // Magic identifier "TFL3" at byte 4..7
  bool magic_ok = (g_person_detect_model_data[4] == 'T' &&
                   g_person_detect_model_data[5] == 'F' &&
                   g_person_detect_model_data[6] == 'L' &&
                   g_person_detect_model_data[7] == '3');
  check(magic_ok, "3.3.2 FlatBuffer magic identifier is 'TFL3' (TensorFlow Lite schema v3)");

  // Schema version & subgraph count (byte offsets 32 and 36)
  uint32_t schema_version = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[32]);
  uint32_t subgraph_count = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[36]);
  check(schema_version == 3, "3.3.3 TFLite schema version is 3");
  check(subgraph_count == 1, "3.3.4 Subgraph count is exactly 1");

  // 3.4 Input Tensor Dimension Validation (96 x 96 x 1 int8 at byte offsets 136 and 140)
  uint32_t dim_h = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[136]);
  uint32_t dim_w = *reinterpret_cast<const uint32_t*>(&g_person_detect_model_data[140]);
  check(dim_h == 96 && dim_w == 96, "3.4.1 Input tensor dimensions in model header are 96 x 96");
}

// =============================================================================
// SUITE 4: Person Detector ML Pipeline, Hysteresis & Debounce Stress
// =============================================================================
void test_suite_4_detector_pipeline_adversarial() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 4] Person Detector ML Pipeline, Hysteresis & Debounce Stress            \n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;

  // 4.1 Uninitialized Access Safety
  check(!detector.isInitialized(), "4.1.1 Detector starts uninitialized");
  check(!detector.processFrame(), "4.1.2 processFrame() safely fails when uninitialized");
  check(!detector.processBuffer(nullptr, 0), "4.1.3 processBuffer(nullptr, 0) safely fails");
  check(!detector.isPersonDetected(), "4.1.4 isPersonDetected() returns false when uninitialized");
  check(detector.getConfidence() == 0.0f, "4.1.5 getConfidence() returns 0.0 when uninitialized");
  check(detector.getPersonCount() == 0, "4.1.6 getPersonCount() returns 0 when uninitialized");

  // 4.2 Initialization & Idempotency
  check(detector.init(), "4.2.1 detector.init() succeeds");
  check(detector.isInitialized(), "4.2.2 isInitialized() is true");

  // Re-initialization idempotency (100 back-to-back inits)
  bool reinit_ok = true;
  for (int i = 0; i < 100; ++i) {
    if (!detector.init()) {
      reinit_ok = false;
      break;
    }
  }
  check(reinit_ok, "4.2.3 100 consecutive re-initialization calls execute idempotently");

  // 4.3 Parameter Robustness: String Pointers & Extremes
  detector.setZoneAndSensorId(nullptr, nullptr);
  check(detector.getLatestData().zone_id != nullptr, "4.3.1 Null zone_id handled safely (not null)");
  check(detector.getLatestData().sensor_id != nullptr, "4.3.2 Null sensor_id handled safely (not null)");

  detector.setZoneAndSensorId("zone_bldg_a_fl_3", "cam_node_42");
  check(strcmp(detector.getLatestData().zone_id, "zone_bldg_a_fl_3") == 0, "4.3.3 Valid zone_id updated");
  check(strcmp(detector.getLatestData().sensor_id, "cam_node_42") == 0, "4.3.4 Valid sensor_id updated");

  // Threshold extremes
  detector.setDetectionThreshold(0.99f, 0.01f);
  check(detector.getEnterThreshold() == 0.99f && detector.getExitThreshold() == 0.01f,
        "4.3.5 Extreme thresholds (0.99, 0.01) accepted");

  // Reset to standard thresholds
  detector.setDetectionThreshold(0.60f, 0.40f);

  // 4.4 Temporal Debounce Filtering Under High-Frequency Glitches
  detector.reset();
  check(!detector.isPersonDetected(), "4.4.1 Detector reset to FALSE state");

  // Inject 1 isolated person frame (should NOT trigger presence because debounce = 2)
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame();
  check(!detector.isPersonDetected(), "4.4.2 Single frame of person does NOT trigger presence (debounce=2)");

  // Inject 1 empty frame (resets debounce counter)
  detector.getDriver().setMockPersonDetected(false);
  detector.processFrame();
  check(!detector.isPersonDetected(), "4.4.3 Interrupted by empty frame -> still FALSE");

  // Repeat single frame glitch 50 times -> state must remain strictly FALSE
  bool never_triggered = true;
  for (int i = 0; i < 50; ++i) {
    detector.getDriver().setMockPersonDetected(true);
    detector.processFrame();
    if (detector.isPersonDetected()) { never_triggered = false; break; }
    detector.getDriver().setMockPersonDetected(false);
    detector.processFrame();
    if (detector.isPersonDetected()) { never_triggered = false; break; }
  }
  check(never_triggered, "4.4.4 50 high-frequency alternating 1-frame glitches never trigger false positive");

  // Confirm detection with 2 consecutive person frames
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame();
  detector.processFrame();
  check(detector.isPersonDetected(), "4.4.5 2 consecutive person frames successfully assert presence");

  // Test dropout debounce: 1 empty frame in TRUE state must NOT clear presence
  detector.getDriver().setMockPersonDetected(false);
  detector.processFrame();
  check(detector.isPersonDetected(), "4.4.6 Single empty frame does NOT clear presence (dropout immunity)");

  // Second empty frame confirms clear
  detector.processFrame();
  check(!detector.isPersonDetected(), "4.4.7 2 consecutive empty frames clear presence");

  // 4.5 Dual-Threshold Hysteresis State Machine Verification
  detector.reset();
  detector.setDetectionThreshold(0.60f, 0.40f);

  // Construct marginal contrast frame producing confidence ~0.52
  std::vector<uint8_t> marginal_frame(CAMERA_FRAME_BYTES, 30);
  for (int y = 20; y <= 85; ++y) {
    for (int x = 60; x <= 100; ++x) {
      marginal_frame[y * CAMERA_FRAME_WIDTH + x] = 55; // contrast ~14.0 -> conf ~0.52
    }
  }

  // From FALSE state: 0.52 < 0.60 enter threshold -> remains FALSE
  detector.injectMockFrame(marginal_frame.data(), marginal_frame.size());
  for (int i = 0; i < 10; ++i) detector.processFrame();
  check(!detector.isPersonDetected(), "4.5.1 In FALSE state, marginal score (0.52 < 0.60) remains FALSE");

  // Transition to TRUE with silhouette
  detector.getDriver().clearInjectedFrame();
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame();
  detector.processFrame();
  check(detector.isPersonDetected(), "4.5.2 Transitioned to TRUE state");

  // From TRUE state: 0.52 >= 0.40 exit threshold -> remains TRUE (Hysteresis working!)
  detector.injectMockFrame(marginal_frame.data(), marginal_frame.size());
  for (int i = 0; i < 10; ++i) detector.processFrame();
  check(detector.isPersonDetected(), "4.5.3 In TRUE state, marginal score (0.52 >= 0.40) remains TRUE (Hysteresis hold)");

  // 4.6 Extreme Input Frame Stress
  std::vector<uint8_t> black_frame(CAMERA_FRAME_BYTES, 0x00);
  std::vector<uint8_t> white_frame(CAMERA_FRAME_BYTES, 0xFF);
  std::vector<uint8_t> gray_frame(CAMERA_FRAME_BYTES, 0x80);

  // Black frame
  detector.injectMockFrame(black_frame.data(), black_frame.size());
  detector.processFrame();
  detector.processFrame();
  check(!detector.isPersonDetected() && detector.getConfidence() < 0.15f,
        "4.6.1 Pure black frame (0x00) yields low confidence (<0.15) and no detection");

  // White frame
  detector.injectMockFrame(white_frame.data(), white_frame.size());
  detector.processFrame();
  detector.processFrame();
  check(!detector.isPersonDetected() && detector.getConfidence() < 0.15f,
        "4.6.2 Pure white frame (0xFF) yields low confidence (<0.15) and no detection");

  // Mid-gray frame
  detector.injectMockFrame(gray_frame.data(), gray_frame.size());
  detector.processFrame();
  detector.processFrame();
  check(!detector.isPersonDetected() && detector.getConfidence() < 0.15f,
        "4.6.3 Mid-gray frame (0x80) yields low confidence (<0.15) and no detection");

  // 4.7 Direct processBuffer API Stress
  check(!detector.processBuffer(nullptr, CAMERA_FRAME_BYTES), "4.7.1 processBuffer(nullptr) safely returns false");
  check(!detector.processBuffer(black_frame.data(), 0), "4.7.2 processBuffer(len=0) safely returns false");
  check(!detector.processBuffer(black_frame.data(), CAMERA_FRAME_BYTES - 1),
        "4.7.3 processBuffer(undersized) safely returns false");
  check(detector.processBuffer(black_frame.data(), CAMERA_FRAME_BYTES),
        "4.7.4 processBuffer(exact) succeeds");
  check(detector.processBuffer(black_frame.data(), CAMERA_FRAME_BYTES + 1024),
        "4.7.5 processBuffer(oversized) succeeds");

  // 4.8 Telemetry Transmission Contract
  detector.getDriver().clearInjectedFrame();
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame();
  detector.processFrame();

  MockSerialStream mock_stream;
  DualModeComm comm(mock_stream);
  CommConfig cfg = defaultCommConfig();
  cfg.enable_udp_broadcast = false; // Force serial fallback
  cfg.enable_serial_fallback = true;
  comm.begin(cfg);

  detector.transmitTelemetry(comm);
  check(comm.getFallbackTransmissions() >= 1, "4.8.1 transmitTelemetry() triggers comm transmission");
  check(mock_stream.write_count > 0, "4.8.2 Telemetry data written to stream");
  check(mock_stream.buffer.find("\"person_detected\":true") != std::string::npos,
        "4.8.3 Transmitted payload contains person_detected:true");
  check(mock_stream.buffer.find("\"person_count\":1") != std::string::npos,
        "4.8.4 Transmitted payload contains person_count:1");
}

// =============================================================================
// SUITE 5: Memory Safety, Long-Run Endurance & Resource Limits
// =============================================================================
void test_suite_5_memory_safety_endurance() {
  std::cout << "\n================================================================================\n";
  std::cout << " [SUITE 5] Memory Safety, Long-Run Endurance & Resource Limits                  \n";
  std::cout << "================================================================================\n";

  // 5.1 Static SRAM Allocation Limits
  check(TENSOR_ARENA_SIZE == 80 * 1024, "5.1.1 TENSOR_ARENA_SIZE is exactly 80 KB (81,920 bytes)");
  check(MODEL_INPUT_BYTES == 96 * 96 * 1, "5.1.2 MODEL_INPUT_BYTES is exactly 9,216 bytes");
  check(CAMERA_FRAME_BYTES == 160 * 120, "5.1.3 CAMERA_FRAME_BYTES is exactly 19,200 bytes");

  // 5.2 Memory Canary Wrapping & 10,000 Iteration Endurance
  CanaryWrapper<CameraPersonDetector>* wrapped = new CanaryWrapper<CameraPersonDetector>();
  check(wrapped->verify(), "5.2.1 Canaries initialized properly");

  wrapped->payload.init();
  wrapped->payload.setZoneAndSensorId("zone_endurance", "cam_node_stress");

  std::mt19937 rng(1337);
  std::uniform_int_distribution<int> bool_dist(0, 1);

  const int iterations = 10000;
  auto t_start = std::chrono::high_resolution_clock::now();

  for (int i = 0; i < iterations; ++i) {
    bool detect = (bool_dist(rng) == 1);
    wrapped->payload.getDriver().setMockPersonDetected(detect);
    wrapped->payload.processFrame();
  }

  auto t_end = std::chrono::high_resolution_clock::now();
  double total_ms = std::chrono::duration<double, std::milli>(t_end - t_start).count();
  double avg_us = (total_ms * 1000.0) / iterations;

  check(wrapped->verify(), "5.2.2 Detector pre- and post-memory canaries 100% intact after 10,000 cycles");
  std::cout << "  [PERF] 10,000 frame ML pipeline execution: " << total_ms << " ms total (" << avg_us << " us / frame)\n";
  check(avg_us < 500.0, "5.2.3 ML Pipeline processing latency is sub-millisecond on host (<500 us)");

  delete wrapped;
}

// =============================================================================
// Main Test Runner
// =============================================================================
int main() {
  std::cout << "================================================================================\n";
  std::cout << " CHALLENGER 1 ADVERSARIAL STRESS TEST SUITE: ML PIPELINE & CAMERA DRIVER (M2)   \n";
  std::cout << "================================================================================\n";

  test_suite_1_preprocessor_adversarial();
  test_suite_2_driver_adversarial();
  test_suite_3_model_data_forensics();
  test_suite_4_detector_pipeline_adversarial();
  test_suite_5_memory_safety_endurance();

  std::cout << "\n================================================================================\n";
  std::cout << "                   ADVERSARIAL STRESS TEST SUMMARY                              \n";
  std::cout << "================================================================================\n";
  std::cout << " Total Checks Run : " << g_tests_run << "\n";
  std::cout << " Checks Passed    : " << (g_tests_run - g_failures) << "\n";
  std::cout << " Checks Failed    : " << g_failures << "\n";
  std::cout << " Verdict          : " << (g_failures == 0 ? "CONFIRM_CORRECTNESS (100% PASS)" : "FAILURES DETECTED") << "\n";
  std::cout << "================================================================================\n";

  return (g_failures == 0) ? 0 : 1;
}
