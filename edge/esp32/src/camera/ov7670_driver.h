// -----------------------------------------------------------------------------
// ov7670_driver.h — OV7670 Camera Driver with I2S DMA & Hardware Simulation Fallback
//
// Manages:
// - Hardware 20 MHz XCLK generation via ESP32 LEDC PWM
// - SCCB / I2C register configuration & product ID verification (REG_PID == 0x76)
// - I2S0 DMA capture and line-by-line Y-channel extraction to 19.2 KB grayscale buffer
// - Graceful hardware absence detection and synthetic frame generation / test injection
// -----------------------------------------------------------------------------
#pragma once

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include "camera_config.h"

// Driver Lifecycle States
enum CameraDriverState {
  DRIVER_UNINITIALIZED    = 0,
  DRIVER_HARDWARE_OK      = 1,
  DRIVER_SIMULATION_MODE  = 2,
  DRIVER_ERROR            = 3
};

// Synthetic Test Patterns
enum SyntheticPattern {
  PATTERN_EMPTY_SCENE       = 0,
  PATTERN_SOLID_BLACK       = 1,
  PATTERN_SOLID_WHITE       = 2,
  PATTERN_SOLID_GRAY        = 3,
  PATTERN_GRADIENT          = 4,
  PATTERN_PERSON_SILHOUETTE = 5,
  PATTERN_CHECKERBOARD      = 6
};

class OV7670Driver {
public:
  OV7670Driver();
  ~OV7670Driver();

  // Lifecycle
  bool init();
  bool captureFrame(uint8_t* out_buffer, size_t max_len);

  // Buffer Accessor
  const uint8_t* getFrameBuffer() const { return frame_buffer_; }

  // Status Queries
  bool isHardwarePresent() const { return hardware_present_; }
  bool isMockMode() const { return mock_mode_; }
  CameraDriverState getState() const { return state_; }
  uint32_t getFrameCounter() const { return frame_counter_; }
  uint32_t getLastCaptureTimeMs() const { return last_capture_time_ms_; }

  // Simulation & Test Injection Controls
  void setMockMode(bool enable);
  void setMockPattern(SyntheticPattern pattern);
  void setMockPersonDetected(bool detected);
  void injectTestFrame(const uint8_t* raw_frame, size_t len);
  void clearInjectedFrame();

  // Register configuration table accessor (for test inspection)
  static const OV7670RegisterConfig* getInitRegisterTable(size_t* count);

private:
  CameraDriverState state_;
  bool              hardware_present_;
  bool              mock_mode_;
  SyntheticPattern  mock_pattern_;
  bool              mock_person_active_;

  uint32_t          frame_counter_;
  uint32_t          last_capture_time_ms_;

  const uint8_t*    injected_frame_;
  size_t            injected_frame_len_;

  // Internal SRAM 19.2 KB Grayscale Frame Buffer (160 x 120)
  alignas(16) uint8_t frame_buffer_[CAMERA_FRAME_BYTES];

  // Helper generators
  void generateSyntheticFrame();
  void generatePersonSilhouette();
  void generateGradientFrame();
  void generateCheckerboardFrame();

#if defined(ESP32) && !defined(HOST_TEST)
  bool initXclk(uint8_t pin, uint32_t freq_hz = 20000000);
  bool writeRegister(uint8_t reg, uint8_t val);
  bool readRegister(uint8_t reg, uint8_t& val);
  bool applyRegisterTable();
  bool initI2sDma();
#endif
};
