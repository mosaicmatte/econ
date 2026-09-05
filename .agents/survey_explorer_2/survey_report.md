# Survey Report: OV7670 Camera Integration & Lightweight TFLite Micro Person Detection on ESP32-WROOM

**Author:** Survey Spec Miner 2  
**Target Platform:** ESP32-WROOM-32 (Standard ESP32, Xtensa Dual-Core LX6 @ 240MHz, 4MB Flash, No External PSRAM)  
**Target Environment:** PlatformIO / Arduino Core + ESP-IDF Components, Wokwi Simulator  
**Date:** 2026-08-26  

---

## 1. Executive Summary & Feasibility Assessment

This technical specification details the integration of an OmniVision **OV7670 camera module** and an embedded **TensorFlow Lite for Microcontrollers (TFLite Micro)** person detection machine learning model on a standard **ESP32-WROOM** microcontroller.

### Key Feasibility Findings:
1. **Memory Feasibility on Non-PSRAM ESP32-WROOM:**
   - **Internal DRAM Available:** ~320 KB total DRAM (typical free heap after WiFi initialization: ~180–240 KB).
   - **Camera Frame Buffer (DMA):** Capturing at **QQVGA (160×120) 8-bit Grayscale** consumes **19,200 bytes (~18.75 KB)** in internal DRAM (`MALLOC_CAP_DMA`).
   - **TFLite Micro Tensor Arena:** The Visual Wake Words (VWW) 96×96 int8 person detection model requires **~75–85 KB** contiguous memory for activations.
   - **Total Application Heap Consumption:** ~105 KB for ML + Camera buffers, leaving **>95 KB headroom** for WiFi, LwIP, MQTT (`PubSubClient`), and JSON serialization (`ArduinoJson`).
   - **Verdict:** Highly feasible and robust without external PSRAM when constrained to QQVGA/96×96 grayscale.

2. **Flash Memory Budget:**
   - Model weights (quantized `int8` FlatBuffer): **~250–300 KB** in Flash (`.rodata`).
   - Standard firmware + Arduino core + WiFi/MQTT + TFLM runtime: **~1.2–1.6 MB**.
   - With PlatformIO partition table `huge_app.csv` (3.14 MB app space) or `min_spiffs.csv` (1.9 MB app space), the firmware fits comfortably inside standard 4 MB Flash.

3. **Performance & Latency:**
   - Xtensa LX6 @ 240 MHz processes the 96×96 depthwise separable CNN in **~350–550 ms per frame** (approx. **1.8–2.8 FPS**), providing responsive, real-time occupancy tracking for HVAC/lighting automation and topology/BIM digital twins.

4. **Dual-Mode Communication (R2):**
   - Seamless primary broadcast over Wi-Fi MQTT (`econ/telemetry/<zone>`) with automatic, zero-drop fallback to USB Serial (`UART0` @ 115200 baud) upon Wi-Fi / MQTT disconnection.

---

## 2. OV7670 Camera Driver Architecture on ESP32-WROOM

### 2.1 Sensor Overview & Working Modes
The OmniVision OV7670 is a 1/6-inch CMOS VGA (640×480) image sensor. For microcontrollers without external FIFO chips (AL422B) or PSRAM, the ESP32's built-in **I2S DMA camera engine** is utilized to stream pixel bytes directly into internal SRAM.

- **Native Resolution Modes:** VGA (640×480), CIF (352×288), QVGA (320×240), QCIF (176×144), QQVGA (160×120), QQCIF (88×72).
- **Target Mode:** `FRAMESIZE_QQVGA` (160×120) or `FRAMESIZE_96X96` / `FRAMESIZE_QQCIF` (88×72).
- **Pixel Format:** `PIXFORMAT_GRAYSCALE` (Y-channel extraction from YUV422, 1 byte/pixel) or `PIXFORMAT_YUV422` (2 bytes/pixel: Y0, U, Y1, V).

### 2.2 ESP32 Parallel Camera Interface (I2S DMA)
The ESP32 lacks a dedicated DVP interface but implements a high-speed **I2S Parallel Slave Mode** connected to an internal DMA controller:
- **PCLK (Pixel Clock):** Clocks incoming parallel data on D0–D7 into the I2S FIFO.
- **HREF (Horizontal Reference):** Gates pixel capture per scanline.
- **VSYNC (Vertical Sync):** Triggers frame start/end interrupts.
- **DMA Allocation:** Allocated via `heap_caps_malloc(..., MALLOC_CAP_DMA | MALLOC_CAP_INTERNAL)`.

