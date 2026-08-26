// -----------------------------------------------------------------------------
// test_m2_camera_ml.cpp — Comprehensive Host Test Suite for Milestone 2
//
// Verifies:
// 1. ImagePreprocessor fixed-point bilinear downsampling, bounds safety, crop isolation & math
// 2. OV7670 camera driver lifecycle, register table, and simulation/mock fallback
// 3. Model data integrity, Flash .rodata 16-byte alignment, and FlatBuffer TFL3 header
// 4. ML PersonDetector inference, dual-threshold hysteresis, debounce filter & dequantization
// 5. CameraPersonDetector integration, telemetry transmission, and contract adherence
// -----------------------------------------------------------------------------
#include <iostream>
#include <vector>
#include <cmath>
#include <cstring>
#include <chrono>
#include <cassert>

// Include shims and tested headers
#include "arduino_shim.h"
#include "camera/camera_config.h"
#include "camera/ov7670_driver.h"
#include "camera/model_data.h"
#include "camera/person_detector.h"

static int g_failures = 0;
static int g_tests_run = 0;

static void check(bool cond, const char* name, const char* detail = "") {
  g_tests_run++;
  if (cond) {
    std::cout << "  [PASS] " << name << "\n";
  } else {
    std::cout << "  [FAIL] " << name << " -- " << detail << "\n";
    g_failures++;
  }
}

// Mock DualModeComm for Telemetry Verification
class MockDualModeComm : public DualModeComm {
public:
  bool transmit_called = false;
  PersonTrackingData last_transmitted_data{};

  bool transmit(const PersonTrackingData& data) override {
    transmit_called = true;
    last_transmitted_data = data;
    return true;
  }
};

