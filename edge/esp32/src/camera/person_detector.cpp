// -----------------------------------------------------------------------------
// person_detector.cpp — CameraPersonDetector Implementation
// -----------------------------------------------------------------------------
#include "person_detector.h"
#if USE_CAMERA

#include <cstring>
#include <cmath>
#include <cstdlib>

#if defined(ESP32) && !defined(HOST_TEST)
#include <Arduino.h>
#include "tensorflow/lite/micro/micro_interpreter.h"
#include "tensorflow/lite/micro/micro_mutable_op_resolver.h"
#include "tensorflow/lite/schema/schema_generated.h"
#include "tensorflow/lite/version.h"
#else
#include <chrono>
static inline uint32_t getDetectorMillis() {
  static auto start = std::chrono::steady_clock::now() - std::chrono::milliseconds(1000);
  auto now = std::chrono::steady_clock::now();
  return (uint32_t)std::chrono::duration_cast<std::chrono::milliseconds>(now - start).count();
}
#endif

CameraPersonDetector::CameraPersonDetector()
    : state_(DetectorState::UNINITIALIZED),
      enter_threshold_(0.60f),
      exit_threshold_(0.40f),
      debounce_frames_(2),
      consecutive_count_(0),
      current_detection_state_(false),
      last_inference_time_ms_(0),
      arena_used_bytes_(0) {
  memset(&latest_data_, 0, sizeof(latest_data_));
  latest_data_.zone_id = "zone_1";
  latest_data_.sensor_id = "esp32_cam_01";
  latest_data_.person_detected = false;
  latest_data_.confidence = 0.0f;
  latest_data_.person_count = 0;
  latest_data_.timestamp_ms = 0;

  memset(preprocessed_tensor_, 0, sizeof(preprocessed_tensor_));
  memset(tensor_arena_, 0, sizeof(tensor_arena_));
}

CameraPersonDetector::~CameraPersonDetector() {}

bool CameraPersonDetector::init() {
  // 1. Validate Flash-resident Model FlatBuffer
  if (g_person_detect_model_data_len < 1024) {
    state_ = DetectorState::ERROR_MODEL;
    return false;
  }

  // Verify FlatBuffer Magic "TFL3" at byte offset 4..7
  if (g_person_detect_model_data[4] != 'T' ||
      g_person_detect_model_data[5] != 'F' ||
      g_person_detect_model_data[6] != 'L' ||
      g_person_detect_model_data[7] != '3') {
    state_ = DetectorState::ERROR_MODEL;
    return false;
  }

  // 2. Initialize Camera Driver
  if (!driver_.init()) {
    state_ = DetectorState::ERROR_HARDWARE;
    return false;
  }

#if defined(ESP32) && !defined(HOST_TEST)
  // 3. Load Model in Real TFLite Micro on ESP32
  const tflite::Model* model = tflite::GetModel(g_person_detect_model_data);
  if (model->version() != TFLITE_SCHEMA_VERSION) {
    state_ = DetectorState::ERROR_MODEL;
    return false;
  }

  // Register selective 8 operators
  static tflite::MicroMutableOpResolver<8> resolver;
  resolver.AddConv2D();
  resolver.AddDepthwiseConv2D();
  resolver.AddAveragePool2D();
  resolver.AddMaxPool2D();
  resolver.AddReshape();
  resolver.AddFullyConnected();
  resolver.AddSoftmax();
  resolver.AddAdd();

  // Instantiate MicroInterpreter with 80 KB Static Internal SRAM Arena
  static tflite::MicroInterpreter static_interpreter(
      model, resolver, tensor_arena_, TENSOR_ARENA_SIZE);

  if (static_interpreter.AllocateTensors() != kTfLiteOk) {
    state_ = DetectorState::ERROR_MODEL;
    return false;
  }

  arena_used_bytes_ = static_interpreter.arena_used_bytes();
#else
  // Host / CI environment
  arena_used_bytes_ = 48 * 1024; // Simulated ~48 KB used
#endif

  state_ = driver_.isHardwarePresent() ? DetectorState::READY : DetectorState::SIMULATION_MODE;
  return true;
}

bool CameraPersonDetector::processFrame() {
  if (state_ == DetectorState::UNINITIALIZED ||
      state_ == DetectorState::ERROR_HARDWARE ||
      state_ == DetectorState::ERROR_MODEL) {
    return false;
  }

  // 1. Capture Raw QQVGA Frame from Camera Driver
  const uint8_t* raw_frame = driver_.getFrameBuffer();
  if (!driver_.captureFrame(nullptr, 0)) {
    return false;
  }

  // 2. Preprocess & Downsample: 160x120 Grayscale -> 96x96 int8 Input Tensor
  return processBuffer(raw_frame, CAMERA_FRAME_BYTES);
}

bool CameraPersonDetector::processBuffer(const uint8_t* qqvga_src, size_t len) {
  if (state_ == DetectorState::UNINITIALIZED ||
      state_ == DetectorState::ERROR_HARDWARE ||
      state_ == DetectorState::ERROR_MODEL) {
    return false;
  }

  if (!qqvga_src || len < CAMERA_FRAME_BYTES) {
    return false;
  }

  // 1. Downsample and normalize (160x120 -> 96x96 int8)
  if (!ImagePreprocessor::preprocessFrame(qqvga_src, len,
                                          preprocessed_tensor_, sizeof(preprocessed_tensor_))) {
    return false;
  }

  // 2. Execute Inference Pipeline
  return runInferenceInternal(preprocessed_tensor_);
}

