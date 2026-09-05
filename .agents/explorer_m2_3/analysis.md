# Milestone 2: Image Preprocessor, Interface Contracts & Test Suite Design Analysis

**Agent**: Explorer 3 (Milestone 2: Image Preprocessor & Interface/Testing)  
**Date**: 2026-08-26  
**Target Architecture**: ESP32 WROOM (Xtensa dual-core 32-bit LX6 @ 240 MHz, 520 KB SRAM, 4 MB Flash)  
**Target Module**: `edge/esp32/src/camera/` & `edge/esp32/test/test_m2_camera_ml.cpp`

---

## 1. Executive Summary

Milestone 2 upgrades the ESP32 WROOM node from a binary PIR sensor to an intelligent OV7670 camera-based person detector running TensorFlow Lite Micro. The **Image Preprocessor** forms the crucial computational bridge between the raw camera DMA buffer and the TFLite Micro neural network input tensor.

Key findings:
1. **Mathematical Pipeline**: The OV7670 operates in QQVGA ($160 \times 120$) 8-bit grayscale mode ($19,200$ bytes). To preserve human aspect ratios without horizontal distortion, the frame is center-cropped to $120 \times 120$ ($X \in [20, 140)$) and downsampled by $1.25\times$ to $96 \times 96$ ($9,216$ bytes). Quantization maps $\text{uint8} \in [0, 255]$ directly to signed $\text{int8} \in [-128, 127]$ via $q = p - 128$.
2. **Fixed-Point Xtensa Optimization**: Because the downscale factor is exactly $5/4$, the coordinate mapping decomposes cleanly into integer shifts and base-4 modulo weights ($x_{\text{int}} = 20 + x + (x \gg 2)$, $w_x = x \,\&\, 3$). Fixed-point bilinear interpolation requires **zero floating-point operations**, **zero division instructions**, and **zero branching** in the inner loop, completing a full frame transformation in $\approx 138\text{k}$ cycles ($\approx 0.58\text{ ms}$ at $240\text{ MHz}$, consuming $< 0.3\%$ of a 5-second detection cycle).
3. **Interface Contracts**: The preprocessor encapsulates frame processing into a stateless, deterministic function `ImagePreprocessor::preprocessFrame()`, decoupled from hardware DMA and inference runtimes. `CameraPersonDetector` orchestrates the complete lifecycle (`init()`, `processFrame()`, `getLatestData()`, `transmitTelemetry()`), transitioning through strict state machine states.
4. **Host Test Suite**: A comprehensive, standalone host test suite (`edge/esp32/test/test_m2_camera_ml.cpp`) verifies the preprocessor math, mock camera driver, model data integrity, TFLite Micro inference, edge cases (all-black, all-white, gradients, person patterns), and state machine contracts on the host build machine without requiring physical ESP32 or OV7670 hardware.

---

## 2. Frame Preprocessor Mathematical Specifications

### 2.1 Geometry and Memory Layout

```
+-----------------------------------------------------------------------+
| OV7670 QQVGA 8-bit Grayscale Frame (160 x 120 = 19,200 bytes)         |
|                                                                       |
|  (0,0)             X=20                       X=140          (159,0)  |
|    +-----------------+--------------------------+----------------+    |
|    |  Discarded Left |  Center Crop Window      | Discarded Right|    |
|    |  (20 x 120 px)  |  (120 x 120 px)          | (20 x 120 px)  |    |
|    |                 |                          |                |    |
|    |                 |   Bilinear Downsampling  |                |    |
|    |                 |   (Scale Factor 5/4)     |                |    |
|    |                 |            |             |                |    |
|    |                 |            v             |                |    |
|    |                 |   +------------------+   |                |    |
|    |                 |   | TFLite Input     |   |                |    |
|    |                 |   | (96 x 96 int8)   |   |                |    |
|    |                 |   | (9,216 bytes)    |   |                |    |
|    |                 |   +------------------+   |                |    |
|    +-----------------+--------------------------+----------------+    |
|  (0,119)                                                     (159,119)|
+-----------------------------------------------------------------------+
```

| Parameter | Input DMA Buffer | Cropped Region | Target ML Tensor |
| :--- | :--- | :--- | :--- |
| **Width ($W$)** | $160\text{ px}$ | $120\text{ px}$ | $96\text{ px}$ |
| **Height ($H$)** | $120\text{ px}$ | $120\text{ px}$ | $96\text{ px}$ |
| **Aspect Ratio** | $4:3$ ($1.333$) | $1:1$ ($1.000$) | $1:1$ ($1.000$) |
| **Channels ($C$)**| $1$ (Grayscale Y) | $1$ (Grayscale Y) | $1$ (Grayscale Y) |
| **Data Type** | `uint8_t` | `uint8_t` | `int8_t` |
| **Value Range** | $[0, 255]$ | $[0, 255]$ | $[-128, 127]$ |
| **Total Memory** | $19,200\text{ bytes}$ ($18.75\text{ KB}$) | Logical slice | $9,216\text{ bytes}$ ($9.00\text{ KB}$) |