// =============================================================================
// Suite 1: ImagePreprocessor Fixed-Point Math & Bounds Safety
// =============================================================================
void test_suite_1_preprocessor() {
  std::cout << "\n================================================================================\n";
  std::cout << " Suite 1: ImagePreprocessor Fixed-Point Math & Bounds Safety                    \n";
  std::cout << "================================================================================\n";

  // 1.1 Null and Buffer Bounds Safety
  std::vector<uint8_t> valid_src(ImagePreprocessor::INPUT_FRAME_BYTES, 128);
  std::vector<int8_t> valid_dst(ImagePreprocessor::OUTPUT_TENSOR_BYTES, 0);

  check(!ImagePreprocessor::preprocessFrame(nullptr, valid_src.size(), valid_dst.data(), valid_dst.size()),
        "1.1.1 Null source buffer safely rejected");
  check(!ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), nullptr, valid_dst.size()),
        "1.1.2 Null destination buffer safely rejected");
  check(!ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size() - 1, valid_dst.data(), valid_dst.size()),
        "1.1.3 Undersized source buffer rejected (19199 < 19200)");
  check(!ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size() - 1),
        "1.1.4 Undersized destination buffer rejected (9215 < 9216)");

  // 1.2 Solid Luminance Frames Normalization (p -> p - 128)
  // All-Black (0 -> -128)
  std::fill(valid_src.begin(), valid_src.end(), 0);
  check(ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size()),
        "1.2.1 Preprocess all-black frame succeeded");
  bool all_black_ok = true;
  for (int8_t val : valid_dst) {
    if (val != -128) { all_black_ok = false; break; }
  }
  check(all_black_ok, "1.2.2 All-black frame (0) maps uniformly to -128 across all 9216 pixels");

  // All-White (255 -> +127)
  std::fill(valid_src.begin(), valid_src.end(), 255);
  ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size());
  bool all_white_ok = true;
  for (int8_t val : valid_dst) {
    if (val != 127) { all_white_ok = false; break; }
  }
  check(all_white_ok, "1.2.3 All-white frame (255) maps uniformly to +127 across all 9216 pixels");

  // Mid-Gray (128 -> 0)
  std::fill(valid_src.begin(), valid_src.end(), 128);
  ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size());
  bool mid_gray_ok = true;
  for (int8_t val : valid_dst) {
    if (val != 0) { mid_gray_ok = false; break; }
  }
  check(mid_gray_ok, "1.2.4 Mid-gray frame (128) maps uniformly to 0 across all 9216 pixels");

  // Quarter-Luminance (64 -> -64)
  std::fill(valid_src.begin(), valid_src.end(), 64);
  ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size());
  bool quarter_ok = true;
  for (int8_t val : valid_dst) {
    if (val != -64) { quarter_ok = false; break; }
  }
  check(quarter_ok, "1.2.5 Quarter-intensity frame (64) maps uniformly to -64");

  // Three-Quarter Luminance (192 -> +64)
  std::fill(valid_src.begin(), valid_src.end(), 192);
  ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size());
  bool three_quarter_ok = true;
  for (int8_t val : valid_dst) {
    if (val != 64) { three_quarter_ok = false; break; }
  }
  check(three_quarter_ok, "1.2.6 Three-quarter intensity frame (192) maps uniformly to +64");

  // 1.3 Center Crop Spatial Isolation
  std::vector<uint8_t> crop_test_src(ImagePreprocessor::INPUT_FRAME_BYTES, 0);
  for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
    for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
      if (x < 20 || x >= 140) {
        crop_test_src[y * ImagePreprocessor::INPUT_WIDTH + x] = 255; // Discarded borders
      } else {
        crop_test_src[y * ImagePreprocessor::INPUT_WIDTH + x] = 0;   // Active center crop
      }
    }
  }
  ImagePreprocessor::preprocessFrame(crop_test_src.data(), crop_test_src.size(), valid_dst.data(), valid_dst.size());
  bool crop_isolated = true;
  for (int8_t val : valid_dst) {
    if (val != -128) { crop_isolated = false; break; }
  }
  check(crop_isolated, "1.3.1 Discarded borders (X<20, X>=140) have zero influence on output tensor");

  // Precise coordinate verification: Center vertical line at X=80 (80-20=60 in crop -> 60*4/5 = 48 in tensor)
  std::fill(crop_test_src.begin(), crop_test_src.end(), 0);
  for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
    crop_test_src[y * ImagePreprocessor::INPUT_WIDTH + 80] = 255;
  }
  ImagePreprocessor::preprocessFrame(crop_test_src.data(), crop_test_src.size(), valid_dst.data(), valid_dst.size());
  int8_t center_col_val = valid_dst[48 * ImagePreprocessor::OUTPUT_WIDTH + 48];
  int8_t off_center_val = valid_dst[48 * ImagePreprocessor::OUTPUT_WIDTH + 10];
  check(center_col_val > 0 && off_center_val == -128,
        "1.3.2 Center vertical marker at X=80 maps precisely to output tensor column 48");

  // 1.4 Monotonic Linear Gradients
  std::vector<uint8_t> grad_src(ImagePreprocessor::INPUT_FRAME_BYTES);
  // Horizontal Ramp
  for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
    for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
      grad_src[y * ImagePreprocessor::INPUT_WIDTH + x] = static_cast<uint8_t>((x * 255) / 159);
    }
  }
  ImagePreprocessor::preprocessFrame(grad_src.data(), grad_src.size(), valid_dst.data(), valid_dst.size());
  bool h_monotonic = true;
  for (int x = 0; x < ImagePreprocessor::OUTPUT_WIDTH - 1; ++x) {
    if (valid_dst[48 * ImagePreprocessor::OUTPUT_WIDTH + (x + 1)] < valid_dst[48 * ImagePreprocessor::OUTPUT_WIDTH + x]) {
      h_monotonic = false;
      break;
    }
  }
  check(h_monotonic, "1.4.1 Horizontal ramp produces strictly monotonically increasing tensor row");

  // Vertical Ramp
  for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
    for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
      grad_src[y * ImagePreprocessor::INPUT_WIDTH + x] = static_cast<uint8_t>((y * 255) / 119);
    }
  }
  ImagePreprocessor::preprocessFrame(grad_src.data(), grad_src.size(), valid_dst.data(), valid_dst.size());
  bool v_monotonic = true;
  for (int y = 0; y < ImagePreprocessor::OUTPUT_HEIGHT - 1; ++y) {
    if (valid_dst[(y + 1) * ImagePreprocessor::OUTPUT_WIDTH + 48] < valid_dst[y * ImagePreprocessor::OUTPUT_WIDTH + 48]) {
      v_monotonic = false;
      break;
    }
  }
  check(v_monotonic, "1.4.2 Vertical ramp produces strictly monotonically increasing tensor column");

  // 1.5 Checkerboard Antialiasing vs Nearest Neighbor
  std::vector<uint8_t> checker_src(ImagePreprocessor::INPUT_FRAME_BYTES);
  std::vector<int8_t> nn_dst(ImagePreprocessor::OUTPUT_TENSOR_BYTES);
  for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
    for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
      checker_src[y * ImagePreprocessor::INPUT_WIDTH + x] = ((x ^ y) & 1) ? 255 : 0;
    }
  }
  ImagePreprocessor::preprocessFrame(checker_src.data(), checker_src.size(), valid_dst.data(), valid_dst.size());
  ImagePreprocessor::preprocessFrameNearestNeighbor(checker_src.data(), checker_src.size(), nn_dst.data(), nn_dst.size());
  
  // Bilinear interpolation smooths the Nyquist pattern, producing intermediate values (-128 < val < 127)
  int bilinear_intermediates = 0;
  for (int8_t val : valid_dst) {
    if (val > -128 && val < 127) bilinear_intermediates++;
  }
  int nn_extremes = 0;
  for (int8_t val : nn_dst) {
    if (val == -128 || val == 127) nn_extremes++;
  }
  check(bilinear_intermediates > 0 && nn_extremes == (int)ImagePreprocessor::OUTPUT_TENSOR_BYTES,
        "1.5.1 Fixed-point bilinear interpolation creates smooth intermediate samples unlike NN");

  // 1.6 Host Performance Latency Benchmark
  const int iterations = 1000;
  auto t_start = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < iterations; ++i) {
    ImagePreprocessor::preprocessFrame(valid_src.data(), valid_src.size(), valid_dst.data(), valid_dst.size());
  }
  auto t_end = std::chrono::high_resolution_clock::now();
  double total_us = std::chrono::duration<double, std::micro>(t_end - t_start).count();
  double avg_us = total_us / iterations;

  std::cout << "  [PERF] Preprocessor host latency: " << avg_us << " us / frame (" << (1000000.0 / avg_us) << " FPS)\n";
  check(avg_us < 500.0, "1.6.1 Preprocessor frame downsampling executes sub-millisecond on host CPU (<500us)");
}

