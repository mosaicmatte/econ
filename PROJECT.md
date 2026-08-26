# Project: ESP32 WROOM OV7670 Person Detection Module

## Architecture
The system upgrades the ESP32 WROOM edge node by replacing a binary PIR motion sensor with an OV7670 camera and an on-device lightweight Machine Learning model (TensorFlow Lite Micro) for real-time person detection, coupled with a dual-mode communication engine (Wi-Fi real-time broadcast + automatic USB Serial fallback).

```
                      +-----------------------------+
                      |   OV7670 Camera Hardware    |
                      |  (I2S DMA / SCCB / Grayscale)|
                      +--------------+--------------+
                                     | QQVGA Frame (19.2 KB)
                                     v
                      +-----------------------------+
                      |   Image Preprocessor        |
                      |  (Crop / Downsample 96x96)  |
                      +--------------+--------------+
                                     | 96x96 int8 Tensor
                                     v
                      +-----------------------------+
                      |   TFLite Micro Inference    |
                      | (int8 VWW Model, 80KB Arena)|
                      +--------------+--------------+
                                     | Person Score / Headcount
                                     v
                      +-----------------------------+
                      | Tracking Payload Serializer |
                      |    (Topology/BIM JSON)      |
                      +--------------+--------------+
                                     |
               +---------------------+---------------------+
               |                                           |
      (Wi-Fi Connected)                           (Wi-Fi Disconnected)
               v                                           v
+-------------------------------+             +-------------------------------+
|  Wi-Fi Broadcast Transport    |             |  USB Serial Fallback Transport|
| (UDP Broadcast :4210 + MQTT)  |             |  (UART0 115200 Framed JSON)   |
+-------------------------------+             +-------------------------------+
```

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Dual-Mode Comm Engine | Non-blocking Wi-Fi UDP Broadcast (:4210) & MQTT transport with auto-reconnect | M1 | ORIGINAL_REQUEST R2 |
| 2 | Serial Fallback Engine | Automatic zero-delay failover to USB Serial (UART0 115200 baud) when offline | M1 | ORIGINAL_REQUEST R2 |
| 3 | Tracking Payload Schema | Standardized JSON payload mapping presence, count, and confidence for BIM model | M1 | ORIGINAL_REQUEST R1, R2 |
| 4 | OV7670 Camera Driver | I2S DMA frame capture, SCCB I2C config, 20MHz XCLK generation with simulation fallback | M2 | ORIGINAL_REQUEST R1 |
| 5 | TFLite Micro ML Pipeline | Int8 quantized person detection model running in ~80KB tensor arena on ESP32 SRAM | M2 | ORIGINAL_REQUEST R1 |
| 6 | Frame Preprocessor | Downsampling/scaling QQVGA (160x120) to 96x96 int8 input tensor | M2 | ORIGINAL_REQUEST R1 |
| 7 | Main System Integration | Seamless substitution of PIR sensor reading with ML person detector in main loop | M3 | ORIGINAL_REQUEST R1 |
| 8 | Strict Module Isolation | Changes strictly confined to camera module scope without altering other sensor drivers | M3 | ORIGINAL_REQUEST R1, Arch |
| 9 | PlatformIO Build & Partitions | Clean compilation for ESP32 target with partition optimization (huge_app / fits limits) | M3 | ORIGINAL_REQUEST Comp |
| 10 | Dual Track Verification | Passing 100% E2E tests, adversarial stress tests, reviewer approvals, and clean forensic audit | M4 | ORIGINAL_REQUEST Judge |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | Dual-Mode Communication | `src/camera/dual_mode_comm.*`, `src/camera/tracking_payload.*`, host tests | none | DONE |
| 2 | Camera Driver & ML Pipeline | `src/camera/ov7670_driver.*`, `src/camera/model_data.*`, `src/camera/person_detector.*`, host tests | none | DONE |
| 3 | Main Integration & Isolation | `src/main.cpp` integration, `platformio.ini`, resource fit verification | M1, M2 | DONE |
| 4 | Final Verification & Audit | E2E test execution, adversarial tests, independent judge reviews, forensic audit | M3 | DONE |

## Interface Contracts
### `TrackingPayload` ↔ `DualModeComm`
```cpp
struct PersonTrackingData {
  bool person_detected;
  float confidence; // 0.0 to 1.0
  int person_count;
  unsigned long timestamp_ms;
  const char* zone_id;
  const char* sensor_id;
};

// Serialization to JSON buffer
size_t serializeTrackingPayload(const PersonTrackingData& data, char* buffer, size_t max_len);
```

### `CameraPersonDetector` ↔ `main.cpp`
```cpp
class CameraPersonDetector {
public:
  bool init();
  bool processFrame();
  bool isPersonDetected() const;
  float getConfidence() const;
  int getPersonCount() const;
  const PersonTrackingData& getLatestData() const;
  void transmitTelemetry(DualModeComm& comm);
};
```

## Code Layout
- `edge/esp32/src/camera/camera_config.h` — OV7670 pin definitions and resolution constants
- `edge/esp32/src/camera/ov7670_driver.h/.cpp` — Hardware driver, SCCB I2C configuration, DMA frame buffer
- `edge/esp32/src/camera/model_data.h/.cpp` — Quantized int8 TFLite Micro person detection model weights
- `edge/esp32/src/camera/person_detector.h/.cpp` — TFLite Micro interpreter, tensor arena, downsampling, inference engine
- `edge/esp32/src/camera/tracking_payload.h/.cpp` — BIM/topology telemetry schema serializer
- `edge/esp32/src/camera/dual_mode_comm.h/.cpp` — Non-blocking Wi-Fi UDP/MQTT broadcaster + USB Serial fallback
- `edge/esp32/src/main.cpp` — Isolated integration of camera module replacing legacy PIR
- `edge/esp32/platformio.ini` — Target environment, library dependencies, partition table
- `edge/esp32/test/` — Host and E2E unit/integration tests