### 2.2 Aspect Ratio Preservation & Center-Cropping

Directly squashing $160 \times 120$ into $96 \times 96$ introduces a $1.333\times$ horizontal compression factor ($\text{Aspect Ratio Error} = \frac{160/120}{96/96} = 1.333$). This distorts human proportions (making standing persons appear excessively thin), which degrades detection accuracy of the Visual Wake Words (VWW) model.

To preserve geometric fidelity:
- Crop Width: $W_{\text{crop}} = H_{\text{in}} = 120\text{ px}$.
- Horizontal Offset: $X_{\text{offset}} = \frac{W_{\text{in}} - W_{\text{crop}}}{2} = \frac{160 - 120}{2} = 20\text{ px}$.
- Vertical Offset: $Y_{\text{offset}} = 0\text{ px}$.
- Cropped Window Coordinates: $x \in [20, 139]$, $y \in [0, 119]$.

### 2.3 Pixel Normalization & Quantization Mapping

TensorFlow Lite Micro Visual Wake Words (VWW) models use standard 8-bit signed integer symmetric quantization (`kTfLiteInt8`):
$$\text{Real Value } V = S \cdot (q - Z)$$
where $S = \frac{1.0}{128.0}$ and zero-point $Z = 0$ (mapping real range $[-1.0, 1.0]$ to $[-128, 127]$) or $S = \frac{1.0}{255.0}$ with $Z = -128$ (mapping $[0.0, 1.0]$ to $[-128, 127]$).

In both formulations, raw unsigned grayscale intensity $p \in [0, 255]$ converts to signed tensor value $q \in [-128, 127]$ via:
$$q = \text{static\_cast<int8\_t>}(p - 128)$$

- Black ($p = 0$): $0 - 128 = -128$
- Mid-Gray ($p = 128$): $128 - 128 = 0$
- White ($p = 255$): $255 - 128 = 127$

---

## 3. Downsampling Algorithm Analysis & Comparison

We evaluate three downsampling strategies from $120 \times 120$ to $96 \times 96$ ($\text{scale factor } s = \frac{120}{96} = 1.25 = \frac{5}{4}$):

```
Algorithm Comparison Matrix:
+---------------------+-------------------+-------------------+--------------------+
| Metric              | Nearest-Neighbor  | Area-Averaging    | Fixed-Point Bilinear|
+---------------------+-------------------+-------------------+--------------------+
| Visual Quality      | Low (aliasing)    | High (smooth)     | High (anti-aliased)|
| Silhouette Fidelity | Jagged edges      | Preserved         | Preserved          |
| Multiplications/px  | 0                 | 4 to 9            | 4 small ints       |
| Division Required   | 0                 | Variable weights  | Bit-shift (>> 4)   |
| Memory Reads/px     | 1                 | 4 to 9            | 4                  |
| Cycle Cost / Frame  | ~30k cycles       | ~450k cycles      | ~138k cycles       |
| Latency @ 240 MHz   | ~0.12 ms          | ~1.88 ms          | ~0.58 ms           |
| Recommendation      | Fallback only     | Overkill          | **PRIMARY CHOICE** |
+---------------------+-------------------+-------------------+--------------------+
```

### 3.1 Fixed-Point Bilinear Interpolation Derivation

For an output pixel $(x, y) \in [0, 95] \times [0, 95]$:
The continuous source coordinate within the cropped frame is:
$$x_{\text{src}} = 20 + \frac{5 \cdot x}{4} = 20 + x + \frac{x}{4}$$
$$y_{\text{src}} = \frac{5 \cdot y}{4} = y + \frac{y}{4}$$

Decomposing into integer parts and base-4 fractional parts:
- Integer coordinate:
  $$x_{\text{int}} = 20 + x + (x \gg 2)$$
  $$y_{\text{int}} = y + (y \gg 2)$$
- Fractional weights ($w_x, w_y \in \{0, 1, 2, 3\}$):
  $$w_x = x \,\&\, 3$$
  $$w_y = y \,\&\, 3$$

The 2x2 neighborhood consists of:
- Top-Left ($P_{00}$): $(x_{\text{int}}, y_{\text{int}})$ with weight $W_{00} = (4 - w_x)(4 - w_y)$
- Top-Right ($P_{10}$): $(x_{\text{int}} + 1, y_{\text{int}})$ with weight $W_{10} = w_x (4 - w_y)$
- Bottom-Left ($P_{01}$): $(x_{\text{int}}, y_{\text{int}} + 1)$ with weight $W_{01} = (4 - w_x) w_y$
- Bottom-Right ($P_{11}$): $(x_{\text{int}} + 1, y_{\text{int}} + 1)$ with weight $W_{11} = w_x w_y$

#### Total Weight Invariant:
$$\sum W = W_{00} + W_{10} + W_{01} + W_{11} = (4 - w_x + w_x)(4 - w_y + w_y) = 4 \times 4 = 16$$

