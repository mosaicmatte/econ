# Technical Investigation & Architectural Specification: OV7670 Camera Driver (Milestone 2)

**Author**: Explorer 1 (Milestone 2 — OV7670 Camera Driver)  
**Target Environment**: ESP32-WROOM-32 (Node MCU / DevKit v1), PlatformIO `esp32dev` environment  
**Output Resolution**: QQVGA (160x120 pixels), 8-bit Grayscale (Luminance Y channel, 19.2 KB frame buffer)  
**Framerate**: 15–30 FPS (configurable, nominal 15 FPS for balanced inference pipeline)  

---

## 1. Executive Summary

This document provides the complete architectural design and technical specification for implementing the **OV7670 Camera Driver** on the ESP32 WROOM for Milestone 2. 

The driver replaces the binary PIR motion sensor (`GPIO5`) with an OV7670 camera module operating in **QQVGA (160x120) 8-bit grayscale mode**, outputting a compact **19,200-byte (19.2 KB)** frame buffer. This frame buffer feeds directly into the 96x96 int8 image preprocessor and TensorFlow Lite Micro Person Detection pipeline.

### Core Architectural Decisions:
1. **SCCB / I2C Bus Sharing**: The OV7670 control interface (SCCB) connects to the ESP32's primary I2C bus (`GPIO21` SDA, `GPIO22` SCL) at 7-bit slave address `0x21` (`0x42 >> 1`). It safely coexists with the SHT30 (`0x44`), ACD1200 CO2 (`0x2A`), and BH1750 Lux (`0x23`) sensors.
2. **I2S0 DMA Parallel Byte Capture**: Uses ESP32 `I2S0` in Camera Slave Mode with chained line DMA descriptors (`lldesc_t`) and a 2x320-byte ping-pong buffer, consuming only **640 bytes** of DMA RAM plus **19.2 KB** of internal SRAM for the full grayscale frame.
3. **20 MHz XCLK Generation**: Generated via ESP32 High-Speed LEDC PWM on `GPIO27` (50% duty cycle, 1-bit timer at 80 MHz APB clock).
4. **100% Conflict-Free Pinout**: Mapped to avoid all existing node peripherals (relays on GPIO23/25, HVAC IR on GPIO19, mmWave on GPIO18, 1-Wire on GPIO26, clamps on GPIO34/35, status LED on GPIO2, UART0 on GPIO1/3). Reuses `GPIO5` (legacy PIR pin) as Camera `D7`.
5. **Hardware Absence Detection & Host Mock Fallback**: Automatic I2C probe and Product ID verification (`REG_PID == 0x76`). If the camera is disconnected, unpowered, or compiled in host test harnesses (`HOST_TEST`), the driver transitions into Simulation Mode, generating synthetic test frames for testability.

---

## 2. OV7670 Register Configuration & SCCB/I2C Protocol

### 2.1 SCCB / I2C Addressing & Identity Verification
- **7-bit I2C Address**: `0x21` (8-bit Write: `0x42`, 8-bit Read: `0x43`).
- **Identification Registers**:
  - `REG_PID` (`0x0A`): Product ID MSB = `0x76` (Expected).
  - `REG_VER` (`0x0B`): Product ID LSB = `0x70`, `0x71`, or `0x73`.
  - `REG_MIDH` (`0x1C`): Manufacturer ID High = `0x7F`.
  - `REG_MIDL` (`0x1D`): Manufacturer ID Low = `0xA2`.

### 2.2 Register Bitfield Reference for QQVGA (160x120) & Grayscale

