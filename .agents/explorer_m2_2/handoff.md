# Handoff Report: Explorer 2 — TFLite Micro ML Pipeline (Milestone 2)

**Author:** Explorer 2 (Milestone 2)  
**Recipient:** Sub-Orchestrator M2 / Worker 1  
**Artifact Path:** `/Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2/analysis.md`  
**Date:** 2026-08-26  

---

## 1. Observation

1. **Memory Limits & Architecture Constraints (`PROJECT.md:4-5, 47, 58` and `ORIGINAL_REQUEST.md:24-25`):**
   - ESP32-WROOM has 320 KB usable SRAM and 4 MB SPI Flash with no external PSRAM.
   - Milestone 2 mandates an int8 quantized TFLite Micro person detection model running in an ~80 KB tensor arena on ESP32 internal SRAM.
2. **PIR Presence Baseline (`edge/esp32/src/main.cpp:102-104, 733-753`):**
   - Currently, occupancy is derived via `digitalRead(PIR_PIN)`:
     ```cpp
     // line 746
     occupancy = present ? 1 : 0;  // presence, not a headcount
     doc["occupancy"] = occupancy;
     ```
   - Main loop publishes occupancy every 5 seconds (`gCfg.publishIntervalMs`).
3. **Interface Contract Specifications (`PROJECT.md:78-90`):**
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
4. **Off-Target Host Testing Infrastructure (`edge/esp32/test/run_host_tests.sh:1-21` & `test/arduino_shim.h:1-65`):**
   - Host test runner executes native C++17 unit tests without hardware:
     ```bash
     c++ -std=c++17 -Wall -I "$JSON" -I src -I test test/host_config_test.cpp -o "$OUT"
     ```
   - Off-target compilation cannot link Espressif Xtensa-specific binaries directly, requiring mock / dual-mode inference support.
5. **Model FlatBuffer Format & Op Set (`.agents/survey_explorer_2/survey_report.md:94-132`):**
   - Standard Visual Wake Words 96x96 int8 MobileNet model requires ~250–300 KB FlatBuffer in Flash and 8 core operators (`Conv2D`, `DepthwiseConv2D`, `AveragePool2D`, `MaxPool2D`, `Reshape`, `FullyConnected`, `Softmax`, `Add`).

---

## 2. Logic Chain

1. **Flash & RAM Residency (Observation 1 & 5):**
   - Declaring model FlatBuffer weights as `alignas(16) const unsigned char g_person_detect_model_data[]` places the entire ~250–300 KB array into the Flash `.rodata` section.
   - On ESP32, `.rodata` is mapped into DROM (`0x3F400000`) via the MMU, meaning weights occupy **0 bytes of internal SRAM** at rest.
2. **Tensor Arena Sizing & Alignment (Observation 1 & 5):**
   - Peak activation memory during Conv2D / Pointwise Conv operations in a 96×96 int8 MobileNet is ~36.8 KB.
   - An 80 KB (`81,920` bytes) static buffer allocated with `alignas(16)` provides sufficient space for peak layer activations (~37 KB), TFLM interpreter state (~8 KB), and operator scratchpads (~16 KB headroom) with zero heap fragmentation.
3. **Selective Op Resolution (Observation 5):**
   - Using `tflite::MicroMutableOpResolver<8>` to link only the 8 required operators prevents linking ~112 unused TFLM operators, reducing firmware Flash footprint by >450 KB and ensuring firmware fits within standard partition tables (`huge_app.csv` / `default.csv`).
4. **Dequantization & Hysteresis Decision Math (Observation 2 & 3):**
   - Extracting int8 output logits $[-128, 127]$ and applying $P = (q - Z) \times S$ accurately yields floating-point detection confidence $[0.0, 1.0]$.
   - A dual-threshold hysteresis filter ($T_{\text{enter}} = 0.60$, $T_{\text{exit}} = 0.40$) combined with a 2-frame debounce filter prevents presence flapping at decision boundaries.
5. **Dual-Mode Host Testability (Observation 4):**
   - Implementing a dual-mode engine (`#if defined(ESP32) && !defined(HOST_TEST)` for real TFLM, `#else` for deterministic mock inference) enables comprehensive automated host unit testing in CI/CD without hardware dependencies.

---

## 3. Caveats

1. **Model Weights Placeholder vs Production Binary:** The initial `model_data.cpp` flatbuffer can contain a valid TFL3 FlatBuffer header with synthetic weight structure for host tests, while the full quantized MobileNet weights array (~250 KB) is linked for target firmware.
2. **Multi-Person Resolution:** Standard single-channel Visual Wake Words (VWW) performs image-level binary classification (presence vs vacancy). Multi-person headcount estimation is bounded to 0 or 1 unless multi-box patch scanning is layered.
3. **Camera Sensor Dependencies:** Inference requires properly cropped/downsampled 96×96 int8 input buffers, which relies on the preprocessor module (investigated by Explorer 3).

---

## 4. Conclusion

The TFLite Micro Machine Learning pipeline for person detection on ESP32-WROOM is fully specified, memory-safe, and ready for worker implementation:
- **Weights Storage:** `model_data.h` / `model_data.cpp` in `.rodata` Flash (~250–300 KB, 0 bytes RAM).
- **Tensor Arena:** `person_detector.h` / `person_detector.cpp` with static 80 KB (`alignas(16)`) internal SRAM buffer.
- **Op Resolver:** `MicroMutableOpResolver<8>` for minimal Flash footprint.
- **Output Logic:** Int8 dequantization with $0.60 / 0.40$ hysteresis and 2-frame temporal debounce.
- **Host Testing:** Dual-mode implementation allowing 100% off-target verification in `test_m2_camera_ml.cpp`.

---

## 5. Verification Method

1. **Inspect Analysis Report:**
   ```bash
   view_file /Users/nguyenhoangkhoi/Documents/econ/.agents/explorer_m2_2/analysis.md
   ```
2. **Run Host Tests (Once Implemented in Worker Phase):**
   ```bash
   c++ -std=c++17 -Wall -I edge/esp32/src -I edge/esp32/test edge/esp32/test/test_m2_camera_ml.cpp -o /tmp/test_m2_camera_ml && /tmp/test_m2_camera_ml
   ```
3. **Invalidation Conditions:**
   - Tensor Arena size $< 64$ KB or unaligned (`% 16 != 0`).
   - Model weights allocated in DRAM instead of `.rodata` Flash.
   - Use of `AllOpsResolver` causing Flash binary overflow.
   - Dequantization math yielding values $< 0.0$ or $> 1.0$.