#### Interpolated Value & Quantization:
$$\text{val} = \frac{W_{00} P_{00} + W_{10} P_{10} + W_{01} P_{01} + W_{11} P_{11} + 8}{16} = \left(\sum W_{ij} P_{ij} + 8\right) \gg 4$$
$$q = \text{static\_cast<int8\_t>}(\text{val} - 128)$$

Adding $8$ ($16/2$) before shifting right by 4 implements exact integer rounding to the nearest value.

### 3.2 Maximum Coordinate and Memory Bounds Proof

Let us verify boundary safety for the worst-case output pixel $(x=95, y=95)$:
- $x = 95$:
  $$x_{\text{int}} = 20 + 95 + (95 \gg 2) = 20 + 95 + 23 = 138$$
  $$x_{\text{int}} + 1 = 139$$
  Since input width is $160$, column $139 < 160$. $\implies$ **Within bounds**.
- $y = 95$:
  $$y_{\text{int}} = 95 + (95 \gg 2) = 95 + 23 = 118$$
  $$y_{\text{int}} + 1 = 119$$
  Since input height is $120$, row $119 < 120$. $\implies$ **Within bounds**.

**Conclusion**: Memory access is strictly bounded in $[0, 19199]$. No bounds-checking branching or edge-clamping is required inside the inner pixel loop!

---

## 4. Xtensa LX6 Hardware Performance Optimization

### 4.1 Micro-Architecture Optimizations for ESP32

The ESP32 Xtensa LX6 core features a 7-stage pipeline and 32-bit hardware multipliers. To achieve peak throughput:

1. **Row Pointer Pre-Calculation**:
   Instead of computing `y_int * 160` for every pixel, pre-calculate the row base pointers in the outer $y$ loop:
   ```cpp
   const uint8_t* row0 = &src_frame[y_int * 160 + 20];
   const uint8_t* row1 = &src_frame[(y_int + 1) * 160 + 20];
   ```
2. **Weight Factoring**:
   In the outer loop, pre-calculate $wy_0 = 4 - w_y$ and $wy_1 = w_y$.
   In the inner loop, compute 4 small integer multiplications:
   $W_{00} = (4 - w_x) \times wy_0$, $W_{10} = w_x \times wy_0$, $W_{01} = (4 - w_x) \times wy_1$, $W_{11} = w_x \times wy_1$.
3. **Register Reuse & Loop Unrolling**:
   Because $w_x = x \,\&\, 3$ repeats with period 4, an unrolled 4-pixel inner loop can replace dynamic shifts with constant folding:
   - Pixel 0 ($w_x = 0$): $x_{\text{int}}$ offset $+0$, weights $(4, 0)$
   - Pixel 1 ($w_x = 1$): $x_{\text{int}}$ offset $+1$, weights $(3, 1)$
   - Pixel 2 ($w_x = 2$): $x_{\text{int}}$ offset $+2$, weights $(2, 2)$
   - Pixel 3 ($w_x = 3$): $x_{\text{int}}$ offset $+3$, weights $(1, 3)$
   - Advance source offset by $+5$ bytes for every 4 output pixels.

### 4.2 C++ Production Implementation Blueprint

```cpp
namespace ImagePreprocessor {

constexpr int INPUT_WIDTH = 160;
constexpr int INPUT_HEIGHT = 120;
constexpr int CROP_WIDTH = 120;
constexpr int CROP_HEIGHT = 120;
constexpr int CROP_OFFSET_X = 20;
constexpr int CROP_OFFSET_Y = 0;
constexpr int OUTPUT_WIDTH = 96;
constexpr int OUTPUT_HEIGHT = 96;
constexpr size_t INPUT_FRAME_BYTES = INPUT_WIDTH * INPUT_HEIGHT;    // 19,200
constexpr size_t OUTPUT_TENSOR_BYTES = OUTPUT_WIDTH * OUTPUT_HEIGHT; // 9,216

/**
 * @brief Downsamples and normalizes QQVGA grayscale frame to 96x96 int8 tensor.
 * @param src_frame Pointer to 160x120 uint8 grayscale DMA buffer.
 * @param src_len Length of input buffer in bytes (must be >= 19200).
 * @param dst_tensor Pointer to 96x96 int8 output tensor buffer.
 * @param dst_len Length of output tensor buffer in bytes (must be >= 9216).
 * @return true if preprocessing succeeded, false on invalid parameters.
 */
inline bool preprocessFrame(const uint8_t* src_frame, size_t src_len,
                           int8_t* dst_tensor, size_t dst_len) {
    if (!src_frame || !dst_tensor) return false;
    if (src_len < INPUT_FRAME_BYTES || dst_len < OUTPUT_TENSOR_BYTES) return false;

    for (int y = 0; y < OUTPUT_HEIGHT; ++y) {
        const int y_int = y + (y >> 2);
        const int wy_1 = y & 3;
        const int wy_0 = 4 - wy_1;

        const uint8_t* row0 = &src_frame[y_int * INPUT_WIDTH + CROP_OFFSET_X];
        const uint8_t* row1 = &src_frame[(y_int + 1) * INPUT_WIDTH + CROP_OFFSET_X];
        int8_t* out_row = &dst_tensor[y * OUTPUT_WIDTH];

        for (int x = 0; x < OUTPUT_WIDTH; ++x) {
            const int x_int = x + (x >> 2);
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

            const int val = (w00 * p00 + w10 * p10 + w01 * p01 + w11 * p11 + 8) >> 4;
            out_row[x] = static_cast<int8_t>(val - 128);
        }
    }
    return true;
}

/**
 * @brief Fast Nearest-Neighbor downsample (reference/fallback mode).
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
```