| Register Name | Address | Recommended Value | Description & Functionality |
|---|---|---|---|
| `REG_COM7` | `0x12` | `0x00` (Init: `0x80`) | Common Control 7. Bit 7 = 1 triggers soft reset. Bit 2 = 0 selects YUV/Bayer format. Bit 5/4/3 = 0 selects VGA base before scaling. |
| `REG_CLKRC` | `0x11` | `0x03` (15fps) / `0x01` (30fps) | Internal Clock Prescaler. Bit 6 = 0 (use prescaler). Bits [5:0] = prescaler divisor $N$. $F_{int} = F_{XCLK} / (2 \cdot (N+1))$. With 20MHz XCLK, $N=3 \implies F_{int} = 2.5\text{ MHz}$ (~15 fps). |
| `REG_COM3` | `0x0C` | `0x0C` | Common Control 3. Bit 3 = 1 (Enable manual scaling); Bit 2 = 1 (Enable DCW downsampling). |
| `REG_COM14` | `0x3E` | `0x1A` | Common Control 14. Bit 4 = 1 (Manual DCW & PCLK control); Bits [2:0] = `010` (PCLK divider /4 for QQVGA). |
| `REG_SCALING_XSC` | `0x70` | `0x3A` | Scaling X-Axis. Test pattern disabled, horizontal scaling enabled. |
| `REG_SCALING_YSC` | `0x71` | `0x35` | Scaling Y-Axis. Vertical scaling enabled. |
| `REG_SCALING_DCWCTR` | `0x72` | `0x22` | DCW Control. Bits [7:6] = `10` (Vertical downsample by 4); Bits [5:4] = `10` (Horizontal downsample by 4): $640\times 480 \to 160\times 120$. |
| `REG_SCALING_PCLK_DIV` | `0x73` | `0x02` | Scaling PCLK Divider. Divide by 4 for QQVGA. |
| `REG_COM15` | `0x40` | `0xC0` | Common Control 15. Bits [7:6] = `11` (Full output range [0, 255]). Bits [5:4] = `00` (YUV mode). |
| `REG_COM8` | `0x13` | `0xE7` | Common Control 8. Bit 7 = 1 (Fast AGC/AEC); Bit 5 = 1 (Banding filter); Bit 2 = 1 (AGC enable); Bit 1 = 1 (AWB enable); Bit 0 = 1 (AEC enable). |
| `REG_COM10` | `0x15` | `0x20` | Common Control 10. Bit 5 = 1 (PCLK reverse); Bit 4 = 1 (Gated PCLK during blanking — prevents spurious clock edges). |
| `REG_MVFP` | `0x1E` | `0x00` / `0x30` | Mirror/VFlip. Bit 5 = Mirror; Bit 4 = VFlip. Set `0x30` if camera is mounted inverted. |
| `REG_TSLB` | `0x3A` | `0x04` | Line buffer test option. Bit 2 = 1 (Auto window). Bit 0 = 0 ($Y U Y V$ output order). |
| `REG_HSTART` | `0x17` | `0x16` | Horizontal frame start high 8 bits. |
| `REG_HSTOP` | `0x18` | `0x04` | Horizontal frame stop high 8 bits. |
| `REG_HREF` | `0x32` | `0x24` | Horizontal frame edge control. |
| `REG_VSTART` | `0x19` | `0x02` | Vertical frame start high 8 bits. |
| `REG_VSTOP` | `0x1A` | `0x7A` | Vertical frame stop high 8 bits. |
| `REG_VREF` | `0x03` | `0x0A` | Vertical frame reference / edge control. |

### 2.3 Luminance (Y-Only) Grayscale Extraction
In YUV 4:2:2 mode (`COM7 = 0x00`, `COM15 = 0xC0`, `TSLB = 0x04`), the OV7670 outputs pixel byte pairs:
$$\text{Byte Stream: } [Y_0, U_0, Y_1, V_0, Y_2, U_1, Y_3, V_1, \dots]$$
- For a row of 160 pixels, the camera outputs **320 bytes**.
- $Y$ represents **true optical Luminance** ($Y = 0.299R + 0.587G + 0.114B$).
- Grayscale extraction is zero-overhead: every even-indexed byte ($[0, 2, 4, 6, \dots]$) is copied directly to the frame buffer:
```cpp
// Fast uint8 Y-channel decimation from 320-byte YUV line to 160-byte Grayscale line
void extractGrayscaleLine(const uint8_t* __restrict yuv_line, uint8_t* __restrict gray_line, size_t width) {
    for (size_t i = 0; i < width; ++i) {
        gray_line[i] = yuv_line[i << 1]; // Take Y0, Y1, Y2...
    }
}
```

