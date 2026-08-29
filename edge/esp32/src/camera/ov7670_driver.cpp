// -----------------------------------------------------------------------------
// ov7670_driver.cpp — OV7670 Camera Driver Implementation
// -----------------------------------------------------------------------------
#include "ov7670_driver.h"
#include <cstring>
#include <cstdlib>

#if defined(ESP32) && !defined(HOST_TEST)
#include <Arduino.h>
#include <Wire.h>
#include <driver/ledc.h>
#include <driver/i2s.h>
#include <driver/gpio.h>
#include <esp_err.h>
#else
#include <chrono>
static inline uint32_t getHostMillis() {
  static auto start = std::chrono::steady_clock::now() - std::chrono::milliseconds(1000);
  auto now = std::chrono::steady_clock::now();
  return (uint32_t)std::chrono::duration_cast<std::chrono::milliseconds>(now - start).count();
}
#endif

// Complete OV7670 QQVGA Grayscale / YUV Initialization Register Table
static const OV7670RegisterConfig kOV7670_QQVGA_InitRegs[] = {
  // 1. Soft Reset
  {OV7670_REG_COM7, 0x80},
  {OV7670_REG_DELAY_TOKEN, 5}, // 5 ms stabilization delay

  // 2. Clock setup (20 MHz XCLK -> ~15 fps)
  {OV7670_REG_CLKRC, 0x03}, // Prescaler /4 -> 2.5 MHz internal clock
  {OV7670_REG_DBLV, 0x4A},  // PLL bypass / disabled

  // 3. Output Format: YUV 4:2:2 Full Range
  {OV7670_REG_COM7, 0x00},  // YUV format, VGA base
  {OV7670_REG_COM15, 0xC0}, // YUV full range [0, 255]
  {OV7670_REG_TSLB, 0x04},  // YUYV sequence, auto window enable

  // 4. QQVGA Windowing & DCW Downsampling (640x480 -> 160x120)
  {OV7670_REG_COM3, 0x0C},  // Scale enable + DCW enable
  {OV7670_REG_COM14, 0x1A}, // DCW manual + PCLK divider /4
  {OV7670_REG_SCALING_XSC, 0x3A}, // Horizontal scale
  {OV7670_REG_SCALING_YSC, 0x35}, // Vertical scale
  {OV7670_REG_SCALING_DCWCTR, 0x22}, // Downsample /4 horizontal & vertical
  {OV7670_REG_SCALING_PCLK_DIV, 0x02}, // Clock divider /4

  // 5. Hardware Timing & Edge Control
  {OV7670_REG_COM10, 0x20}, // PCLK reverse, gated PCLK
  {OV7670_REG_HSTART, 0x16},
  {OV7670_REG_HSTOP, 0x04},
  {OV7670_REG_HREF, 0x24},
  {OV7670_REG_VSTART, 0x02},
  {OV7670_REG_VSTOP, 0x7A},
  {OV7670_REG_VREF, 0x0A},

  // 6. Automatic Image Quality Controls (AEC, AGC, AWB)
  {OV7670_REG_COM8, 0xE7}, // Fast AEC/AGC, Banding filter, AWB enable
  {OV7670_REG_GAIN, 0x00}, // AGC gain base
  {OV7670_REG_AECH, 0x00}, // Exposure base
  {OV7670_REG_COM9, 0x18}, // 4x AGC ceiling
  {OV7670_REG_AEW, 0x95},  // AGC upper threshold
  {OV7670_REG_AEB, 0x33},  // AGC lower threshold
  {OV7670_REG_VPT, 0xE3},  // Fast AGC control

  // End Token
  {OV7670_REG_DELAY_TOKEN, OV7670_REG_DELAY_TOKEN}
};

const OV7670RegisterConfig* OV7670Driver::getInitRegisterTable(size_t* count) {
  if (count) {
    *count = sizeof(kOV7670_QQVGA_InitRegs) / sizeof(kOV7670_QQVGA_InitRegs[0]);
  }
  return kOV7670_QQVGA_InitRegs;
}