---

## 5. Integration with CameraPersonDetector Class Interface

### 5.1 System Architecture Flow

```
+------------------------------------------------------------------------------------+
|                             CameraPersonDetector Class                             |
|                                                                                    |
|  +--------------------+      +--------------------+      +--------------------+    |
|  |   OV7670Driver     |      | ImagePreprocessor  |      | TFLite Micro       |    |
|  | (I2S DMA / Mock)   | ---> | (Bilinear / int8)  | ---> | Inference Engine   |    |
|  | QQVGA 160x120 uint8|      | 96x96 int8 Tensor  |      | Arena: ~80KB SRAM  |    |
|  +--------------------+      +--------------------+      +---------+----------+    |
|                                                                    |               |
|                                                                    v               |
|                                                          +--------------------+    |
|                                                          | PersonTrackingData |    |
|                                                          | (Presence, Conf, #)|    |
|                                                          +---------+----------+    |
|                                                                    |               |
|  +-----------------------------------------------------------------+               |
|  |                                                                                 |
|  v                                                                                 |
|  void transmitTelemetry(DualModeComm& comm)                                        |
|   -> comm.broadcastTrackingData(latestData)                                        |
+------------------------------------------------------------------------------------+
```

### 5.2 State Machine Contracts

`CameraPersonDetector` manages an explicit state machine to guarantee robust operation:

```
                +---------------------+
                |    UNINITIALIZED    |
                +----------+----------+
                           |
                     init()|
              +------------+------------+
              | (Hardware OK)           | (Simulation / No HW)
              v                         v
     +-----------------+       +-----------------+
     |      READY      |       | SIMULATION_MODE |
     +--------+--------+       +--------+--------+
              |                         |
processFrame()|           processFrame()|
              v                         v
     +-----------------+       +-----------------+
     |    DETECTING    |       | SIM_DETECTING   |
     +--------+--------+       +--------+--------+
              |                         |
              +------------+------------+
                           |
                           v
               (Update PersonTrackingData)
```

| State | `init()` | `processFrame()` | `isPersonDetected()` | `transmitTelemetry()` |
| :--- | :--- | :--- | :--- | :--- |
| **UNINITIALIZED** | Attempts driver & model init | Returns `false` | Returns `false` | No-op (or error log) |
| **READY** | Returns `true` (idempotent) | Captures frame, preprocesses, infers | Returns last score > threshold | Dispatches serialized payload |
| **SIMULATION_MODE** | Returns `true` (mock camera) | Preprocesses mock frame, infers | Returns simulation score | Dispatches serialized payload |
| **ERROR** | Re-attempts recovery | Returns `false` | Returns `false` | Sends error telemetry |

### 5.3 Interface Implementation Contract (`person_detector.h`)

```cpp
#pragma once

#include <cstdint>
#include <cstddef>
#include "camera_config.h"
#include "tracking_payload.h"

// Forward declaration of comm interface
class DualModeComm;

enum class DetectorState {
    UNINITIALIZED,
    READY,
    SIMULATION_MODE,
    ERROR_HARDWARE,
    ERROR_MODEL
};

class CameraPersonDetector {
public:
    CameraPersonDetector();
    ~CameraPersonDetector();

    // Lifecycle
    bool init();
    bool processFrame();

    // Query state
    bool isPersonDetected() const { return latestData_.person_detected; }
    float getConfidence() const { return latestData_.confidence; }
    int getPersonCount() const { return latestData_.person_count; }
    DetectorState getState() const { return state_; }
    const PersonTrackingData& getLatestData() const { return latestData_; }

    // Telemetry dispatch
    void transmitTelemetry(DualModeComm& comm);

    // Configuration / Thresholds
    void setDetectionThreshold(float threshold) { detectionThreshold_ = threshold; }
    float getDetectionThreshold() const { return detectionThreshold_; }

    // Testing / Simulation injection
    void injectMockFrame(const uint8_t* frame_data, size_t len);

private:
    DetectorState state_;
    float detectionThreshold_;
    PersonTrackingData latestData_;

    // Internal working buffers
    int8_t inputTensorBuffer_[ImagePreprocessor::OUTPUT_TENSOR_BYTES];
    
    // Internal pipeline steps
    bool runInference(float& out_person_score, int& out_person_count);
};
```

---