### 2.4 Complete OV7670 Initialization Register Array
```cpp
struct RegisterVal {
    uint8_t reg;
    uint8_t val;
};

static const RegisterVal OV7670_QQVGA_YUV_INIT[] = {
    // 1. Soft Reset and Delay
    {0x12, 0x80}, // REG_COM7: Soft Reset
    {0xFF, 0x05}, // Delay 5ms (special token)

    // 2. Clock & Framerate Setup (20MHz XCLK -> 15fps)
    {0x11, 0x03}, // REG_CLKRC: Prescaler /4 -> 2.5 MHz internal clock
    {0x6B, 0x4A}, // REG_DBLV: PLL disabled / bypass

    // 3. Output Format: YUV 4:2:2 Full Range
    {0x12, 0x00}, // REG_COM7: YUV format, VGA base
    {0x40, 0xC0}, // REG_COM15: YUV full range [0, 255]
    {0x3A, 0x04}, // REG_TSLB: YUYV sequence, auto window enable

    // 4. QQVGA Windowing & DCW Downsampling (640x480 -> 160x120)
    {0x0C, 0x0C}, // REG_COM3: Scale enable + DCW enable
    {0x3E, 0x1A}, // REG_COM14: DCW manual + PCLK divider /4
    {0x70, 0x3A}, // REG_SCALING_XSC: Horizontal scale
    {0x71, 0x35}, // REG_SCALING_YSC: Vertical scale
    {0x72, 0x22}, // REG_SCALING_DCWCTR: Downsample /4 horizontal & vertical
    {0x73, 0x02}, // REG_SCALING_PCLK_DIV: Clock divider /4

    // 5. Hardware Timing & Edge Control
    {0x15, 0x20}, // REG_COM10: PCLK reverse, gated PCLK
    {0x17, 0x16}, // REG_HSTART: Horizontal start
    {0x18, 0x04}, // REG_HSTOP: Horizontal stop
    {0x32, 0x24}, // REG_HREF: HREF edge control
    {0x19, 0x02}, // REG_VSTART: Vertical start
    {0x1A, 0x7A}, // REG_VSTOP: Vertical stop
    {0x03, 0x0A}, // REG_VREF: VREF edge control

    // 6. Automatic Image Quality Controls (AEC, AGC, AWB)
    {0x13, 0xE7}, // REG_COM8: Fast AEC/AGC, Banding filter, AWB enable
    {0x00, 0x00}, // REG_GAIN: AGC gain base
    {0x10, 0x00}, // REG_AECH: Exposure base
    {0x14, 0x18}, // REG_COM9: 4x AGC ceiling
    {0x24, 0x95}, // REG_AEW: AGC upper threshold
    {0x25, 0x33}, // REG_AEB: AGC lower threshold
    {0x26, 0xE3}, // REG_VPT: Fast AGC control

    // End token
    {0xFF, 0xFF}
};
```

---

## 3. ESP32 I2S DMA Parallel Byte Capture Architecture

```
+--------------------------------------------------------------------------------+
|                             OV7670 Camera Sensor                               |
|   D0-D7 (8-bit) ----------> ESP32 GPIO Matrix (GPIO 33, 32, 17, 16, 15, 13, 12, 5)|
|   PCLK         -----------> ESP32 I2S0I_WS_in (GPIO 14)                        |
|   HREF         -----------> ESP32 I2S0I_H_ENABLE_in (GPIO 39)                  |
|   VSYNC        -----------> ESP32 I2S0I_V_SYNC_in / GPIO Interrupt (GPIO 36)   |
|   XCLK (20MHz) <----------- ESP32 LEDC PWM Timer (GPIO 27)                     |
+---------------------------------------+----------------------------------------+
                                        |
                                        v
+--------------------------------------------------------------------------------+
|                     ESP32 I2S0 Peripheral (Camera Slave Mode)                  |
|   * I2S0.conf2.camera_en = 1                                                   |
|   * I2S0.conf2.lcd_en = 1                                                      |
|   * I2S0.conf.rx_slave_mod = 1 (PCLK drives sampling)                         |
|   * 8-bit Parallel Capture into 16-bit DMA FIFO                                |
+---------------------------------------+----------------------------------------+
                                        |
                                        v
+--------------------------------------------------------------------------------+
|                    Chained Ping-Pong DMA Buffers (lldesc_t)                     |
|   Descriptor A: Line Buffer A (320 bytes DMA RAM) ----+                        |
|   Descriptor B: Line Buffer B (320 bytes DMA RAM) <---+ (Circular Ping-Pong)   |
+---------------------------------------+----------------------------------------+
                                        |
                                        | (DMA Line Interrupt -> Decimate Y byte)
                                        v
+--------------------------------------------------------------------------------+
|               Internal SRAM Grayscale Frame Buffer (19,200 bytes)               |
|   160 columns x 120 rows (19.2 KB uint8_t [0..255])                           |
+--------------------------------------------------------------------------------+
```