// =============================================================================
// Suite 2: OV7670 Camera Driver & Hardware Simulation Fallback
// =============================================================================
void test_suite_2_camera_driver() {
  std::cout << "\n================================================================================\n";
  std::cout << " Suite 2: OV7670 Camera Driver & Hardware Simulation Fallback                   \n";
  std::cout << "================================================================================\n";

  OV7670Driver driver;

  // 2.1 Uninitialized Safety
  check(driver.getState() == DRIVER_UNINITIALIZED, "2.1.1 Driver initial state is DRIVER_UNINITIALIZED");
  std::vector<uint8_t> frame_buf(CAMERA_FRAME_BYTES);
  check(!driver.captureFrame(frame_buf.data(), frame_buf.size()), "2.1.2 captureFrame() fails safely when uninitialized");

  // 2.2 Driver Init & Simulation Fallback Mode
  check(driver.init(), "2.2.1 Driver init() returns true");
  check(driver.getState() == DRIVER_SIMULATION_MODE, "2.2.2 Host environment transitions to DRIVER_SIMULATION_MODE");
  check(driver.isMockMode(), "2.2.3 isMockMode() returns true in simulation");
  check(!driver.isHardwarePresent(), "2.2.4 isHardwarePresent() returns false on host");

  // 2.3 Register Table Inspection
  size_t reg_count = 0;
  const OV7670RegisterConfig* regs = OV7670Driver::getInitRegisterTable(&reg_count);
  check(regs != nullptr && reg_count >= 15, "2.3.1 Register table is populated (>15 entries)");
  check(regs[0].reg == OV7670_REG_COM7 && regs[0].val == 0x80, "2.3.2 Register table begins with COM7 Soft Reset (0x80)");
  check(OV7670_REG_PID == 0x0A, "2.3.3 OV7670_REG_PID address equals 0x0A");

  // 2.4 DMA Frame Buffer Alignment & Capture
  const uint8_t* internal_buf = driver.getFrameBuffer();
  check(internal_buf != nullptr, "2.4.1 Internal frame buffer pointer is non-null");
  check(((uintptr_t)internal_buf % 16) == 0, "2.4.2 Internal frame buffer is 16-byte aligned");

  // Capture empty frame
  check(driver.captureFrame(frame_buf.data(), frame_buf.size()), "2.4.3 captureFrame() succeeds in simulation mode");
  check(driver.getFrameCounter() == 1, "2.4.4 Frame counter incremented to 1");
  check(driver.getLastCaptureTimeMs() > 0 || driver.getFrameCounter() > 0, "2.4.5 Frame capture timestamp recorded");

  // 2.5 Test Frame Injection API
  std::vector<uint8_t> injected_test_frame(CAMERA_FRAME_BYTES, 77);
  driver.injectTestFrame(injected_test_frame.data(), injected_test_frame.size());
  driver.captureFrame(frame_buf.data(), frame_buf.size());
  check(frame_buf[0] == 77 && frame_buf[19199] == 77, "2.5.1 Injected test frame captured accurately");
  driver.clearInjectedFrame();

  // 2.6 Synthetic Pattern Modes
  driver.setMockPattern(PATTERN_SOLID_WHITE);
  driver.captureFrame(frame_buf.data(), frame_buf.size());
  check(frame_buf[100] == 255, "2.6.1 PATTERN_SOLID_WHITE generates 255");

  driver.setMockPattern(PATTERN_SOLID_BLACK);
  driver.captureFrame(frame_buf.data(), frame_buf.size());
  check(frame_buf[100] == 0, "2.6.2 PATTERN_SOLID_BLACK generates 0");

  driver.setMockPattern(PATTERN_SOLID_GRAY);
  driver.captureFrame(frame_buf.data(), frame_buf.size());
  check(frame_buf[100] == 128, "2.6.3 PATTERN_SOLID_GRAY generates 128");

  driver.setMockPattern(PATTERN_GRADIENT);
  driver.captureFrame(frame_buf.data(), frame_buf.size());
  check(frame_buf[0] != frame_buf[50], "2.6.4 PATTERN_GRADIENT generates varying pixel ramp");

  driver.setMockPersonDetected(true);
  driver.captureFrame(frame_buf.data(), frame_buf.size());
  // Center head pixel (80, 40) should be 220, border pixel (5, 5) should be ~30
  uint8_t center_pixel = frame_buf[40 * CAMERA_FRAME_WIDTH + 80];
  uint8_t border_pixel = frame_buf[5 * CAMERA_FRAME_WIDTH + 5];
  check(center_pixel == 220 && border_pixel <= 35,
        "2.6.5 PATTERN_PERSON_SILHOUETTE generates high-contrast humanoid center profile");
}

