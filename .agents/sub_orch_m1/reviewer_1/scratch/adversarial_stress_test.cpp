#include "arduino_shim.h"
#include "WiFi.h"
#include "WiFiUdp.h"
#include "PubSubClient.h"
#include "tracking_payload.h"
#include "dual_mode_comm.h"
#include <cassert>
#include <cmath>
#include <iostream>
#include <climits>

int main() {
    std::cout << "[ADV-TEST] Starting adversarial stress tests..." << std::endl;

    // 1. Extreme floats (NaN, +Inf, -Inf)
    PersonTrackingData extremeData;
    initTrackingData(&extremeData);
    extremeData.confidence = std::nanf("");
    extremeData.person_count = INT_MAX;
    extremeData.timestamp_ms = UINT64_MAX;

    char buf[512];
    size_t len = serializeTrackingPayload(extremeData, buf, sizeof(buf));
    std::cout << "[ADV-TEST] NaN confidence output len: " << len << ", payload: " << buf << std::endl;
    assert(len > 0);

    extremeData.confidence = INFINITY;
    len = serializeTrackingPayload(extremeData, buf, sizeof(buf));
    std::cout << "[ADV-TEST] +Inf confidence output len: " << len << ", payload: " << buf << std::endl;
    assert(len > 0);

    extremeData.confidence = -INFINITY;
    len = serializeTrackingPayload(extremeData, buf, sizeof(buf));
    std::cout << "[ADV-TEST] -Inf confidence output len: " << len << ", payload: " << buf << std::endl;
    assert(len > 0);

    extremeData.person_count = INT_MIN;
    len = serializeTrackingPayload(extremeData, buf, sizeof(buf));
    std::cout << "[ADV-TEST] INT_MIN person_count output len: " << len << ", payload: " << buf << std::endl;
    assert(len > 0);

    // 2. Huge strings (> buffer size)
    std::string hugeSensor(1000, 'X');
    std::string hugeZone(1000, 'Y');
    extremeData.sensor_id = hugeSensor.c_str();
    extremeData.zone_id = hugeZone.c_str();
    extremeData.person_count = 1;
    extremeData.confidence = 0.5f;

    // A small buffer of 256 bytes will not fit 2000 chars -> should return 0 safely, no buffer overrun
    char smallBuf[256];
    memset(smallBuf, 0x55, sizeof(smallBuf));
    len = serializeTrackingPayload(extremeData, smallBuf, sizeof(smallBuf));
    assert(len == 0);
    assert(smallBuf[0] == '\0');
    std::cout << "[ADV-TEST] Huge strings on 256B buffer safely rejected without overflow." << std::endl;

    // 3. Rapid cycle state machine stress test (50,000 flips)
    WiFiUDP mockUdp;
    PubSubClient mockMqtt;
    SerialShim mockSerial;
    DualModeComm comm(mockUdp, mockMqtt, mockSerial);
    CommConfig cfg = defaultCommConfig();
    cfg.wifi_ssid = "StressNet";
    comm.begin(cfg);

    for (int i = 0; i < 50000; i++) {
        if (i % 2 == 0) {
            WiFi.setMockStatus(WL_CONNECTED);
            mockMqtt.setMockConnected(true);
        } else {
            WiFi.setMockStatus(WL_DISCONNECTED);
            mockMqtt.setMockConnected(false);
        }
        comm.tick();
        bool tx = comm.transmit(extremeData); // With huge strings, returns false or fallback false
        (void)tx;
    }
    std::cout << "[ADV-TEST] 50,000 rapid state transitions completed successfully." << std::endl;

    // 4. Memory Canary Test with DualModeComm
    uint8_t canaryBlock[64 + sizeof(DualModeComm) + 64];
    memset(canaryBlock, 0xCC, sizeof(canaryBlock));
    DualModeComm* canComm = new (canaryBlock + 64) DualModeComm(mockUdp, mockMqtt, mockSerial);
    canComm->begin(cfg);
    canComm->tick();
    canComm->~DualModeComm();

    for (int i = 0; i < 64; i++) {
        assert(canaryBlock[i] == 0xCC);
        assert(canaryBlock[64 + sizeof(DualModeComm) + i] == 0xCC);
    }
    std::cout << "[ADV-TEST] DualModeComm placement canary uncorrupted." << std::endl;

    std::cout << "[ADV-TEST] All adversarial stress tests PASSED!" << std::endl;
    return 0;
}
