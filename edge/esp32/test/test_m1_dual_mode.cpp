// -----------------------------------------------------------------------------
// test_m1_dual_mode.cpp — Comprehensive Host Test Suite for Milestone 1
//
// Tests:
//   1. TrackingPayload JSON Serialization & Schema Compliance
//   2. DualModeComm Wi-Fi Connected Mode (UDP Broadcast :4210 + MQTT)
//   3. DualModeComm Wi-Fi Disconnected Mode (Zero-Delay USB Serial Fallback)
//   4. Dynamic Failover Transitions (Online -> Offline -> Online, Socket Errors)
//   5. Non-Blocking Timing & Execution Budget Verification (<0.2ms per tick)
// -----------------------------------------------------------------------------

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
#include <chrono>
#include <string>
#include <vector>

static int g_failures = 0;
static int g_total_tests = 0;

static void check(bool condition, const char* description) {
  g_total_tests++;
  if (condition) {
    printf("  [PASS] %s\n", description);
  } else {
    printf("  [FAIL] %s\n", description);
    g_failures++;
  }
}

// -----------------------------------------------------------------------------
// Test Group 1: TrackingPayload JSON Serialization Tests
// -----------------------------------------------------------------------------
static void run_tracking_payload_tests() {
  printf("\n=== [1/5] TrackingPayload JSON Serialization & Schema Tests ===\n");

  // 1. Nominal payload serialization
  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "esp32_cam_01";
  data.zone_id = "zone_1";
  data.timestamp_ms = 1724645160000ULL;
  data.person_detected = true;
  data.confidence = 0.94f;
  data.person_count = 2;

  char buffer[256];
  size_t len = serializeTrackingPayload(data, buffer, sizeof(buffer));
  check(len > 0, "Nominal serialization returns non-zero length");

  StaticJsonDocument<512> doc;
  DeserializationError err = deserializeJson(doc, buffer);
  check(!err, "Serialized payload is valid parseable JSON");
  check(doc["sensor_id"] == "esp32_cam_01", "sensor_id matches esp32_cam_01");
  check(doc["zone_id"] == "zone_1", "zone_id matches zone_1");
  check(doc["timestamp_ms"].as<uint64_t>() == 1724645160000ULL, "timestamp_ms preserves full 64-bit epoch");
  check(doc["person_detected"].as<bool>() == true, "person_detected is true");
  check(std::abs(doc["confidence"].as<float>() - 0.94f) < 1e-3f, "confidence matches 0.94");
  check(doc["person_count"].as<int>() == 2, "person_count is 2");

  // 2. 64-bit epoch timestamp serialization
  PersonTrackingData tsData = data;
  tsData.timestamp_ms = 18446744073709551615ULL;
  char tsBuffer[256];
  len = serializeTrackingPayload(tsData, tsBuffer, sizeof(tsBuffer));
  check(len > 0, "Large uint64 timestamp serialization succeeds");
  check(strstr(tsBuffer, "18446744073709551615") != nullptr, "64-bit max timestamp formatted faithfully in JSON");

  // 3. Float precision formatting (%.2f)
  PersonTrackingData precData = data;
  precData.confidence = 0.942857f;
  char precBuffer[256];
  len = serializeTrackingPayload(precData, precBuffer, sizeof(precBuffer));
  check(strstr(precBuffer, "\"confidence\":0.94") != nullptr, "Confidence formatted with 2 decimal places (0.94)");

  // 4. Edge values: zero detections
  PersonTrackingData zeroData;
  initTrackingData(&zeroData);
  zeroData.sensor_id = "cam_entry";
  zeroData.zone_id = "zone_lobby";
  zeroData.timestamp_ms = 1000ULL;
  zeroData.person_detected = false;
  zeroData.confidence = 0.0f;
  zeroData.person_count = 0;

  len = serializeTrackingPayload(zeroData, buffer, sizeof(buffer));
  check(len > 0, "Zero-detection payload serialization succeeds");
  doc.clear();
  deserializeJson(doc, buffer);
  check(doc["person_detected"].as<bool>() == false, "person_detected is false for 0 count");
  check(doc["person_count"].as<int>() == 0, "person_count is 0");
  check(std::abs(doc["confidence"].as<float>() - 0.0f) < 1e-3f, "confidence is 0.0");

  // 5. Out-of-bounds clamping
  PersonTrackingData oobData;
  initTrackingData(&oobData);
  oobData.confidence = 1.85f;  // should clamp to 1.00
  oobData.person_count = -5;   // should clamp to 0
  len = serializeTrackingPayload(oobData, buffer, sizeof(buffer));
  check(len > 0, "Out-of-bounds inputs serialize without error");
  doc.clear();
  deserializeJson(doc, buffer);
  check(std::abs(doc["confidence"].as<float>() - 1.00f) < 1e-3f, "Confidence > 1.0 clamped to 1.00");
  check(doc["person_count"].as<int>() == 0, "Negative person_count clamped to 0");

  oobData.confidence = -0.45f; // should clamp to 0.00
  len = serializeTrackingPayload(oobData, buffer, sizeof(buffer));
  doc.clear();
  deserializeJson(doc, buffer);
  check(std::abs(doc["confidence"].as<float>() - 0.00f) < 1e-3f, "Confidence < 0.0 clamped to 0.00");

  // 6. Null pointer safety
  PersonTrackingData nullData;
  initTrackingData(&nullData);
  nullData.sensor_id = nullptr;
  nullData.zone_id = nullptr;
  len = serializeTrackingPayload(nullData, buffer, sizeof(buffer));
  check(len > 0, "Null string pointers handled safely without crashing");
  doc.clear();
  deserializeJson(doc, buffer);
  check(doc["sensor_id"] == "unknown_sensor", "Null sensor_id replaced with unknown_sensor");
  check(doc["zone_id"] == "unknown_zone", "Null zone_id replaced with unknown_zone");

  // 7. Buffer boundary safety & truncation
  char tinyBuffer[15];
  size_t truncLen = serializeTrackingPayload(data, tinyBuffer, sizeof(tinyBuffer));
  check(truncLen == 0, "Undersized buffer returns 0 bytes written");
  check(tinyBuffer[0] == '\0', "Undersized buffer safely null-terminated");

  size_t nullBufLen = serializeTrackingPayload(data, nullptr, 128);
  check(nullBufLen == 0, "Null buffer returns 0 bytes safely");

  size_t zeroLen = serializeTrackingPayload(data, buffer, 0);
  check(zeroLen == 0, "Zero max_len returns 0 safely");

  // 8. Canary byte overflow protection
  uint8_t memoryChunk[32 + 256 + 32];
  memset(memoryChunk, 0xAA, sizeof(memoryChunk));
  char* targetBuf = (char*)(memoryChunk + 32);
  serializeTrackingPayload(data, targetBuf, 256);
  
  bool lowerCanaryOk = true, upperCanaryOk = true;
  for (int i = 0; i < 32; i++) {
    if (memoryChunk[i] != 0xAA) lowerCanaryOk = false;
    if (memoryChunk[32 + 256 + i] != 0xAA) upperCanaryOk = false;
  }
  check(lowerCanaryOk && upperCanaryOk, "Buffer boundary canary bytes completely uncorrupted");

  // 9. Serial fallback schema with _topic
  len = serializeTrackingPayloadForSerial(data, "econ/telemetry/zone_1", buffer, sizeof(buffer));
  check(len > 0, "Serial fallback payload serialization succeeds");
  doc.clear();
  deserializeJson(doc, buffer);
  check(doc["_topic"] == "econ/telemetry/zone_1", "Serial payload contains _topic field");
  check(doc["sensor_id"] == "esp32_cam_01", "Serial payload contains sensor_id");

  // 10. Extended Tracking Payload (Inference timing & Bounding Boxes)
  TrackingBoundingBox bboxes[2] = {
    {0.10f, 0.20f, 0.50f, 0.80f, 0.92f, 0},
    {0.55f, 0.30f, 0.90f, 0.85f, 0.88f, 0}
  };
  PersonTrackingData extData = data;
  extData.inference_time_ms = 45;
  extData.fps = 12.5f;
  extData.bboxes = bboxes;
  extData.bbox_count = 2;

  char extBuffer[512];
  len = serializeExtendedTrackingPayload(extData, extBuffer, sizeof(extBuffer));
  check(len > 0, "Extended payload serialization succeeds");
  StaticJsonDocument<512> extDoc;
  deserializeJson(extDoc, extBuffer);
  check(extDoc["inference_ms"].as<int>() == 45, "Extended payload contains inference_ms: 45");
  check(std::abs(extDoc["fps"].as<float>() - 12.5f) < 1e-2f, "Extended payload contains fps: 12.5");
  check(extDoc["bboxes"].size() == 2, "Extended payload contains 2 bounding boxes");
  check(std::abs(extDoc["bboxes"][0]["xmin"].as<float>() - 0.10f) < 1e-2f, "BBox 0 xmin matches 0.10");

  // 11. Deserializer loopback test
  PersonTrackingData loopData;
  initTrackingData(&loopData);
  loopData.sensor_id = "esp32_cam_01";
  loopData.zone_id = "zone_1";
  loopData.timestamp_ms = 1724645160000ULL;
  loopData.person_detected = true;
  loopData.confidence = 0.94f;
  loopData.person_count = 2;

  char loopBuf[256];
  size_t loopLen = serializeTrackingPayload(loopData, loopBuf, sizeof(loopBuf));
  check(loopLen > 0, "serializeTrackingPayload for loopback succeeds");

  PersonTrackingData parsedData;
  char zoneBuf[32];
  char sensorBuf[32];
  bool parseOk = deserializeTrackingPayload(loopBuf, loopLen, &parsedData, zoneBuf, sizeof(zoneBuf), sensorBuf, sizeof(sensorBuf));
  check(parseOk, "deserializeTrackingPayload parses serialized JSON correctly");
  check(parsedData.person_detected == true, "Deserialized person_detected matches");
  check(parsedData.person_count == 2, "Deserialized person_count matches");
  check(strcmp(sensorBuf, "esp32_cam_01") == 0, "Deserialized sensorBuf matches esp32_cam_01");
}