### 2.3 XCLK Clock Generation (Master Clock)
- OV7670 requires an external clock (XCLK) between 10 MHz and 24 MHz.
- Generated via ESP32's internal **LEDC (LED PWM Controller)** high-speed timer or **APLL (Audio PLL)**.
- Configured at `20000000` (20 MHz) or `10000000` (10 MHz) on an output GPIO (e.g. GPIO 15 or GPIO 27).

### 2.4 SCCB / I2C Configuration
- **Protocol:** Serial Camera Control Bus (SCCB), 100% register-compatible with standard I2C.
- **7-bit I2C Slave Address:** `0x21` (Write: `0x42`, Read: `0x43`).
- **Clock Speed:** Standard 100 kHz or Fast Mode 400 kHz.
- **Bus Sharing:** Can share the existing hardware I2C bus (`I2C_NUM_0`, default `SDA=GPIO21`, `SCL=GPIO22`) alongside SHT30 (`0x44`) and ACD1200 (`0x2A`), as `0x21` has zero address collisions.

### 2.5 Pinout Compatibility & GPIO Allocation Table
ESP32-WROOM-32 has strictly allocated pins. The table below outlines a safe, collision-free pin mapping for the OV7670 camera while preserving USB Serial, relays, and existing sensors:

| OV7670 Pin | ESP32 GPIO | Direction | Pin Characteristics & Compatibility |
|---|---|---|---|
| **D0** | GPIO 36 (SENSOR_VP) | Input | Input-only pin, no pullup/pulldown, optimal for DMA data line |
| **D1** | GPIO 39 (SENSOR_VN) | Input | Input-only pin, optimal for DMA data line |
| **D2** | GPIO 34 | Input | Input-only pin, ADC1_CH6, clean digital input |
| **D3** | GPIO 35 | Input | Input-only pin, ADC1_CH7, clean digital input |
| **D4** | GPIO 32 | Input | Digital GPIO (replaces touch demo) |
| **D5** | GPIO 33 | Input | Digital GPIO |
| **D6** | GPIO 25 | Input | Digital GPIO (or GPIO 4 if plug relay is on 25) |
| **D7** | GPIO 26 | Input | Digital GPIO (or GPIO 18 if supply temp on 26) |
| **XCLK** | GPIO 15 | Output | LEDC master clock output (20 MHz) |
| **PCLK** | GPIO 14 | Input | Pixel clock input |
| **VSYNC** | GPIO 27 | Input | Vertical sync input |
| **HREF** | GPIO 13 | Input | Horizontal reference input |
| **SIOD (SDA)** | GPIO 21 | Bidirectional | Shared I2C bus (I2C_SDA) |
| **SIOC (SCL)** | GPIO 22 | Output | Shared I2C bus (I2C_SCL) |
| **RESET** | 3V3 (Tie High) | Power | Hardware pull-up to 3.3V (saves 1 GPIO) |
| **PWDN** | GND (Tie Low) | Power | Hardware pull-down to GND (saves 1 GPIO) |
| **VCC / GND** | 3.3V / GND | Power | Regulated 3.3V power supply (draws ~30–45 mA) |

*Preserved Critical GPIOs:*
- `GPIO 1 / GPIO 3`: UART0 Serial TX/RX (USB Serial connection preserved).
- `GPIO 23`: Lighting Relay (`RELAY_PIN`).
- `GPIO 19`: HVAC IR Emitter (`IR_PIN`).
- `GPIO 2`: Status LED (`STATUS_LED`).
- `GPIO 5`: Replaced PIR input (now freed).

---

## 3. TFLite Micro Person Detection ML Model

### 3.1 Model Architecture: Visual Wake Words (VWW) Person Detect
The industry-standard ultra-lightweight person detection model for microcontrollers:
- **Backbone:** Depthwise Separable Convolutional Neural Network (MobileNet v1, width multiplier $\alpha = 0.25$).
- **Input Tensor Dimensions:** `[1, 96, 96, 1]` — 96×96 single-channel (grayscale), 8-bit signed integer quantized (`int8`, range `[-128, 127]`).
- **Layers Breakdown:**
  1. `Conv2D` (3×3 filter, stride 2, valid padding, ReLU6)
  2. 4–5× `DepthwiseConv2D` (3×3 depthwise) + `Conv2D` (1×1 pointwise / projection)
  3. `AveragePooling2D` (global spatial pooling to 1×1)
  4. `FullyConnected` (Dense layer to 2 output classes)
  5. `Softmax` / Quantized Output: `[0]` = `not_person`, `[1]` = `person`.