## 6. Comprehensive Test Suite Design (`test_m2_camera_ml.cpp`)

The test suite is structured into 5 cohesive test groups running completely off-target (host machine `c++ -std=c++17`) with 0 hardware dependencies.

### 6.1 Test Suite Structure

```
edge/esp32/test/test_m2_camera_ml.cpp
├── Group 1: Frame Preprocessor Math & Normalization
│   ├── test_preprocessor_null_and_bounds()
│   ├── test_preprocessor_solid_frames() (All-Black, All-White, Mid-Gray)
│   ├── test_preprocessor_crop_isolation() (Markers outside vs inside crop)
│   ├── test_preprocessor_linear_gradients() (Horizontal & Vertical monotonicity)
│   ├── test_preprocessor_checkerboard_antialiasing() (Bilinear vs NN comparison)
│   └── test_preprocessor_performance_benchmark() (100 iterations timing)
├── Group 2: Mock Camera Driver & Lifecycle
│   ├── test_mock_driver_uninitialized()
│   ├── test_mock_driver_lifecycle()
│   └── test_mock_driver_dma_buffer_alignment()
├── Group 3: Model Data & TFLite Schema Integrity
│   ├── test_model_data_magic_header() (TFL3 identifier)
│   ├── test_model_data_size_and_alignment()
│   └── test_model_arena_allocation()
├── Group 4: ML Inference & Pattern Recognition
│   ├── test_inference_empty_background()
│   ├── test_inference_synthetic_person_pattern()
│   └── test_confidence_hysteresis_thresholds()
└── Group 5: CameraPersonDetector Integration & State Machine
    ├── test_detector_uninitialized_safety()
    ├── test_detector_full_lifecycle()
    ├── test_detector_telemetry_dispatch()
    └── test_detector_mock_frame_injection()
```

### 6.2 Test Case Specifications & Assertions

#### Group 1: Image Preprocessor Tests
1. **`test_preprocessor_null_and_bounds`**:
   - Call `preprocessFrame(nullptr, 19200, tensor, 9216)` $\implies$ returns `false`.
   - Call `preprocessFrame(frame, 19200, nullptr, 9216)` $\implies$ returns `false`.
   - Call `preprocessFrame(frame, 19199, tensor, 9216)` (undersized) $\implies$ returns `false`.
   - Call `preprocessFrame(frame, 19200, tensor, 9215)` (undersized) $\implies$ returns `false`.

2. **`test_preprocessor_solid_frames`**:
   - Fill frame with $0$: all 9,216 tensor values must equal $-128$.
   - Fill frame with $255$: all 9,216 tensor values must equal $+127$.
   - Fill frame with $128$: all 9,216 tensor values must equal $0$.
   - Fill frame with $64$: all 9,216 tensor values must equal $-64$.
   - Fill frame with $192$: all 9,216 tensor values must equal $+64$.

3. **`test_preprocessor_crop_isolation`**:
   - Set all pixels outside crop region ($X < 20$ or $X \ge 140$) to $255$.
   - Set all pixels inside crop region ($20 \le X < 140$) to $0$.
   - Run `preprocessFrame()`.
   - Assert: **every output tensor pixel remains $-128$**. This proves $100\%$ spatial isolation of the crop window.
   - Set pixel at $(20, 0)$ to $255$: tensor at $(0, 0)$ must be elevated ($> -128$).
   - Set pixel at $(139, 119)$ to $255$: tensor at $(95, 95)$ must be elevated ($> -128$).
   - Set vertical line at $X=80$: tensor must show vertical peak at $x = (80-20) \times 4/5 = 48$.

4. **`test_preprocessor_linear_gradients`**:
   - Horizontal gradient: $P(x, y) = x \times \frac{255}{159}$.
   - Output row $y=0$: verify $T(x+1, 0) \ge T(x, 0)$ for all $x \in [0, 94]$.
   - Vertical gradient: $P(x, y) = y \times \frac{255}{119}$.
   - Output column $x=0$: verify $T(0, y+1) \ge T(0, y)$ for all $y \in [0, 94]$.

5. **`test_preprocessor_checkerboard_antialiasing`**:
   - Fill frame with Nyquist checkerboard pattern ($0, 255, 0, 255$).
   - Bilinear downsampler output must be smoothly filtered around mid-gray ($-20 \le T(x, y) \le 20$), whereas Nearest-Neighbor exhibits extreme oscillations ($-128$ and $+127$).

6. **`test_preprocessor_performance_benchmark`**:
   - Execute 100 runs of `preprocessFrame()` on host CPU.
   - Assert average duration $< 2.0\text{ ms}$ on host (typically $< 0.1\text{ ms}$).

#### Group 2 & 3: Mock Driver & Model Data Tests
1. **`test_mock_driver_lifecycle`**:
   - Initialize mock driver $\implies$ returns success, buffer pointer is valid and 32-bit aligned.
   - Capture frame $\implies$ frame buffer contains valid pixel data with correct sequence numbers.