OV7670Driver::OV7670Driver()
    : state_(DRIVER_UNINITIALIZED),
      hardware_present_(false),
      mock_mode_(true),
      mock_pattern_(PATTERN_EMPTY_SCENE),
      mock_person_active_(false),
      frame_counter_(0),
      last_capture_time_ms_(0),
      injected_frame_(nullptr),
      injected_frame_len_(0) {
  memset(frame_buffer_, 0, sizeof(frame_buffer_));
}

OV7670Driver::~OV7670Driver() {}

bool OV7670Driver::init() {
#if defined(ESP32) && !defined(HOST_TEST)
  // 1. Initialize 20 MHz XCLK on PIN_CAM_XCLK
  if (!initXclk(PIN_CAM_XCLK, 20000000)) {
    Serial.println("[camera] LEDC XCLK initialization failed -> Fallback to Simulation Mode");
    state_ = DRIVER_SIMULATION_MODE;
    hardware_present_ = false;
    mock_mode_ = true;
    return true;
  }
  delay(10); // Wait 10ms for oscillator stabilization

  // 2. Initialize SCCB / I2C Bus
  Wire.begin(PIN_CAM_SIOD, PIN_CAM_SIOC, 100000);

  // 3. Probe Camera Product ID (REG_PID == 0x76)
  uint8_t pid = 0;
  if (!readRegister(OV7670_REG_PID, pid) || pid != 0x76) {
    Serial.printf("[camera] OV7670 hardware not detected on I2C (got PID 0x%02X) -> Fallback to Simulation Mode\n", pid);
    state_ = DRIVER_SIMULATION_MODE;
    hardware_present_ = false;
    mock_mode_ = true;
    return true;
  }

  // 4. Hardware Detected — Apply Register Configuration
  if (!applyRegisterTable()) {
    Serial.println("[camera] Failed to configure OV7670 registers -> Fallback to Simulation Mode");
    state_ = DRIVER_SIMULATION_MODE;
    hardware_present_ = false;
    mock_mode_ = true;
    return true;
  }

  // 5. Initialize I2S DMA
  if (!initI2sDma()) {
    Serial.println("[camera] I2S DMA initialization failed -> Fallback to Simulation Mode");
    state_ = DRIVER_SIMULATION_MODE;
    hardware_present_ = false;
    mock_mode_ = true;
    return true;
  }

  Serial.println("[camera] OV7670 Hardware Initialized Successfully (QQVGA Grayscale 160x120)");
  state_ = DRIVER_HARDWARE_OK;
  hardware_present_ = true;
  mock_mode_ = false;
  return true;

#else
  // Host test / Simulation mode
  state_ = DRIVER_SIMULATION_MODE;
  hardware_present_ = false;
  mock_mode_ = true;
  return true;
#endif
}

#if defined(ESP32) && !defined(HOST_TEST)
bool OV7670Driver::initXclk(uint8_t pin, uint32_t freq_hz) {
  ledc_timer_config_t timer_conf = {};
  timer_conf.speed_mode      = LEDC_HIGH_SPEED_MODE;
  timer_conf.duty_resolution = LEDC_TIMER_1_BIT;
  timer_conf.timer_num       = LEDC_TIMER_0;
  timer_conf.freq_hz         = freq_hz;
  timer_conf.clk_cfg         = LEDC_AUTO_CLK;
  if (ledc_timer_config(&timer_conf) != ESP_OK) return false;

  ledc_channel_config_t ch_conf = {};
  ch_conf.gpio_num   = (gpio_num_t)pin;
  ch_conf.speed_mode = LEDC_HIGH_SPEED_MODE;
  ch_conf.channel    = LEDC_CHANNEL_0;
  ch_conf.intr_type  = LEDC_INTR_DISABLE;
  ch_conf.timer_sel  = LEDC_TIMER_0;
  ch_conf.duty       = 1; // 50% duty cycle
  ch_conf.hpoint     = 0;
  return (ledc_channel_config(&ch_conf) == ESP_OK);
}

bool OV7670Driver::writeRegister(uint8_t reg, uint8_t val) {
  Wire.beginTransmission(OV7670_I2C_ADDR);
  Wire.write(reg);
  Wire.write(val);
  return (Wire.endTransmission() == 0);
}

bool OV7670Driver::readRegister(uint8_t reg, uint8_t& val) {
  Wire.beginTransmission(OV7670_I2C_ADDR);
  Wire.write(reg);
  if (Wire.endTransmission() != 0) return false;

  if (Wire.requestFrom((uint8_t)OV7670_I2C_ADDR, (uint8_t)1) != 1) return false;
  val = Wire.read();
  return true;
}

