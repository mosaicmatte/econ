// -----------------------------------------------------------------------------
// camera_config.h — OV7670 Camera Driver Pinout, Geometry & Register Constants
//
// Defines conflict-free pin mappings for ESP32-WROOM, QQVGA resolution constants,
// SCCB/I2C registers, and TFLite Micro tensor arena dimensions.
// -----------------------------------------------------------------------------
#pragma once

#ifndef USE_CAMERA
  #define USE_CAMERA 1
#endif

#include <stdint.h>
#include <stddef.h>

// =============================================================================
// 1. Resolution & Buffer Geometry Constants
// =============================================================================
#define CAMERA_FRAME_WIDTH         160
#define CAMERA_FRAME_HEIGHT        120
#define CAMERA_FRAME_BYTES         (CAMERA_FRAME_WIDTH * CAMERA_FRAME_HEIGHT) // 19,200 bytes (Grayscale)
#define CAMERA_YUV_BYTES_PER_ROW   (CAMERA_FRAME_WIDTH * 2)                  // 320 bytes (YUYV)
#define CAMERA_YUV_TOTAL_BYTES     (CAMERA_FRAME_WIDTH * CAMERA_FRAME_HEIGHT * 2) // 38,400 bytes

// Preprocessor Center-Crop & Downsample Geometry
#define PREPROC_CROP_WIDTH         120
#define PREPROC_CROP_HEIGHT        120
#define PREPROC_CROP_OFFSET_X      20                                        // (160 - 120) / 2
#define PREPROC_CROP_OFFSET_Y      0

// TFLite Micro Input Tensor Geometry
#define MODEL_INPUT_WIDTH          96
#define MODEL_INPUT_HEIGHT         96
#define MODEL_INPUT_CHANNELS       1                                         // Grayscale (Luminance Y)
#define MODEL_INPUT_BYTES          (MODEL_INPUT_WIDTH * MODEL_INPUT_HEIGHT * MODEL_INPUT_CHANNELS) // 9,216 bytes

// Static Tensor Arena Memory Budget (Internal SRAM)
#define TENSOR_ARENA_SIZE          (80 * 1024)                               // 80 KB (81,920 bytes)

// =============================================================================
// 2. Conflict-Free ESP32 WROOM Pinout Mappings
// =============================================================================
// Camera Timing and Clock Signals
#ifndef PIN_CAM_XCLK
  #define PIN_CAM_XCLK   27 // 20 MHz LEDC PWM clock output
#endif
#ifndef PIN_CAM_PCLK
  #define PIN_CAM_PCLK   14 // Pixel Clock input from camera
#endif
#ifndef PIN_CAM_VSYNC
  #define PIN_CAM_VSYNC  36 // Vertical Sync input (SENSOR_VP, input-only)
#endif
#ifndef PIN_CAM_HREF
  #define PIN_CAM_HREF   39 // Horizontal Reference input (SENSOR_VN, input-only)
#endif

// Parallel 8-Bit Data Bus (D0..D7)
#ifndef PIN_CAM_D0
  #define PIN_CAM_D0     33 // Data bit 0 (LSB)
#endif
#ifndef PIN_CAM_D1
  #define PIN_CAM_D1     32 // Data bit 1
#endif
#ifndef PIN_CAM_D2
  #define PIN_CAM_D2     17 // Data bit 2
#endif
#ifndef PIN_CAM_D3
  #define PIN_CAM_D3     16 // Data bit 3
#endif
#ifndef PIN_CAM_D4
  #define PIN_CAM_D4     15 // Data bit 4
#endif
#ifndef PIN_CAM_D5
  #define PIN_CAM_D5     13 // Data bit 5
#endif
#ifndef PIN_CAM_D6
  #define PIN_CAM_D6     12 // Data bit 6
#endif
#ifndef PIN_CAM_D7
  #define PIN_CAM_D7     5  // Data bit 7 (MSB) — Repurposed from legacy PIR GPIO5
#endif

// SCCB / I2C Configuration Bus (Shared with SHT30 0x44, ACD1200 0x2A, BH1750 0x23)
#ifndef PIN_CAM_SIOD
  #define PIN_CAM_SIOD   21 // I2C SDA