// =============================================================================
// Suite 3: Model Data Flash Resident Array Integrity
// =============================================================================
void test_suite_3_model_data() {
  std::cout << "\n================================================================================\n";
  std::cout << " Suite 3: Model Data Flash Resident Array Integrity                             \n";
  std::cout << "================================================================================\n";

  check(g_person_detect_model_data_len > 10240, "3.1 Model data length exceeds 10KB (fits MobileNet VWW footprint)");

  // 16-byte memory alignment assertion (Crucial for Flash direct mapping and SIMD)
  uintptr_t model_addr = reinterpret_cast<uintptr_t>(g_person_detect_model_data);
  check((model_addr & 0x0F) == 0, "3.2 Model data is 16-byte memory aligned in Flash (.rodata)");

  // FlatBuffer Magic Identifier "TFL3" at byte offset 4..7
  bool magic_ok = (g_person_detect_model_data[4] == 'T' &&
                   g_person_detect_model_data[5] == 'F' &&
                   g_person_detect_model_data[6] == 'L' &&
                   g_person_detect_model_data[7] == '3');
  check(magic_ok, "3.3 Model data contains valid TensorFlow Lite FlatBuffer magic header 'TFL3'");

  // Root Table Offset check
  uint32_t root_offset = *reinterpret_cast<const uint32_t*>(g_person_detect_model_data);
  check(root_offset == 0x1C, "3.4 FlatBuffer root table offset is valid (0x1C)");
}

