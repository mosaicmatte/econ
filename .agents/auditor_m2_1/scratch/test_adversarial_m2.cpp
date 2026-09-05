#include <iostream>
#include <vector>
#include <cstdint>
#include <cassert>
#include <cstring>
#include <chrono>
#include <random>

#include "camera/camera_config.h"
#include "camera/ov7670_driver.h"
#include "camera/model_data.h"
#include "camera/person_detector.h"

int main() {
    std::cout << "====================================================================\n";
    std::cout << "          ADVERSARIAL STRESS TESTING FOR MILESTONE 2                \n";
    std::cout << "====================================================================\n";

    int passed = 0;
    int failed = 0;

    auto assert_test = [&](bool cond, const char* desc) {
        if (cond) {
            std::cout << "  [PASS] " << desc << "\n";
            passed++;
        } else {
            std::cout << "  [FAIL] " << desc << "\n";
            failed++;
        }
    };

    // 1. Stress test preprocessor with 1,000 randomized noise frames
    std::mt19937 rng(1337);
    std::uniform_int_distribution<int> dist(0, 255);
    std::vector<uint8_t> rand_frame(CAMERA_FRAME_BYTES);
    std::vector<int8_t> out_tensor(MODEL_INPUT_BYTES);

    bool preproc_all_valid = true;
    for (int i = 0; i < 1000; ++i) {
        for (size_t j = 0; j < rand_frame.size(); ++j) {
            rand_frame[j] = static_cast<uint8_t>(dist(rng));
        }
        if (!ImagePreprocessor::preprocessFrame(rand_frame.data(), rand_frame.size(),
                                               out_tensor.data(), out_tensor.size())) {
            preproc_all_valid = false;
            break;
        }
        // Verify all output bytes are within [-128, 127]
        for (int8_t b : out_tensor) {
            if (b < -128 || b > 127) {
                preproc_all_valid = false;
                break;
            }
        }
    }
    assert_test(preproc_all_valid, "1000 randomized noise frames preprocessed safely");

    // 2. Stress test detector lifecycle across 10,000 rapid cycles
    CameraPersonDetector detector;
    assert_test(detector.init(), "Detector initialized successfully");

    bool loop_ok = true;
    for (int i = 0; i < 10000; ++i) {
        if (i % 2 == 0) {
            detector.getDriver().setMockPersonDetected(true);
        } else {
            detector.getDriver().setMockPersonDetected(false);
        }
        if (!detector.processFrame()) {
            loop_ok = false;
            break;
        }
        float conf = detector.getConfidence();
        if (conf < 0.0f || conf > 1.0f || std::isnan(conf)) {
            loop_ok = false;
            break;
        }
    }
    assert_test(loop_ok, "10,000 rapid alternating frames processed without crash or NaN confidence");

    // 3. Test resilience to buffer size fuzzing
    assert_test(!detector.processBuffer(rand_frame.data(), 0), "Fuzz: 0 length rejected");
    assert_test(!detector.processBuffer(rand_frame.data(), 1), "Fuzz: 1 byte rejected");
    assert_test(!detector.processBuffer(rand_frame.data(), 19199), "Fuzz: 19,199 bytes rejected");
    assert_test(detector.processBuffer(rand_frame.data(), 19200), "Fuzz: exact 19,200 bytes accepted");
    assert_test(detector.processBuffer(rand_frame.data(), 50000), "Fuzz: oversized buffer 50,000 accepted");

    // 4. Memory footprint verification
    assert_test(sizeof(CameraPersonDetector) <= 120 * 1024, "CameraPersonDetector object size <= 120 KB (fits ESP32 SRAM)");
    assert_test(sizeof(OV7670Driver) <= 25 * 1024, "OV7670Driver object size <= 25 KB");

    // 5. Verification of FlatBuffer alignment
    uintptr_t model_ptr = (uintptr_t)g_person_detect_model_data;
    assert_test((model_ptr % 16) == 0, "Model array address is strictly 16-byte aligned");

    std::cout << "\n====================================================================\n";
    std::cout << " Stress Results: " << passed << " passed, " << failed << " failed\n";
    std::cout << "====================================================================\n";

    return (failed == 0) ? 0 : 1;
}