// -----------------------------------------------------------------------------
// Test Group 2: DualModeComm Wi-Fi Connected Mode Tests
// -----------------------------------------------------------------------------
static void run_wifi_connected_tests() {
  printf("\n=== [2/5] DualModeComm Wi-Fi Connected Mode (Primary Transport) Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;

  DualModeComm comm(mockUdp, mockMqtt, mockSerial);

  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "TestWiFi_AP";
  cfg.wifi_pass = "SecurePass123";
  cfg.mqtt_host = "192.168.1.10";
  cfg.mqtt_port = 1883;
  cfg.mqtt_topic = "econ/telemetry/zone_1";
  cfg.udp_port = 4210;
  cfg.broadcast_ip = IPAddress(255, 255, 255, 255);
  cfg.reconnect_interval_ms = 5000;

  comm.begin(cfg);

  // Set connected state in mocks
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  mockMqtt.clearHistory();

  comm.tick();
  check(comm.isWifiConnected(), "comm.isWifiConnected() is true");
  check(comm.isPrimaryTransportActive(), "comm.isPrimaryTransportActive() is true");
  check(!comm.isSerialFallbackActive(), "comm.isSerialFallbackActive() is false");
  check(comm.getActiveTransport() == COMM_TRANSPORT_WIFI_DUAL, "Active transport is COMM_TRANSPORT_WIFI_DUAL");

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "esp32_cam_01";
  data.zone_id = "zone_1";
  data.timestamp_ms = 1724645160000ULL;
  data.person_detected = true;
  data.confidence = 0.95f;
  data.person_count = 1;

  mockSerial.setCapture(true, false);
  bool txResult = comm.transmit(data);

  check(txResult, "comm.transmit returns true in connected mode");
  check(comm.getSuccessfulTransmissions() == 1, "getSuccessfulTransmissions() incremented to 1");
  check(comm.getFallbackTransmissions() == 0, "getFallbackTransmissions() remains 0");

  // Verify UDP Broadcast
  check(mockUdp.getPacketCount() == 1, "Exactly 1 UDP broadcast packet emitted");
  if (mockUdp.getPacketCount() == 1) {
    const UDPSentPacket& pkt = mockUdp.getLastPacket();
    check(pkt.destPort == 4210, "UDP packet destination port is 4210");
    check(pkt.destIP == IPAddress(255, 255, 255, 255), "UDP packet destination IP is 255.255.255.255");
    std::string payload = mockUdp.getLastPacketPayload();
    check(payload.find("\"person_detected\":true") != std::string::npos, "UDP packet contains person_detected:true");
    check(payload.find("\"confidence\":0.95") != std::string::npos, "UDP packet contains confidence:0.95");
  }

  // Verify MQTT Publish
  check(mockMqtt.getPublishCount() == 1, "Exactly 1 MQTT message published");
  if (mockMqtt.getPublishCount() == 1) {
    const MqttMessageRecord& msg = mockMqtt.getLastPublished();
    check(msg.topic == "econ/telemetry/zone_1", "MQTT topic is econ/telemetry/zone_1");
    std::string payload = mockMqtt.getLastPublishedPayload();
    check(payload.find("\"sensor_id\":\"esp32_cam_01\"") != std::string::npos, "MQTT payload contains sensor_id");
  }

  // Verify Serial silence during active Wi-Fi broadcast
  check(mockSerial.getCaptured().empty(), "Serial transport remains silent when Wi-Fi broadcast is active");
}

// -----------------------------------------------------------------------------
// Test Group 3: DualModeComm Wi-Fi Disconnected Fallback Tests
// -----------------------------------------------------------------------------
static void run_wifi_disconnected_fallback_tests() {
  printf("\n=== [3/5] DualModeComm Wi-Fi Disconnected (Serial Fallback) Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;

  DualModeComm comm(mockUdp, mockMqtt, mockSerial);
  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "TestWiFi_AP";
  cfg.mqtt_topic = "econ/telemetry/zone_2";
  comm.begin(cfg);

  // Set disconnected state
  WiFi.setMockStatus(WL_DISCONNECTED);
  mockMqtt.setMockConnected(false);
  mockSerial.clearCapture();
  mockSerial.setCapture(true, false);
  mockUdp.clearHistory();
  mockMqtt.clearHistory();

  comm.tick();
  check(!comm.isWifiConnected(), "comm.isWifiConnected() is false");
  check(!comm.isPrimaryTransportActive(), "comm.isPrimaryTransportActive() is false");
  check(comm.isSerialFallbackActive(), "comm.isSerialFallbackActive() is true");
  check(comm.getActiveTransport() == COMM_TRANSPORT_SERIAL, "Active transport is COMM_TRANSPORT_SERIAL");

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "cam_fallback_01";
  data.zone_id = "zone_2";
  data.timestamp_ms = 1724645200000ULL;
  data.person_detected = true;
  data.confidence = 0.88f;
  data.person_count = 3;

  auto t0 = std::chrono::high_resolution_clock::now();
  bool txResult = comm.transmit(data);
  auto t1 = std::chrono::high_resolution_clock::now();
  double latencyUs = std::chrono::duration<double, std::micro>(t1 - t0).count();

  check(txResult, "comm.transmit returns true via Serial fallback");
  check(latencyUs < 100.0, "Zero-delay failover execution latency < 100 us");
  check(comm.getFallbackTransmissions() == 1, "getFallbackTransmissions() is 1");
  check(comm.getSuccessfulTransmissions() == 0, "getSuccessfulTransmissions() is 0");

  check(mockUdp.getPacketCount() == 0, "Zero UDP packets emitted while offline");
  check(mockMqtt.getPublishCount() == 0, "Zero MQTT messages emitted while offline");

  std::string serialOut = mockSerial.getCaptured();
  check(!serialOut.empty(), "Serial output is non-empty");
  check(serialOut.back() == '\n', "Serial output ends with newline '\\n'");

  StaticJsonDocument<512> doc;
  DeserializationError err = deserializeJson(doc, serialOut);
  check(!err, "Serial fallback output is valid parseable JSON");
  check(doc["sensor_id"] == "cam_fallback_01", "Serial payload sensor_id matches");
  check(doc["person_count"].as<int>() == 3, "Serial payload person_count matches 3");
  check(doc["_topic"] == "econ/telemetry/zone_2", "Serial payload contains _topic for gateway bridge");

  // Unconfigured SSID Serial-Only Mode
  WiFiUDP uncfgUdp;
  PubSubClient uncfgMqtt;
  SerialShim uncfgSerial;
  DualModeComm uncfgComm(uncfgUdp, uncfgMqtt, uncfgSerial);
  CommConfig uncfgCfg = defaultCommConfig();
  uncfgCfg.wifi_ssid = nullptr; // Unconfigured
  uncfgComm.begin(uncfgCfg);

  check(uncfgComm.getState() == COMM_STATE_SERIAL_ONLY, "Unconfigured SSID enters COMM_STATE_SERIAL_ONLY");
  check(uncfgComm.isSerialFallbackActive(), "Serial fallback active for unconfigured node");
}

// -----------------------------------------------------------------------------
// Test Group 4: Failover Transitions & Partial Failure Resilience Tests
// -----------------------------------------------------------------------------
static void run_failover_transition_tests() {
  printf("\n=== [4/5] DualModeComm Failover Transitions & Fault Resilience Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);
  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Corporate_IoT";
  cfg.mqtt_topic = "econ/telemetry/zone_1";
  comm.begin(cfg);

  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "esp32_cam_01";
  data.zone_id = "zone_1";
  data.timestamp_ms = 1000ULL;
  data.person_detected = true;
  data.confidence = 0.90f;
  data.person_count = 1;

  mockSerial.setCapture(true, false);

  // Step 1: Online Mode
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  mockMqtt.clearHistory();
  comm.tick();

  comm.transmit(data);
  check(mockUdp.getPacketCount() == 1, "Cycle 1 (Online): UDP broadcast active");
  check(mockMqtt.getPublishCount() == 1, "Cycle 1 (Online): MQTT publish active");
  check(mockSerial.getCaptured().empty(), "Cycle 1 (Online): Serial silent");

  // Step 2: Wi-Fi Drop (WL_CONNECTION_LOST)
  WiFi.setMockStatus(WL_CONNECTION_LOST);
  mockMqtt.setMockConnected(false);
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  mockMqtt.clearHistory();
  comm.tick();

  check(comm.getFailoverCount() == 1, "Cycle 2 (Dropped): Failover count incremented to 1");
  comm.transmit(data);
  check(mockUdp.getPacketCount() == 0, "Cycle 2 (Dropped): Zero UDP packets");
  check(mockMqtt.getPublishCount() == 0, "Cycle 2 (Dropped): Zero MQTT messages");
  check(!mockSerial.getCaptured().empty(), "Cycle 2 (Dropped): Serial fallback engaged seamlessly");

  // Step 3: Wi-Fi Restoration (WL_CONNECTED)
  WiFi.setMockStatus(WL_CONNECTED);
  mockMqtt.setMockConnected(true);
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  mockMqtt.clearHistory();
  comm.tick();

  comm.transmit(data);
  check(mockUdp.getPacketCount() == 1, "Cycle 3 (Restored): UDP broadcast restored");
  check(mockMqtt.getPublishCount() == 1, "Cycle 3 (Restored): MQTT publish restored");
  check(mockSerial.getCaptured().empty(), "Cycle 3 (Restored): Serial output silent again");

  // Step 4: UDP Send Error Fallback (e.g. Socket Write Failure)
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  mockUdp.failNextSend = true; // Inject UDP failure
  uint32_t priorFailovers = comm.getFailoverCount();

  bool txRes = comm.transmit(data);
  check(txRes, "Cycle 4 (UDP Socket Error): Returns true via instant Serial failover");
  check(!mockSerial.getCaptured().empty(), "Cycle 4 (UDP Socket Error): Fallback Serial received frame");
  check(comm.getFailoverCount() == priorFailovers + 1, "Cycle 4 (UDP Socket Error): Failover counter incremented");

  // Step 5: Programmatic forceDisconnect() and reconnect()
  mockSerial.clearCapture();
  mockUdp.clearHistory();
  comm.forceDisconnect();
  check(comm.isSerialFallbackActive(), "forceDisconnect() immediately switches to Serial fallback");
  comm.reconnect();
  check(comm.isPrimaryTransportActive(), "reconnect() restores primary Wi-Fi transport");
}

// -----------------------------------------------------------------------------
// Test Group 5: Non-Blocking Timing & State Machine Execution Tests
// -----------------------------------------------------------------------------
static void run_timing_and_nonblocking_tests() {
  printf("\n=== [5/5] Non-Blocking Timing & State Machine Loop Tests ===\n");

  WiFiUDP mockUdp;
  PubSubClient mockMqtt;
  SerialShim mockSerial;
  DualModeComm comm(mockUdp, mockMqtt, mockSerial);
  CommConfig cfg = defaultCommConfig();
  cfg.wifi_ssid = "Corporate_IoT";
  cfg.wifi_pass = "Password";
  cfg.reconnect_interval_ms = 5000;
  comm.begin(cfg);

  WiFi.setMockStatus(WL_DISCONNECTED);
  setSimulatedTime(true, 1000);

  // Benchmark tick() execution time across 10,000 iterations
  const int iterations = 10000;
  auto start = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < iterations; i++) {
    comm.tick();
  }
  auto end = std::chrono::high_resolution_clock::now();
  double elapsedUs = std::chrono::duration<double, std::micro>(end - start).count();
  double usPerTick = elapsedUs / iterations;

  printf("  Benchmark: %.3f us per tick() across %d iterations\n", usPerTick, iterations);
  check(usPerTick < 200.0, "tick() execution time strictly < 200 us (<0.2ms budget)");
  check(usPerTick < 20.0, "tick() execution time is ultra-fast (<20 us)");

  // Benchmark serializeTrackingPayload execution time across 10,000 iterations
  PersonTrackingData data;
  initTrackingData(&data);
  data.sensor_id = "esp32_cam_01";
  data.zone_id = "zone_1";
  data.timestamp_ms = 1724645160000ULL;
  data.person_detected = true;
  data.confidence = 0.94f;
  data.person_count = 2;

  char buf[256];
  start = std::chrono::high_resolution_clock::now();
  for (int i = 0; i < iterations; i++) {
    serializeTrackingPayload(data, buf, sizeof(buf));
  }
  end = std::chrono::high_resolution_clock::now();
  double serializeUs = std::chrono::duration<double, std::micro>(end - start).count() / iterations;
  printf("  Benchmark: %.3f us per serializeTrackingPayload across %d iterations\n", serializeUs, iterations);
  check(serializeUs < 20.0, "Serialization execution time strictly < 20 us budget");

  // Reconnection hysteresis & cooldown verification
  comm.forceDisconnect();
  WiFi.reset();
  int initialBegins = WiFi.beginCount;

  setSimulatedMillis(6000);
  comm.tick();
  int beginsAfterFirstTick = WiFi.beginCount;
  check(beginsAfterFirstTick == initialBegins + 1, "First disconnected tick triggers non-blocking WiFi.begin()");

  // Advance clock within 5000ms cooldown (t = 8000ms)
  setSimulatedMillis(8000);
  comm.tick();
  check(WiFi.beginCount == beginsAfterFirstTick, "Tick within 5000ms cooldown skips reconnect attempt");

  // Advance clock past 5000ms cooldown (t = 12000ms)
  setSimulatedMillis(12000);
  comm.tick();
  check(WiFi.beginCount == beginsAfterFirstTick + 1, "Tick after 5000ms initiates next reconnect attempt");
}

// -----------------------------------------------------------------------------
// Main Test Runner
// -----------------------------------------------------------------------------
int main() {
  printf("====================================================================\n");
  printf("  Milestone 1: Dual-Mode Communication & Tracking Payload Unit Tests\n");
  printf("====================================================================\n");

  run_tracking_payload_tests();
  run_wifi_connected_tests();
  run_wifi_disconnected_fallback_tests();
  run_failover_transition_tests();
  run_timing_and_nonblocking_tests();

  printf("\n====================================================================\n");
  printf("Summary: %d / %d tests passed\n", g_total_tests - g_failures, g_total_tests);
  printf("Result: %s (%d failure%s)\n", g_failures == 0 ? "PASSED (100% SUCCESS)" : "FAILED", g_failures, g_failures == 1 ? "" : "s");
  printf("====================================================================\n");

  return g_failures ? 1 : 0;
}