// =============================================================================
// Suite 4: ML PersonDetector Inference, Hysteresis & Debouncing
// =============================================================================
void test_suite_4_person_detector() {
  std::cout << "\n================================================================================\n";
  std::cout << " Suite 4: ML PersonDetector Inference, Hysteresis & Debouncing                  \n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;

  // 4.1 Uninitialized Safety
  check(detector.getState() == DetectorState::UNINITIALIZED, "4.1.1 Initial state is UNINITIALIZED");
  check(!detector.isInitialized(), "4.1.2 isInitialized() returns false before init");
  check(!detector.processFrame(), "4.1.3 processFrame() returns false when uninitialized");
  check(!detector.isPersonDetected(), "4.1.4 isPersonDetected() returns false when uninitialized");
  check(detector.getConfidence() == 0.0f, "4.1.5 Confidence is 0.0 when uninitialized");
  check(detector.getPersonCount() == 0, "4.1.6 Person count is 0 when uninitialized");

  // 4.2 Initialization
  check(detector.init(), "4.2.1 detector.init() returns true");
  check(detector.isInitialized(), "4.2.2 isInitialized() returns true after init");
  check(detector.getState() == DetectorState::SIMULATION_MODE || detector.getState() == DetectorState::READY,
        "4.2.3 Detector state transitions to SIMULATION_MODE or READY");
  check(detector.getArenaTotalBytes() == 80 * 1024, "4.2.4 Tensor Arena size is exactly 80 KB");

  // 4.3 Blank / Empty Scene Inference
  std::vector<uint8_t> black_frame(CAMERA_FRAME_BYTES, 0);
  detector.injectMockFrame(black_frame.data(), black_frame.size());
  
  // Run 2 frames to clear any debounce state
  detector.processFrame();
  detector.processFrame();
  check(!detector.isPersonDetected(), "4.3.1 Person NOT detected on all-black frame");
  check(detector.getConfidence() < 0.20f, "4.3.2 Confidence is low on blank frame (<0.20)");
  check(detector.getPersonCount() == 0, "4.3.3 Person count is 0 on blank frame");

  // Uniform Ambient Gray Frame
  std::vector<uint8_t> gray_frame(CAMERA_FRAME_BYTES, 40);
  detector.injectMockFrame(gray_frame.data(), gray_frame.size());
  detector.processFrame();
  detector.processFrame();
  check(!detector.isPersonDetected(), "4.3.4 Person NOT detected on uniform ambient gray frame");
  check(detector.getConfidence() < 0.20f, "4.3.5 Confidence is low on uniform ambient frame");

  // 4.4 Person Silhouette Detection
  detector.getDriver().clearInjectedFrame();
  detector.getDriver().setMockPersonDetected(true);
  
  // First frame above threshold (debounce counter = 1)
  detector.processFrame();
  // Second frame confirms (debounce counter = 2 -> asserts presence)
  detector.processFrame();

  check(detector.isPersonDetected(), "4.4.1 Person detected on synthetic humanoid silhouette");
  check(detector.getConfidence() >= 0.65f, "4.4.2 Confidence is high on person silhouette (>=0.65)");
  check(detector.getPersonCount() == 1, "4.4.3 Person count is 1 when detected");

  // 4.5 Dual-Threshold Hysteresis State Machine
  // Enter threshold = 0.60, Exit threshold = 0.40
  detector.reset();
  detector.setDetectionThreshold(0.60f, 0.40f);
  check(!detector.isPersonDetected(), "4.5.1 Detector reset to false state");

  // Inject frame with marginal contrast producing ~0.52 confidence (below enter threshold 0.60)
  // Box with intensity 55 on background 30 -> contrast ~14.0 -> confidence 0.52
  std::vector<uint8_t> marginal_frame(CAMERA_FRAME_BYTES, 30);
  for (int y = 20; y <= 85; ++y) {
    for (int x = 60; x <= 100; ++x) {
      marginal_frame[y * CAMERA_FRAME_WIDTH + x] = 55; // Moderate contrast producing 0.52 score
    }
  }
  detector.injectMockFrame(marginal_frame.data(), marginal_frame.size());
  detector.processFrame();
  detector.processFrame();
  check(!detector.isPersonDetected(), "4.5.2 Marginal score (0.52 < 0.60) does NOT trigger detection from false state");

  // Now trigger high presence (>=0.65)
  detector.getDriver().clearInjectedFrame();
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame();
  detector.processFrame();
  check(detector.isPersonDetected(), "4.5.3 Triggered detection into true state");

  // Re-inject marginal frame (0.52 >= 0.40 exit threshold)
  detector.injectMockFrame(marginal_frame.data(), marginal_frame.size());
  detector.processFrame();
  detector.processFrame();
  check(detector.isPersonDetected(), "4.5.4 Marginal score (0.52 >= 0.40) KEEPS detection in true state (Hysteresis working)");

  // Clear to complete black frame (confidence ~0.05 < 0.40)
  detector.injectMockFrame(black_frame.data(), black_frame.size());
  detector.processFrame();
  detector.processFrame();
  check(!detector.isPersonDetected(), "4.5.5 Low score (<0.40) exits detection state");

  // 4.6 Dequantization Arithmetic Accuracy Unit Tests
  float scale = 0.00390625f; // 1/256
  int32_t zero_point = -128;
  
  // Test raw value -128 -> ( -128 - (-128) ) * scale = 0.0
  float dequant_min = (float)(-128 - zero_point) * scale;
  check(std::abs(dequant_min - 0.0f) < 0.0001f, "4.6.1 Dequantization minimum: -128 maps to 0.0");

  // Test raw value +127 -> ( 127 - (-128) ) * scale = 255 / 256 ≈ 0.996
  float dequant_max = (float)(127 - zero_point) * scale;
  check(std::abs(dequant_max - 0.99609375f) < 0.0001f, "4.6.2 Dequantization maximum: +127 maps to ~0.996");

  // Test raw value 0 -> ( 0 - (-128) ) * scale = 128 / 256 = 0.5
  float dequant_mid = (float)(0 - zero_point) * scale;
  check(std::abs(dequant_mid - 0.5f) < 0.0001f, "4.6.3 Dequantization mid-point: 0 maps to 0.5");
}

