// -----------------------------------------------------------------------------
// person_detector.h — TFLite Micro Person Detection Engine & Frame Preprocessor
//
// Strictly conforms to PROJECT.md interface contracts:
// - Fast integer fixed-point bilinear downsampling (160x120 -> 96x96 int8)
// - Quantized int8 inference engine with ~80KB SRAM static tensor arena
// - Dual-threshold hysteresis (0.60 / 0.40) and temporal debounce filter
// - Full simulation & host test execution support
// -----------------------------------------------------------------------------
#pragma once

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>
#include "camera_config.h"
#include "ov7670_driver.h"
#include "model_data.h"

// Include tracking_payload.h if present, or define PersonTrackingData
#if __has_include("tracking_payload.h")
#include "tracking_payload.h"
#elif __has_include("camera/tracking_payload.h")
#include "camera/tracking_payload.h"
#else
#ifndef PERSON_TRACKING_DATA_DEFINED
#define PERSON_TRACKING_DATA_DEFINED
struct PersonTrackingData {
  bool          person_detected; // Binary presence flag
  float         confidence;      // 0.0 to 1.0
  int           person_count;    // Estimated headcount (>= 0)
  uint64_t      timestamp_ms;    // Monotonic timestamp in ms
  const char*   zone_id;         // Zone identifier (e.g. "zone_1")
  const char*   sensor_id;       // Sensor identifier (e.g. "esp32_cam_01")
};
#endif
#endif

// Include dual_mode_comm.h if present, or forward declare DualModeComm
#if __has_include("dual_mode_comm.h")
#include "dual_mode_comm.h"
#define DUAL_MODE_COMM_DEFINED
#elif __has_include("camera/dual_mode_comm.h")
#include "camera/dual_mode_comm.h"
#define DUAL_MODE_COMM_DEFINED
#else
#ifndef DUAL_MODE_COMM_DEFINED
#define DUAL_MODE_COMM_DEFINED
class DualModeComm;
#endif
#endif

// Detection Lifecycle States
enum class DetectorState {
  UNINITIALIZED,
  READY,
  SIMULATION_MODE,
  ERROR_HARDWARE,
  ERROR_MODEL
};

// =============================================================================
// Image Preprocessor Namespace
// =============================================================================
namespace ImagePreprocessor {

constexpr int INPUT_WIDTH          = CAMERA_FRAME_WIDTH;         // 160
constexpr int INPUT_HEIGHT         = CAMERA_FRAME_HEIGHT;        // 120
constexpr int CROP_WIDTH           = PREPROC_CROP_WIDTH;         // 120
constexpr int CROP_HEIGHT          = PREPROC_CROP_HEIGHT;        // 120
constexpr int CROP_OFFSET_X        = PREPROC_CROP_OFFSET_X;      // 20
constexpr int CROP_OFFSET_Y        = PREPROC_CROP_OFFSET_Y;      // 0
constexpr int OUTPUT_WIDTH         = MODEL_INPUT_WIDTH;          // 96
constexpr int OUTPUT_HEIGHT        = MODEL_INPUT_HEIGHT;         // 96
constexpr size_t INPUT_FRAME_BYTES   = CAMERA_FRAME_BYTES;       // 19,200
constexpr size_t OUTPUT_TENSOR_BYTES = MODEL_INPUT_BYTES;        // 9,216

/**
 * @brief Downsamples and normalizes QQVGA grayscale frame to 96x96 int8 tensor
 *        using fast fixed-point bilinear interpolation (zero floating point).
 * @param src_frame Pointer to 160x120 uint8 grayscale DMA buffer.
 * @param src_len Length of input buffer in bytes (must be >= 19,200).
 * @param dst_tensor Pointer to 96x96 int8 output tensor buffer.
 * @param dst_len Length of output tensor buffer in bytes (must be >= 9,216).
 * @return true if successful, false on invalid parameters or buffer overflow.
 */
inline bool preprocessFrame(const uint8_t* src_frame, size_t src_len,
                            int8_t* dst_tensor, size_t dst_len) {
  if (!src_frame || !dst_tensor) return false;
  if (src_len < INPUT_FRAME_BYTES || dst_len < OUTPUT_TENSOR_BYTES) return false;

  for (int y = 0; y < OUTPUT_HEIGHT; ++y) {
    const int y_int = y + (y >> 2); // y * 5 / 4
    const int wy_1 = y & 3;
    const int wy_0 = 4 - wy_1;

    const uint8_t* row0 = &src_frame[y_int * INPUT_WIDTH + CROP_OFFSET_X];
    const uint8_t* row1 = &src_frame[(y_int + 1) * INPUT_WIDTH + CROP_OFFSET_X];
    int8_t* out_row = &dst_tensor[y * OUTPUT_WIDTH];

    for (int x = 0; x < OUTPUT_WIDTH; ++x) {
      const int x_int = x + (x >> 2); // x * 5 / 4
      const int wx_1 = x & 3;
      const int wx_0 = 4 - wx_1;

      const int w00 = wx_0 * wy_0;
      const int w10 = wx_1 * wy_0;
      const int w01 = wx_0 * wy_1;
      const int w11 = wx_1 * wy_1;

      const int p00 = row0[x_int];
      const int p10 = row0[x_int + 1];
      const int p01 = row1[x_int];
      const int p11 = row1[x_int + 1];

      // Sum of weights is always 16. Shift right by 4 with +8 rounding
      const int val = (w00 * p00 + w10 * p10 + w01 * p01 + w11 * p11 + 8) >> 4;
      out_row[x] = static_cast<int8_t>(val - 128); // Normalization to int8 [-128..127]
    }
  }
  return true;
}

/**
 * @brief Fast Nearest-Neighbor downsample (fallback/reference).
 */
inline bool preprocessFrameNearestNeighbor(const uint8_t* src_frame, size_t src_len,
                                           int8_t* dst_tensor, size_t dst_len) {
  if (!src_frame || !dst_tensor) return false;
  if (src_len < INPUT_FRAME_BYTES || dst_len < OUTPUT_TENSOR_BYTES) return false;

  for (int y = 0; y < OUTPUT_HEIGHT; ++y) {
    const int y_in = y + (y >> 2);
    const uint8_t* row = &src_frame[y_in * INPUT_WIDTH + CROP_OFFSET_X];
    int8_t* out_row = &dst_tensor[y * OUTPUT_WIDTH];

    for (int x = 0; x < OUTPUT_WIDTH; ++x) {
      const int x_in = x + (x >> 2);
      out_row[x] = static_cast<int8_t>(static_cast<int>(row[x_in]) - 128);
    }
  }
  return true;
}

} // namespace ImagePreprocessor