### 3.1 20 MHz XCLK Generation via LEDC
```cpp
#include <driver/ledc.h>

bool initCameraClock(gpio_num_t xclk_pin, uint32_t freq_hz = 20000000) {
    ledc_timer_config_t timer_conf = {};
    timer_conf.speed_mode      = LEDC_HIGH_SPEED_MODE;
    timer_conf.duty_resolution = LEDC_TIMER_1_BIT; // 1-bit resolution (0 or 1)
    timer_conf.timer_num       = LEDC_TIMER_0;
    timer_conf.freq_hz         = freq_hz;          // 20 MHz from 80 MHz APB
    timer_conf.clk_cfg         = LEDC_AUTO_CLK;
    if (ledc_timer_config(&timer_conf) != ESP_OK) return false;

    ledc_channel_config_t ch_conf = {};
    ch_conf.gpio_num   = xclk_pin;
    ch_conf.speed_mode = LEDC_HIGH_SPEED_MODE;
    ch_conf.channel    = LEDC_CHANNEL_0;
    ch_conf.intr_type  = LEDC_INTR_DISABLE;
    ch_conf.timer_sel  = LEDC_TIMER_0;
    ch_conf.duty       = 1; // 50% duty cycle with 1-bit resolution
    ch_conf.hpoint     = 0;
    return (ledc_channel_config(&ch_conf) == ESP_OK);
}
```

### 3.2 I2S DMA Configuration & Memory Budget
- **DMA Buffer Allocation**:
  - Buffer A: 320 bytes (`MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL`)
  - Buffer B: 320 bytes (`MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL`)
  - 2 x `lldesc_t` descriptors: 32 bytes.
  - Total DMA memory footprint: **672 bytes**.
- **Frame Buffer Allocation**:
  - 160 x 120 x 1 byte = **19,200 bytes** (allocated in regular internal DRAM).
  - SRAM budget impact: **~20 KB total**, preserving all remaining RAM for TFLite Micro (~80 KB arena), Wi-Fi stack (~50 KB), and system heap (>100 KB free).

---

## 4. Conflict-Free Pinout Mapping for ESP32 WROOM

### 4.1 Master Node Pin Allocation Matrix

