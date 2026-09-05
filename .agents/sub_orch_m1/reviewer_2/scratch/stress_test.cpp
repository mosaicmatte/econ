#include "arduino_shim.h"
#include "WiFi.h"
#include "WiFiUdp.h"
#include "PubSubClient.h"
#include <ArduinoJson.h>

#include "tracking_payload.h"
#include "dual_mode_comm.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cmath>
#include <cassert>
#include <string>
#include <vector>

int main() {
    printf("Running Reviewer 2 Independent Adversarial Stress Tests...\n");

    // 1. Stress test: buffer boundary conditions with exact length
    PersonTrackingData data;
    initTrackingData(&data);
    data.sensor_id = "esp32_cam_01";
    data.zone_id = "zone_1";
    data.timestamp_ms = 123456789ULL;
    data.person_detected = true;
    data.confidence = 0.95f;
    data.person_count = 1;

    char fullBuf[256];
    size_t fullLen = serializeTrackingPayload(data, fullBuf, sizeof(fullBuf));
    printf("Nominal JSON length: %zu\n", fullLen);
    assert(fullLen > 0);

    // Exact size buffer: fullLen + 1 (null terminator)
    char exactBuf[256];
    size_t exactLen = serializeTrackingPayload(data, exactBuf, fullLen + 1);
    assert(exactLen == fullLen);
    assert(strcmp(fullBuf, exactBuf) == 0);

    // 1 byte less than required: fullLen
    char tightBuf[256];
    size_t tightLen = serializeTrackingPayload(data, tightBuf, fullLen);
    assert(tightLen == 0);
    assert(tightBuf[0] == '\0');
    printf("[PASS] Exact buffer size boundary checks.\n");

    // 2. Stress test: Rapid flapping Wi-Fi
    WiFiUDP mockUdp;
    PubSubClient mockMqtt;
    SerialShim mockSerial;
    DualModeComm comm(mockUdp, mockMqtt, mockSerial);
    CommConfig cfg = defaultCommConfig();
    cfg.wifi_ssid = "FlappingNet";
    comm.begin(cfg);
    mockSerial.setCapture(true, false);

    for (int i = 0; i < 1000; i++) {
        if (i % 2 == 0) {
            WiFi.setMockStatus(WL_CONNECTED);
        } else {
            WiFi.setMockStatus(WL_DISCONNECTED);
        }
        comm.tick();
        bool ok = comm.transmit(data);
        assert(ok == true);
    }
    printf("[PASS] 1000 Rapid Wi-Fi flapping transitions without failure.\n");

    // 3. Stress test: UDP write partial failure recovery
    WiFi.setMockStatus(WL_CONNECTED);
    comm.tick();
    mockUdp.clearHistory();
    mockSerial.clearCapture();

    // Inject failure on every 3rd transmit
    for (int i = 0; i < 300; i++) {
        if (i % 3 == 0) {
            mockUdp.failNextSend = true;
        }
        bool ok = comm.transmit(data);
        assert(ok == true);
    }
    printf("Total UDP packets sent: %zu, fallback serial captured: %zu bytes\n",
           mockUdp.getPacketCount(), mockSerial.getCaptured().size());
    assert(mockUdp.getPacketCount() == 200);
    assert(mockSerial.getCaptured().size() > 0);
    printf("[PASS] Intermittent UDP socket failures gracefully failed over to Serial.\n");

    // 4. Stress test: 0ms delay failover verification
    WiFi.setMockStatus(WL_DISCONNECTED);
    comm.tick();
    mockSerial.clearCapture();

    auto t0 = std::chrono::high_resolution_clock::now();
    comm.transmit(data);
    auto t1 = std::chrono::high_resolution_clock::now();
    double latency = std::chrono::duration<double, std::micro>(t1 - t0).count();
    printf("Failover serial transmit latency: %.3f µs\n", latency);
    assert(latency < 100.0);
    printf("[PASS] Zero-delay failover latency budget confirmed.\n");

    printf("ALL ADVERSARIAL STRESS TESTS COMPLETED SUCCESSFULLY!\n");
    return 0;
}