bool CameraPersonDetector::runInferenceInternal(const int8_t* input_tensor) {
#if defined(ESP32) && !defined(HOST_TEST)
  uint32_t t_start = millis();
#else
  uint32_t t_start = getDetectorMillis();
#endif

  float person_score = 0.0f;
  float not_person_score = 0.0f;

#if defined(ESP32) && !defined(HOST_TEST)
  // Copy input tensor to TFLM interpreter input
  const tflite::Model* model = tflite::GetModel(g_person_detect_model_data);
  static tflite::MicroMutableOpResolver<8> resolver;
  static tflite::MicroInterpreter interpreter(model, resolver, tensor_arena_, TENSOR_ARENA_SIZE);

  TfLiteTensor* input = interpreter.input(0);
  if (!input || input->bytes != MODEL_INPUT_BYTES) {
    return false;
  }
  memcpy(input->data.int8, input_tensor, MODEL_INPUT_BYTES);

  // Invoke Inference
  if (interpreter.Invoke() != kTfLiteOk) {
    return false;
  }

  // Extract Output & Dequantize
  TfLiteTensor* output = interpreter.output(0);
  if (!output || output->type != kTfLiteInt8) {
    return false;
  }

  const float scale = output->params.scale;
  const int32_t zero_point = output->params.zero_point;
  int8_t raw_not_person = output->data.int8[0];
  int8_t raw_person = output->data.int8[1];

  not_person_score = (float)(raw_not_person - zero_point) * scale;
  person_score = (float)(raw_person - zero_point) * scale;

#else
  // Deterministic Host Analysis of the 96x96 int8 Input Tensor
  int32_t center_sum = 0;
  int32_t bg_sum = 0;
  int center_count = 0;
  int bg_count = 0;

  for (int y = 0; y < MODEL_INPUT_HEIGHT; ++y) {
    for (int x = 0; x < MODEL_INPUT_WIDTH; ++x) {
      int8_t val = input_tensor[y * MODEL_INPUT_WIDTH + x];
      // Center bounding box: X in [24..71], Y in [16..79]
      if (x >= 24 && x <= 71 && y >= 16 && y <= 79) {
        center_sum += val;
        center_count++;
      } else {
        bg_sum += val;
        bg_count++;
      }
    }
  }

  float center_avg = (float)center_sum / center_count;
  float bg_avg = (float)bg_sum / bg_count;
  float contrast = center_avg - bg_avg;

  if (contrast > 30.0f) {
    person_score = 0.88f;
    not_person_score = 0.12f;
  } else if (contrast > 18.0f) {
    person_score = 0.65f;
    not_person_score = 0.35f;
  } else if (contrast > 10.0f) {
    person_score = 0.52f; // Edge of hysteresis band
    not_person_score = 0.48f;
  } else {
    person_score = 0.05f;
    not_person_score = 0.95f;
  }
#endif

  // Clamping probability outputs to [0.0, 1.0]
  if (person_score < 0.0f) person_score = 0.0f;
  if (person_score > 1.0f) person_score = 1.0f;
  if (not_person_score < 0.0f) not_person_score = 0.0f;
  if (not_person_score > 1.0f) not_person_score = 1.0f;

  // Normalized Confidence
  float sum = person_score + not_person_score;
  float confidence = (sum > 0.0001f) ? (person_score / sum) : person_score;

  // Dual-Threshold Hysteresis State Machine
  bool raw_detected = current_detection_state_ ? (confidence >= exit_threshold_)
                                               : (confidence >= enter_threshold_);

  // Temporal Debounce Filtering (2 consecutive agreeing frames)
  if (raw_detected != current_detection_state_) {
    consecutive_count_++;
    if (consecutive_count_ >= debounce_frames_) {
      current_detection_state_ = raw_detected;
      consecutive_count_ = 0;
    }
  } else {
    consecutive_count_ = 0;
  }

#if defined(ESP32) && !defined(HOST_TEST)
  last_inference_time_ms_ = millis() - t_start;
  latest_data_.timestamp_ms = millis();
#else
  last_inference_time_ms_ = getDetectorMillis() - t_start;
  latest_data_.timestamp_ms = getDetectorMillis();
#endif

  latest_data_.person_detected = current_detection_state_;
  latest_data_.confidence = confidence;
  latest_data_.person_count = current_detection_state_ ? 1 : 0;

  return true;
}

void CameraPersonDetector::transmitTelemetry(DualModeComm& comm) {
  comm.transmit(latest_data_);
}

void CameraPersonDetector::setDetectionThreshold(float enter_thresh, float exit_thresh) {
  enter_threshold_ = enter_thresh;
  exit_threshold_ = exit_thresh;
}

void CameraPersonDetector::setZoneAndSensorId(const char* zone_id, const char* sensor_id) {
  if (zone_id) latest_data_.zone_id = zone_id;
  if (sensor_id) latest_data_.sensor_id = sensor_id;
}

void CameraPersonDetector::reset() {
  consecutive_count_ = 0;
  current_detection_state_ = false;
  latest_data_.person_detected = false;
  latest_data_.confidence = 0.0f;
  latest_data_.person_count = 0;
}

void CameraPersonDetector::injectMockFrame(const uint8_t* frame_data, size_t len) {
  driver_.injectTestFrame(frame_data, len);
}
#endif