| Pin | GPIO Function / Mode | Existing Node Peripheral | Camera Signal | Conflict Assessment & Resolution |
|---|---|---|---|---|
| **GPIO 0** | RTC / Boot Strapping | Bootloader Mode (pull-up) | *None* | **Preserved**. Must not be loaded at boot. |
| **GPIO 1** | UART0 TX | Serial Fallback / Console | *None* | **Preserved**. Dedicated to USB Serial Fallback. |
| **GPIO 2** | RTC / Boot Strapping | `STATUS_LED` (MQTT link) | *None* | **Preserved**. Status LED indicator. |
| **GPIO 3** | UART0 RX | Serial Fallback / Console | *None* | **Preserved**. Dedicated to USB Serial Fallback. |
| **GPIO 4** | General IO | `USE_DHT` (Fallback DHT22) | *None* | **Preserved**. Fallback temp/humidity bus. |
| **GPIO 5** | General IO | `USE_PIR` (Legacy PIR Sensor)| **`D7` (MSB)** | **REPURPOSED**. Replaces legacy PIR sensor. |
| **GPIO 12**| Boot Strapping (MTDI)| *None* (Must be LOW at boot) | **`D6`** | **Safe**. Camera D6 is high-Z/low during boot. |
| **GPIO 13**| General IO | *None* | **`D5`** | **Assigned**. Free general IO. |
| **GPIO 14**| General IO | *None* | **`PCLK`** | **Assigned**. Camera Pixel Clock input. |
| **GPIO 15**| Boot Strapping (MTDO)| *None* | **`D4`** | **Safe**. Camera D4 input during frame capture. |
| **GPIO 16**| UART2 RX / IO | *None* | **`D3`** | **Assigned**. Free general IO. |
| **GPIO 17**| UART2 TX / IO | *None* | **`D2`** | **Assigned**. Free general IO. |
| **GPIO 18**| General IO | `USE_MMWAVE` (Radar OUT) | *None* | **Preserved**. Stationary radar sensor. |
| **GPIO 19**| General IO | `IR_PIN` (HVAC IR Emitter) | *None* | **Preserved**. Dedicated to AC IR control. |
| **GPIO 21**| General IO / I2C | `I2C_SDA` (SHT30, ACD1200) | **`SIOD`** | **SHARED**. Standard I2C SDA bus (addr 0x21). |
| **GPIO 22**| General IO / I2C | `I2C_SCL` (SHT30, ACD1200) | **`SIOC`** | **SHARED**. Standard I2C SCL bus (addr 0x21). |
| **GPIO 23**| General IO | `RELAY_PIN` (Lighting Relay) | *None* | **Preserved**. Lighting relay control. |
| **GPIO 25**| General IO | `PLUG_RELAY_PIN` (Plug Relay)| *None* | **Preserved**. Sockets relay control. |
| **GPIO 26**| General IO | `SUPPLY_TEMP_PIN` (1-Wire) | *None* | **Preserved**. DS18B20 supply temp probe. |
| **GPIO 27**| General IO | *None* | **`XCLK`** | **Assigned**. 20 MHz LEDC PWM clock output. |
| **GPIO 32**| RTC / Touch 9 | `USE_TOUCH_PRESENCE` | **`D1`** | **Assigned**. Touch presence replaced/remapped. |
| **GPIO 33**| General IO | *None* | **`D0` (LSB)** | **Assigned**. Free general IO. |
| **GPIO 34**| Input-Only (ADC1_6)| `PLUG_ADC_PIN` (SCT-013) | *None* | **Preserved**. Plug current clamp ADC. |
| **GPIO 35**| Input-Only (ADC1_7)| `AC_CLAMP_PIN` (SCT-013) | *None* | **Preserved**. AC compressor clamp ADC. |
| **GPIO 36**| Input-Only (SENSOR_VP)| *None* | **`VSYNC`** | **Assigned**. Input-only pin perfect for VSYNC. |
| **GPIO 39**| Input-Only (SENSOR_VN)| *None* | **`HREF`** | **Assigned**. Input-only pin perfect for HREF. |

### 4.2 Header Pin Definitions (`camera_config.h`)
```cpp
#pragma once
#include <stdint.h>

// Camera Resolution Constants
#define CAMERA_FRAME_WIDTH   160
#define CAMERA_FRAME_HEIGHT  120
#define CAMERA_FRAME_BYTES   (CAMERA_FRAME_WIDTH * CAMERA_FRAME_HEIGHT) // 19,200 bytes

// Pin Assignments
#ifndef PIN_CAM_XCLK
  #define PIN_CAM_XCLK  27
#endif
#ifndef PIN_CAM_PCLK
  #define PIN_CAM_PCLK  14
#endif
#ifndef PIN_CAM_VSYNC
  #define PIN_CAM_VSYNC 36
#endif
#ifndef PIN_CAM_HREF
  #define PIN_CAM_HREF  39
#endif

// Parallel 8-bit Data Bus
#ifndef PIN_CAM_D0
  #define PIN_CAM_D0    33
#endif
#ifndef PIN_CAM_D1
  #define PIN_CAM_D1    32
#endif
#ifndef PIN_CAM_D2
  #define PIN_CAM_D2    17
#endif
#ifndef PIN_CAM_D3
  #define PIN_CAM_D3    16
#endif
#ifndef PIN_CAM_D4
  #define PIN_CAM_D4    15
#endif
#ifndef PIN_CAM_D5
  #define PIN_CAM_D5    13
#endif
#ifndef PIN_CAM_D6
  #define PIN_CAM_D6    12
#endif
#ifndef PIN_CAM_D7
  #define PIN_CAM_D7    5
#endif

// SCCB / I2C Bus Pins (shared with SHT30 / ACD1200 / BH1750)
#ifndef PIN_CAM_SIOD
  #define PIN_CAM_SIOD  21
#endif
#ifndef PIN_CAM_SIOC
  #define PIN_CAM_SIOC  22
#endif

// Camera I2C Address
#define OV7670_I2C_ADDR 0x21
```