// =============================================================================
// CameraPersonDetector Class Definition
// =============================================================================
class CameraPersonDetector {
public:
  CameraPersonDetector();
  ~CameraPersonDetector();

  // Core Lifecycle (PROJECT.md Interface Contract)
  bool init();
  bool processFrame();
  bool processBuffer(const uint8_t* qqvga_src, size_t len);

  // Telemetry Getters (PROJECT.md Interface Contract)
  bool isPersonDetected() const { return latest_data_.person_detected; }
  float getConfidence() const { return latest_data_.confidence; }
  int getPersonCount() const { return latest_data_.person_count; }
  const PersonTrackingData& getLatestData() const { return latest_data_; }
  void transmitTelemetry(DualModeComm& comm);

  // Status & Diagnostics
  DetectorState getState() const { return state_; }
  bool isInitialized() const { return state_ != DetectorState::UNINITIALIZED; }
  uint32_t getLastInferenceTimeMs() const { return last_inference_time_ms_; }
  size_t getArenaUsedBytes() const { return arena_used_bytes_; }
  size_t getArenaTotalBytes() const { return TENSOR_ARENA_SIZE; }

  // Configuration & Thresholds
  void setDetectionThreshold(float enter_thresh, float exit_thresh = 0.40f);
  float getEnterThreshold() const { return enter_threshold_; }
  float getExitThreshold() const { return exit_threshold_; }
  void setZoneAndSensorId(const char* zone_id, const char* sensor_id);
  void reset();

  // Simulation & Mock Injection APIs
  void injectMockFrame(const uint8_t* frame_data, size_t len);
  OV7670Driver& getDriver() { return driver_; }
  const OV7670Driver& getDriver() const { return driver_; }

private:
  DetectorState      state_;
  float              enter_threshold_;
  float              exit_threshold_;
  int                debounce_frames_;
  int                consecutive_count_;
  bool               current_detection_state_;

  uint32_t           last_inference_time_ms_;
  size_t             arena_used_bytes_;

  PersonTrackingData latest_data_;
  OV7670Driver       driver_;

  // Preprocessed 96x96 int8 Tensor Buffer (9,216 bytes)
  int8_t             preprocessed_tensor_[MODEL_INPUT_BYTES];

  // Static Internal SRAM Tensor Arena (80 KB, alignas(16))
  alignas(16) uint8_t tensor_arena_[TENSOR_ARENA_SIZE];

  // Internal Inference Pipeline
  bool runInferenceInternal(const int8_t* input_tensor);
};
