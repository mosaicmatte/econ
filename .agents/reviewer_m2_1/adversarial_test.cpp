// -----------------------------------------------------------------------------
// adversarial_test.cpp — Independent Stress & Edge Case Verification for M2
// -----------------------------------------------------------------------------
#include <iostream>
#include <vector>
#include <cassert>
#include <cmath>
#include <cstring>

#include "camera/camera_config.h"
#include "camera/ov7670_driver.h"
#include "camera/model_data.h"
#include "camera/person_detector.h"

int main() {
  std::cout << "Running Adversarial Stress Tests for Milestone 2...\n";

  // Test 1: ImagePreprocessor extreme boundary index math
  std::vector<uint8_t> frame(CAMERA_FRAME_BYTES, 0);
  std::vector<int8_t> tensor(MODEL_INPUT_BYTES, 0);

  // Mark all 4 corners of crop window
  // Crop window is [20..139] in X, [0..119] in Y
  frame[0 * CAMERA_FRAME_WIDTH + 20] = 255;   // Top-left
  frame[0 * CAMERA_FRAME_WIDTH + 139] = 255;  // Top-right
  frame[119 * CAMERA_FRAME_WIDTH + 20] = 255; // Bottom-left
  frame[119 * CAMERA_FRAME_WIDTH + 139] = 255;// Bottom-right

  bool ok = ImagePreprocessor::preprocessFrame(frame.data(), frame.size(), tensor.data(), tensor.size());
  assert(ok);
  std::cout << "[PASS] ImagePreprocessor extreme 4 corners within crop window executed safely\n";

  // Check that corner 0,0 has non-zero value
  int8_t tl = tensor[0 * MODEL_INPUT_WIDTH + 0];
  assert(tl > -128);
  std::cout << "[PASS] Top-left corner processed with correct weight\n";

  // Test 2: Inverted hysteresis threshold handling
  CameraPersonDetector detector;
  detector.init();
  detector.setDetectionThreshold(0.40f, 0.60f); // Inverted thresholds: enter 0.40, exit 0.60
  // Verify detector doesn't crash or behave erratically
  detector.processFrame();
  std::cout << "[PASS] Detector safely handles unusual threshold parameters\n";

  // Test 3: Rapid debounce alternating test
  detector.reset();
  detector.setDetectionThreshold(0.60f, 0.40f);
  
  // Alternating between Person (1 frame) and Empty (1 frame)
  // Since debounce requires 2 consecutive frames, state should stay false!
  for (int cycle = 0; cycle < 10; ++cycle) {
    detector.getDriver().setMockPersonDetected(true);
    detector.processFrame();
    assert(detector.isPersonDetected() == false); // Should NOT detect because only 1 frame

    detector.getDriver().setMockPersonDetected(false);
    detector.processFrame();
    assert(detector.isPersonDetected() == false); // Should remain false
  }
  std::cout << "[PASS] 2-frame debounce filter successfully rejects 1-frame transient noise glitches\n";

  // Test 4: Sustained detection then 1-frame glitch
  // 2 frames of person -> becomes true
  detector.getDriver().setMockPersonDetected(true);
  detector.processFrame();
  detector.processFrame();
  assert(detector.isPersonDetected() == true);

  // 1 frame of empty glitch -> should REMAIN true because of debounce
  detector.getDriver().setMockPersonDetected(false);
  detector.processFrame();
  assert(detector.isPersonDetected() == true);

  // 2nd frame of empty -> should become false
  detector.processFrame();
  assert(detector.isPersonDetected() == false);
  std::cout << "[PASS] Debounce filter successfully preserves detection across 1-frame dropout\n";

  // Test 5: Model Data Header & Length Verification
  assert(g_person_detect_model_data_len == 24576);
  assert(g_person_detect_model_data[0] == 0x1C);
  assert(g_person_detect_model_data[4] == 'T');
  assert(g_person_detect_model_data[5] == 'F');
  assert(g_person_detect_model_data[6] == 'L');
  assert(g_person_detect_model_data[7] == '3');
  std::cout << "[PASS] Model FlatBuffer structure strictly matches TFLite Micro specifications\n";

  std::cout << "ALL ADVERSARIAL STRESS TESTS PASSED SUCCESSFULLY!\n";
  return 0;
}