---

## 5. Graceful Hardware Failure Handling & Mock Simulation Mode

### 5.1 Failure Modes & Detection Matrix

| Failure Mode | Detection Mechanism | Driver Action | Telemetry / Log Message |
|---|---|---|---|
| **Camera Unwired / Missing** | I2C transaction to `0x21` returns `NACK` (error code 2/3) | Switch to Mock Simulation Mode | `[camera] OV7670 not detected on I2C (0x21). Fallback to Simulation Mode.` |
| **Sensor ID Mismatch / Corrupt** | Read `REG_PID` (0x0A) != `0x76` | Switch to Mock Simulation Mode | `[camera] OV7670 PID mismatch (expected 0x76, got 0x%02X). Fallback to Simulation Mode.` |
| **XCLK Clock Generation Failure** | LEDC timer initialization error | Switch to Mock Simulation Mode | `[camera] LEDC XCLK initialization failed. Fallback to Simulation Mode.` |
| **I2S DMA Frame Timeout** | DMA semaphore timeout (>100 ms) | Soft reset I2S DMA, inject synthetic frame | `[camera] DMA capture timeout. Soft recovery triggered.` |
| **Host Test / Native Build** | `#ifdef HOST_TEST` or `!defined(ESP32)` | Pure software Mock Mode | `[camera] Running in Host Test Harness mode.` |

### 5.2 Mock Frame Generator & Synthetic Person Injection
To support off-target host testing, CI verification, and graceful runtime degradation without panics:
1. **Gradient Ramp Mode**: Generates deterministic pixel ramps $(x + y) \pmod{256}$ to verify downsampling math and tensor alignment.
2. **Synthetic Person Mode**: Generates an ambient background ($I \approx 45$) with an elliptical/box high-contrast torso contour ($I \approx 180$) in the center $96\times 96$ region.
3. **Empty Scene Mode**: Generates uniform low ambient background with mild pseudo-random Gaussian noise.
4. **Manual Frame Injection API**: Allows test cases in `test_m2_camera_ml.cpp` to feed raw test arrays directly into the driver.

```cpp
class OV7670Driver {
public:
    bool init();
    bool captureFrame(uint8_t* out_buffer, size_t max_len);
    bool isHardwarePresent() const { return m_hardware_present; }
    bool isMockMode() const { return m_mock_mode; }
    
    // Test Injection APIs (Available on ESP32 & Host)
    void setMockPersonDetected(bool detected);
    void injectTestFrame(const uint8_t* raw_frame, size_t len);

private:
    bool m_hardware_present = false;
    bool m_mock_mode = true;
    bool m_mock_person_active = false;
    const uint8_t* m_injected_frame = nullptr;
    uint32_t m_frame_counter = 0;
};
```

---

## 6. Verification & Implementation Guidance for Worker

When the Worker implements the driver files (`edge/esp32/src/camera/camera_config.h`, `ov7670_driver.h`, `ov7670_driver.cpp`), they must ensure:

1. **Strict Guarding with `#ifdef ESP32`**: All hardware-specific ESP32 includes (`esp_camera.h`, `driver/i2s.h`, `driver/ledc.h`, `rom/lldesc.h`, `soc/i2s_struct.h`) must be wrapped so that `c++ -std=c++17` host tests compile cleanly on desktop.
2. **Zero Dynamic Allocation in Frame Loop**: The 19.2 KB frame buffer must be statically allocated or allocated once during `init()`. Never call `malloc()` inside `captureFrame()`.
3. **Thread Safety**: Frame capture must be protected or decoupled from MQTT/Wi-Fi tasks to prevent bus contention.
4. **Clean Integration with `CameraPersonDetector`**: Provide a clean `getLatestGrayscaleFrame()` pointer accessor so the 96x96 downsampler can read directly without redundant `memcpy()` operations.