bool OV7670Driver::applyRegisterTable() {
  size_t count = 0;
  const OV7670RegisterConfig* regs = getInitRegisterTable(&count);

  for (size_t i = 0; i < count; ++i) {
    if (regs[i].reg == OV7670_REG_DELAY_TOKEN) {
      if (regs[i].val == OV7670_REG_DELAY_TOKEN) break; // End of table
      delay(regs[i].val);
    } else {
      if (!writeRegister(regs[i].reg, regs[i].val)) {
        return false;
      }
    }
  }
  return true;
}

bool OV7670Driver::initI2sDma() {
  // Configures I2S0 in parallel camera slave mode
  i2s_config_t i2s_config = {};
  i2s_config.mode = (i2s_mode_t)(I2S_MODE_MASTER | I2S_MODE_RX);
  i2s_config.sample_rate = 10000000;
  i2s_config.bits_per_sample = I2S_BITS_PER_SAMPLE_8BIT;
  i2s_config.channel_format = I2S_CHANNEL_FMT_ONLY_RIGHT;
  i2s_config.communication_format = I2S_COMM_FORMAT_STAND_I2S;
  i2s_config.intr_alloc_flags = ESP_INTR_FLAG_LEVEL1;
  i2s_config.dma_buf_count = 2;
  i2s_config.dma_buf_len = CAMERA_YUV_BYTES_PER_ROW;
  i2s_config.use_apll = false;

  if (i2s_driver_install(I2S_NUM_0, &i2s_config, 0, NULL) != ESP_OK) {
    return false;
  }
  return true;
}
#endif

bool OV7670Driver::captureFrame(uint8_t* out_buffer, size_t max_len) {
  if (state_ == DRIVER_UNINITIALIZED) {
    return false;
  }

#if defined(ESP32) && !defined(HOST_TEST)
  last_capture_time_ms_ = millis();
#else
  last_capture_time_ms_ = getHostMillis();
#endif

  // 1. Check for manual test frame injection
  if (injected_frame_ != nullptr && injected_frame_len_ >= CAMERA_FRAME_BYTES) {
    memcpy(frame_buffer_, injected_frame_, CAMERA_FRAME_BYTES);
  }
  // 2. Check for simulation mode
  else if (mock_mode_ || state_ == DRIVER_SIMULATION_MODE) {
    generateSyntheticFrame();
  }
  // 3. Hardware Capture
  else {
#if defined(ESP32) && !defined(HOST_TEST)
    // Capture 120 rows from I2S DMA, extracting the Y-channel (even bytes)
    uint8_t yuv_line[CAMERA_YUV_BYTES_PER_ROW];
    size_t bytes_read = 0;

    for (int row = 0; row < CAMERA_FRAME_HEIGHT; ++row) {
      esp_err_t err = i2s_read(I2S_NUM_0, yuv_line, sizeof(yuv_line), &bytes_read, pdMS_TO_TICKS(100));
      if (err != ESP_OK || bytes_read < sizeof(yuv_line)) {
        // Recovery: generate synthetic frame on DMA timeout
        generateSyntheticFrame();
        break;
      }
      uint8_t* dst_row = &frame_buffer_[row * CAMERA_FRAME_WIDTH];
      for (int col = 0; col < CAMERA_FRAME_WIDTH; ++col) {
        dst_row[col] = yuv_line[col << 1]; // Take Y0, Y1, Y2...
      }
    }
#endif
  }

  frame_counter_++;

  // Copy to caller's buffer if supplied
  if (out_buffer != nullptr && max_len >= CAMERA_FRAME_BYTES) {
    memcpy(out_buffer, frame_buffer_, CAMERA_FRAME_BYTES);
  }

  return true;
}

void OV7670Driver::setMockMode(bool enable) {
  mock_mode_ = enable;
  if (enable && state_ == DRIVER_HARDWARE_OK) {
    state_ = DRIVER_SIMULATION_MODE;
  } else if (!enable && hardware_present_) {
    state_ = DRIVER_HARDWARE_OK;
  }
}