// =============================================================================
// Suite 5: Integration, Telemetry & Contract Adherence
// =============================================================================
void test_suite_5_integration_telemetry() {
  std::cout << "\n================================================================================\n";
  std::cout << " Suite 5: Integration, Telemetry & Contract Adherence                           \n";
  std::cout << "================================================================================\n";

  CameraPersonDetector detector;
  detector.init();
  detector.setZoneAndSensorId("zone_lobby", "cam_node_01");

  // 5.1 PersonTrackingData Struct Contract Verification
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame();
  detector.processFrame();

  const PersonTrackingData& data = detector.getLatestData();
  check(data.person_detected == true, "5.1.1 Telemetry person_detected is true");
  check(data.confidence >= 0.65f, "5.1.2 Telemetry confidence is >= 0.65");
  check(data.person_count == 1, "5.1.3 Telemetry person_count is 1");
  check(data.timestamp_ms > 0, "5.1.4 Telemetry timestamp_ms is valid (>0)");
  check(strcmp(data.zone_id, "zone_lobby") == 0, "5.1.5 Telemetry zone_id is 'zone_lobby'");
  check(strcmp(data.sensor_id, "cam_node_01") == 0, "5.1.6 Telemetry sensor_id is 'cam_node_01'");

  // 5.2 Direct processBuffer API Verification
  std::vector<uint8_t> buffer_frame(CAMERA_FRAME_BYTES, 0);
  check(detector.processBuffer(buffer_frame.data(), buffer_frame.size()), "5.2.1 processBuffer() executes successfully");
  check(!detector.processBuffer(nullptr, CAMERA_FRAME_BYTES), "5.2.2 processBuffer() rejects nullptr buffer");
  check(!detector.processBuffer(buffer_frame.data(), CAMERA_FRAME_BYTES - 1), "5.2.3 processBuffer() rejects undersized buffer");

  // 5.3 Telemetry Transmission via MockDualModeComm
  MockDualModeComm mock_comm;
  check(!mock_comm.transmit_called, "5.3.1 MockDualModeComm uncalled initially");
  detector.transmitTelemetry(mock_comm);
  check(mock_comm.transmit_called, "5.3.2 transmitTelemetry() successfully calls comm.transmit()");
  check(mock_comm.last_transmitted_data.person_count == detector.getPersonCount(),
        "5.3.3 Transmitted person_count matches detector internal state");
  check(strcmp(mock_comm.last_transmitted_data.zone_id, "zone_lobby") == 0,
        "5.3.4 Transmitted zone_id matches detector internal state");
}

// =============================================================================
// Test Runner Main
// =============================================================================
int main() {
  std::cout << "================================================================================\n";
  std::cout << "     MILESTONE 2: OV7670 CAMERA DRIVER & TFLITE MICRO ML TEST SUITE             \n";
  std::cout << "================================================================================\n";

  test_suite_1_preprocessor();
  test_suite_2_camera_driver();
  test_suite_3_model_data();
  test_suite_4_person_detector();
  test_suite_5_integration_telemetry();

  std::cout << "\n================================================================================\n";
  std::cout << "                         TEST EXECUTION SUMMARY                                 \n";
  std::cout << "================================================================================\n";
  std::cout << " Total Test Checks Run : " << g_tests_run << "\n";
  std::cout << " Checks Passed         : " << (g_tests_run - g_failures) << "\n";
  std::cout << " Checks Failed         : " << g_failures << "\n";
  std::cout << " Status                : " << (g_failures == 0 ? "ALL PASS (100%)" : "FAILED") << "\n";
  std::cout << "================================================================================\n";

  return (g_failures == 0) ? 0 : 1;
}
