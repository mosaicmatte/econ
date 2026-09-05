#include <iostream>
#include <vector>
#include <cstdint>
#include <cassert>
#include "camera/camera_config.h"
#include "camera/person_detector.h"

int main() {
    std::cout << "Verifying Preprocessor Math & Boundaries exhaustively...\n";
    std::vector<uint8_t> src(CAMERA_FRAME_BYTES, 0);
    std::vector<int8_t> dst(MODEL_INPUT_BYTES, 0);

    // Test extreme values
    for (int test_val = 0; test_val <= 255; ++test_val) {
        std::fill(src.begin(), src.end(), static_cast<uint8_t>(test_val));
        bool ok = ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size());
        assert(ok);
        for (size_t i = 0; i < dst.size(); ++i) {
            int expected = test_val - 128;
            if (dst[i] != expected) {
                std::cerr << "Mismatch at val=" << test_val << " expected=" << expected << " got=" << (int)dst[i] << "\n";
                return 1;
            }
        }
    }
    std::cout << "All 256 uniform values mapped with mathematical exactness.\n";

    // Test edge indices
    std::fill(src.begin(), src.end(), 0);
    // Write distinct values at the 4 corners of crop: (20, 0), (139, 0), (20, 119), (139, 119)
    src[0 * CAMERA_FRAME_WIDTH + 20] = 100;
    src[0 * CAMERA_FRAME_WIDTH + 139] = 150;
    src[119 * CAMERA_FRAME_WIDTH + 20] = 200;
    src[119 * CAMERA_FRAME_WIDTH + 139] = 250;

    bool ok = ImagePreprocessor::preprocessFrame(src.data(), src.size(), dst.data(), dst.size());
    assert(ok);
    std::cout << "Corner tests completed successfully without memory access faults.\n";
    return 0;
}