void OV7670Driver::setMockPattern(SyntheticPattern pattern) {
  mock_pattern_ = pattern;
  if (pattern == PATTERN_PERSON_SILHOUETTE) {
    mock_person_active_ = true;
  } else if (pattern == PATTERN_EMPTY_SCENE) {
    mock_person_active_ = false;
  }
}

void OV7670Driver::setMockPersonDetected(bool detected) {
  mock_person_active_ = detected;
  mock_pattern_ = detected ? PATTERN_PERSON_SILHOUETTE : PATTERN_EMPTY_SCENE;
}

void OV7670Driver::injectTestFrame(const uint8_t* raw_frame, size_t len) {
  injected_frame_ = raw_frame;
  injected_frame_len_ = len;
}

void OV7670Driver::clearInjectedFrame() {
  injected_frame_ = nullptr;
  injected_frame_len_ = 0;
}

void OV7670Driver::generateSyntheticFrame() {
  if (mock_person_active_ || mock_pattern_ == PATTERN_PERSON_SILHOUETTE) {
    generatePersonSilhouette();
    return;
  }

  switch (mock_pattern_) {
    case PATTERN_SOLID_BLACK:
      memset(frame_buffer_, 0, sizeof(frame_buffer_));
      break;
    case PATTERN_SOLID_WHITE:
      memset(frame_buffer_, 255, sizeof(frame_buffer_));
      break;
    case PATTERN_SOLID_GRAY:
      memset(frame_buffer_, 128, sizeof(frame_buffer_));
      break;
    case PATTERN_GRADIENT:
      generateGradientFrame();
      break;
    case PATTERN_CHECKERBOARD:
      generateCheckerboardFrame();
      break;
    case PATTERN_EMPTY_SCENE:
    default: {
      // Generate uniform ambient scene (intensity ~35 with minor noise)
      for (size_t i = 0; i < CAMERA_FRAME_BYTES; ++i) {
        frame_buffer_[i] = (uint8_t)(35 + (i % 5));
      }
      break;
    }
  }
}

void OV7670Driver::generatePersonSilhouette() {
  // 1. Ambient Background (intensity ~30)
  for (size_t i = 0; i < CAMERA_FRAME_BYTES; ++i) {
    frame_buffer_[i] = (uint8_t)(30 + (i % 4));
  }

  // 2. High-Contrast Center Humanoid Silhouette (Center of 160x120: X~80, Y~60)
  // Head Oval: Y in [30..50], X in [70..90] -> intensity 220
  for (int y = 30; y <= 50; ++y) {
    for (int x = 70; x <= 90; ++x) {
      int dx = x - 80;
      int dy = y - 40;
      if (dx * dx + dy * dy <= 100) {
        frame_buffer_[y * CAMERA_FRAME_WIDTH + x] = 220;
      }
    }
  }

  // Torso Rectangle: Y in [51..90], X in [65..95] -> intensity 200
  for (int y = 51; y <= 90; ++y) {
    for (int x = 65; x <= 95; ++x) {
      frame_buffer_[y * CAMERA_FRAME_WIDTH + x] = 200;
    }
  }

  // Legs: Y in [91..115], X in [68..76] and [84..92] -> intensity 180
  for (int y = 91; y <= 115; ++y) {
    for (int x = 68; x <= 76; ++x) {
      frame_buffer_[y * CAMERA_FRAME_WIDTH + x] = 180;
    }
    for (int x = 84; x <= 92; ++x) {
      frame_buffer_[y * CAMERA_FRAME_WIDTH + x] = 180;
    }
  }
}

void OV7670Driver::generateGradientFrame() {
  for (int y = 0; y < CAMERA_FRAME_HEIGHT; ++y) {
    for (int x = 0; x < CAMERA_FRAME_WIDTH; ++x) {
      frame_buffer_[y * CAMERA_FRAME_WIDTH + x] = (uint8_t)((x + y + frame_counter_) % 256);
    }
  }
}

void OV7670Driver::generateCheckerboardFrame() {
  for (int y = 0; y < CAMERA_FRAME_HEIGHT; ++y) {
    for (int x = 0; x < CAMERA_FRAME_WIDTH; ++x) {
      frame_buffer_[y * CAMERA_FRAME_WIDTH + x] = ((x ^ y) & 1) ? 255 : 0;
    }
  }
}