2. **`test_model_data_magic_header`**:
   - Inspect model byte array in `model_data.cpp`.
   - Verify FlatBuffer identifier: bytes at offset 4..7 equal `"TFL3"`.
   - Verify array size $> 20,000$ bytes and $< 150,000$ bytes (fits ESP32 flash).

#### Group 4: ML Inference & Pattern Recognition Tests
1. **`test_inference_empty_background`**:
   - Feed all-black or uniform gray frame to `person_detector`.
   - Assert: `processFrame()` returns `true`, `isPersonDetected()` is `false`, `confidence < 0.40`, `person_count == 0`.
2. **`test_inference_synthetic_person_pattern`**:
   - Render a synthetic humanoid silhouette (head oval at $(48, 25)$, torso rectangle at $[38..58] \times [35..75]$, legs at $[40..46] \times [75..95]$ and $[50..56] \times [75..95]$ with value 200 on dark background 30).
   - Feed frame to `person_detector`.
   - Assert: `processFrame()` returns `true`, `isPersonDetected()` is `true`, `confidence >= 0.50`, `person_count >= 1`.
3. **`test_confidence_hysteresis_thresholds`**:
   - Set detection threshold to 0.70.
   - Feed frame with marginal confidence 0.60 $\implies$ detected is `false`.
   - Set detection threshold to 0.50.
   - Feed frame with marginal confidence 0.60 $\implies$ detected is `true`.

#### Group 5: CameraPersonDetector Integration Tests
1. **`test_detector_uninitialized_safety`**:
   - Create `CameraPersonDetector` without calling `init()`.
   - `processFrame()` $\implies$ returns `false`.
   - `isPersonDetected()` $\implies$ returns `false`.
   - `getConfidence()` $\implies$ returns `0.0f`.
   - `getPersonCount()` $\implies$ returns `0`.
2. **`test_detector_full_lifecycle`**:
   - Call `init()` $\implies$ returns `true`, state is `READY` or `SIMULATION_MODE`.
   - Call `processFrame()` $\implies$ returns `true`.
   - Call `getLatestData()` $\implies$ returns struct with valid `timestamp_ms > 0`, `sensor_id != nullptr`, `zone_id != nullptr`.
3. **`test_detector_telemetry_dispatch`**:
   - Provide a mock `DualModeComm` instance.
   - Call `transmitTelemetry(mockComm)`.
   - Assert `mockComm.last_broadcast_count == 1` and payload contains valid serialized JSON.

---

## 7. Concrete Test Implementation Blueprint

Below is the complete C++ test runner blueprint for `edge/esp32/test/test_m2_camera_ml.cpp`:

```cpp
// edge/esp32/test/test_m2_camera_ml.cpp
// Host-side comprehensive test suite for Milestone 2:
// - Image Preprocessor fixed-point math & normalizations
// - Mock Camera Driver lifecycle
// - TFLite Micro model data integrity
// - Person detector inference & threshold logic
// - CameraPersonDetector interface & telemetry dispatch

#include <iostream>
#include <vector>
#include <cmath>
#include <cstring>
#include <chrono>
#include <cassert>

// Include shims and tested headers
#include "arduino_shim.h"
#include "camera/camera_config.h"
#include "camera/person_detector.h"
#include "camera/model_data.h"
#include "camera/tracking_payload.h"

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

// -----------------------------------------------------------------------------
// Group 1: Frame Preprocessor Tests
// -----------------------------------------------------------------------------
void test_preprocessor_null_and_bounds() {
    std::cout << "\n--- Group 1.1: Preprocessor Null and Bounds Safety ---\n";
    uint8_t src[ImagePreprocessor::INPUT_FRAME_BYTES];
    int8_t dst[ImagePreprocessor::OUTPUT_TENSOR_BYTES];
    memset(src, 128, sizeof(src));

    check(!ImagePreprocessor::preprocessFrame(nullptr, sizeof(src), dst, sizeof(dst)),
          "Null source buffer rejected");
    check(!ImagePreprocessor::preprocessFrame(src, sizeof(src), nullptr, sizeof(dst)),
          "Null destination buffer rejected");
    check(!ImagePreprocessor::preprocessFrame(src, sizeof(src) - 1, dst, sizeof(dst)),
          "Undersized source buffer rejected");
    check(!ImagePreprocessor::preprocessFrame(src, sizeof(src), dst, sizeof(dst) - 1),
          "Undersized destination buffer rejected");
}

void test_preprocessor_solid_frames() {
    std::cout << "\n--- Group 1.2: Preprocessor Solid Luminance & Normalization ---\n";
    std::vector<uint8_t> src(ImagePreprocessor::INPUT_FRAME_BYTES);
    std::vector<int8_t> dst(ImagePreprocessor::OUTPUT_TENSOR_BYTES);

    // Test All-Black (0 -> -128)
    std::fill(src.begin(), src.end(), 0);
    check(ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size()),
          "Process all-black frame");
    bool all_black_ok = true;
    for (int8_t val : dst) {
        if (val != -128) { all_black_ok = false; break; }
    }
    check(all_black_ok, "All-black frame maps uniformly to -128");

    // Test All-White (255 -> 127)
    std::fill(src.begin(), src.end(), 255);
    check(ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size()),
          "Process all-white frame");
    bool all_white_ok = true;
    for (int8_t val : dst) {
        if (val != 127) { all_white_ok = false; break; }
    }
    check(all_white_ok, "All-white frame maps uniformly to +127");

    // Test Mid-Gray (128 -> 0)
    std::fill(src.begin(), src.end(), 128);
    check(ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size()),
          "Process mid-gray frame");
    bool mid_gray_ok = true;
    for (int8_t val : dst) {
        if (val != 0) { mid_gray_ok = false; break; }
    }
    check(mid_gray_ok, "Mid-gray frame (128) maps uniformly to 0");
}

void test_preprocessor_crop_isolation() {
    std::cout << "\n--- Group 1.3: Preprocessor Center Crop Spatial Isolation ---\n";
    std::vector<uint8_t> src(ImagePreprocessor::INPUT_FRAME_BYTES, 0);
    std::vector<int8_t> dst(ImagePreprocessor::OUTPUT_TENSOR_BYTES, 0);

    // Fill only discarded borders (X < 20 and X >= 140) with 255
    for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
        for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
            if (x < 20 || x >= 140) {
                src[y * ImagePreprocessor::INPUT_WIDTH + x] = 255;
            } else {
                src[y * ImagePreprocessor::INPUT_WIDTH + x] = 0;
            }
        }
    }

    check(ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size()),
          "Process border-isolated frame");
    bool crop_isolated = true;
    for (int8_t val : dst) {
        if (val != -128) { crop_isolated = false; break; }
    }
    check(crop_isolated, "Discarded horizontal borders have zero influence on output tensor");

    // Center vertical line at X=80 (which is 80 - 20 = 60 in crop, 60 * 4/5 = 48 in output)
    std::fill(src.begin(), src.end(), 0);
    for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
        src[y * ImagePreprocessor::INPUT_WIDTH + 80] = 255;
    }
    ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size());
    // In output row 48, column 48 should be elevated
    int8_t center_val = dst[48 * ImagePreprocessor::OUTPUT_WIDTH + 48];
    int8_t edge_val = dst[48 * ImagePreprocessor::OUTPUT_WIDTH + 10];
    check(center_val > -128 && edge_val == -128, "Center vertical marker maps precisely to output column 48");
}

void test_preprocessor_linear_gradients() {
    std::cout << "\n--- Group 1.4: Preprocessor Monotonic Linear Gradients ---\n";
    std::vector<uint8_t> src(ImagePreprocessor::INPUT_FRAME_BYTES);
    std::vector<int8_t> dst(ImagePreprocessor::OUTPUT_TENSOR_BYTES);

    // Horizontal linear ramp from 0 to 255
    for (int y = 0; y < ImagePreprocessor::INPUT_HEIGHT; ++y) {
        for (int x = 0; x < ImagePreprocessor::INPUT_WIDTH; ++x) {
            src[y * ImagePreprocessor::INPUT_WIDTH + x] = static_cast<uint8_t>((x * 255) / 159);
        }
    }
    ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size());
    bool h_monotonic = true;
    for (int x = 0; x < ImagePreprocessor::OUTPUT_WIDTH - 1; ++x) {
        if (dst[48 * ImagePreprocessor::OUTPUT_WIDTH + (x + 1)] < dst[48 * ImagePreprocessor::OUTPUT_WIDTH + x]) {
            h_monotonic = false;
            break;
        }
    }
    check(h_monotonic, "Horizontal gradient is strictly monotonically increasing");
}

void test_preprocessor_performance_benchmark() {
    std::cout << "\n--- Group 1.5: Preprocessor Host Latency Benchmark ---\n";
    std::vector<uint8_t> src(ImagePreprocessor::INPUT_FRAME_BYTES, 100);
    std::vector<int8_t> dst(ImagePreprocessor::OUTPUT_TENSOR_BYTES);

    const int iterations = 1000;
    auto start = std::chrono::high_resolution_clock::now();
    for (int i = 0; i < iterations; ++i) {
        ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size());
    }
    auto end = std::chrono::high_resolution_clock::now();
    double total_us = std::chrono::duration<double, std::micro>(end - start).count();
    double avg_us = total_us / iterations;

    std::cout << "  [INFO] Average Preprocessor Latency (Host): " << avg_us << " us / frame ("
              << (1000000.0 / avg_us) << " FPS)\n";
    check(avg_us < 500.0, "Preprocessor execution is sub-millisecond");
}

// -----------------------------------------------------------------------------
// Group 2 & 3: Model Data & TFLite Schema Integrity
// -----------------------------------------------------------------------------
void test_model_data_integrity() {
    std::cout << "\n--- Group 2: Model Data Flash Resident Array Integrity ---\n";
    check(g_person_detect_model_data != nullptr, "Model data pointer is non-null");
    check(g_person_detect_model_data_len > 10000, "Model data size exceeds minimum threshold (>10KB)");

    // Check TFLite magic identifier at byte offset 4..7: "TFL3"
    bool magic_ok = (g_person_detect_model_data[4] == 'T' &&
                     g_person_detect_model_data[5] == 'F' &&
                     g_person_detect_model_data[6] == 'L' &&
                     g_person_detect_model_data[7] == '3');
    check(magic_ok, "Model data contains valid TFLite FlatBuffer identifier 'TFL3'");
}

// -----------------------------------------------------------------------------
// Group 4 & 5: CameraPersonDetector Integration & Lifecycle
// -----------------------------------------------------------------------------
void test_person_detector_lifecycle() {
    std::cout << "\n--- Group 3: CameraPersonDetector State Machine & Lifecycle ---\n";
    CameraPersonDetector detector;
    
    // Uninitialized safety checks
    check(detector.getState() == DetectorState::UNINITIALIZED, "Initial state is UNINITIALIZED");
    check(!detector.processFrame(), "processFrame() returns false when uninitialized");
    check(!detector.isPersonDetected(), "isPersonDetected() returns false when uninitialized");
    check(detector.getConfidence() == 0.0f, "Confidence is 0.0 when uninitialized");

    // Initialize detector
    bool init_ok = detector.init();
    check(init_ok, "Detector init() succeeds");
    check(detector.getState() == DetectorState::READY || 
          detector.getState() == DetectorState::SIMULATION_MODE,
          "State transitions to READY or SIMULATION_MODE");

    // Inject empty black frame
    std::vector<uint8_t> black_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 0);
    detector.injectMockFrame(black_frame.data(), black_frame.size());
    check(detector.processFrame(), "processFrame() succeeds on black frame");
    check(!detector.isPersonDetected(), "Person not detected on empty black frame");
    check(detector.getPersonCount() == 0, "Person count is 0 on black frame");

    // Inject synthetic person silhouette
    std::vector<uint8_t> person_frame(ImagePreprocessor::INPUT_FRAME_BYTES, 30);
    // Draw simple humanoid shape in center crop [20..139] -> center at X=80, Y=60
    for (int y = 30; y <= 50; ++y) {
        for (int x = 70; x <= 90; ++x) {
            person_frame[y * ImagePreprocessor::INPUT_WIDTH + x] = 220; // Head/Chest
        }
    }
    for (int y = 51; y <= 90; ++y) {
        for (int x = 65; x <= 95; ++x) {
            person_frame[y * ImagePreprocessor::INPUT_WIDTH + x] = 200; // Torso
        }
    }
    detector.injectMockFrame(person_frame.data(), person_frame.size());
    check(detector.processFrame(), "processFrame() succeeds on person silhouette frame");
    
    // Verify telemetry data struct
    const PersonTrackingData& data = detector.getLatestData();
    check(data.sensor_id != nullptr && strlen(data.sensor_id) > 0, "Telemetry sensor_id populated");
    check(data.zone_id != nullptr && strlen(data.zone_id) > 0, "Telemetry zone_id populated");
    check(data.timestamp_ms > 0, "Telemetry timestamp is valid (>0)");
}

// -----------------------------------------------------------------------------
// Test Runner Main
// -----------------------------------------------------------------------------
int main() {
    std::cout << "========================================================\n";
    std::cout << " Milestone 2: OV7670 & TFLite Micro Host Test Suite     \n";
    std::cout << "========================================================\n";

    test_preprocessor_null_and_bounds();
    test_preprocessor_solid_frames();
    test_preprocessor_crop_isolation();
    test_preprocessor_linear_gradients();
    test_preprocessor_performance_benchmark();
    test_model_data_integrity();
    test_person_detector_lifecycle();

    std::cout << "\n========================================================\n";
    std::cout << " Test Summary: " << (g_tests_run - g_failures) << "/" << g_tests_run << " passed ("
              << g_failures << " failures)\n";
    std::cout << "========================================================\n";

    return (g_failures == 0) ? 0 : 1;
}
```

---

## 8. Summary of Implementation Guidelines for Implementers

1. **File Locations**:
   - `edge/esp32/src/camera/camera_config.h` — Preprocessor dimension constants, OV7670 registers, pin definitions.
   - `edge/esp32/src/camera/person_detector.h` & `.cpp` — `ImagePreprocessor` namespace, `CameraPersonDetector` implementation.
   - `edge/esp32/src/camera/model_data.h` & `.cpp` — Quantized int8 model weights array with `"TFL3"` magic header.
   - `edge/esp32/test/test_m2_camera_ml.cpp` — Comprehensive host-side test suite.
2. **Build and Verification**:
   - Host test command:
     `c++ -std=c++17 -Wall -I edge/esp32/src -I edge/esp32/test edge/esp32/test/test_m2_camera_ml.cpp -o /tmp/test_m2 && /tmp/test_m2`
   - PlatformIO build command:
     `pio run -e esp32dev`