#endif
#ifndef PIN_CAM_SIOC
  #define PIN_CAM_SIOC   22 // I2C SCL
#endif

// =============================================================================
// 3. OV7670 SCCB / I2C Addresses & Register Bitfields
// =============================================================================
#define OV7670_I2C_ADDR            0x21 // 7-bit slave address (0x42 write, 0x43 read)

// Register Addresses
#define OV7670_REG_GAIN            0x00 // AGC Gain control
#define OV7670_REG_BLUE            0x01 // AWB Blue channel gain
#define OV7670_REG_RED             0x02 // AWB Red channel gain
#define OV7670_REG_VREF            0x03 // Vertical frame reference
#define OV7670_REG_COM1            0x04 // Common Control 1
#define OV7670_REG_BAVE            0x05 // U/B Average
#define OV7670_REG_GbAVE           0x06 // Y/Gb Average
#define OV7670_REG_AECHH           0x07 // Exposure high bits
#define OV7670_REG_RAVE            0x08 // V/R Average
#define OV7670_REG_COM2            0x09 // Common Control 2
#define OV7670_REG_PID             0x0A // Product ID MSB (Expected: 0x76)
#define OV7670_REG_VER             0x0B // Product ID LSB (Expected: 0x70, 0x71, 0x73)
#define OV7670_REG_COM3            0x0C // Common Control 3 (Scaling enable)
#define OV7670_REG_COM4            0x0D // Common Control 4
#define OV7670_REG_COM5            0x0E // Common Control 5
#define OV7670_REG_COM6            0x0F // Common Control 6
#define OV7670_REG_AECH            0x10 // Exposure low 8 bits
#define OV7670_REG_CLKRC           0x11 // Internal Clock Prescaler
#define OV7670_REG_COM7            0x12 // Common Control 7 (Reset, format select)
#define OV7670_REG_COM8            0x13 // Common Control 8 (AEC, AGC, AWB enable)
#define OV7670_REG_COM9            0x14 // Common Control 9 (AGC ceiling)
#define OV7670_REG_COM10           0x15 // Common Control 10 (PCLK options)
#define OV7670_REG_HSTART          0x17 // Horizontal frame start
#define OV7670_REG_HSTOP           0x18 // Horizontal frame stop
#define OV7670_REG_VSTART          0x19 // Vertical frame start
#define OV7670_REG_VSTOP           0x1A // Vertical frame stop
#define OV7670_REG_PSHFT           0x1B // Pixel delay shift
#define OV7670_REG_MIDH            0x1C // Manufacturer ID MSB (0x7F)
#define OV7670_REG_MIDL            0x1D // Manufacturer ID LSB (0xA2)
#define OV7670_REG_MVFP            0x1E // Mirror / VFlip
#define OV7670_REG_AEW             0x24 // AGC/AEC Upper threshold
#define OV7670_REG_AEB             0x25 // AGC/AEC Lower threshold
#define OV7670_REG_VPT             0x26 // Fast AGC/AEC window
#define OV7670_REG_HREF            0x32 // HREF edge control
#define OV7670_REG_TSLB            0x3A // Line buffer option / YUYV sequence
#define OV7670_REG_COM14           0x3E // Common Control 14 (PCLK divider)
#define OV7670_REG_EDGE            0x3F // Edge enhancement
#define OV7670_REG_COM15           0x40 // Common Control 15 (Output range & format)
#define OV7670_REG_COM16           0x41 // Common Control 16
#define OV7670_REG_COM17           0x42 // Common Control 17
#define OV7670_REG_DBLV            0x6B // PLL / Clock Doubler
#define OV7670_REG_SCALING_XSC     0x70 // Horizontal scale factor
#define OV7670_REG_SCALING_YSC     0x71 // Vertical scale factor
#define OV7670_REG_SCALING_DCWCTR  0x72 // DCW Control (Downsample /4)
#define OV7670_REG_SCALING_PCLK_DIV 0x73 // PCLK divider for scaling

// Special tokens for initialization sequences
#define OV7670_REG_DELAY_TOKEN     0xFF

// Register Configuration Entry
struct OV7670RegisterConfig {
  uint8_t reg;
  uint8_t val;
};