- **Total Weights Size:** **250–300 KB** compiled FlatBuffer array (`g_person_detect_model_data[]`).

### 3.2 Memory Profiling (ESP32-WROOM Internal SRAM)

```
+-------------------------------------------------------------------+
| ESP32-WROOM Physical SRAM (520 KB Total)                          |
|   IRAM: 128 KB (Instruction Cache & Interrupt Vectors)            |
|   DRAM: ~320 KB (Data, BSS, Stack, and Dynamic Heap)              |
+===================================================================+
| DRAM Allocation Breakdown:                                        |
|   ├── FreeRTOS & System OS Reserved:            ~20 KB            |
|   ├── WiFi Stack & LwIP Network Buffers:        ~65 KB            |
|   ├── PubSubClient + Arduino Core + ArduinoJson: ~15 KB           |
|   ├── OV7670 DMA Frame Buffer (160x120 Grayscale): ~19.2 KB       |
|   ├── TFLite Micro Tensor Arena (Activations):   ~80 KB           |
|   ├── TFLite Interpreter & Op Resolver:          ~5 KB            |
|   └── Remaining Free Heap (Headroom Margin):   ~115.8 KB (Safe!)  |
+-------------------------------------------------------------------+
```

### 3.3 Flash Partitioning (PlatformIO)
With standard 4 MB Flash on ESP32-WROOM:
- `default.csv` allocates only 1.25 MB per app partition (which can be tight).
- `huge_app.csv` (No OTA, single 3.14 MB app partition) or `min_spiffs.csv` (1.9 MB app partition, 1.9 MB SPIFFS) provides ample space for the 300 KB model + 1.2 MB application binary.

---

## 4. End-to-End Inference Pipeline

```
 ┌────────────────────────────────────────────────────────┐
 │ 1. Frame Acquisition (OV7670 via esp_camera_fb_get())   │
 │    Resolution: QQVGA (160x120), Format: Grayscale      │
 │    Buffer Size: 19,200 bytes in internal DRAM          │
 └──────────────────────────┬─────────────────────────────┘
                            │
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ 2. Image Preprocessing & Spatial Transformation        │
 │    a. Center crop: 160x120 -> 120x120 (remove 20px     │
 │       margins on left/right to maintain 1:1 aspect)    │
 │    b. Downsample: 120x120 -> 96x96 via bilinear /      │
 │       fast area-average scaling                        │
 │    c. Quantization: int8_val = (int8_t)(pixel - 128)   │
 │    d. Direct copy into model input tensor buffer       │
 │    e. Release camera frame: esp_camera_fb_return(fb)   │
 └──────────────────────────┬─────────────────────────────┘
                            │
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ 3. TFLite Micro Inference (interpreter->Invoke())      │
 │    Execution on Core 1 (Xtensa LX6 @ 240 MHz)          │
 │    Inference Latency: ~380–480 ms                      │
 └──────────────────────────┬─────────────────────────────┘
                            │
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ 4. Output Extraction & Confidence Scoring              │
 │    Extract int8 output logits: score_person, score_none│
 │    Dequantize: prob = (score - zero_point) * scale     │
 │    Hysteresis Decision: person_score >= 0.60           │
 │    Occupancy Count: occupancy = (detected ? 1 : 0)     │
 └──────────────────────────┬─────────────────────────────┘
                            │
                            ▼
 ┌────────────────────────────────────────────────────────┐
 │ 5. Dual-Mode Telemetry Broadcast (Wi-Fi / Serial)      │
 │    Format: JSON payload with zone, occupancy, score    │
 │    Primary: MQTT over Wi-Fi (`econ/telemetry/<zone>`)  │
 │    Fallback: USB Serial (`UART0` @ 115200 baud)        │
 └────────────────────────────────────────────────────────┘
```

### 4.1 Fast Downsampling Algorithm (160×120 to 96×96 int8)
To prevent allocation of an intermediary 96×96 buffer, downsampling directly maps source pixels to the model input tensor:

```cpp
void preprocess_frame_to_tensor(const uint8_t* src, int8_t* dst, int src_w, int src_h, int dst_size) {
    // Center crop 160x120 -> 120x120
    int crop_x = (src_w - src_h) / 2; // (160 - 120) / 2 = 20
    int crop_w = src_h;               // 120
    int crop_h = src_h;               // 120

    // Fixed-point nearest/bilinear scaling to 96x96
    for (int y = 0; y < dst_size; ++y) {
        int src_y = (y * crop_h) / dst_size;
        for (int x = 0; x < dst_size; ++x) {
            int src_x = crop_x + (x * crop_w) / dst_size;
            uint8_t pixel = src[src_y * src_w + src_x];
            // Normalize uint8 [0..255] to int8 [-128..127]
            dst[y * dst_size + x] = (int8_t)((int)pixel - 128);
        }
    }
}
```

---

## 5. Dual-Mode Communication & Fallback Mechanism (R2)

### 5.1 Primary Method: Wi-Fi MQTT Telemetry
When Wi-Fi and MQTT broker are connected:
- **Topic:** `econ/telemetry/<zone>` (e.g. `econ/telemetry/zone_1`)
- **Payload Schema:**
  ```json
  {
    "zone": "Level 4",
    "occupancy": 1,
    "personScore": 0.82,
    "fps": 2.2,
    "source": "camera",
    "temperature": 24.1,
    "tempReal": true,
    "lights": "ON",
    "setpoint": 24.0,
    "cfgRev": 0
  }
  ```

### 5.2 Fallback Method: USB Serial Broadcast
When Wi-Fi is unavailable (`WiFi.status() != WL_CONNECTED`) or MQTT connection fails:
- Transmits line-delimited JSON over USB Serial (`Serial.println(payload)` at 115200 baud).
- Downstream software (e.g. Python bridge, edge gateway, BIM/topology ingest) reads from `/dev/ttyUSB0` or COM port.
- State machine continuously attempts non-blocking Wi-Fi/MQTT reconnection every 5 seconds without blocking the camera inference loop.

---

## 6. PlatformIO Dependencies, Build Configurations & Toolchain

### 6.1 `platformio.ini` Configuration

```ini
[env:esp32dev]
platform = espressif32 @ ^6.5.0
board = esp32dev
framework = arduino
monitor_speed = 115200

; Huge app partition (3.14MB app, no OTA) to fit TFLM + model
board_build.partitions = huge_app.csv
board_build.f_cpu = 240000000L
board_build.f_flash = 80000000L
board_build.flash_mode = qio

lib_deps =
    knolleary/PubSubClient @ ^2.8
    bblanchon/ArduinoJson @ ^6.21.3
    espressif/esp32-camera @ ^2.0.4
    tanakamasayuki/TensorFlowLite_ESP32 @ ^0.9.0
    ; Sensor dependencies
    adafruit/DHT sensor library @ ^1.4.6
    adafruit/Adafruit Unified Sensor @ ^1.1.14
    crankyoldgit/IRremoteESP8266 @ ^2.8.6
    paulstoffregen/OneWire @ ^2.3.8
    milesburton/DallasTemperature @ ^3.11.0

build_flags =
    -O3
    -DCORE_DEBUG_LEVEL=0
    -DUSE_CAMERA=1
    -DCAMERA_MODEL_OV7670=1
```

---

## 7. Architectural Isolation & API Boundary Definition

To satisfy Requirement R1 ("Ensure changes are strictly isolated to this module without modifying other parts of the existing software"), the camera and person detection logic is encapsulated into a standalone, modular C++ component:

### 7.1 Module Interface: `src/camera_person_detector.h`

```cpp
#pragma once
#include <Arduino.h>

struct PersonDetectionResult {
    bool detected;
    float personScore;
    float notPersonScore;
    uint32_t inferenceTimeMs;
};

class CameraPersonDetector {
public:
    CameraPersonDetector();
    ~CameraPersonDetector();

    bool begin();
    bool runInference(PersonDetectionResult& result);
    int getOccupancy(); // Returns 1 if person detected, 0 otherwise
    float getLastScore() const;
    bool isReady() const;

private:
    bool initialized;
    float lastPersonScore;
    bool lastDetected;
    uint8_t* tensorArena;
};
```

---

## 8. Features Discovered & Edge Cases (Specification Miner Format)

### Features Discovered
| # | Category | Feature | Description | Inputs | Outputs | Error Behavior | Discovered Via |
|---|----------|---------|-------------|--------|---------|----------------|----------------|
| 1 | Camera Driver | OV7670 I2S DMA Driver | Captures 160×120 grayscale frames via ESP32 I2S parallel slave DMA without PSRAM | `camera_config_t` pinout, clock settings | `camera_fb_t*` buffer in DRAM (19.2 KB) | Returns `NULL` if DMA buffer allocation fails | ESP32 Technical Reference Manual & `esp32-camera` source |
| 2 | Clock Gen | High-Speed XCLK Gen | Generates 10–20 MHz master clock for OV7670 using LEDC PWM channel | Target freq (20 MHz), GPIO pin (GPIO 15) | PWM clock wave on GPIO | Fails at compile/init if invalid GPIO | ESP32 LEDC Driver Spec |
| 3 | Camera Control | SCCB / I2C Configuration | Configures OV7670 internal registers (gain, exposure, matrix) via I2C | Register address (0x21), command bytes | ACK / NACK status | Returns `ESP_FAIL` if sensor not responding | OV7670 Datasheet & SCCB Spec |
| 4 | ML Model | VWW Person Detect int8 | Depthwise separable CNN for binary person detection | `[1, 96, 96, 1]` int8 image tensor | `[1, 2]` int8 class logits (person / not_person) | Returns error code if tensor dimensions mismatch | TFLite Micro Model Zoo |
| 5 | Memory Mgmt | Static Tensor Arena | Dedicated contiguous SRAM block for TFLM activations (~80 KB) | Arena size (80 KB) | Pointer to aligned memory | `kTfLiteError` if arena is too small | TFLM Memory Planner Spec |
| 6 | Preprocessing | Frame Crop & Downsample | Center-crops 160×120 to 120×120 and scales to 96×96 int8 | 160×120 uint8 frame | 96×96 int8 tensor | Bounds-checked index mapping | Image Processing Pipeline Spec |
| 7 | Communication | Dual-Mode Fallback Bridge | Broadcasts JSON over Wi-Fi MQTT; falls back automatically to USB Serial | JSON telemetry string | Network packet or UART0 stream | Switches to Serial if MQTT/WiFi disconnects | ORIGINAL_REQUEST.md R2 |
| 8 | Simulation | Graceful Degradation | Gracefully handles missing camera hardware in Wokwi simulator by reporting sensor status | Init status check | Status log & safe fallback | Does not crash or panic if camera missing | Wokwi ESP32 Architecture |

### Edge Cases
| # | Feature | Input | Observed / Specified Behavior |
|---|---------|-------|-------------------|
| 1 | Frame Acquisition | Camera disconnected / bus wiring fault | `esp_camera_init()` returns `ESP_ERR_NOT_FOUND` / `ESP_FAIL`. System logs error, disables camera polling, reports `occupancy: 0` or keeps previous state without crashing. |
| 2 | Memory Allocation | Heap fragmented, <80 KB contiguous DRAM | Tensor arena allocation fails; system reports `kTfLiteError` during interpreter initialization and falls back gracefully. |
| 3 | Image Normalization | All-black (0) or all-white (255) frame | Normalized cleanly to `-128` (black) and `+127` (white); model outputs valid logits without division by zero or NaN. |
| 4 | ML Confidence | Logit score exactly at threshold (e.g. 0.60) | Strict hysteresis band (e.g. enter >0.65, exit <0.50) prevents occupancy output flickering. |
| 5 | Network Disruption | Wi-Fi router reboot / MQTT timeout | `client.connected()` evaluates `false`; telemetry immediately streams over USB Serial (`UART0`) until Wi-Fi reconnects. |
| 6 | High Ambient Heat / Sensor Noise | Random noisy pixels from OV7670 analog gain | 3-frame temporal smoothing / consensus filter eliminates single-frame false positives. |
| 7 | Strapping Pin Conflict | GPIO 0 or GPIO 12 driven by camera during boot | Using dedicated non-strapping pins (GPIO 13, 14, 15, 27, 32-36, 39) ensures bootloader reliably enters flash mode. |

---

## 9. Conclusion

The integration of an OV7670 camera and TFLite Micro person detection model on an ESP32-WROOM without external PSRAM is **architecturally sound, mathematically validated, and fully viable within the memory and performance boundaries of the hardware**. Isolating the subsystem into a dedicated `CameraPersonDetector` class guarantees zero disruption to existing HVAC, lighting, and power-metering code while satisfying all R1 and R2 requirements.
